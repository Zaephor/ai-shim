# Session-scoped sidecar teardown — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every session a unique container label so tearing down one session cannot destroy a sibling session's DIND sidecar and netns holder.

**Architecture:** A new `ai-shim.session` label, set once in `BuildSpec` with the agent container name as its value, is inherited by every sidecar through `spec.Labels`. The two teardown filter builders move to `internal/container` and match on that label instead of agent+profile+workspace, and `stopDINDForSession` moves out of `cmd/ai-shim` into `internal/dind` as an exported, error-returning function — which is what makes the teardown testable against real containers in a package CI already runs with a daemon. A reattach guard reports a destroyed netns owner rather than silently attaching to a network-less container.

**Tech Stack:** Go 1.26.5, Docker Engine API via `github.com/docker/docker/client`, testify (`require`/`assert`), Docker-backed E2E tests under `test/e2e`.

**Design doc:** `docs/plans/2026-07-30-session-scoped-sidecar-teardown-design.md`

## Global Constraints

- Conventional Commits for every commit: `<type>(<scope>)?: <description>`, imperative, lowercase after the type, no trailing period, subject ≤72 chars.
- Do not push. Commits stay local; the human handles the remote.
- Label constant value is exactly `ai-shim.session`. Its value is the agent container name — never the random suffix alone.
- No compatibility fallback for containers lacking the session label. A zero-match teardown lookup is a silent no-op plus a debug log line.
- Do not change sibling *discovery*: `FindRunningSessionsInWorkspace`, `FindRunningSession`, `FindAllRunningSessions` and `staleContainerFilters` keep their agent+profile+workspace scoping.
- `make ci` must pass before Task 7 is considered done. E2E requires a working Docker daemon.
- Work happens on branch `feat/session-scoped-sidecar-teardown`, which already exists and holds the design doc.

---

### Task 1: Session label constant, set in BuildSpec

**Files:**
- Modify: `internal/container/labels.go`
- Modify: `internal/container/builder.go:495-500`
- Test: `internal/container/builder_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `container.LabelSession` (string const, value `"ai-shim.session"`). Every later task depends on it. `BuildSpec` guarantees `spec.Labels[LabelSession] == spec.Name`.

- [ ] **Step 1: Write the failing test**

Append to `internal/container/builder_test.go`:

```go
func TestBuildSpec_SessionLabelMatchesContainerName(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Equal(t, spec.Name, spec.Labels[LabelSession],
		"session label must carry the agent container name so sidecars can be tied to exactly one session")
}

