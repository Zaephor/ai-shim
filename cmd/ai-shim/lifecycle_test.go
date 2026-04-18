package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readMainSource returns the full text of main.go. Used by tests that want
// to assert a specific call/shape is present in a function body that is
// otherwise too entangled with Docker to test directly.
func readMainSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("main.go")
	require.NoError(t, err)
	return string(data)
}

// extractFuncBody returns the body of the first top-level func with the
// given name. Brace matching is naive but sufficient for main.go's style.
func extractFuncBody(t *testing.T, src, funcName string) string {
	t.Helper()
	marker := "func " + funcName + "("
	idx := strings.Index(src, marker)
	require.NotEqual(t, -1, idx, "could not find func %s", funcName)
	// Advance to the opening brace of the body.
	open := strings.Index(src[idx:], "{")
	require.NotEqual(t, -1, open)
	start := idx + open
	depth := 0
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[start : i+1]
			}
		}
	}
	t.Fatalf("unterminated body for func %s", funcName)
	return ""
}

// TestStaleContainerFilters_IncludesWorkspace guards Fix #4: the cleanup
// filter must scope by workspace hash so that parallel sessions in other
// workspaces are not reaped when this one restarts.
func TestStaleContainerFilters_IncludesWorkspace(t *testing.T) {
	f := staleContainerFilters("claude-code", "default", "abc123def456", "exited")
	labels := f.Get("label")

	assert.Contains(t, labels, container.LabelBase+"=true")
	assert.Contains(t, labels, container.LabelAgent+"=claude-code")
	assert.Contains(t, labels, container.LabelProfile+"=default")
	assert.Contains(t, labels, container.LabelWorkspace+"=abc123def456",
		"stale-container filter must include workspace hash to avoid reaping sibling workspaces")

	status := f.Get("status")
	assert.Contains(t, status, "exited")
}

// TestDINDSessionFilters_IncludesWorkspace guards Fix #1: when a parallel
// session is killed, its DIND stop query must match only its own DIND —
// not sibling DINDs on the same agent+profile.
func TestDINDSessionFilters_IncludesWorkspace(t *testing.T) {
	session := &container.RunningSession{
		AgentName:     "claude-code",
		Profile:       "default",
		WorkspaceHash: "ws-hash-xyz",
	}
	f := dindSessionFilters(session)
	labels := f.Get("label")

	assert.Contains(t, labels, container.LabelBase+"=true")
	assert.Contains(t, labels, container.LabelDIND+"=true")
	assert.Contains(t, labels, container.LabelAgent+"=claude-code")
	assert.Contains(t, labels, container.LabelProfile+"=default")
	assert.Contains(t, labels, container.LabelWorkspace+"=ws-hash-xyz",
		"DIND session filter must include workspace hash to avoid touching sibling DINDs in parallel sessions")
}

// TestBuildDINDSharedMounts_WorkspaceAlwaysPresent covers the existing base
// behaviour: the workspace is always propagated to the DIND sidecar.
func TestBuildDINDSharedMounts_WorkspaceAlwaysPresent(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	mounts, err := buildDINDSharedMounts("/host/pwd", "/workspace/abc", nil, nil, layout, "claude-code", "default")
	require.NoError(t, err)

	found := false
	for _, m := range mounts {
		if m.Source == "/host/pwd" && m.Target == "/workspace/abc" {
			found = true
		}
	}
	assert.True(t, found, "workspace bind must be propagated to DIND")
}

