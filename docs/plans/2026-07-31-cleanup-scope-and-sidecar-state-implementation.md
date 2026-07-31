# Cleanup scope and sidecar state — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `ai-shim manage cleanup` from force-removing running containers across every workspace, and let session-scoped sidecar teardown reach sidecars that are not currently running.

**Architecture:** `Cleanup` gains a pure state predicate (`isOrphanedContainer`) applied client-side to the list it already fetches with `All: true`, plus a `force bool` parameter that restores the old unscoped sweep behind an explicit CLI flag. Independently, the two sidecar filter builders drop their `status=running` argument and `StopForSession`'s two `ContainerList` calls gain `All: true`, so a crash-looping DIND and its volumes are reachable.

**Tech Stack:** Go 1.26 (`toolchain go1.26.4` pinned in `go.mod`), `github.com/docker/docker` SDK v28.5.2, `testify` (`assert`/`require`), `containerd/errdefs` for Docker error classification.

**Design doc:** `docs/plans/2026-07-31-cleanup-scope-and-sidecar-state-design.md`

## Global Constraints

- Conventional Commits for every commit: `<type>(<scope>)?: <description>`, imperative mood, lowercase after the type, no trailing period, subject ≤72 chars.
- Every commit message body must be comprehensive — a full summary of what changed and why.
- Every commit ends with the footer `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`.
- Do not push, pull, or otherwise touch the remote. The human handles all remote operations.
- `make ci` (fmt-check, vet, tidy-check, check-silent-failures, test-race, fuzz, vuln) must pass before the branch is considered done. Do not claim CI-green from a partial local run.
- Docker-dependent tests must call `testutil.SkipIfNoDocker(t)` and additionally skip on `testing.Short()`, matching the existing pattern in `internal/dind/teardown_test.go`.
- `container_types.Summary.State` is a plain `string`. Compare it by converting: `container_types.ContainerState(c.State)`. This is verified to compile — do not compare the raw string against the typed constants directly.

---

## File Structure

| File | Status | Responsibility |
|------|--------|----------------|
| `internal/container/sidecar_filters.go` | Modify | Session-scoped sidecar identity filters. Drops `status`. |
| `internal/container/sidecar_filters_test.go` | Modify | Asserts filters carry no `status` key. |
| `internal/dind/teardown.go` | Modify | `StopForSession` lists with `All: true`. |
| `internal/dind/teardown_test.go` | Modify | Adds an exited-DIND teardown test. |
| `internal/cli/cleanup_select.go` | Create | `isOrphanedContainer` — the pure state predicate. Own file so the decision is unit-testable with no daemon. |
| `internal/cli/cleanup_select_test.go` | Create | Table test over every Docker container state plus unknown. |
| `internal/cli/manage.go` | Modify | `Cleanup(force bool)` applies the predicate; volume/network passes skip in-use errors. |
| `internal/cli/manage_test.go` | Modify | Existing `Cleanup()` call sites updated for the new signature. |
| `cmd/ai-shim/main.go` | Modify | Parses `--force`, passes it to `Cleanup`, prints the warning, updates help text. |
| `test/e2e/user_journey_test.go` | Modify | Existing `cli.Cleanup()` call updated for the new signature. |
| `test/e2e/cleanup_test.go` | Create | Real-daemon proof that a running container survives and an exited one does not. |
| `CHANGELOG.md` | Modify | Rewrites the rollout note; adds the cleanup behavior change. |
| `README.md` | Modify | Documents `cleanup --force`. |
| `docs/plans/2026-07-31-cleanup-scope-and-sidecar-state-design.md` | Modify | Moves `--force` from future work into scope. |

---

## Task 1: Sidecar filters become identity-only

**Files:**
- Modify: `internal/container/sidecar_filters.go:13-33`
- Modify: `internal/container/sidecar_filters_test.go:18-36`
- Modify: `internal/dind/teardown.go:44-46`, `internal/dind/teardown.go:82-84`
- Test: `internal/container/sidecar_filters_test.go`, `internal/dind/teardown_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `container.DINDSessionFilters(session *RunningSession) filters.Args` and `container.HolderSessionFilters(session *RunningSession) filters.Args` — signatures unchanged, but the returned `filters.Args` no longer contains a `status` key. Callers that want running-only must pass `All: false` (the `ListOptions` zero value).

- [ ] **Step 1: Update the filter unit tests to require no status key**

In `internal/container/sidecar_filters_test.go`, replace line 25 (in `TestDINDSessionFilters_ScopedToSession`) and line 35 (in `TestHolderSessionFilters_ScopedToSession`). Each currently reads:

```go
	assert.Contains(t, f.Get("status"), "running")
