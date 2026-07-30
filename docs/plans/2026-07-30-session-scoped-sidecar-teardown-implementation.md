# Session-scoped sidecar teardown — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every session a unique container label so tearing down one session cannot destroy a sibling session's DIND sidecar and netns holder.

**Architecture:** A new `ai-shim.session` label, set once in `BuildSpec` with the agent container name as its value, is inherited by every sidecar through `spec.Labels`. The two teardown filter builders move to `internal/container` and match on that label instead of agent+profile+workspace. A reattach guard reports a destroyed netns owner rather than silently attaching to a network-less container.

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

### Task 3: Move teardown filters to internal/container and scope them by session

This is the fix.

**Files:**
- Create: `internal/container/sidecar_filters.go`
- Create: `internal/container/sidecar_filters_test.go`
- Modify: `cmd/ai-shim/main.go:1707-1733` (delete both local builders), `main.go:1738`, `main.go:1772` (call sites)
- Modify: `cmd/ai-shim/lifecycle_test.go:72-117` (the two moved tests), `lifecycle_test.go:119-134` (source-text assertion)

**Interfaces:**
- Consumes: `LabelSession` (Task 1), `RunningSession.ContainerName` (already populated by every lookup in `internal/container/lookup.go`).
- Produces: `container.DINDSessionFilters(session *RunningSession) filters.Args` and `container.HolderSessionFilters(session *RunningSession) filters.Args`. Task 6's E2E test calls both.

- [ ] **Step 1: Write the failing test**

Create `internal/container/sidecar_filters_test.go`:

```go
package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func sessionA() *RunningSession {
	return &RunningSession{
		ContainerName: "claude-code-work-abc123-aaaaaaaa",
		AgentName:     "claude-code",
		Profile:       "work",
		WorkspaceHash: "abc123",
	}
}

func TestDINDSessionFilters_ScopedToSession(t *testing.T) {
	labels := DINDSessionFilters(sessionA()).Get("label")

	assert.Contains(t, labels, LabelBase+"=true")
	assert.Contains(t, labels, LabelDIND+"=true")
	assert.Contains(t, labels, LabelSession+"=claude-code-work-abc123-aaaaaaaa",
		"DIND teardown must match the one session that owns the sidecar")
	assert.Contains(t, DINDSessionFilters(sessionA()).Get("status"), "running")
}

func TestHolderSessionFilters_ScopedToSession(t *testing.T) {
	labels := HolderSessionFilters(sessionA()).Get("label")

	assert.Contains(t, labels, LabelBase+"=true")
	assert.Contains(t, labels, LabelRole+"=netns-holder")
	assert.Contains(t, labels, LabelSession+"=claude-code-work-abc123-aaaaaaaa",
		"holder teardown must match the one session that owns the holder")
	assert.Contains(t, HolderSessionFilters(sessionA()).Get("status"), "running")
}

// Parallel sessions share agent, profile and workspace labels. If either
// filter still carries those, teardown for one session matches every
// sibling's sidecars — the defect this change exists to fix.
func TestSidecarFilters_DoNotUseWorkspaceScoping(t *testing.T) {
	for name, labels := range map[string][]string{
		"dind":   DINDSessionFilters(sessionA()).Get("label"),
		"holder": HolderSessionFilters(sessionA()).Get("label"),
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

- [ ] **Step 5: Delete the local builders and repoint the call sites**

In `cmd/ai-shim/main.go`, delete both `dindSessionFilters` and `holderSessionFilters` (the whole block from the `// dindSessionFilters builds the filter...` comment through the closing brace of `holderSessionFilters`).

At `main.go:1738`, change:

```go
	list, err := cli.ContainerList(ctx, container_types.ListOptions{Filters: dindSessionFilters(session)})
```

to:

```go
	list, err := cli.ContainerList(ctx, container_types.ListOptions{Filters: container.DINDSessionFilters(session)})
```

At `main.go:1772`, change:

```go
	holderList, err := cli.ContainerList(ctx, container_types.ListOptions{Filters: holderSessionFilters(session)})
```

to:

```go
	holderList, err := cli.ContainerList(ctx, container_types.ListOptions{Filters: container.HolderSessionFilters(session)})
```

Also add a debug line for the zero-match case, immediately after the `list` error check in `stopDINDForSession`:

```go
	if len(list) == 0 {
		logging.Debug("no DIND sidecar found for session %s", session.ContainerName)
	}
```

