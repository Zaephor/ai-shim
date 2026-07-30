# Session-scoped sidecar teardown — design

**Date:** 2026-07-30
**Status:** approved (brainstorm), pending implementation plan
**Scope:** Give every session a unique container label and scope sidecar
teardown to it, so ending one session cannot destroy a sibling session's DIND
sidecar and netns holder.

## Problem

Sidecar teardown resolves its victims by label, and parallel sessions carry
identical labels. Ending one session therefore tears down every sibling
session's DIND sidecar and netns holder in the same agent+profile+workspace.

The label set is `ai-shim`, `.agent`, `.profile`, `.workspace`, `.role`,
`.version` (`internal/container/labels.go`). None of them identifies a session.
Parallel siblings differ only in container name, which carries a random suffix
(`builder.go:497`). `main.go:1146` states the intent outright: "labels stay
identical so all siblings remain discoverable" — correct for the session
picker, load-bearing in the wrong direction for teardown.

`stopDINDForSession` (`main.go:1737`) queries `dindSessionFilters` /
`holderSessionFilters` (`main.go:1712-1733`), both scoped to
base+agent+profile+workspace+running, then loops `ContainerStop` +
`ContainerRemove{Force}` over every match. Callers: `handleReattach` on
container exit (`main.go:1653`, reachable with or without a TTY via
`manage attach`), `stopSession` from the picker's `new` and `k<N>` paths, and
`manage stop`.

The agent container survives, because it is removed by ID. Only the sidecars
are resolved by label. A session whose holder is destroyed cannot recover:
a `container:<id>` dependent keeps only `lo`, and `eth0` never returns even if
the target is restarted with the same ID (validated 2026-07-03). DIND's
`unless-stopped` policy cannot help — its netns target no longer exists.

Blast radius grows with sessions per project, which matches the reported
correlation with parallel agent count.

### Observed instance

Reconstructed from host daemon logs over a 20-hour window. Five sessions ran in
one workspace (DIND starts at 01:07, 01:09, 05:38, 06:15, 06:16). Two ended
normally at 14:18 — agent, DIND and holder deleted with no SIGTERM-timeout
lines, i.e. the correct by-ID run-path teardown.

At 15:05 one session ended and swept three:

```
15:05:43  agent container removed          (session S4, the one that ended)
15:05:43  stopping restart-manager         DIND of S4
15:05:44  stopping restart-manager         DIND of S2   <- still-running session
15:05:45  stopping restart-manager         DIND of S1   <- still-running session
15:05:51  failed to exit within 5s of signal 15   holder of S4
15:05:57  failed to exit within 5s of signal 15   holder of S2
15:06:02  failed to exit within 5s of signal 15   holder of S1
```

All DINDs first, then all holders, matching the two loops in
`stopDINDForSession`. S1 and S2 remained running with destroyed netns holders
and no path to recovery. An identically shaped sweep appears earlier in the
same log for a different workspace.

The two paths are distinguishable in daemon logs. Run-path teardown uses
`ContainerRemove{Force}` (`Sidecar.Stop`, `Holder.Stop`) and leaves only a
`task-delete`. The label sweep uses `ContainerStop{Timeout:5}`, and the holder
runs `sleep infinity` as PID 1 with no SIGTERM handler, so it always burns the
full five seconds and logs `failed to exit within 5s of signal 15`. That line
appears only on the sweep path.

Ruled out by the same log: daemon restart (dockerd PID unchanged for the whole
window), OOM (no kernel or daemon OOM records), reaping by
`cleanupStaleContainers` (removing an already-exited container emits neither
`stopping restart-manager` nor a SIGTERM-timeout line; both are present), and
`manage cleanup` (it would have taken the agent containers too).

## Goal / non-goals

**Goal:** ending, killing or reattach-exiting one session affects only that
session's containers. Sibling sessions in the same workspace keep their DIND
sidecar, holder and networking.

**Non-goals (this unit):**

- Recovering a session whose holder is already destroyed. Not possible without
  recreating the container; the reattach guard below only reports it.
- Changing sibling *discovery*. Workspace-scoped lookup drives the picker and
  reattach and works correctly; it stays as-is.
- Backward compatibility with containers launched before this change. A running
  session is owned by the binary that launched it, so adopting the fix means
  draining sessions regardless. See Rollout.
- `manage doctor` / `repair` recovery flows (backlog item 4).

## Label contract

Add to `internal/container/labels.go`:

```go
LabelSession = "ai-shim.session" // unique per session; value = agent container name
```

Set in `BuildSpec` immediately after `name` is generated (`builder.go:497`),
value = that name. Docker enforces container-name uniqueness daemon-wide, so
the value is unique by construction, and `docker inspect` on any sidecar names
its owning agent container directly — the property that would have made the
incident above readable without cross-referencing timestamps.