```

Replace each with:

```go
	assert.Empty(t, f.Get("status"),
		"the filter matches by identity; liveness is the caller's choice via ListOptions.All, "+
			"and a status here hides a crash-looping sidecar from teardown")
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/container/ -run 'TestDINDSessionFilters_ScopedToSession|TestHolderSessionFilters_ScopedToSession' -v -count=1`

Expected: both FAIL, each reporting the `status` slice is not empty (it contains `running`).

- [ ] **Step 3: Drop the status argument from both filter builders**

In `internal/container/sidecar_filters.go`, delete the `filters.Arg("status", "running"),` line from `DINDSessionFilters` (line 18) and from `HolderSessionFilters` (line 32). Then append this paragraph to **both** doc comments, immediately before the `func` line:

```go
// The filter matches by identity only. Liveness is the caller's choice via
// ListOptions.All: a status argument here would hide a crash-looping,
// paused or exited sidecar from teardown, which then outlives its session
// and leaks its volumes. Do not add one back.
```

After the edit `DINDSessionFilters` returns exactly:

```go
	return filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelDIND+"=true"),
		filters.Arg("label", LabelSession+"="+session.ContainerName),
	)
```

and `HolderSessionFilters` returns exactly:

```go
	return filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelRole+"=netns-holder"),
		filters.Arg("label", LabelSession+"="+session.ContainerName),
	)
```

- [ ] **Step 4: Run the filter tests to verify they pass**

Run: `go test ./internal/container/ -run 'SessionFilters|SidecarFilters' -v -count=1`

Expected: PASS, including the pre-existing `TestSidecarFilters_DoNotUseWorkspaceScoping`.

- [ ] **Step 5: Write the failing teardown test for an exited DIND**

Append to `internal/dind/teardown_test.go`. The fixture helper starts the container running, so the test stops it first to produce the exited state:

```go
// TestStopForSession_RemovesExitedDIND covers the crash-looping sidecar. A
// DIND carries RestartPolicyUnlessStopped, so one that is between restarts —
// or has exited outright — is not "running". While the filters carried
// status=running and StopForSession listed with All defaulted to false, such
// a sidecar was invisible to teardown: it outlived the session that owned it
// and its socket and certs volumes leaked with it.
func TestStopForSession_RemovesExitedDIND(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()
	session := fmt.Sprintf("ai-shim-teardown-exited-%d", stamp)

	dindID := teardownFixture(t, ctx, cli, session+"-dind", session, "dind", "")

	// Drive the fixture to "exited". `sleep infinity` as PID 1 ignores
	// SIGTERM, so a graceful stop would burn its full timeout; kill instead.
	require.NoError(t, cli.ContainerKill(ctx, dindID, "SIGKILL"))
	require.Eventually(t, func() bool {
		insp, err := cli.ContainerInspect(ctx, dindID)
		return err == nil && insp.State != nil && !insp.State.Running
	}, 30*time.Second, 200*time.Millisecond, "fixture DIND must reach a non-running state")

	socketVol := session + "-dind-socket"
	certsVol := session + "-dind-certs"
	for _, name := range []string{socketVol, certsVol} {
		_, err := cli.VolumeCreate(ctx, dvolume.CreateOptions{Name: name})
		require.NoError(t, err, "creating fixture volume %q", name)
		t.Cleanup(func(name string) func() {
			return func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = cli.VolumeRemove(cleanupCtx, name, true)
			}
		}(name))
	}

	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: session,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	require.NoError(t, err)

	_, err = cli.ContainerInspect(ctx, dindID)
	assert.True(t, cerrdefs.IsNotFound(err),
		"an exited DIND must still be removed by teardown, got err=%v", err)

	for _, name := range []string{socketVol, certsVol} {
		_, err := cli.VolumeInspect(ctx, name)
		assert.True(t, cerrdefs.IsNotFound(err),
			"an exited DIND's volume %q must be removed too, got err=%v", name, err)
	}
}
```

- [ ] **Step 6: Run the teardown test to verify it fails**

Run: `go test ./internal/dind/ -run TestStopForSession_RemovesExitedDIND -v -count=1`

Expected: FAIL. `StopForSession` lists with `All` defaulted to `false`, so the exited DIND never matches; the container still exists and `ContainerInspect` returns no error, so the `cerrdefs.IsNotFound` assertion fails.

- [ ] **Step 7: List all container states in StopForSession**

In `internal/dind/teardown.go`, add `All: true` to both `ContainerList` calls.

At line 44, change:

```go
	list, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: ai_container.DINDSessionFilters(session),
	})
```

to:

```go
	list, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: ai_container.DINDSessionFilters(session),
	})
```

At line 82, change:

```go
	holderList, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: ai_container.HolderSessionFilters(session),
	})
```

to:

```go
	holderList, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: ai_container.HolderSessionFilters(session),
	})
```

Both changes are required alongside Step 3. Either restriction alone — the `status` argument or the defaulted `All` — is enough to hide the sidecar.

- [ ] **Step 8: Run the teardown test to verify it passes**

Run: `go test ./internal/dind/ -run TestStopForSession_RemovesExitedDIND -v -count=1`

Expected: PASS.

- [ ] **Step 9: Run the full teardown suite for regressions**

Run: `go test ./internal/dind/ ./internal/container/ -count=1`

Expected: PASS. `TestStopForSession_LeavesSiblingSidecarsAlone` must still pass — the session label, not the status argument, is what keeps siblings safe.

- [ ] **Step 10: Commit**

```bash
git add internal/container/sidecar_filters.go internal/container/sidecar_filters_test.go \
        internal/dind/teardown.go internal/dind/teardown_test.go
