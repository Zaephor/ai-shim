# netns_mode + DIND survival — design

**Date:** 2026-07-08
**Status:** approved (brainstorm), pending implementation plan
**Scope:** Make the agent container survive DIND sidecar death, and replace the
`dind_shared_netns` boolean with a three-way `netns_mode` selector.

## Problem

Today the DIND sidecar owns the network namespace and the agent joins it via
Docker's `container:<dindID>` network mode (`dind_shared_netns: true`, the
default). This binds the agent's networking to the DIND container's lifecycle:

- When DIND dies (typically OOM), the netns is destroyed. The agent keeps only
  `lo`; `eth0` never returns even if DIND is restarted with the same ID. The
  agent container must be recreated — losing the running session.
- Separately, a restarted dockerd recreates `/var/run/docker.sock` as
  `root:2375`. The agent's socket-group access is applied once by a post-start
  `chgrp` exec, so any restart leaves the socket unreachable by the agent.

Empirically validated (real containers, Docker 29.5.1):

- Restarting a dead netns owner — same ID, manual or via restart-policy — never
  restores a `container:X` dependent's networking. The dependent must be
  recreated.
- A dedicated "holder" container that both the agent and DIND join fully
  survives repeated DIND death/recreation.
- Ports cannot be published on a container that joins another's netns (hard
  Docker error) — they must be published on the netns owner.
- Best dead-netns detection: inspect the dependent's `HostConfig.NetworkMode`,
  then inspect the target ID's `State.Status`/existence (two API calls, no exec).

## Goal / non-goals

**Goal:** agent *process* continuity across DIND death. After a DIND OOM the
agent keeps its network namespace and its docker client reconnects once DIND
auto-restarts — no agent relaunch.

**Non-goals (this unit):**

- Preserving in-DIND state (nested containers/images) across a DIND restart —
  a restarted dockerd is fresh; that state is lost by design.
- Publishing ports under a shared netns (documented future item).
- Diagnosing the DIND OOM root cause (tracked separately).

## Config surface

- **New** `netns_mode: agent | dind | holder` (env `AI_SHIM_NETNS_MODE`).
  **Default `holder`.**
- **New** `dind_netns_holder_image` (env `AI_SHIM_DIND_NETNS_HOLDER_IMAGE`).
  Empty → reuse the DIND image (no extra pull).
- **Removed (hard, no alias)** `dind_shared_netns` /
  `AI_SHIM_DIND_SHARED_NETNS`. The repo is pre-1.0 (0.9.0); the field is
  dropped rather than deprecated. An unknown YAML key or env var has no effect.
  Migration: old `true` → `dind`, old `false` → `agent`. CHANGELOG carries a
  breaking-change note.

**Validation**

- `netns_mode` is only meaningful when DIND is enabled. `dind` or `holder` with
  DIND off is a validation error.
- `holder` (or `dind`) with `ports` set keeps the existing "published ports are
  ignored under a shared DIND network namespace" warning, reworded to name the
  holder as the future publish point.

## Topology per mode

| mode     | netns owner   | agent netns          | DIND netns           | holder container |
|----------|---------------|----------------------|----------------------|------------------|
| `agent`  | agent (own)   | bridge `NetworkID`   | bridge `NetworkID`   | none             |
| `dind`   | DIND          | `container:<dind>`   | bridge `NetworkID`   | none             |
| `holder` | holder        | `container:<holder>` | `container:<holder>` | yes              |

`agent` == old `dind_shared_netns: false`. `dind` == old `true`. `holder` is new.

### Holder is the netns-config anchor

A `container:` joiner inherits the owner's `/etc/hosts`, `/etc/resolv.conf` and
`/etc/hostname` (Docker bind-mounts them from the owner) and cannot set its own.
So in `holder` mode every netns-scoped setting currently split between the agent
and DIND moves onto the holder:

- bridge attachment (`NetworkID`)
- `ExtraHosts`: `host.docker.internal:host-gateway` + the registry-cache alias
  (`CacheHostAlias:<gatewayIP>`)
- hostname — agent and DIND converge on the holder's hostname (the agent already
  inherits DIND's hostname in `dind` mode today)
- future: published ports

The holder runs `dind_netns_holder_image` (default: the DIND image) with a no-op
long-lived command (`sleep infinity` or equivalent). It is labeled
`role=netns-holder` plus the standard session labels so `manage status` and
`manage cleanup` see it.

### SPOF trade-off (explicit)

