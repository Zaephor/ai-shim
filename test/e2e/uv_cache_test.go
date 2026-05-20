package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUVCache_PopulatedAndReused proves that UV_CACHE_DIR is populated with
// real cached packages after a uv tool install inside a container, and that
// the cache persists and is reused across a second container launch.
//
// Phase 1: install ruff via "uv tool install ruff", then verify UV_CACHE_DIR
// on the host contains cached entries.
//
// Phase 2: launch a fresh container sharing the same layout (same bind mount),
// verify UV_CACHE_DIR is non-empty (proving cache reuse), and run "uv tool
// install ruff" again (should be instant from cache).
func TestUVCache_PopulatedAndReused(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow UV cache E2E test")
	}

	// --- Setup ---
	root := dockerTempDir(t)
	layout := storage.NewLayout(root)

	agentDef, ok := agent.Lookup("opencode")
	require.True(t, ok, "opencode agent must exist in registry")

	require.NoError(t, layout.EnsureDirectories(agentDef.Name, "default"))
	require.NoError(t, layout.EnsureAgentData("default", agentDef.DataDirs, agentDef.DataFiles))

	// uv tool config: data_dir:true, cache_scope:"global", env_var:"UV_CACHE_DIR",
	// install script bootstraps uv via curl+sh.
	uvInstall := `export PATH="$HOME/.local/bin:$PATH"
if ! command -v uv >/dev/null 2>&1; then
  curl -LsSf https://astral.sh/uv/install.sh | sh
fi`

	cfg := config.Config{
		Image: container.DefaultImage,
		Tools: map[string]config.ToolDef{
			"uv": {
				Type:       "custom",
				DataDir:    true,
				CacheScope: "global",
				EnvVar:     "UV_CACHE_DIR",
				Install:    uvInstall,
			},
		},
	}

	plat := platform.Detect()

	// Resolve the host-side UV cache path and clean it up on test exit.
	uvCacheHost, err := storage.ToolCachePath(layout, "uv", "global", agentDef.Name, "default")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(uvCacheHost) })

	// --- Phase 1: first launch — install ruff and populate cache ---
	t.Log("Phase 1: installing ruff via uv tool install, expecting cache population")
	{
		ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
		defer cancel()

		runner, err := container.NewRunner(ctx)
		require.NoError(t, err)
		defer runner.Close()

		require.NoError(t, runner.EnsureImage(ctx, cfg.Image))

		spec, err := container.BuildSpec(container.BuildParams{
			Config:   cfg,
			Agent:    agentDef,
			Profile:  "default",
			Layout:   layout,
			Platform: plat,
			HomeDir:  "/home/user",
		})
		require.NoError(t, err)

		// Extract the tool provisioning preamble (sets UV_CACHE_DIR, installs uv),
		// then append commands to install ruff and list cache contents.
		toolScript := extractToolScript(t, spec.Entrypoint[2])
		script := fmt.Sprintf(`%s
export PATH="$HOME/.local/bin:$PATH"
echo "--- Installing ruff via uv tool install ---"
uv tool install ruff || { echo "ERROR: uv tool install ruff failed"; exit 1; }
echo "--- UV_CACHE_DIR contents ---"
ls -la "$UV_CACHE_DIR" || echo "WARNING: ls UV_CACHE_DIR failed"
echo "--- Done ---"
`, toolScript)

		spec.Entrypoint = []string{"sh", "-c", script}
		spec.Cmd = nil
		spec.TTY = false
		spec.Stdin = false

		result, err := runner.Run(ctx, spec)
		require.NoError(t, err, "phase 1 container run should not error")
		assert.Equal(t, 0, result.ExitCode,
			"phase 1 should exit 0 — uv tool install ruff should succeed")
	}

	// Verify host-side cache dir contains entries after phase 1.
	entries, err := os.ReadDir(uvCacheHost)
	require.NoError(t, err, "UV cache host dir should be readable after phase 1")
	require.NotEmpty(t, entries,
		"UV_CACHE_DIR should contain cached packages after uv tool install (got %d entries)",
		len(entries))
	t.Logf("Phase 1: UV cache dir has %d entries after first launch", len(entries))

	// --- Phase 2: second launch — verify cache reuse ---
	t.Log("Phase 2: verifying UV cache is reused in a fresh container")
	{
		ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
		defer cancel()

		runner, err := container.NewRunner(ctx)
		require.NoError(t, err)
		defer runner.Close()

		// Build a fresh spec using the SAME layout so the bind mount is consistent.
		spec, err := container.BuildSpec(container.BuildParams{
			Config:   cfg,
			Agent:    agentDef,
			Profile:  "default",
			Layout:   layout,
			Platform: plat,
			HomeDir:  "/home/user",
		})
		require.NoError(t, err)

		// Extract tool script, then check cache is non-empty and re-install
		// (should be instant from cache).
		toolScript := extractToolScript(t, spec.Entrypoint[2])
		script := fmt.Sprintf(`%s
export PATH="$HOME/.local/bin:$PATH"
echo "--- Checking UV_CACHE_DIR is non-empty ---"
if [ -z "$UV_CACHE_DIR" ]; then
  echo "ERROR: UV_CACHE_DIR is not set"
  exit 1
fi
cache_count=$(ls -1 "$UV_CACHE_DIR" 2>/dev/null | wc -l)
if [ "$cache_count" -eq 0 ]; then
  echo "ERROR: UV_CACHE_DIR is empty on second launch"
  exit 1
fi
echo "UV_CACHE_DIR has $cache_count entries (expected >0)"
echo "--- Re-installing ruff (should use cache) ---"
uv tool install ruff || { echo "ERROR: second uv tool install ruff failed"; exit 1; }
echo "--- Done ---"
`, toolScript)

		spec.Entrypoint = []string{"sh", "-c", script}
		spec.Cmd = nil
		spec.TTY = false
		spec.Stdin = false

		result, err := runner.Run(ctx, spec)
		require.NoError(t, err, "phase 2 container run should not error")
		assert.Equal(t, 0, result.ExitCode,
			"phase 2 should exit 0 — cache should persist and uv tool install should succeed from cache")
	}

	// After phase 2, verify host-side cache dir still contains entries.
	entriesAfter, err := os.ReadDir(uvCacheHost)
	require.NoError(t, err, "UV cache host dir should be readable after phase 2")
	assert.NotEmpty(t, entriesAfter,
		"UV_CACHE_DIR should still contain cached packages after second launch (got %d entries)",
		len(entriesAfter))
	t.Logf("Phase 2: UV cache dir has %d entries after second launch", len(entriesAfter))
}