git commit -F - <<'MSG'
fix(dind): tear down sidecars that are not currently running

Drop the status=running argument from DINDSessionFilters and
HolderSessionFilters, and list with All:true in StopForSession. Either
restriction alone hid a sidecar that was not running at teardown time:
a DIND carries RestartPolicyUnlessStopped, so one that is crash-looping,
between restarts, paused or exited survived the session that owned it,
and its socket and certs volumes leaked with it.

Dropping the status argument only became safe once the session label
landed. Before it, the filters matched on agent+profile+workspace, which
every parallel sibling shares, so widening the state would have widened
the blast radius onto live sessions. Scoped by session, the filter is
identity-precise and state is free to drop.

Both doc comments now state that the filters match by identity and that
liveness is the caller's choice, so the argument is not restored later
as an apparent oversight. The two e2e call sites list with the
ListOptions zero value and stay running-only, so their "exactly one
sidecar" assertions keep their current meaning.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
MSG
```

---

## Task 2: The orphan predicate

**Files:**
- Create: `internal/cli/cleanup_select.go`
- Test: `internal/cli/cleanup_select_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func isOrphanedContainer(c container_types.Summary) bool` in package `cli` — unexported, consumed by `Cleanup` in Task 3. Returns `true` only for `exited`, `created` and `dead`.

- [ ] **Step 1: Write the failing test**

Create `internal/cli/cleanup_select_test.go`:

```go
package cli

import (
	"testing"

	container_types "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
)