// Two specs built from identical params must still get distinct session
// labels: the container name carries a random suffix, and that uniqueness is
// what keeps one session's teardown off its siblings' sidecars.
func TestBuildSpec_SessionLabelIsUniquePerSpec(t *testing.T) {
	first, err := BuildSpec(defaultBuildParams())
	require.NoError(t, err)
	second, err := BuildSpec(defaultBuildParams())
	require.NoError(t, err)

	assert.NotEqual(t, first.Labels[LabelSession], second.Labels[LabelSession],
		"parallel sessions for the same agent+profile+workspace must not share a session label")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/container/ -run 'TestBuildSpec_SessionLabel' -v`
Expected: FAIL — `undefined: LabelSession`.

- [ ] **Step 3: Add the constant**

In `internal/container/labels.go`, add to the const block:

```go
	// LabelSession uniquely identifies one session. Its value is the agent
	// container name, which Docker guarantees is unique daemon-wide. Every
	// container, volume and network created for a session inherits it via
	// spec.Labels, and sidecar teardown matches on it so ending one session
	// cannot touch a parallel sibling's sidecars.
	//
	// On shared network scopes (global/profile/workspace) EnsureNetwork
	// applies labels only when it creates the network, so the label there
	// names the session that created it. Harmless: network cleanup is
	// attachment-count based, never label-identity based.
	LabelSession = "ai-shim.session"
```

- [ ] **Step 4: Set it in BuildSpec**

In `internal/container/builder.go`, the block currently reads:

```go
	labels[LabelWorkspace] = wsHash
	labels[LabelWorkspaceDir] = pwd
```

Change to:

```go
	labels[LabelWorkspace] = wsHash
	labels[LabelWorkspaceDir] = pwd
	labels[LabelSession] = name
```

`name` is assigned four lines above by `generateContainerName`. No other edit is needed: the DIND sidecar, its socket and certs volumes, the netns holder and the session network all copy `spec.Labels`.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/container/ -run 'TestBuildSpec' -v`
Expected: PASS, including the pre-existing `TestBuildSpec_Labels`.

- [ ] **Step 6: Commit**

```bash
git add internal/container/labels.go internal/container/builder.go internal/container/builder_test.go
git commit -m "feat(container): add per-session label set from container name"
```

---

### Task 2: Prove the label survives sidecar label construction

**Files:**
- Modify: `internal/dind/dind.go:196-206`
- Test: `internal/dind/dind_test.go`
- Test: `cmd/ai-shim/lifecycle_test.go`

**Interfaces:**
- Consumes: `container.LabelSession` from Task 1.
- Produces: `dind.sidecarLabels(cfg Config) map[string]string` — unexported, pure, returns the DIND container's label map. Used only inside `dind.Start`.

The DIND label map is built inline inside `Start`, which needs a live daemon and so cannot be unit-tested. Extract it to a pure function so the inheritance guarantee has a real test. `dindHolderLabels` in `cmd/ai-shim/main.go:148` is already pure and only needs a test.

- [ ] **Step 1: Write the failing test**

Append to `internal/dind/dind_test.go`:

```go
func TestSidecarLabels_PreservesSessionLabel(t *testing.T) {
	cfg := Config{
		Version:   "1.2.3",
		CacheAddr: "127.0.0.1:5000",
		Labels: map[string]string{
			ai_container.LabelBase:    "true",
			ai_container.LabelSession: "claude-code-work-abc123-deadbeef",
			ai_container.LabelRole:    "agent",
		},
	}

	got := sidecarLabels(cfg)

	assert.Equal(t, "claude-code-work-abc123-deadbeef", got[ai_container.LabelSession],
		"the DIND sidecar must inherit its session's label or teardown cannot find it")
	assert.Equal(t, "dind", got[ai_container.LabelRole], "role must be overridden from agent to dind")
	assert.Equal(t, "true", got[ai_container.LabelDIND])
	assert.Equal(t, "1.2.3", got[ai_container.LabelVersion])
	assert.Equal(t, "true", got[ai_container.LabelUsesCache])

	assert.Equal(t, "agent", cfg.Labels[ai_container.LabelRole],
		"sidecarLabels must not mutate the caller's map")
}
```

Check the import alias at the top of `dind_test.go`. Production code imports the package as `ai_container "github.com/Zaephor/ai-shim/internal/container"`; match whatever the test file already uses, adding the import if absent.

Append to `cmd/ai-shim/lifecycle_test.go`:

```go
func TestDINDHolderLabels_PreservesSessionLabel(t *testing.T) {
	base := map[string]string{
		container.LabelBase:    "true",
		container.LabelSession: "claude-code-work-abc123-deadbeef",
		container.LabelRole:    "agent",
	}

	got := dindHolderLabels(base)

	assert.Equal(t, "claude-code-work-abc123-deadbeef", got[container.LabelSession],
		"the netns holder must inherit its session's label or teardown cannot find it")
	assert.Equal(t, "netns-holder", got[container.LabelRole])
	assert.Equal(t, "agent", base[container.LabelRole], "dindHolderLabels must not mutate the caller's map")
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/dind/ -run TestSidecarLabels -v`
Expected: FAIL — `undefined: sidecarLabels`.

Run: `go test ./cmd/ai-shim/ -run TestDINDHolderLabels -v`
Expected: PASS already — `dindHolderLabels` copies every key, so the label is inherited today. That is the point of the test: it pins the behaviour so a later refactor to an explicit allowlist cannot silently drop the session label.

- [ ] **Step 3: Extract the pure function**

In `internal/dind/dind.go`, this block inside `Start`:

```go
	// Copy labels to avoid mutating the caller's map.
	// Override role from the parent's "agent" to "dind" so the session
	// picker and cleanup queries can distinguish agent containers from
	// their sidecars via a positive label filter.
	labels := make(map[string]string, len(cfg.Labels)+3)
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	labels[ai_container.LabelRole] = "dind"
	labels[ai_container.LabelDIND] = "true"
	labels[ai_container.LabelVersion] = cfg.Version
	if cfg.CacheAddr != "" {
		labels[ai_container.LabelUsesCache] = "true"
	}
```

becomes a single call:

```go
	labels := sidecarLabels(cfg)
```

and the extracted function goes above `Start`:

```go
// sidecarLabels builds the DIND container's label map from the session's
// labels. It copies to avoid mutating the caller's map, and overrides role
// from the parent's "agent" to "dind" so the session picker and cleanup
// queries can distinguish agent containers from their sidecars via a
// positive label filter. Every other label — including LabelSession, which
// teardown matches on — is inherited unchanged.
func sidecarLabels(cfg Config) map[string]string {
	labels := make(map[string]string, len(cfg.Labels)+4)
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	labels[ai_container.LabelRole] = "dind"
	labels[ai_container.LabelDIND] = "true"
	labels[ai_container.LabelVersion] = cfg.Version
	if cfg.CacheAddr != "" {
		labels[ai_container.LabelUsesCache] = "true"
	}
	return labels
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dind/ ./cmd/ai-shim/ -short -v -run 'TestSidecarLabels|TestDINDHolderLabels'`
Expected: PASS both.

- [ ] **Step 5: Commit**

```bash
git add internal/dind/dind.go internal/dind/dind_test.go cmd/ai-shim/lifecycle_test.go
git commit -m "refactor(dind): extract sidecarLabels and pin session-label inheritance"
```

---

### Task 3: Move teardown to internal/dind and scope it by session

This is the fix. It is the largest task in the plan: the filter builders move to
`internal/container`, `stopDINDForSession` moves to `internal/dind` as an
exported, error-returning function, and the first behavioral test of the
teardown path replaces three source-text and hand-copied ones.

**Files:**
- Create: `internal/container/sidecar_filters.go`
- Create: `internal/container/sidecar_filters_test.go`
- Create: `internal/dind/teardown.go`
- Create: `internal/dind/teardown_test.go`
- Modify: `cmd/ai-shim/main.go` — delete `dindSessionFilters`, `holderSessionFilters` and `stopDINDForSession` (lines 1707-1790); repoint the two call sites at `main.go:1653` and `main.go:1703`
- Modify: `cmd/ai-shim/lifecycle_test.go` — delete the three tests that covered the moved code

**Interfaces:**
- Consumes: `LabelSession` (Task 1); `RunningSession.ContainerName`, already populated by every lookup in `internal/container/lookup.go`.
- Produces:
  - `container.DINDSessionFilters(session *RunningSession) filters.Args`
  - `container.HolderSessionFilters(session *RunningSession) filters.Args`
  - `dind.StopForSession(ctx context.Context, cli *client.Client, session *ai_container.RunningSession) error` — returns `errors.Join` of every failure encountered; nil when the teardown was clean or matched nothing.
- Task 4 extends `internal/dind/teardown_test.go`. Task 6 uses both filter functions.

Import direction: `internal/dind` already imports `internal/container` (as
`ai_container`); `internal/network` imports nothing from ai-shim. So
`dind` → `container` and `dind` → `network` are both cycle-free. The filters
live in `container` because they are label queries over `RunningSession`; the
teardown lives in `dind` because it is DIND and holder lifecycle, next to
`Start` and `Sidecar.Stop`.

- [ ] **Step 1: Write the failing filter test**

Create `internal/container/sidecar_filters_test.go`:

```go
package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func filterTestSession() *RunningSession {
	return &RunningSession{
		ContainerName: "claude-code-work-abc123-aaaaaaaa",
		AgentName:     "claude-code",
		Profile:       "work",
		WorkspaceHash: "abc123",
	}
}

func TestDINDSessionFilters_ScopedToSession(t *testing.T) {
	f := DINDSessionFilters(filterTestSession())

	assert.Contains(t, f.Get("label"), LabelBase+"=true")
	assert.Contains(t, f.Get("label"), LabelDIND+"=true")
	assert.Contains(t, f.Get("label"), LabelSession+"=claude-code-work-abc123-aaaaaaaa",
		"DIND teardown must match the one session that owns the sidecar")
	assert.Contains(t, f.Get("status"), "running")
}

func TestHolderSessionFilters_ScopedToSession(t *testing.T) {
	f := HolderSessionFilters(filterTestSession())

	assert.Contains(t, f.Get("label"), LabelBase+"=true")
	assert.Contains(t, f.Get("label"), LabelRole+"=netns-holder")
	assert.Contains(t, f.Get("label"), LabelSession+"=claude-code-work-abc123-aaaaaaaa",
		"holder teardown must match the one session that owns the holder")
	assert.Contains(t, f.Get("status"), "running")
}

// Parallel sessions share agent, profile and workspace labels. If either
// filter still carries those, teardown for one session matches every
// sibling's sidecars — the defect this change exists to fix.
func TestSidecarFilters_DoNotUseWorkspaceScoping(t *testing.T) {
	for name, labels := range map[string][]string{
		"dind":   DINDSessionFilters(filterTestSession()).Get("label"),
		"holder": HolderSessionFilters(filterTestSession()).Get("label"),
	} {
		for _, shared := range []string{
			LabelAgent + "=claude-code",
			LabelProfile + "=work",
			LabelWorkspace + "=abc123",
		} {
			assert.NotContains(t, labels, shared,
				"%s filter must not match on %s: parallel siblings share it, so teardown would sweep them too", name, shared)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/container/ -run 'SessionFilters|SidecarFilters' -v`
Expected: FAIL — `undefined: DINDSessionFilters`, `undefined: HolderSessionFilters`.

- [ ] **Step 3: Create the filter builders**

Create `internal/container/sidecar_filters.go`:

```go
package container

import "github.com/docker/docker/api/types/filters"

// DINDSessionFilters locates the DIND sidecar belonging to one session.
//
// The scope is the session label alone. Agent, profile and workspace are
// deliberately absent: parallel sessions share all three, so a filter built
// from them matches every sibling's sidecar, and teardown for one session
// then destroys the DIND of sessions that are still running. Their agent
// containers survive — those are removed by ID — leaving them attached to a
// destroyed network namespace with no way to recover it.
func DINDSessionFilters(session *RunningSession) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelDIND+"=true"),
		filters.Arg("label", LabelSession+"="+session.ContainerName),
		filters.Arg("status", "running"),
	)
}

// HolderSessionFilters locates the netns holder belonging to one session
// (holder mode). Scoped by session label for the same reason as
// DINDSessionFilters — and more urgently: destroying a sibling's holder is
// unrecoverable, because a container:<id> dependent keeps only lo and never
// regains eth0, even if the owner is restarted with the same ID.
func HolderSessionFilters(session *RunningSession) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelRole+"=netns-holder"),
		filters.Arg("label", LabelSession+"="+session.ContainerName),
		filters.Arg("status", "running"),
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/container/ -run 'SessionFilters|SidecarFilters' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing behavioral teardown test**

This is the test that exercises what actually broke. It needs a Docker daemon.

Create `internal/dind/teardown_test.go`:

```go
package dind

import (
	"context"
	"fmt"
	"testing"
	"time"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// teardownFixture creates a fake sidecar for one session: a long-sleeping
// alpine container carrying the labels production stamps on a DIND sidecar
// or a netns holder. Registers its own cleanup.
//
// `sleep infinity` as PID 1 matters: PID 1 installs no default SIGTERM
// handler, so these containers ignore SIGTERM exactly as the real holder
// does. A teardown that stops them gracefully waits out its full timeout.
func teardownFixture(t *testing.T, ctx context.Context, cli *client.Client, name, sessionName, role string) string {
	t.Helper()

	labels := map[string]string{
		ai_container.LabelBase:      "true",
		ai_container.LabelAgent:     "test-teardown",
		ai_container.LabelProfile:   "default",
		ai_container.LabelWorkspace: "wshash",
		ai_container.LabelSession:   sessionName,
		ai_container.LabelRole:      role,
	}
	if role == "dind" {
		labels[ai_container.LabelDIND] = "true"
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:      "alpine:latest",
			Entrypoint: []string{"sleep", "infinity"},
			Labels:     labels,
		},
		&container.HostConfig{AutoRemove: false},
		nil, nil, name,
	)
	require.NoError(t, err, "creating fixture %q", name)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanupCtx, resp.ID, container.RemoveOptions{Force: true})
	})

	require.NoError(t, cli.ContainerStart(ctx, resp.ID, container.StartOptions{}), "starting fixture %q", name)
	return resp.ID
}