Propagation is by inheritance from `spec.Labels`; no new fields on
`dind.Config` or `HolderConfig`. One insertion point reaches all five
consumers:

| Consumer | Site |
| --- | --- |
| Agent container | `spec.Labels` |
| DIND sidecar | `dind.go:199` (copies `cfg.Labels`) |
| DIND socket volume | `dind.go:175` |
| DIND certs volume | `dind.go:217` |
| netns holder | `dindHolderLabels(spec.Labels)`, `main.go:148` |
| Session network | `EnsureNetwork(..., spec.Labels)`, `main.go:1230` |

On shared network scopes (`global`, `profile`, `workspace`) `EnsureNetwork`
applies labels only when it creates the network, so the session label there
names whichever session created it. Harmless: network cleanup is
attachment-count based (`network.go:136`), never label-identity based, and is
unchanged by this work. Document the caveat next to the constant.

## Teardown

Both filter builders become:

```
ai-shim=true
ai-shim.role=dind | netns-holder
ai-shim.session=<session.ContainerName>
status=running
```

Agent, profile and workspace arguments drop out. They are redundant once
identity is unique, and retaining them would imply a scoping that no longer
does the work. `RunningSession.ContainerName` already carries the value, so
`RunningSession` is unchanged and every caller — `handleReattach`,
`stopSession` (picker `new` and `k<N>`), `manage stop` — is fixed by this one
edit.

Holder removal switches from `ContainerStop{Timeout:5}` + `ContainerRemove` to
`ContainerRemove{Force}`, matching `Holder.Stop`. The stop was always a
five-second no-op: `sleep` as PID 1 has no SIGTERM handler.

A zero-match lookup stays a silent no-op, as today, with a debug log line.

### Filter builders move to `internal/container`

`dindSessionFilters` and `holderSessionFilters` move to `internal/container`,
next to the labels they read, and are exported. They are pure functions over a
`*RunningSession`.

This is what makes the regression test meaningful. `test/e2e` cannot reach
`package main`, so `TestParallel_DINDWorkspaceFilterIsolation` hand-copies the
production filter and asserts against the copy — which is why a filter missing
session scope shipped green. After the move, e2e asserts against the real
function. `stopDINDForSession` itself stays in `main`; only the filters move.

## Reattach guard

Add a pure helper for the netns-owner reference:

```go
func parseNetnsOwner(networkMode string) (id string, ok bool)
```

It recognises the `container:<id>` form and returns the target ID. The caller
inspects that ID and warns when the target is missing or not running.

Hooked at the top of `handleReattach`, which covers both the picker's reattach
path and `manage attach`. Two API calls, no exec — the detection method
validated 2026-07-03.

Warning only; reattach proceeds. A wedged session may still hold work worth
retrieving, and the user, not the tool, decides whether to discard it. The
message states that the netns owner is gone, that networking cannot be
restored for this container, and that recovery means killing the session and
starting a new one.

## Rollout

A running session is served by the binary that launched it, so an upgraded
binary does not change the behaviour of sessions already running. While any
pre-change session is live, its teardown still sweeps by
agent+profile+workspace and will destroy new sessions' sidecars too — the new
labels do not protect against an old sweep, because the old filter never looks
at them.

Adopting the fix therefore requires draining all active sessions. No
compatibility fallback is built for label-less containers: it would either
reproduce the bug (falling back to the sweep) or add a code path that is
unreachable in a correctly drained deployment.

## Testing

Unit:

- `BuildSpec` sets `LabelSession` to the generated container name.
- DIND label construction and `dindHolderLabels` both preserve `LabelSession`.
- Filter builders include the session label and match only the owning
  session's sidecars — given two sibling sessions' label maps, the filter for
  one must not match the other's containers.
- `parseNetnsOwner` table test: `container:<id>`, `bridge`, `host`, empty,
  malformed.

E2E (`test/e2e`), the coverage gap that let this ship:

- Two sessions in the *same* workspace, each with a DIND sidecar and holder.
  Tear down one using the real exported filters. Assert the survivor's DIND and
  holder are still running, and that its agent container still has working
  networking.
- Retain `TestParallel_DINDWorkspaceFilterIsolation` (different workspaces),
  repointed at the exported filters rather than its local copy.

Gate: `make ci` plus the e2e suite before the work is considered done.

## Future enhancements (with adoption triggers)

- `manage doctor` / `repair`: detect a dead netns owner across all sessions and
  offer recreate. Adopt when wedged sessions need recovery rather than
  relaunch. (Backlog item 4.)
- Session label on the picker display, so operators can correlate a session to
  its sidecars without inspecting. Adopt if forensics need it again.