// TestIsOrphanedContainer covers every state the Docker daemon can report
// plus the unrecognized case. `manage cleanup` force-removes whatever this
// returns true for, so a wrong answer here destroys a live session — the
// same defect class as the unscoped sidecar teardown.
func TestIsOrphanedContainer(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  bool
		why   string
	}{
		{"running", false, "a running container is a live session"},
		{"restarting", false, "a restarting container is a live session mid-restart"},
		{"paused", false, "a paused container is a live session and is resumable"},
		{"removing", false, "the daemon already owns a removing container"},
		{"exited", true, "an exited container is an orphan"},
		{"created", true, "a created container never started"},
		{"dead", true, "a dead container is unrecoverable; removal is the only useful action"},
		{"", false, "an empty state must not authorize a force-remove"},
		{"some-future-state", false, "an unrecognized state must not authorize a force-remove"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			got := isOrphanedContainer(container_types.Summary{State: tc.state})
			assert.Equal(t, tc.want, got, tc.why)
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestIsOrphanedContainer -v -count=1`

Expected: FAIL to build with `undefined: isOrphanedContainer`.

- [ ] **Step 3: Write the predicate**

Create `internal/cli/cleanup_select.go`:

```go
package cli

import (
	container_types "github.com/docker/docker/api/types/container"
)

// isOrphanedContainer reports whether an ai-shim container is an orphan and
// may be force-removed by `manage cleanup`.
//
// The default is to keep. An unrecognized or empty state must never
// authorize a force-remove: a container left behind is recoverable by
// running cleanup again, while a destroyed live session is not — its agent
// loses a netns it cannot regain, which is the failure mode session-scoped
// teardown exists to prevent.
//
// Only containers are checked. Docker itself refuses to remove an in-use
// volume or network regardless of the force flag, and the cleanup passes
// run containers-first, so declining to remove a live container transitively
// protects that session's volumes and network too.
func isOrphanedContainer(c container_types.Summary) bool {
	switch container_types.ContainerState(c.State) {
	case container_types.StateExited, container_types.StateCreated, container_types.StateDead:
		return true
	default:
		// running, restarting, paused, removing, and anything the daemon
		// starts reporting in a future API version.
		return false
	}
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run TestIsOrphanedContainer -v -count=1`

Expected: PASS, all nine subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/cleanup_select.go internal/cli/cleanup_select_test.go
git commit -F - <<'MSG'
feat(cleanup): add the orphaned-container state predicate

Add isOrphanedContainer, the pure state decision `manage cleanup` will
use to stop force-removing live sessions. Exited, created and dead are
orphans; running, restarting, paused and removing are not.

The default is keep. An unrecognized or empty state must never authorize
a force-remove, because the two outcomes are not symmetric: a container
left behind is reclaimed by the next cleanup run, while a destroyed live
session strands its agent on a network namespace it cannot regain.

The predicate lives in its own file with a unit test rather than inline
in Cleanup because CI's Docker job runs ./test/..., ./internal/container/,
./internal/dind/, ./internal/docker/ and ./internal/network/ but never
./internal/cli/ — a daemon-dependent test placed beside the existing
Cleanup tests would never guard this in CI. This test needs no daemon and
runs in every job, including under -short.

Not yet wired into Cleanup; that follows with the --force flag so the
exported signature changes exactly once.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
MSG
```

---

## Task 3: Wire the predicate into `Cleanup`, behind `--force`

**Files:**
- Modify: `internal/cli/manage.go:745-774`
- Modify: `internal/cli/manage_test.go:254`, `:1243`, `:1556`
- Modify: `test/e2e/user_journey_test.go:462`
- Modify: `cmd/ai-shim/main.go:543`, `:831-832`
- Test: covered behaviorally by Task 5 — this task is wiring, and a daemon is
  required to prove it. `./internal/cli/` runs in no Docker-enabled CI job, so
  a test placed here would guard nothing.

**Interfaces:**
- Consumes: `isOrphanedContainer(c container_types.Summary) bool` from Task 2.
- Produces: `func Cleanup(force bool) (CleanupResult, error)` — the exported signature gains one parameter. `force == false` removes only orphans; `force == true` reproduces the pre-change unscoped sweep. `CleanupResult` is unchanged.

- [ ] **Step 1: Update all four existing call sites**

`Cleanup` currently takes no arguments. Change each of these from `Cleanup()` to `Cleanup(false)`:

- `internal/cli/manage_test.go:254`
- `internal/cli/manage_test.go:1243`
- `internal/cli/manage_test.go:1556`
- `test/e2e/user_journey_test.go:462`

Do this first so both packages still build once the signature changes.

`test/e2e/user_journey_test.go:412`'s `TestJourney_CleanupRemovesOrphans` runs `echo orphan` and waits for the container to exit before cleaning up, so it exercises the exited case and stays correct under the new behavior. Its doc comment already says "removes stopped containers" — leave it as is; only the call changes.

- [ ] **Step 2: Change the signature and apply the predicate**

In `internal/cli/manage.go`, change the doc comment and signature at lines 744-746 from:

```go
// Cleanup finds and removes orphaned ai-shim containers, networks, and volumes.
func Cleanup() (CleanupResult, error) {
```

to:

```go
// Cleanup finds and removes orphaned ai-shim containers, networks, and volumes.
//
// An orphan is a container that is not running — see isOrphanedContainer.
// Containers belonging to live sessions are left alone, in any workspace and
// for any user of the daemon. Because the passes run containers-first and
// Docker refuses to remove an in-use volume or network, keeping a live
// container also keeps that session's volumes and network.
//
// force restores the pre-2026-07-31 behavior: every ai-shim-labelled
// container is force-removed regardless of state or workspace. It exists for
// the rollout case in the CHANGELOG — sidecars leaked by a session launched
// before the ai-shim.session label existed are still running, carry
// RestartPolicyUnlessStopped, and are therefore not orphans by state. It
// will also destroy any live session sharing the daemon, so callers must
// warn before passing true.
func Cleanup(force bool) (CleanupResult, error) {
```

Then, in the container loop at line 767, insert the guard as the first statement of the loop body:

```go
	for _, c := range containers {
		if !force && !isOrphanedContainer(c) {
			continue
		}
		name := containerDisplayName(c)
```

Leave `All: true` on the `ContainerList` call at line 758 — the predicate needs to see every state in order to classify it.

- [ ] **Step 3: Add the flag and the warning at the call site**

In `cmd/ai-shim/main.go`, replace lines 831-832:

```go
	case "cleanup":
		result, err := cli.Cleanup()
```

with:

```go
	case "cleanup":
		force := false
		for _, a := range args[1:] {
			if a == "--force" || a == "-f" {
				force = true
			}
		}
		if force {
			fmt.Fprintln(os.Stderr,
				"ai-shim: warning: --force removes RUNNING ai-shim containers in every workspace, "+
					"including sessions belonging to other users of this Docker daemon")
		}
		result, err := cli.Cleanup(force)
```

- [ ] **Step 4: Update the subcommand help text**

In `cmd/ai-shim/main.go`, replace line 543:

```go
		"cleanup":        "Usage: ai-shim manage cleanup\n\n  Remove orphaned ai-shim containers, networks, and volumes.",
```

with:

```go
		"cleanup":        "Usage: ai-shim manage cleanup [--force]\n\n  Remove orphaned ai-shim containers, networks, and volumes.\n  An orphan is a container that is not running; live sessions are left\n  alone, in every workspace.\n\n  --force, -f  Also remove RUNNING containers, in every workspace and for\n               every user of this Docker daemon. Needed only to reclaim\n               sidecars leaked by sessions launched before the\n               ai-shim.session label existed.",
```

- [ ] **Step 5: Verify the whole tree builds and the unit tests pass**

Run: `go build ./... && go vet ./... && go test -short ./... -count=1`

Expected: build and vet succeed, tests PASS. `go vet ./...` matters here: `test/e2e` is a test-only package that `go build ./...` does not compile, so a missed call site there surfaces only under vet or the e2e run. If anything fails, find remaining call sites with `grep -rn '\bCleanup()' --include=*.go . | grep -v 't.Cleanup'`

- [ ] **Step 6: Commit**

```bash
git add internal/cli/manage.go internal/cli/manage_test.go cmd/ai-shim/main.go \
        test/e2e/user_journey_test.go
git commit -F - <<'MSG'
fix(cleanup): stop removing running containers by default

manage cleanup listed every ai-shim-labelled container with All:true and
force-removed all of them, with no status, session or workspace scope. It
took running agent containers, DIND sidecars, netns holders and the shared
registry cache, across every workspace on the host, including sessions
belonging to other users of the same daemon. This contradicted the
command's own help text, the README, and the error string at its call
site, all of which describe it as removing orphans.

It is also the same defect class the session-scoped teardown work fixed —
an under-scoped label query destroying live sessions — left in the one
path that work did not touch.

Cleanup now applies isOrphanedContainer before removing, so only exited,
created and dead containers go. Because the passes run containers-first
and Docker refuses to remove an in-use volume or network regardless of the
force flag, keeping a live container also keeps that session's volumes and
network; no separate scoping is needed for those.

Add `manage cleanup --force` (`-f`) to restore the old sweep behind an
explicit flag and a stderr warning. This is not hypothetical: the
CHANGELOG's rollout note tells operators to run cleanup to reclaim
sidecars leaked by pre-label sessions, and those sidecars carry
RestartPolicyUnlessStopped, so they are still running and are not orphans
by state. Without the flag the documented rollout path would no longer
work.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
MSG
```

---

## Task 4: In-use volumes and networks are skips, not failures

**Files:**
- Modify: `internal/cli/manage.go:776-806`
- Modify: `internal/cli/cleanup_select.go`
- Create: (none)
- Test: `internal/cli/cleanup_select_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func isInUseError(err error) bool` in package `cli` — unexported, used by the volume and network passes in `Cleanup`.

**Context:** verified against Docker 29.5.1, `docker volume rm -f` on an in-use volume fails with `remove <name>: volume is in use - [<id>]`, and `docker network rm` on a network with an attached container fails with `network <name> has active endpoints`. The `force` argument to `VolumeRemove` suppresses not-found, not in-use. After Task 3 these errors start appearing for every live session and land in `result.Failed`, which reports retention of a live session's resources as a failure.

- [ ] **Step 1: Write the failing test**

Append to `internal/cli/cleanup_select_test.go`. Add `"errors"` to its import block:

```go
// TestIsInUseError distinguishes the daemon's in-use refusals from real
// failures. After cleanup stopped removing live containers, their volumes
// and networks stay attached, and the daemon refuses to remove them. That is
// the correct outcome, not a failure to report to the user.
//
// Matching is on the message rather than errdefs because the daemon maps
// both of these to a generic conflict, which would also swallow unrelated
// conflicts such as a name collision.
func TestIsInUseError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "volume in use",
			err:  errors.New("Error response from daemon: remove ai-shim-vol: volume is in use - [69ddd49dc028]"),
			want: true,
		},
		{
			name: "network has active endpoints",
			err:  errors.New(`Error response from daemon: error while removing network: network ai-shim-net has active endpoints (name:"c" id:"ed84de40e81a")`),
			want: true,
		},
		{
			name: "unrelated conflict is a real failure",
			err:  errors.New("Error response from daemon: a volume with the name ai-shim-vol already exists"),
			want: false,
		},
		{
			name: "permission denied is a real failure",
			err:  errors.New("permission denied while trying to connect to the Docker daemon socket"),
			want: false,
		},
		{
			name: "nil is not an in-use error",
			err:  nil,
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isInUseError(tc.err))
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestIsInUseError -v -count=1`

Expected: FAIL to build with `undefined: isInUseError`.

- [ ] **Step 3: Write the helper**

Append to `internal/cli/cleanup_select.go`, and add `"strings"` to its import block:

```go
// isInUseError reports whether err is the daemon refusing to remove a volume
// or network because something is still attached to it.
//
// Since cleanup stopped removing live containers, their volumes and networks
// necessarily stay in use, and the daemon rejects removing them. Reporting
// that as a removal failure is wrong: the resource belongs to a running
// session and retaining it is the intended outcome.
//
// The match is on the message. containerd/errdefs maps both refusals to a
// generic conflict, which would also swallow unrelated conflicts such as a
// name collision — a real failure the user should see.
func isInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "volume is in use") ||
		strings.Contains(msg, "has active endpoints")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/cli/ -run TestIsInUseError -v -count=1`

Expected: PASS, all five subtests.

- [ ] **Step 5: Apply the skip in both passes**

In `internal/cli/manage.go`, in the network loop at line 783, change:

```go
			if err := cli.NetworkRemove(ctx, n.ID); err != nil && !cerrdefs.IsNotFound(err) {
				result.Failed = append(result.Failed, fmt.Sprintf("network %s: %v", n.Name, err))
			} else {
				result.RemovedNetworks = append(result.RemovedNetworks, n.Name)
			}
```

to:

```go
			err := cli.NetworkRemove(ctx, n.ID)
			switch {
			case err == nil:
				result.RemovedNetworks = append(result.RemovedNetworks, n.Name)
			case cerrdefs.IsNotFound(err), isInUseError(err):
				// Already gone, or still serving a live session. Neither is
				// a failure and neither is a removal.
			default:
				result.Failed = append(result.Failed, fmt.Sprintf("network %s: %v", n.Name, err))
			}
```

Then in the volume loop at line 799, change:

```go
			if err := cli.VolumeRemove(ctx, v.Name, true); err != nil {
				result.Failed = append(result.Failed, fmt.Sprintf("volume %s: %v", v.Name, err))
			} else {
				result.RemovedVolumes = append(result.RemovedVolumes, v.Name)
			}
```

to:

```go
			err := cli.VolumeRemove(ctx, v.Name, true)
			switch {
			case err == nil:
				result.RemovedVolumes = append(result.RemovedVolumes, v.Name)
			case cerrdefs.IsNotFound(err), isInUseError(err):
				// Already gone, or still mounted by a live session. The
				// force argument suppresses not-found, not in-use.
			default:
				result.Failed = append(result.Failed, fmt.Sprintf("volume %s: %v", v.Name, err))
			}
```

Note the network loop previously counted a not-found network as removed; it is now correctly counted as neither.

- [ ] **Step 6: Verify the package builds and its tests pass**

Run: `go build ./... && go test -short ./internal/cli/ -count=1`

Expected: build succeeds, tests PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/cleanup_select.go internal/cli/cleanup_select_test.go internal/cli/manage.go
git commit -F - <<'MSG'
fix(cleanup): report in-use volumes and networks as skips

Now that cleanup keeps running containers, their volumes and networks
necessarily stay attached and the daemon refuses to remove them. Both
refusals were being appended to CleanupResult.Failed and printed to the
user, framing the retention of a live session's resources as a removal
failure.

Add isInUseError and skip those refusals silently in both passes, next to
the existing not-found skip. The match is on the daemon's message rather
than errdefs: errdefs maps both refusals to a generic conflict, which
would also swallow unrelated conflicts such as a name collision — a real
failure the user should see. Verified against Docker 29.5.1: an in-use
volume fails with "volume is in use - [<id>]" and an attached network with
"has active endpoints", and the force argument to VolumeRemove suppresses
not-found, not in-use.

Also stop counting a not-found network as removed. The previous
if/else counted every non-not-found error as a failure and everything
else — including not-found — as a successful removal.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
MSG
```

---

## Task 5: End-to-end proof against a real daemon

**Files:**
- Create: `test/e2e/cleanup_test.go`
- Test: `test/e2e/cleanup_test.go`

**Interfaces:**
- Consumes: `cli.Cleanup(force bool) (cli.CleanupResult, error)` from Task 3.
- Produces: nothing consumed by later tasks.

**Context:** this is the only test in the plan that exercises `Cleanup` against a daemon. `./internal/cli/` is absent from every Docker-enabled CI job (`ci.yml:258` and `:363` run `./test/...`, `./internal/container/`, `./internal/dind/`, `./internal/docker/` and `./internal/network/`), so it must live in `test/e2e` to run in CI at all. `Cleanup` operates daemon-wide, so the test must create its own labelled fixtures and remove them itself.

- [ ] **Step 1: Write the failing test**

Create `test/e2e/cleanup_test.go`:

```go
package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/cli"
	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	dvolume "github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupFixture creates a container carrying the ai-shim base label, which
// is the only thing `manage cleanup` matches on. Registers its own removal.
//
// The Docker client parameter is named dcli, not cli: this package already
// imports internal/cli as cli, and shadowing it here would hide the package
// from the rest of the function.
//
// If volName is non-empty, a volume of that name is created with the same
// base label and mounted, so the volume pass of Cleanup encounters it while
// it is in use.
func cleanupFixture(t *testing.T, ctx context.Context, dcli *client.Client, name, session, role, volName string) string {
	t.Helper()

	hostConfig := &container.HostConfig{AutoRemove: false}
	if volName != "" {
		_, err := dcli.VolumeCreate(ctx, dvolume.CreateOptions{
			Name:   volName,
			Labels: map[string]string{ai_container.LabelBase: "true"},
		})
		require.NoError(t, err, "creating fixture volume %q", volName)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = dcli.VolumeRemove(cleanupCtx, volName, true)
		})
		hostConfig.Binds = []string{volName + ":/data"}
	}

	resp, err := dcli.ContainerCreate(ctx,
		&container.Config{
			Image:      "alpine:latest",
			Entrypoint: []string{"sleep", "infinity"},
			Labels: map[string]string{
				ai_container.LabelBase:      "true",
				ai_container.LabelAgent:     "test-cleanup",
				ai_container.LabelProfile:   "default",
				ai_container.LabelWorkspace: "wshash",
				ai_container.LabelSession:   session,
				ai_container.LabelRole:      role,
			},
		},
		hostConfig,
		nil, nil, name,
	)
	require.NoError(t, err, "creating fixture %q", name)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = dcli.ContainerRemove(cleanupCtx, resp.ID, container.RemoveOptions{Force: true})
	})

	require.NoError(t, dcli.ContainerStart(ctx, resp.ID, container.StartOptions{}), "starting fixture %q", name)
	return resp.ID
}