// running reports whether a container exists and is running. A removed
// container reports false, not an error.
func running(t *testing.T, ctx context.Context, cli *client.Client, id string) bool {
	t.Helper()
	insp, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		return false
	}
	return insp.State != nil && insp.State.Running
}

// TestStopForSession_LeavesSiblingSidecarsAlone is the regression test for
// the defect this change fixes. Two sessions run in the same workspace with
// identical agent, profile and workspace labels — the parallel-session case.
// Tearing one down must not touch the other's DIND sidecar or netns holder.
func TestStopForSession_LeavesSiblingSidecarsAlone(t *testing.T) {
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
	sessionA := fmt.Sprintf("ai-shim-teardown-a-%d", stamp)
	sessionB := fmt.Sprintf("ai-shim-teardown-b-%d", stamp)

	dindA := teardownFixture(t, ctx, cli, sessionA+"-dind", sessionA, "dind")
	holderA := teardownFixture(t, ctx, cli, sessionA+"-netns", sessionA, "netns-holder")
	dindB := teardownFixture(t, ctx, cli, sessionB+"-dind", sessionB, "dind")
	holderB := teardownFixture(t, ctx, cli, sessionB+"-netns", sessionB, "netns-holder")

	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: sessionA,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	require.NoError(t, err)

	assert.False(t, running(t, ctx, cli, dindA), "session A's DIND must be gone")
	assert.False(t, running(t, ctx, cli, holderA), "session A's netns holder must be gone")
	assert.True(t, running(t, ctx, cli, dindB),
		"sibling session B's DIND must survive: destroying it strands a running agent")
	assert.True(t, running(t, ctx, cli, holderB),
		"sibling session B's netns holder must survive: destroying it is unrecoverable")
}