`logging` is already imported in `main.go`.

- [ ] **Step 6: Update the tests that referenced the deleted functions**

In `cmd/ai-shim/lifecycle_test.go`, delete `TestDINDSessionFilters_IncludesWorkspace` and `TestHolderSessionFilters_IncludesWorkspace` — both assert the workspace scoping this change removes, and their replacements live in `internal/container/sidecar_filters_test.go`.

In `TestStopDINDForSession_RemovesNetnsHolder`, the source-text assertions still reference the old names. Change both occurrences of `"holderSessionFilters(session)"` to `"container.HolderSessionFilters(session)"`:

```go
	assert.Contains(t, body, "container.HolderSessionFilters(session)",
		"stopDINDForSession must list containers via container.HolderSessionFilters to find the netns holder")

	holderIdx := strings.Index(body, "container.HolderSessionFilters(session)")
```

Leave the rest of that test — the ordering assertion against `network.RemoveOrphanedForSession` is still valid and still load-bearing.

- [ ] **Step 7: Run the full unit suite**

Run: `go test -short ./... -count=1`
Expected: PASS. Any failure here is a call site of the deleted functions that step 5 missed; the compiler names it.

- [ ] **Step 8: Commit**

```bash
git add internal/container/sidecar_filters.go internal/container/sidecar_filters_test.go cmd/ai-shim/main.go cmd/ai-shim/lifecycle_test.go
git commit -m "fix(cleanup): scope sidecar teardown to the owning session"
```

---

### Task 4: Drop the five-second stall on holder removal

**Files:**
- Modify: `cmd/ai-shim/main.go:1776-1784` (the holder loop in `stopDINDForSession`)
- Test: `cmd/ai-shim/lifecycle_test.go`

**Interfaces:**
- Consumes: `container.HolderSessionFilters` (Task 3).
- Produces: nothing new.

The holder runs `sleep infinity` as PID 1. PID 1 ignores SIGTERM unless it installs a handler, and `sleep` installs none, so `ContainerStop{Timeout:5}` always burns the full five seconds and the daemon force-kills anyway — that is the `failed to exit within 5s of signal 15` line in the incident log. `Holder.Stop` (`internal/dind/holder.go:77`) already removes directly; make the teardown path match.

- [ ] **Step 1: Write the failing test**

Append to `cmd/ai-shim/lifecycle_test.go`:

```go
// TestStopDINDForSession_HolderRemovedWithoutGracefulStop guards against
// reintroducing a graceful stop on the netns holder. The holder is `sleep
// infinity` as PID 1 with no SIGTERM handler, so ContainerStop can only
// wait out its full timeout before the daemon force-kills it — pure latency
// on every teardown, multiplied by the number of holders matched.
func TestStopDINDForSession_HolderRemovedWithoutGracefulStop(t *testing.T) {
	src := readMainSource(t)
	body := extractFuncBody(t, src, "stopDINDForSession")

	holderIdx := strings.Index(body, "container.HolderSessionFilters(session)")
	require.NotEqual(t, -1, holderIdx, "HolderSessionFilters call not found")

	holderSection := body[holderIdx:]
	assert.NotContains(t, holderSection, "ContainerStop",
		"the netns holder must be force-removed, not gracefully stopped: sleep as PID 1 ignores SIGTERM")
	assert.Contains(t, holderSection, "ContainerRemove",
		"the netns holder must still be removed")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ai-shim/ -run TestStopDINDForSession_HolderRemovedWithoutGracefulStop -v`
Expected: FAIL — `holderSection` still contains `ContainerStop`.

- [ ] **Step 3: Remove the graceful stop**

In `stopDINDForSession`, this loop:

```go
	for _, c := range holderList {
		stopTimeout := 5
		if err := cli.ContainerStop(ctx, c.ID, container_types.StopOptions{Timeout: &stopTimeout}); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to stop netns holder container %s: %v\n", c.ID, err)
		}
		if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove netns holder container %s: %v\n", c.ID, err)
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
		if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove netns holder container %s: %v\n", c.ID, err)
		}
	}
```

Leave the DIND loop above untouched: `dockerd` does handle SIGTERM, and a graceful shutdown there is worth the wait.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/ai-shim/ -run TestStopDINDForSession -v`
Expected: PASS — both the holder-ordering test and the new one.

- [ ] **Step 5: Commit**