// TestCleanup_KeepsRunningRemovesExited is the regression test for cleanup's
// unscoped sweep. It force-removed every ai-shim container on the host
// regardless of state or workspace, so running an unrelated `manage cleanup`
// destroyed live sessions — including other users' sessions on a shared
// daemon — while its help text advertised removing orphans.
func TestCleanup_KeepsRunningRemovesExited(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed cleanup test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	dcli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()
	liveSession := fmt.Sprintf("ai-shim-cleanup-live-%d", stamp)
	deadSession := fmt.Sprintf("ai-shim-cleanup-dead-%d", stamp)
	liveVol := liveSession + "-data"

	liveID := cleanupFixture(t, ctx, dcli, liveSession, liveSession, "agent", liveVol)
	deadID := cleanupFixture(t, ctx, dcli, deadSession, deadSession, "agent", "")

	// Drive the second fixture to "exited". `sleep infinity` as PID 1
	// ignores SIGTERM, so kill rather than stop.
	require.NoError(t, dcli.ContainerKill(ctx, deadID, "SIGKILL"))
	require.Eventually(t, func() bool {
		insp, err := dcli.ContainerInspect(ctx, deadID)
		return err == nil && insp.State != nil && !insp.State.Running
	}, 30*time.Second, 200*time.Millisecond, "the exited fixture must reach a non-running state")

	result, err := cli.Cleanup(false)
	require.NoError(t, err)

	insp, err := dcli.ContainerInspect(ctx, liveID)
	require.NoError(t, err, "the running container must survive cleanup")
	require.NotNil(t, insp.State)
	assert.True(t, insp.State.Running,
		"cleanup must not stop a running session; it removed live sessions in every workspace before this fix")

	_, err = dcli.ContainerInspect(ctx, deadID)
	assert.True(t, cerrdefs.IsNotFound(err),
		"cleanup must still remove an exited container, got err=%v", err)

	// The live session's volume is still mounted, so the daemon refuses to
	// remove it. That is the intended outcome, not a failure to report.
	_, err = dcli.VolumeInspect(ctx, liveVol)
	assert.NoError(t, err, "a live session's volume must survive cleanup")
	for _, f := range result.Failed {
		assert.NotContains(t, f, liveVol,
			"an in-use volume must be skipped silently, not reported as a removal failure")
	}
}