// TestStopForSession_NoMatchIsNotAnError covers the upgrade case and the
// double-teardown case: a session with no sidecars left tears down cleanly.
func TestStopForSession_NoMatchIsNotAnError(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	err = StopForSession(ctx, runner.Client(), &ai_container.RunningSession{
		ContainerName: fmt.Sprintf("ai-shim-teardown-absent-%d", time.Now().UnixNano()),
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	assert.NoError(t, err, "a session with no sidecars must tear down cleanly")
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/dind/ -run TestStopForSession -v -count=1`
Expected: FAIL — `undefined: StopForSession`.

- [ ] **Step 7: Create the teardown function**

Create `internal/dind/teardown.go`. This is a behavior-preserving move of
`stopDINDForSession` from `cmd/ai-shim/main.go:1737-1790`, with warnings
converted to joined errors so the package stays free of CLI formatting —
the same shape `network.RemoveOrphanedForSession` already uses.

```go
package dind

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/logging"
	"github.com/Zaephor/ai-shim/internal/network"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// StopForSession tears down the sidecars belonging to exactly one session:
// its DIND container and that container's socket and certs volumes, its
// netns holder, and finally the session network if nothing remains attached.
//
// Scoping is by session label. Parallel sessions in one workspace share
// agent, profile and workspace labels, so a coarser lookup destroys sidecars
// out from under sessions that are still running — and a destroyed netns
// holder cannot be recovered, because a container:<id> dependent never
// regains eth0.
//
// Best-effort: every failure is collected and the teardown continues, so one
// wedged container cannot strand the rest. The joined error is for reporting,
// not control flow.
func StopForSession(ctx context.Context, cli *client.Client, session *ai_container.RunningSession) error {
	var errs []error

	list, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: ai_container.DINDSessionFilters(session),
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("listing DIND containers for session %s: %w", session.ContainerName, err))
	}
	if len(list) == 0 {
		logging.Debug("no DIND sidecar found for session %s", session.ContainerName)
	}

	for _, c := range list {
		// Derive volume names from the container name before removing the
		// container. Names in the Docker API carry a leading "/".
		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}

		stopTimeout := 5
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			errs = append(errs, fmt.Errorf("stopping DIND container %s: %w", c.ID, err))
		}
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("removing DIND container %s: %w", c.ID, err))
		}

		if containerName != "" {
			for _, vol := range []string{containerName + "-socket", containerName + "-certs"} {
				if err := cli.VolumeRemove(ctx, vol, true); err != nil && !cerrdefs.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("removing volume %s: %w", vol, err))
				}
			}
		}
	}

	// Remove the netns holder (holder mode only). This must happen before
	// RemoveOrphanedForSession below: the holder stays attached to the
	// session's bridge network for as long as it runs, so leaving it would
	// make the network look non-orphaned and leak it too.
	holderList, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: ai_container.HolderSessionFilters(session),
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("listing netns holder for session %s: %w", session.ContainerName, err))
	}
	for _, c := range holderList {
		stopTimeout := 5
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			errs = append(errs, fmt.Errorf("stopping netns holder %s: %w", c.ID, err))
		}
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("removing netns holder %s: %w", c.ID, err))
		}
	}

	if err := network.RemoveOrphanedForSession(ctx, cli, session.AgentName, session.Profile, session.WorkspaceHash); err != nil {
		errs = append(errs, fmt.Errorf("removing session network: %w", err))
	}

	return errors.Join(errs...)
}
```

Note: the holder loop still calls `ContainerStop` here. That is deliberate —
this step is a behavior-preserving move, and Task 4 removes the stall with
its own test. Do not fold Task 4's change into this step.

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/dind/ -run TestStopForSession -v -count=1`
Expected: PASS, both tests.