The netns single-point-of-failure moves from DIND (a real workload that OOMs) to
the holder (an idle sleep process, effectively zero OOM risk). Restarting a dead
holder cannot help — a restart yields a new netns — so the holder gets **no**
restart-policy; it is simply kept minimal. If the holder dies, the session
relaunches, exactly as a DIND death forces today, but far more rarely.

## DIND survival (holder and dind modes)

- **RestartPolicy `unless-stopped`** on the DIND sidecar (`dind.go` ~line 210;
  `cache.go` already uses this pattern). On OOM, DIND auto-restarts into the
  surviving netns (holder mode), so the agent keeps `eth0`.
- **Durable socket group:** pass `dockerd --group=<agentGID>` via the DIND
  container `Cmd` so the daemon creates `/var/run/docker.sock` with the agent's
  group on *every* start, closing the restart-perm trap. The existing one-shot
  post-start `chgrp` stays as belt-and-suspenders. (The image's entrypoint
  prepends `dockerd` + host/TLS flags when the first `Cmd` arg starts with `-`,
  so leading-dash flags pass through — same mechanism as the registry-mirror
  flags.)
- **Restart window is transient, not mediated by ai-shim.** During the
  seconds-long DIND restart the agent's own `docker` calls (the agent shells out
  directly; ai-shim does not proxy them) get connection-refused, then recover
  once dockerd is back. ai-shim cannot inject a retry into the agent's CLI
  calls, so this is documented expected behavior, not a code path here. A
  retrying docker-CLI wrapper is a future enhancement (see below).

**Boundary (documented, not fixed):** a DIND restart is a fresh dockerd — any
containers/images created inside DIND are lost. The win is agent process
continuity, not in-DIND state. Teardown uses `docker stop` (which sets the
container stopped, so `unless-stopped` does not fight teardown).

## Lifecycle

**Launch (holder mode)**

1. Create the shared network (`NetworkID`) — existing step.
2. Create and start the holder (bridge attach, `ExtraHosts`, hostname). Readiness
   = container running; no health probe needed for a sleep process.
3. Start DIND with `NetworkMode=container:<holder>`, RestartPolicy
   `unless-stopped`, and `--group=<agentGID>`. No `ExtraHosts`/bridge on DIND
   (inherited from holder).
4. `WaitForReady(DIND)` via `docker info` exec (exec is netns-independent).
5. One-shot `chgrp` fallback (unchanged).
6. Start/attach the agent with `NetworkMode=container:<holder>`.

`agent` and `dind` modes launch as today (no holder).

**Teardown (reverse):** agent → `docker stop` DIND → stop + remove holder →
remove socket volume → remove network.

**Reattach:** preserve holder + DIND + network. Verify the holder (the netns
owner) is alive; if the holder is dead the netns is gone and the session must
relaunch.

## Future enhancements (with adoption triggers)

- **DIND Healthcheck** (`docker info` as a Docker-native healthcheck) so
  `manage status` reads `.State.Health` instead of the "Up"-string heuristic.
  *Adopt when* DIND starts flapping and the status heuristic misreports state.
- **`manage doctor` / `repair`:** per-session DIND health, dead-netns detection
  (the two-API-call inspect above), and recreate-the-pair recovery.
  *Adopt when* wedged holders/DIND need manual recovery often enough to script.
- **Ports on the holder:** publish `-p` specs on the holder so ports work under
  a shared netns. *Adopt when* a workflow actually needs published ports
  (currently rare; the present pain is collision/predictability, not need).
- **Default DIND memory limit + prune policy.** *Adopt when* the OOM root cause
  is confirmed and a hard cap is wanted.
- **Retrying docker-CLI wrapper** (installed in the agent via SharedBin) that
  backs off across the DIND restart window so the agent's `docker` calls don't
  fail transiently. *Adopt when* the transient-failure window proves disruptive
  in real workflows.

## Testing

**Unit**

- Config parse/validate/merge for `netns_mode` and `dind_netns_holder_image`;
  removal of `dind_shared_netns` (old key/env has no effect).
- Validation: `dind`/`holder` with DIND off errors; `holder` + ports warns.
- Spec-build per mode: holder carries `NetworkID` + `ExtraHosts` + hostname;
  agent and DIND set `NetworkMode=container:<holder>` and omit their own
  `ExtraHosts`; DIND `Cmd` includes `--group=<agentGID>`.

**e2e (Linux)**

- Replace `test/e2e/dind_shared_netns_test.go` with per-mode netns coverage.
- **Survival test:** in `holder` mode, kill the DIND container, assert the agent
  retains `eth0` and its docker client reconnects after DIND auto-restarts.
- Gate: `make ci` (vuln/fuzz/tidy/race/vet/fmt) + e2e green before resolving the
  phase.