// TestCleanup_ForceRemovesRunning covers the documented rollout path. A
// sidecar leaked by a session launched before the ai-shim.session label
// existed carries RestartPolicyUnlessStopped, so it is still running and is
// not an orphan by state. --force is the only way to reclaim it.
func TestCleanup_ForceRemovesRunning(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed cleanup test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	dcli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	session := fmt.Sprintf("ai-shim-cleanup-force-%d", time.Now().UnixNano())
	id := cleanupFixture(t, ctx, dcli, session, session, "dind", "")

	_, err = cli.Cleanup(true)
	require.NoError(t, err)

	_, err = dcli.ContainerInspect(ctx, id)
	assert.True(t, cerrdefs.IsNotFound(err),
		"--force must remove a running container, got err=%v", err)
}
```

- [ ] **Step 2: Run the tests**

Run: `go test ./test/e2e/ -run 'TestCleanup_KeepsRunningRemovesExited|TestCleanup_ForceRemovesRunning' -v -count=1 -timeout 600s`

Expected: both PASS against the implementation from Tasks 3 and 4.

- [ ] **Step 3: Prove the first test would have caught the original defect**

Temporarily invert the guard in `internal/cli/manage.go` — change `if !force && !isOrphanedContainer(c)` to `if false && !isOrphanedContainer(c)` — and re-run:

Run: `go test ./test/e2e/ -run TestCleanup_KeepsRunningRemovesExited -v -count=1 -timeout 600s`

Expected: FAIL on `the running container must survive cleanup`, because the live fixture is force-removed and `ContainerInspect` returns not-found.

Restore the guard to `if !force && !isOrphanedContainer(c)` and re-run to confirm PASS. Do not commit the inverted guard.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/cleanup_test.go
git commit -F - <<'MSG'
test(e2e): pin cleanup's container-state scoping against a real daemon

Two daemon-backed tests for manage cleanup: a running ai-shim container
must survive a default cleanup while an exited one is removed, and
--force must remove a running one.

These live in test/e2e rather than beside the existing TestCleanup_*
tests because CI's Docker-enabled jobs run ./test/..., ./internal/container/,
./internal/dind/, ./internal/docker/ and ./internal/network/ but never
./internal/cli/ — a daemon-dependent test there is skipped in the unit
job for lacking Docker and never scheduled in the Docker job, so it would
guard nothing.

Cleanup operates daemon-wide, so both tests create their own labelled
fixtures and remove them in t.Cleanup rather than relying on the suite's
shared state.

Verified the first test fails against the pre-fix behavior by inverting
the guard in Cleanup: the live fixture is force-removed and the survival
assertion fails.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
MSG
```