- [ ] **Step 9: Delete the moved code from main and repoint the call sites**

In `cmd/ai-shim/main.go`, delete `dindSessionFilters`, `holderSessionFilters`
and `stopDINDForSession` entirely — the whole block from the
`// dindSessionFilters builds the filter...` comment through the closing
brace of `stopDINDForSession`.

At `main.go:1653` (inside `handleReattach`), change:

```go
	// Clean up DIND sidecar if present.
	if cfg.IsDINDEnabled() {
		stopDINDForSession(ctx, runner.Client(), session)
	}
```

to:

```go
	// Clean up DIND sidecar if present.
	if cfg.IsDINDEnabled() {
		if err := dind.StopForSession(ctx, runner.Client(), session); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: %v\n", err)
		}
	}
```

At `main.go:1703` (inside `stopSession`), change:

```go
	stopDINDForSession(ctx, cli, session)
	dind.MaybeStopCache(ctx, cli)
```

to:

```go
	if err := dind.StopForSession(ctx, cli, session); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: %v\n", err)
	}
	dind.MaybeStopCache(ctx, cli)
```

`dind` is already imported in `main.go`. Check whether `strings`,
`filters` or `network` are still used elsewhere in the file after the
deletion; remove any import that is now unused — `go build ./...` names them.

- [ ] **Step 10: Delete the tests that covered the moved code**

In `cmd/ai-shim/lifecycle_test.go`, delete all three of:

- `TestDINDSessionFilters_IncludesWorkspace` — asserts the workspace scoping this change removes; replaced by `internal/container/sidecar_filters_test.go`.
- `TestHolderSessionFilters_IncludesWorkspace` — same.
- `TestStopDINDForSession_RemovesNetnsHolder` — a source-text assertion on a function that no longer lives in this package; replaced by the behavioral tests in `internal/dind/teardown_test.go`, which assert the holder-before-network ordering by outcome instead of by string matching.

Leave `readMainSource` and `extractFuncBody` in place — other tests still use them.

- [ ] **Step 11: Run the full unit suite and build**

Run: `go build ./... && go test -short ./... -count=1`
Expected: PASS. Any compile error is a call site or import the deletion missed; the compiler names it.

- [ ] **Step 12: Commit**

```bash
git add internal/container/sidecar_filters.go internal/container/sidecar_filters_test.go \
        internal/dind/teardown.go internal/dind/teardown_test.go \
        cmd/ai-shim/main.go cmd/ai-shim/lifecycle_test.go
git commit -m "fix(cleanup): scope sidecar teardown to the owning session"
```

---

### Task 4: Drop the five-second stall on holder removal

**Files:**
- Modify: `internal/dind/teardown.go` (the holder loop)
- Test: `internal/dind/teardown_test.go`

**Interfaces:**
- Consumes: `dind.StopForSession` and the `teardownFixture` / `running` helpers from Task 3.
- Produces: nothing new.

The holder runs `sleep infinity` as PID 1. PID 1 installs no default SIGTERM
handler and `sleep` installs none of its own, so `ContainerStop{Timeout:5}`
always burns the full five seconds before the daemon force-kills it — that is
the `failed to exit within 5s of signal 15` line in the incident daemon log,
once per holder matched. `Holder.Stop` (`internal/dind/holder.go:77`) already
removes directly; make the teardown path match.

- [ ] **Step 1: Write the failing test**

Append to `internal/dind/teardown_test.go`:

```go
// TestStopForSession_DoesNotWaitOutHolderSIGTERM pins the holder's removal
// to a force-remove. The holder is `sleep infinity` as PID 1, which ignores
// SIGTERM, so a graceful stop can only wait out its full timeout before the
// daemon force-kills it anyway. The fixture reproduces that exactly, so a
// reintroduced ContainerStop shows up as elapsed time.
func TestStopForSession_DoesNotWaitOutHolderSIGTERM(t *testing.T) {
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

	session := fmt.Sprintf("ai-shim-teardown-stall-%d", time.Now().UnixNano())
	holder := teardownFixture(t, ctx, cli, session+"-netns", session, "netns-holder")

	start := time.Now()
	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: session,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, running(t, ctx, cli, holder), "the netns holder must be gone")
	assert.Less(t, elapsed, 4*time.Second,
		"holder removal waited on SIGTERM (%s); sleep as PID 1 ignores it, so the wait can only ever time out", elapsed)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dind/ -run TestStopForSession_DoesNotWaitOutHolderSIGTERM -v -count=1`
Expected: FAIL — elapsed is just over 5s, from the `ContainerStop` timeout.

- [ ] **Step 3: Remove the graceful stop**

In `internal/dind/teardown.go`, this loop:

```go
	for _, c := range holderList {
		stopTimeout := 5
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			errs = append(errs, fmt.Errorf("stopping netns holder %s: %w", c.ID, err))
		}
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("removing netns holder %s: %w", c.ID, err))
		}
	}
```

becomes:

```go
	for _, c := range holderList {
		// Force-remove rather than stop-then-remove. The holder is `sleep
		// infinity` as PID 1, which installs no SIGTERM handler, so a
		// graceful stop can only wait out its full timeout before the
		// daemon force-kills it anyway. Holder.Stop does the same.
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("removing netns holder %s: %w", c.ID, err))
		}
	}
```

Leave the DIND loop above untouched: `dockerd` does handle SIGTERM, and a
graceful shutdown there is worth the wait.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dind/ -run TestStopForSession -v -count=1`
Expected: PASS, all three tests.

- [ ] **Step 5: Commit**

```bash
git add internal/dind/teardown.go internal/dind/teardown_test.go
git commit -m "perf(dind): force-remove netns holder instead of waiting on SIGTERM"
```

---

### Task 5: Report a dead netns owner on reattach

**Files:**
- Create: `internal/container/netns.go`
- Create: `internal/container/netns_test.go`
- Modify: `cmd/ai-shim/main.go` — call the check at the top of `handleReattach:1629`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `container.ParseNetnsOwner(networkMode string) (id string, ok bool)`
  - `container.NetnsOwnerAlive(ctx context.Context, cli *client.Client, containerID string) (owner string, joined bool, alive bool)` — `joined` false means the container is not sharing another container's namespace, in which case `alive` is meaningless. Never returns an error: an inspect failure reports `joined=false`, because a diagnostic must not block a reattach.

- [ ] **Step 1: Write the failing parser test**

Create `internal/container/netns_test.go`:

```go
package container

import "testing"

func TestParseNetnsOwner(t *testing.T) {
	tests := []struct {
		name        string
		networkMode string
		wantID      string
		wantOK      bool
	}{
		{"joins a container", "container:abc123def456", "abc123def456", true},
		{"bridge", "bridge", "", false},
		{"host", "host", "", false},
		{"none", "none", "", false},
		{"named network", "ai-shim-work-abc123", "", false},
		{"empty", "", "", false},
		{"prefix with no id", "container:", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := ParseNetnsOwner(tt.networkMode)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("ParseNetnsOwner(%q) = (%q, %v), want (%q, %v)",
					tt.networkMode, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/container/ -run TestParseNetnsOwner -v`
Expected: FAIL — `undefined: ParseNetnsOwner`.

- [ ] **Step 3: Write the failing behavioral test**

