# Cleanup scope and sidecar state — design

**Date:** 2026-07-31
**Status:** implemented
**Scope:** Stop `manage cleanup` from force-removing running containers, and
let session-scoped sidecar teardown reach sidecars that are not running.

## Problem

Two defects, both left open by the session-scoped teardown work merged on
2026-07-30 (`docs/plans/2026-07-30-session-scoped-sidecar-teardown-design.md`),
and both about which containers a label query is allowed to destroy.

### `manage cleanup` sweeps running containers

`Cleanup` (`internal/cli/manage.go:745`) lists with `All: true` filtered on
`ai-shim=true` alone, then force-removes every match:

```go
containers, err := cli.ContainerList(ctx, container_types.ListOptions{
	All:     true,
	Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
})
...
if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
```

There is no status, session, or workspace scope. Running agent containers,
DIND sidecars, netns holders and the shared registry cache are all removed,
across every workspace on the host, including sessions belonging to other
users of the same daemon.

This contradicts the command's stated contract. `README.md:369` describes it
as "remove orphaned ai-shim containers"; `docs/plans/2026-03-21-ai-shim-design.md:184`
says containers are labeled "so `ai-shim manage cleanup` can find and remove
orphans"; `main.go:834` reports errors as "cleaning up orphaned resources" and
prints "No orphaned resources found." Only the implementation disagrees.

It is also the same defect class the 2026-07-30 branch existed to fix — an
under-scoped label query destroying live sessions — left in the one code path
that branch did not touch. The teardown design doc rules `manage cleanup` out
as the cause of the observed incident (line 74) precisely because it *would*
have taken the agent containers too.

### Sidecar teardown cannot reach a non-running sidecar

`DINDSessionFilters` and `HolderSessionFilters`
(`internal/container/sidecar_filters.go`) both carry
`filters.Arg("status", "running")`, and both `ContainerList` calls in
`StopForSession` (`internal/dind/teardown.go:44`, `:82`) omit `All`, which
defaults to running-only. Either restriction alone is sufficient to hide a
sidecar that is not running.

A DIND sidecar carries `RestartPolicyUnlessStopped`. One that is crash-looping,
paused, exited, or between restarts is invisible to teardown, so it survives
the session that owns it, and its `-socket` and `-certs` volumes leak with it.

The status filter was safe to remove only after the session label landed:
before it, a stateless filter matched every sibling's sidecar, and widening
the state would have widened the blast radius. The label makes the filter
identity-precise, so state is now free to drop.

## Empirical finding

Verified against Docker 29.5.1, not assumed.

Docker refuses to remove an in-use volume or network regardless of `force`:

```
$ docker volume rm -f ai-shim-probe-vol
Error response from daemon: remove ai-shim-probe-vol: volume is in use - [69ddd49dc028...]

$ docker network rm ai-shim-probe-net
Error response from daemon: error while removing network: network ai-shim-probe-net has active endpoints (name:"ai-shim-probe" id:"ed84de40e81a")
```

`force` on `VolumeRemove` suppresses not-found, not in-use. So `Cleanup`'s
destructive power is **containers only** — and that is sufficient, because
the loops run in order: removing the containers un-uses their volumes and
networks, and the same invocation then removes those successfully. Scoping
the container pass therefore protects all three resource kinds transitively.

## Goal / non-goals

**Goal:** `manage cleanup` removes only containers that are not running, and
sidecar teardown reaches its own session's sidecars in any state.

**Non-goals:**

- Session-liveness scoping — protecting a *stopped* sidecar whose session is
  still live, or a volume whose session label matches a running container.
  Deferred; recorded under Future work.
- Adding `./internal/cli/` to the Docker CI job. The coverage gap is real and
  recorded under Future work, but the tests here are placed to land in jobs
  that already exist.
- The `errors.Join` warning-prefix defect at `cmd/ai-shim/main.go:1662`
  and `:1714`, tracked separately.

## Cleanup selection

A pure predicate decides, in a new file, so the decision is testable without
a daemon:

```go
// internal/cli/cleanup_select.go
func isOrphanedContainer(c container_types.Summary) bool
```

`Summary.State` is the only input. States are Docker's container state
vocabulary:

| State        | Decision | Reason                                          |
|--------------|----------|-------------------------------------------------|
| `running`    | keep     | live session                                    |
| `restarting` | keep     | live session, mid-restart                       |
| `paused`     | keep     | live session, resumable                         |
| `removing`   | keep     | daemon already owns it                          |
| `exited`     | remove   | orphan                                          |
| `created`    | remove   | never started                                   |
| `dead`       | remove   | unrecoverable, removal is the only useful action |
| anything else| keep     | fail-safe                                       |