```bash
git add cmd/ai-shim/main.go cmd/ai-shim/lifecycle_test.go
git commit -m "perf(cleanup): force-remove netns holder instead of waiting on SIGTERM"
```

---

### Task 5: Warn on reattach when the netns owner is gone

**Files:**
- Create: `internal/container/netns.go`
- Create: `internal/container/netns_test.go`
- Modify: `cmd/ai-shim/main.go` (add `warnIfNetnsOwnerDead`, call it from `handleReattach:1629-1633`)
- Test: `cmd/ai-shim/lifecycle_test.go`

**Interfaces:**
- Consumes: `RunningSession` (existing).
- Produces: `container.ParseNetnsOwner(networkMode string) (id string, ok bool)`, and `warnIfNetnsOwnerDead(ctx, cli, session)` in `package main`.

- [ ] **Step 1: Write the failing test**

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

- [ ] **Step 3: Implement the parser**

Create `internal/container/netns.go`:

```go
package container

import "strings"

// netnsOwnerPrefix is Docker's network-mode form for joining another
// container's network namespace.
const netnsOwnerPrefix = "container:"

// ParseNetnsOwner returns the container ID whose network namespace the given
// network mode joins, and whether the mode is that kind at all. Any other
// mode — bridge, host, none, a named network — returns ok=false.
//
// Callers use this to detect an unrecoverable session: if the owner is gone,
// the dependent keeps only lo and never regains eth0, even if a container
// with the same ID is started again. The container must be recreated.
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/container/ -run TestParseNetnsOwner -v`
Expected: PASS, all seven subtests.

- [ ] **Step 5: Write the failing test for the reattach hook**

Append to `cmd/ai-shim/lifecycle_test.go`:

```go
// TestHandleReattach_ChecksNetnsOwnerBeforeAttaching guards the diagnostic
// added for wedged sessions: a session whose netns owner was destroyed has
// no network and cannot regain one, so the user must be told before they
// are dropped into it rather than after they hit a failing command.
func TestHandleReattach_ChecksNetnsOwnerBeforeAttaching(t *testing.T) {
	src := readMainSource(t)
	body := extractFuncBody(t, src, "handleReattach")

	warnIdx := strings.Index(body, "warnIfNetnsOwnerDead")
	attachIdx := strings.Index(body, "runner.Reattach")
	require.NotEqual(t, -1, warnIdx, "handleReattach must check the netns owner")
	require.NotEqual(t, -1, attachIdx, "runner.Reattach call not found")
	assert.Less(t, warnIdx, attachIdx,
		"the netns-owner warning must be printed before attaching, not after")
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./cmd/ai-shim/ -run TestHandleReattach_ChecksNetnsOwner -v`
Expected: FAIL — `warnIfNetnsOwnerDead` not found in the function body.

- [ ] **Step 7: Implement the guard**

In `cmd/ai-shim/main.go`, add above `handleReattach`:

```go
// warnIfNetnsOwnerDead reports a session whose network namespace owner no
// longer exists or is no longer running. Such a session keeps only lo: a
// container:<id> dependent never regains eth0, not even if the owner is
// restarted with the same ID, so the container has to be recreated.
//
// Two inspect calls, no exec. Best-effort throughout — an inspect failure
// here must never block a reattach.
func warnIfNetnsOwnerDead(ctx context.Context, cli *client.Client, session *container.RunningSession) {
	insp, err := cli.ContainerInspect(ctx, session.ContainerID)
	if err != nil || insp.HostConfig == nil {
		return
	}
	ownerID, ok := container.ParseNetnsOwner(string(insp.HostConfig.NetworkMode))
	if !ok {
		return // not sharing a namespace; nothing to check
	}
	ownerInsp, err := cli.ContainerInspect(ctx, ownerID)
	if err == nil && ownerInsp.State != nil && ownerInsp.State.Running {
		return // owner alive
	}
	fmt.Fprintf(os.Stderr,
		"ai-shim: warning: the network namespace owner for %s is gone.\n"+
			"ai-shim: this session has no network connectivity and cannot regain it.\n"+
			"ai-shim: stop the session and start a new one to restore networking.\n",
		session.ContainerName)
}
```

Then call it as the first statement of `handleReattach`, before the existing status line:

```go
func handleReattach(ctx context.Context, runner *container.Runner, session *container.RunningSession, cfg config.Config, logDir string) (int, error) {
	warnIfNetnsOwnerDead(ctx, runner.Client(), session)

	fmt.Fprintf(os.Stderr, "ai-shim: reattaching to %s...\n", session.ContainerName)
```

This one hook covers both entry points: the picker's reattach choice and `manage attach <name>`.

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./cmd/ai-shim/ ./internal/container/ -short -count=1`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/container/netns.go internal/container/netns_test.go cmd/ai-shim/main.go cmd/ai-shim/lifecycle_test.go
git commit -m "feat(reattach): warn when the session's netns owner is gone"
```

---

### Task 6: E2E regression — same-workspace siblings survive a teardown

This is the test that would have caught the defect.

**Files:**
- Modify: `test/e2e/parallel_session_test.go` (add one test; repoint `TestParallel_DINDWorkspaceFilterIsolation:307-385`)

**Interfaces:**
- Consumes: `container.DINDSessionFilters`, `container.HolderSessionFilters` (Task 3), `container.LabelSession` (Task 1), and the file's existing helpers `newTestParams`, `parallelTestParams.sessionLabels`, `startFakeSession`, `waitForRunning`.
- Produces: nothing consumed by later tasks.

Existing E2E coverage hand-copies the production filter into the test, so a filter missing session scope passes. Both tests below call the real exported functions instead.

- [ ] **Step 1: Write the failing test**

Append to `test/e2e/parallel_session_test.go`:

```go
// TestParallel_SidecarTeardownIsSessionScoped is the regression guard for
// the sibling-teardown defect: two sessions in the SAME workspace share
// every label except the session label, so a teardown scoped by anything
// coarser matches both and destroys a still-running session's sidecars.
// Its agent container survives — that one is removed by ID — leaving it
// attached to a destroyed netns with no path to recovery.
func TestParallel_SidecarTeardownIsSessionScoped(t *testing.T) {
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

	// One workspace, two sibling sessions: identical agent, profile and
	// workspace labels, distinct session labels.
	params := newTestParams(t)
	nameA := fmt.Sprintf("ai-shim-sess-a-%d", time.Now().UnixNano())
	nameB := fmt.Sprintf("ai-shim-sess-b-%d", time.Now().UnixNano())

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

	// Tear session A's sidecars down the way stopDINDForSession does.
	require.NoError(t, cli.ContainerRemove(ctx, dindHits[0].ID, dockercontainer.RemoveOptions{Force: true}))
	require.NoError(t, cli.ContainerRemove(ctx, holderHits[0].ID, dockercontainer.RemoveOptions{Force: true}))

	for _, id := range []string{dindB, holderB} {
		insp, err := cli.ContainerInspect(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, insp.State)
		assert.True(t, insp.State.Running,
			"sibling session B's sidecar %s must still be running after session A is torn down", id)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./test/e2e/ -run TestParallel_SidecarTeardownIsSessionScoped -v -count=1 -timeout 600s`
Expected: FAIL to compile if Tasks 1 and 3 were skipped (`undefined: container.LabelSession`). If Tasks 1-3 are already in place it PASSES — that is expected and correct: the fix precedes the test here because the test asserts against the production filter, which no longer exists in its broken form. To see it fail meaningfully, temporarily add `filters.Arg("label", LabelWorkspace+"="+session.WorkspaceHash)` back to `DINDSessionFilters` and drop the session arg; the `require.Len(..., 1)` assertions then fail with 2 hits. Revert immediately.

- [ ] **Step 3: Repoint the existing isolation test at the real filters**

In `TestParallel_DINDWorkspaceFilterIsolation`, `makeDINDLabels` gains a session label, and the hand-copied filter is replaced. Change:

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
	// workspaces, and each has its own session.
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

Update the test's doc comment: the invariant it now guards is session scoping, with different workspaces as the scenario. Keep the `github.com/docker/docker/api/types/filters` import — `TestParallel_CacheOrphanGuard` still uses it at `parallel_session_test.go:416`.

- [ ] **Step 4: Run the parallel E2E suite**

Run: `go test ./test/e2e/ -run TestParallel -v -count=1 -timeout 600s`
Expected: PASS, all `TestParallel_*` tests including both changed ones.

- [ ] **Step 5: Commit**

```bash
git add test/e2e/parallel_session_test.go
git commit -m "test(e2e): guard sibling sidecars against same-workspace teardown"
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