Append to `internal/container/netns_test.go` (add the imports it needs:
`context`, `fmt`, `time`, `testutil`, `dockercontainer "github.com/docker/docker/api/types/container"`, testify's `assert` and `require`):

```go
// TestNetnsOwnerAlive covers the three states that matter on reattach: a
// container not sharing a namespace at all, one sharing a live owner, and
// one whose owner has been destroyed — the unrecoverable case, where the
// dependent keeps only lo and never regains eth0.
func TestNetnsOwnerAlive(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed netns test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()

	start := func(name string, hostCfg *dockercontainer.HostConfig) string {
		resp, err := cli.ContainerCreate(ctx,
			&dockercontainer.Config{
				Image:      "alpine:latest",
				Entrypoint: []string{"sleep", "infinity"},
			},
			hostCfg, nil, nil, name,
		)
		require.NoError(t, err, "creating %q", name)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = cli.ContainerRemove(cleanupCtx, resp.ID, dockercontainer.RemoveOptions{Force: true})
		})
		require.NoError(t, cli.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}), "starting %q", name)
		return resp.ID
	}

	ownerID := start(fmt.Sprintf("ai-shim-netns-owner-%d", stamp), &dockercontainer.HostConfig{})
	joinerID := start(fmt.Sprintf("ai-shim-netns-joiner-%d", stamp), &dockercontainer.HostConfig{
		NetworkMode: dockercontainer.NetworkMode("container:" + ownerID),
	})

	t.Run("not sharing a namespace", func(t *testing.T) {
		_, joined, _ := NetnsOwnerAlive(ctx, cli, ownerID)
		assert.False(t, joined, "a container on its own network must report joined=false")
	})

	t.Run("owner alive", func(t *testing.T) {
		owner, joined, alive := NetnsOwnerAlive(ctx, cli, joinerID)
		assert.True(t, joined)
		assert.True(t, alive, "a live owner must report alive")
		assert.Equal(t, ownerID, owner)
	})

	t.Run("owner destroyed", func(t *testing.T) {
		require.NoError(t, cli.ContainerRemove(ctx, ownerID, dockercontainer.RemoveOptions{Force: true}))

		_, joined, alive := NetnsOwnerAlive(ctx, cli, joinerID)
		assert.True(t, joined, "the joiner still records its owner in HostConfig after the owner is gone")
		assert.False(t, alive, "a destroyed owner must report not alive: this session cannot recover networking")
	})
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/container/ -run 'TestParseNetnsOwner|TestNetnsOwnerAlive' -v -count=1`
Expected: FAIL — `undefined: ParseNetnsOwner`, `undefined: NetnsOwnerAlive`.

- [ ] **Step 5: Implement both**

Create `internal/container/netns.go`:

```go
package container

import (
	"context"
	"strings"

	"github.com/docker/docker/client"
)

// netnsOwnerPrefix is Docker's network-mode form for joining another
// container's network namespace.
const netnsOwnerPrefix = "container:"

// ParseNetnsOwner returns the container ID whose network namespace the given
// network mode joins, and whether the mode is that kind at all. Any other
// mode — bridge, host, none, a named network — returns ok=false.
func ParseNetnsOwner(networkMode string) (string, bool) {
	if !strings.HasPrefix(networkMode, netnsOwnerPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(networkMode, netnsOwnerPrefix)
	if id == "" {
		return "", false
	}
	return id, true
}

// NetnsOwnerAlive reports whether the given container shares another
// container's network namespace and, if so, whether that owner still exists
// and is running.
//
// A dead owner is terminal for the dependent: it keeps only lo, and eth0
// never returns — not even if a container with the same ID is started again.
// The dependent has to be recreated.
//
// Two inspect calls, no exec. Deliberately returns no error: this is a
// diagnostic, and a Docker hiccup here must never block a reattach. An
// inspect failure reports joined=false, the "nothing to say" answer.
func NetnsOwnerAlive(ctx context.Context, cli *client.Client, containerID string) (owner string, joined bool, alive bool) {
	insp, err := cli.ContainerInspect(ctx, containerID)
	if err != nil || insp.HostConfig == nil {
		return "", false, false
	}
	ownerID, ok := ParseNetnsOwner(string(insp.HostConfig.NetworkMode))
	if !ok {
		return "", false, false
	}
	ownerInsp, err := cli.ContainerInspect(ctx, ownerID)
	if err != nil || ownerInsp.State == nil {
		return ownerID, true, false
	}
	return ownerID, true, ownerInsp.State.Running
}
```

Imports for this file are exactly `context`, `strings` and
`github.com/docker/docker/client`. `insp.HostConfig.NetworkMode` is converted
straight to `string`, so the Docker `container` types package is not needed
here.

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/container/ -run 'TestParseNetnsOwner|TestNetnsOwnerAlive' -v -count=1`
Expected: PASS, all subtests.

- [ ] **Step 7: Wire it into handleReattach**

In `cmd/ai-shim/main.go`, add as the first statement of `handleReattach`,
before the existing "reattaching to" line:

```go
func handleReattach(ctx context.Context, runner *container.Runner, session *container.RunningSession, cfg config.Config, logDir string) (int, error) {
	if _, joined, alive := container.NetnsOwnerAlive(ctx, runner.Client(), session.ContainerID); joined && !alive {
		fmt.Fprintf(os.Stderr,
			"ai-shim: warning: the network namespace owner for %s is gone.\n"+
				"ai-shim: this session has no network connectivity and cannot regain it.\n"+
				"ai-shim: stop the session and start a new one to restore networking.\n",
			session.ContainerName)
	}

	fmt.Fprintf(os.Stderr, "ai-shim: reattaching to %s...\n", session.ContainerName)
```

This one hook covers both entry points: the picker's reattach choice and
`manage attach <name>`. The warning does not block the reattach — a wedged
session may still hold work worth retrieving, and discarding it is the
user's call.

- [ ] **Step 8: Build and run the unit suite**

Run: `go build ./... && go test -short ./... -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/container/netns.go internal/container/netns_test.go cmd/ai-shim/main.go
git commit -m "feat(reattach): report a destroyed netns owner before attaching"
```

---

### Task 6: E2E regression — same-workspace siblings survive a teardown

**Files:**
- Modify: `test/e2e/parallel_session_test.go` — add one test; repoint `TestParallel_DINDWorkspaceFilterIsolation` at the exported filters

**Interfaces:**
- Consumes: `container.DINDSessionFilters`, `container.HolderSessionFilters` (Task 3), `container.LabelSession` (Task 1), and the file's existing helpers `newTestParams`, `parallelTestParams.sessionLabels`, `startFakeSession`, `waitForRunning`.
- Produces: nothing consumed by later tasks.

`internal/dind/teardown_test.go` now covers the teardown behaviorally. What
remains uncovered here is the filter-level invariant in the suite an operator
reads first, and the existing isolation test's hand-copied filter — a copy
that passes no matter what the production filter does.

- [ ] **Step 1: Write the test**

Append to `test/e2e/parallel_session_test.go`:

```go
// TestParallel_SidecarFiltersAreSessionScoped asserts at the filter level
// what internal/dind covers at the behavioral level: two sessions in the
// SAME workspace share every label except the session label, so a lookup
// scoped by anything coarser matches both and tears down a still-running
// session's sidecars.
func TestParallel_SidecarFiltersAreSessionScoped(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow parallel session test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	params := newTestParams(t)
	stamp := time.Now().UnixNano()
	nameA := fmt.Sprintf("ai-shim-sess-a-%d", stamp)
	nameB := fmt.Sprintf("ai-shim-sess-b-%d", stamp)

	sidecarLabels := func(sessionName, role string) map[string]string {
		l := params.sessionLabels()
		l[container.LabelSession] = sessionName
		l[container.LabelRole] = role
		if role == "dind" {
			l[container.LabelDIND] = "true"
		}
		return l
	}

	dindA := startFakeSession(t, ctx, cli, nameA+"-dind", sidecarLabels(nameA, "dind"))
	holderA := startFakeSession(t, ctx, cli, nameA+"-netns", sidecarLabels(nameA, "netns-holder"))
	dindB := startFakeSession(t, ctx, cli, nameB+"-dind", sidecarLabels(nameB, "dind"))
	holderB := startFakeSession(t, ctx, cli, nameB+"-netns", sidecarLabels(nameB, "netns-holder"))
	for _, id := range []string{dindA, holderA, dindB, holderB} {
		waitForRunning(t, ctx, cli, id)
	}

	sessA := &container.RunningSession{
		ContainerName: nameA,
		AgentName:     params.AgentName,
		Profile:       params.Profile,
		WorkspaceHash: params.WsHash,
	}

	dindHits, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		Filters: container.DINDSessionFilters(sessA),
	})
	require.NoError(t, err)
	require.Len(t, dindHits, 1, "session A's DIND filter must match exactly one sidecar, not the sibling's too")
	assert.Equal(t, dindA, dindHits[0].ID)

	holderHits, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		Filters: container.HolderSessionFilters(sessA),
	})
	require.NoError(t, err)
	require.Len(t, holderHits, 1, "session A's holder filter must match exactly one holder, not the sibling's too")
	assert.Equal(t, holderA, holderHits[0].ID)

	// Session B's sidecars must be untouched by A's lookups.
	for _, id := range []string{dindB, holderB} {
		insp, err := cli.ContainerInspect(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, insp.State)
		assert.True(t, insp.State.Running, "sibling session B's sidecar %s must still be running", id)
	}
}
```

- [ ] **Step 2: Run it**

Run: `go test ./test/e2e/ -run TestParallel_SidecarFiltersAreSessionScoped -v -count=1 -timeout 600s`
Expected: PASS. With Tasks 1 and 3 in place the production filter is already
correct, so this test passes on first run — it is a regression guard, not a
red-green step. To confirm it has teeth, temporarily swap the session
argument in `DINDSessionFilters` for `LabelWorkspace+"="+session.WorkspaceHash`;
the `require.Len(..., 1)` assertion then fails with 2 hits. Revert immediately.

- [ ] **Step 3: Repoint the existing isolation test at the real filters**

In `TestParallel_DINDWorkspaceFilterIsolation`, change:

```go
	makeDINDLabels := func(p parallelTestParams) map[string]string {
		return map[string]string{
			container.LabelBase:      "true",
			container.LabelAgent:     p.AgentName,
			container.LabelProfile:   p.Profile,
			container.LabelWorkspace: p.WsHash,
			container.LabelDIND:      "true",
		}
	}