The unknown-state default is **keep**. An unrecognized state must never
authorize a force-remove; a leaked container is recoverable, a destroyed live
session is not.

`Cleanup` keeps `All: true` and skips non-orphans before `ContainerRemove`.
`Cleanup`'s signature, `CleanupResult`, and the `main.go:831` command body are
unchanged. No flag, no config key.

## In-use volumes and networks are skips, not failures

Once the container pass stops removing live containers, the volume and network
passes start failing on those live containers' resources, and the existing
code appends every such failure to `result.Failed` — printed to the user as a
removal failure.

That reads wrong: a volume attached to a running session is not an orphan and
its retention is not a failure. Both passes gain an in-use check alongside the
existing `cerrdefs.IsNotFound` skip (`manage.go:783`), and a matched error is
dropped silently rather than recorded.

The check matches on the daemon's error text (`is in use`, `has active
endpoints`) because `containerd/errdefs` maps both to a generic conflict, which
would also swallow unrelated conflicts. `CleanupResult` gains no field: a
skipped in-use resource is simply not reported, consistent with how the
command reports only what it removed.

## Sidecar filters become identity-only

`DINDSessionFilters` and `HolderSessionFilters` drop their `status` argument,
leaving base label, role/DIND label, and session label. Both `ContainerList`
calls in `StopForSession` gain `All: true`.

Both changes are required — either alone leaves the sidecar hidden. Each
function's doc comment states that it matches by identity and that liveness is
the caller's choice, so the next reader does not restore the status argument
believing it was an oversight.

`ContainerStop` on an already-exited container returns nil, so the existing
loop body in `StopForSession` needs no new branching. Volume removal already
tolerates not-found.

The two e2e call sites (`test/e2e/parallel_session_test.go:367`, `:543`) list
with `ListOptions{}`, so they remain running-only by default and their
"exactly one sidecar" assertions keep their current meaning.

## Testing

Placement is driven by which CI job runs which package. `ci.yml:258` and
`:363` run the Docker-enabled suites over `./test/...`, `./internal/container/`,
`./internal/dind/`, `./internal/docker/` and `./internal/network/`.
`./internal/cli/` is in none of them, so a daemon-dependent test placed beside
the existing `TestCleanup_*` tests would never guard anything in CI.

| Test | Package | Runs in |
|------|---------|---------|
| `isOrphanedContainer` table over every state, including unknown | `internal/cli` | every job, `-short` safe |
| Running labeled container survives `Cleanup`; exited one is removed | `test/e2e` | Docker job |
| In-use volume is skipped, not reported in `Failed` | `test/e2e` | Docker job |
| Filters carry no `status` key | `internal/container` | every job |
| Exited DIND is torn down and its socket/certs volumes removed | `internal/dind` | Docker job |
| `--force` removes a running container | `test/e2e` | Docker job |

The e2e cleanup test must create its own labeled fixtures and remove them in
`t.Cleanup`, since `Cleanup` operates daemon-wide and the suite shares a
daemon with other tests.

## `--force`

`cleanup --force` (`-f`) restores the unscoped sweep behind an explicit flag
and a stderr warning naming the blast radius.

It ships in this change rather than being deferred, because the shipped
`[Unreleased]` rollout note directs operators at `manage cleanup` to reclaim
sidecars leaked by pre-label sessions. Those sidecars carry
`RestartPolicyUnlessStopped`, so they are still running and are not orphans
by state — without the flag, the documented rollout path stops working the
moment this change lands.

## Risk

`manage cleanup` stops reaping a container that is wedged but still reports
`running`. That is a real capability loss for anyone using the command as a
blunt instrument. The workaround is `docker rm -f` by name, or `manage
cleanup --force`. Recorded in the CHANGELOG under `[Unreleased]`.

No rollout constraint: unlike the session-label change, neither fix depends on
labels applied at launch, so both take effect for already-running sessions the
moment the new binary runs.

## Future work (with adoption triggers)

- **Session-liveness scoping** — keep a stopped sidecar, volume or network
  whose `ai-shim.session` label matches a container that is still running.
  Adopt when a crash-looping sidecar belonging to a live session is observed
  being reaped by `cleanup`.
- **`./internal/cli/` in the Docker CI job** — closes the coverage gap that
  forced the test placement above. Adopt when a second daemon-dependent
  behavior lands in that package.