---

## Task 6: Documentation

**Files:**
- Modify: `CHANGELOG.md:26-30`
- Modify: `README.md:369`
- Modify: `docs/plans/2026-07-31-cleanup-scope-and-sidecar-state-design.md`

**Interfaces:**
- Consumes: the behavior established in Tasks 1, 3 and 4.
- Produces: nothing consumed by later tasks.

- [ ] **Step 1: Fix the rollout note in the CHANGELOG**

The existing `[Unreleased]` breaking-change entry ends with "(`ai-shim manage cleanup` removes them)". That is no longer true by default: a leaked DIND carries `RestartPolicyUnlessStopped`, so it is still running and is not an orphan by state. In `CHANGELOG.md`, replace the trailing parenthetical of that entry with a reference to the flag:

```markdown
* **dind:** teardown now scopes sidecar removal to a unique per-session label; adopting this requires draining all active sessions first, since sessions launched by an earlier binary carry no `ai-shim.session` label and their DIND sidecar, netns holder and volumes will not be removed by the new teardown. Leaked sidecars carry a restart policy and are therefore still running, so reclaiming them needs `ai-shim manage cleanup --force` — plain `ai-shim manage cleanup` removes only containers that are not running.
```

- [ ] **Step 2: Add the cleanup behavior change to the CHANGELOG**