```

to:

```go
	// Session names are per-workspace here: this test's scenario is two
	// workspaces, each running one session.
	sessionName := func(p parallelTestParams) string {
		return "ai-shim-sess-" + p.WsHash
	}

	makeDINDLabels := func(p parallelTestParams) map[string]string {
		return map[string]string{
			container.LabelBase:      "true",
			container.LabelAgent:     p.AgentName,
			container.LabelProfile:   p.Profile,
			container.LabelWorkspace: p.WsHash,
			container.LabelSession:   sessionName(p),
			container.LabelDIND:      "true",
		}
	}
```

and replace the `queryWorkspace` closure body:

```go
	// Call the production filter directly. The previous version of this test
	// hand-copied the filter, so a filter that lost its scoping still passed.
	queryWorkspace := func(p parallelTestParams) []dockercontainer.Summary {
		session := &container.RunningSession{
			ContainerName: sessionName(p),
			AgentName:     p.AgentName,
			Profile:       p.Profile,
			WorkspaceHash: p.WsHash,
		}
		list, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
			Filters: container.DINDSessionFilters(session),
		})
		require.NoError(t, err)
		return list
	}
```

Update the test's doc comment: the invariant it now guards is session
scoping, with different workspaces as the scenario. Keep the
`github.com/docker/docker/api/types/filters` import —
`TestParallel_CacheOrphanGuard` still uses it at `parallel_session_test.go:416`.

- [ ] **Step 4: Run the parallel E2E suite**

Run: `go test ./test/e2e/ -run TestParallel -v -count=1 -timeout 600s`
Expected: PASS, all `TestParallel_*` tests including both changed ones.

- [ ] **Step 5: Commit**

```bash
git add test/e2e/parallel_session_test.go
git commit -m "test(e2e): assert sidecar filters are scoped to one session"
```

---

### Task 7: Full gate

**Files:**
- Modify: `docs/plans/2026-07-30-session-scoped-sidecar-teardown-design.md` (status line only)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

- [ ] **Step 1: Run the non-Docker CI gate**

Run: `make ci`
Expected: PASS. It runs fmt-check, vet, tidy-check, check-silent-failures, test-race, fuzz and vuln. Do not claim CI-green from a narrower command.

- [ ] **Step 2: Run the Docker-backed suites**

Run: `go test ./test/... ./internal/container/ ./internal/dind/ ./internal/network/ ./internal/docker/ -count=1 -timeout 2400s`
Expected: PASS. This mirrors the e2e job's package set in `.github/workflows/ci.yml:363`.

- [ ] **Step 3: Run the linter**

Run: `make lint`
Expected: PASS, no new findings.

- [ ] **Step 4: Update the design doc status**

In `docs/plans/2026-07-30-session-scoped-sidecar-teardown-design.md`, change:

```markdown
**Status:** approved (brainstorm), pending implementation plan
```

to:

```markdown
**Status:** implemented
```

- [ ] **Step 5: Commit**

```bash
git add docs/plans/2026-07-30-session-scoped-sidecar-teardown-design.md
git commit -m "docs(plans): mark session-scoped teardown design implemented"
```

- [ ] **Step 6: Report the rollout requirement**

Tell the human, in the completion summary rather than in a checked-in file: sessions already running are served by the binary that launched them, so they keep sweeping by agent+profile+workspace and will still destroy new sessions' sidecars. Adopting the fix requires draining all active sessions. Any session whose holder was already destroyed cannot be recovered and needs to be killed and relaunched.