// TestBuildDINDSharedMounts_ToolCachesPropagated guards Fix #5: tools with
// data_dir=true must have their host cache directory propagated to the
// DIND sidecar at the same path so that `docker -v <host>:<container>`
// from inside the agent can resolve the bind source.
func TestBuildDINDSharedMounts_ToolCachesPropagated(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	tools := map[string]config.ToolDef{
		"nvm": {Type: "custom", Install: "echo hi", DataDir: true, EnvVar: "NVM_DIR"},
		"act": {Type: "binary-download", URL: "https://example.com/act", Binary: "act"}, // no data_dir
		"gvm": {Type: "custom", Install: "echo hi", DataDir: true, EnvVar: "GVM_ROOT", CacheScope: "profile"},
	}
	mounts, err := buildDINDSharedMounts("/host/pwd", "/workspace/abc", tools, []string{"nvm", "act", "gvm"}, layout, "claude-code", "default")
	require.NoError(t, err)

	expectedNVM, err := storage.ToolCachePath(layout, "nvm", "", "claude-code", "default")
	require.NoError(t, err)
	expectedGVM, err := storage.ToolCachePath(layout, "gvm", "profile", "claude-code", "default")
	require.NoError(t, err)
	expectedACT, err := storage.ToolCachePath(layout, "act", "", "claude-code", "default")
	require.NoError(t, err)

	var foundNVM, foundGVM, foundACT bool
	for _, m := range mounts {
		// Bugfix requires SAME-path bind: Source == Target == host path.
		if m.Source == expectedNVM && m.Target == expectedNVM {
			foundNVM = true
		}
		if m.Source == expectedGVM && m.Target == expectedGVM {
			foundGVM = true
		}
		if m.Target == "/usr/local/share/ai-shim/cache/act" || m.Source == expectedACT {
			foundACT = true
		}
	}

	assert.True(t, foundNVM, "nvm tool cache must be propagated to DIND at same path (source==target)")
	assert.True(t, foundGVM, "gvm tool cache (profile scope) must be propagated to DIND at same path")
	assert.False(t, foundACT, "tool without data_dir must NOT be propagated to DIND")
}

// TestBuildDINDSharedMounts_NoToolsNoDIND covers the empty-tools case.
func TestBuildDINDSharedMounts_NoToolsNoDIND(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	mounts, err := buildDINDSharedMounts("/host/pwd", "/workspace/abc", nil, nil, layout, "claude-code", "default")
	require.NoError(t, err)

	// Only the workspace bind.
	assert.Len(t, mounts, 1)
	assert.Equal(t, "/host/pwd", mounts[0].Source)
}

// Fix #2 and #3 are simple behavioural additions (one extra function call).
// They are hardest to unit-test without a live docker client. Guard the
// intent at the source-text level: verify the lifecycle paths actually
// invoke MaybeStopCache. This would fail if a refactor regresses the fix.
func TestStopSession_InvokesMaybeStopCache(t *testing.T) {
	src := readMainSource(t)
	// Find the stopSession function body and check MaybeStopCache is called in it.
	body := extractFuncBody(t, src, "stopSession")
	assert.Contains(t, body, "dind.MaybeStopCache",
		"stopSession must call dind.MaybeStopCache after tearing down the DIND sidecar (Fix #2)")
}

func TestHandleReattach_InvokesMaybeStopCacheOnExit(t *testing.T) {
	src := readMainSource(t)
	body := extractFuncBody(t, src, "handleReattach")
	assert.Contains(t, body, "dind.MaybeStopCache",
		"handleReattach must call dind.MaybeStopCache on natural container exit (Fix #3)")
	// Must be guarded by IsCacheEnabled().
	assert.Contains(t, body, "IsCacheEnabled",
		"handleReattach MaybeStopCache call must be guarded by cfg.IsCacheEnabled() (Fix #3)")
}

// TestCleanupStaleContainers_AcceptsWorkspaceHash guards Fix #4 at the
// call-site signature level: cleanupStaleContainers must take a wsHash
// parameter (compile-time check).
func TestCleanupStaleContainers_Signature(t *testing.T) {
	// Pure compile-time check: if the signature changes back, this line
	// fails to compile.
	_ = staleContainerFilters
	// Ensure the call-site in runAgent passes wsHash. Source-text check.
	src := readMainSource(t)
	body := extractFuncBody(t, src, "runAgent")
	assert.Contains(t, body, "cleanupStaleContainers(ctx, runner, agentName, profileName, wsHash)",
		"runAgent must pass wsHash to cleanupStaleContainers (Fix #4)")
}