Directly under the existing `### ⚠ BREAKING CHANGES` list in `[Unreleased]`, append a third bullet:

```markdown
* **cleanup:** `ai-shim manage cleanup` no longer removes running containers. It previously force-removed every `ai-shim`-labelled container on the host regardless of state or workspace, including live sessions belonging to other users of the same Docker daemon. It now removes only exited, created and dead containers, matching the behavior its help text and the README have always described. Use `ai-shim manage cleanup --force` to restore the old sweep.
```

- [ ] **Step 3: Document the flag in the README**

`README.md:369` currently reads:

```
ai-shim manage cleanup          # remove orphaned ai-shim containers
```

Replace with:

```
ai-shim manage cleanup          # remove orphaned (non-running) ai-shim containers
ai-shim manage cleanup --force  # also remove RUNNING containers, all workspaces
```

- [ ] **Step 4: Move `--force` out of future work in the design doc**

In `docs/plans/2026-07-31-cleanup-scope-and-sidecar-state-design.md`:

1. In the **Non-goals** section, delete the bullet beginning "A `--force` flag restoring today's sweep."
2. In the **Future work** section, delete the `**cleanup --force**` bullet.
3. Append a new section immediately before **Risk**:

```markdown
## `--force`

`cleanup --force` (`-f`) restores the unscoped sweep behind an explicit flag
and a stderr warning naming the blast radius.

It ships in this change rather than being deferred, because the shipped
`[Unreleased]` rollout note directs operators at `manage cleanup` to reclaim
sidecars leaked by pre-label sessions. Those sidecars carry
`RestartPolicyUnlessStopped`, so they are still running and are not orphans
by state — without the flag, the documented rollout path stops working the
moment this change lands.
```

4. In the **Risk** section, replace "or the deferred `--force` flag" with "or `manage cleanup --force`".
5. In the **Testing** table, add a row for the flag:

```markdown
| `--force` removes a running container | `test/e2e` | Docker job |
```

6. Change the `**Status:**` line at the top of the file from `designed` to `implemented`.

- [ ] **Step 5: Verify no stale claim survives**

Run: `grep -rn 'manage cleanup' README.md CHANGELOG.md docs/plans/2026-07-31-*.md docs/plans/2026-07-30-*.md cmd/ai-shim/main.go`

Expected: every hit either says orphans/non-running, or names `--force`. The `2026-07-30` design doc's line about `manage cleanup` ("it would have taken the agent containers too") is a statement about past behavior in a ruled-out-causes list and is correct as written — leave it.

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md docs/plans/2026-07-31-cleanup-scope-and-sidecar-state-design.md
git commit -F - <<'MSG'
docs: record cleanup's new scoping and the --force escape hatch

The [Unreleased] rollout note told operators that `ai-shim manage cleanup`
reclaims sidecars leaked by sessions launched before the ai-shim.session
label existed. Those sidecars carry RestartPolicyUnlessStopped, so they
are still running and are no longer orphans by state — the note now
directs at `manage cleanup --force` and states what plain cleanup covers.

Add the cleanup scoping itself as a breaking change: it removed every
ai-shim container on the host regardless of state or workspace, and now
removes only exited, created and dead ones.

Document the flag in the README, and fold --force into the design doc's
scope with the reason it shipped now instead of being deferred.

Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>
MSG
```

---

## Task 7: Full verification

**Files:** none modified.

- [ ] **Step 1: Run the full local CI gate**

Run: `make ci`

Expected: PASS through every target — `fmt-check`, `vet`, `tidy-check`, `check-silent-failures`, `test-race`, `fuzz`, `vuln`. `make` aborts on the first failure, so reaching `vuln: OK` means all prior targets passed. Do not claim CI-green from a subset.

- [ ] **Step 2: Run the Docker-backed suites CI actually schedules**

Run: `go test ./test/... ./internal/container/ ./internal/dind/ ./internal/network/ ./internal/docker/ -count=1 -timeout 2400s`

Expected: PASS. This is the package set from `ci.yml:363`.

- [ ] **Step 3: Report results**

Report the actual output of both commands. If anything fails, report the failure verbatim and stop — do not mark the branch complete.
