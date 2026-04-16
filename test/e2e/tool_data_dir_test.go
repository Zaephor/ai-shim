package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestToolDataDir_EnvVarAndMount verifies that a tool with data_dir:true gets:
//  1. export <env_var>="<mount-path>" injected into the generated entrypoint
//  2. a bind mount targeting the container-side cache path
//  3. the host-side directory created by BuildSpec/buildMounts
//
// This is a wiring/plumbing test — no Docker pull or network required.
func TestToolDataDir_EnvVarAndMount(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	agentDef, ok := agent.Lookup("opencode")
	require.True(t, ok, "opencode agent must exist in registry")

	require.NoError(t, layout.EnsureDirectories(agentDef.Name, "default"))
	require.NoError(t, layout.EnsureAgentData("default", agentDef.DataDirs, agentDef.DataFiles))

	cfg := config.Config{
		Image: "alpine:latest",
		Tools: map[string]config.ToolDef{
			"test-tool": {
				Type:    "custom",
				Install: "echo ok",
				DataDir: true,
				EnvVar:  "TEST_TOOL_DIR",
			},
		},
	}

	plat := platform.Info{UID: os.Getuid(), GID: os.Getgid(), Hostname: "testhost", Username: "testuser"}

	spec, err := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  "default",
		Layout:   layout,
		Platform: plat,
		HomeDir:  "/home/user",
	})
	require.NoError(t, err)

	// The entrypoint script is spec.Entrypoint[2] — the sh -c argument.
	require.Len(t, spec.Entrypoint, 3, "entrypoint should be [sh, -c, <script>]")
	script := spec.Entrypoint[2]

	wantExport := `export TEST_TOOL_DIR="/usr/local/share/ai-shim/cache/test-tool"`
	assert.Contains(t, script, wantExport,
		"entrypoint should export TEST_TOOL_DIR pointing at the container-side mount path")

	// Verify a bind mount targets the correct container path.
	wantTarget := "/usr/local/share/ai-shim/cache/test-tool"
	var found *mount.Mount
	for i := range spec.Mounts {
		if spec.Mounts[i].Target == wantTarget {
			found = &spec.Mounts[i]
			break
		}
	}
	require.NotNil(t, found, "spec.Mounts should contain a mount targeting %q", wantTarget)
	assert.Equal(t, mount.TypeBind, found.Type, "tool cache mount should be TypeBind")

	// Verify the host directory was created by buildMounts (via os.MkdirAll).
	info, err := os.Stat(found.Source)
	require.NoError(t, err, "host-side cache directory %q should exist after BuildSpec", found.Source)
	assert.True(t, info.IsDir(), "host-side cache path should be a directory")

	// Clean up: remove the host-side tool cache directory.
	t.Cleanup(func() { os.RemoveAll(found.Source) })
}

// TestToolDataDir_CacheScopes verifies that different cache_scope values produce
// different host paths rooted at the layout root:
//   - "" or "global" → layout.SharedCache/{tool}/
//   - "profile"      → layout.Root/profiles/{profile}/cache/{tool}/
//   - "agent"        → layout.Root/agents/{agent}/cache/{tool}/
//
// This is a unit-ish test using storage.ToolCachePath directly — no Docker needed.
func TestToolDataDir_CacheScopes(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	agentName := "opencode"
	profile := "default"
	toolName := "mytool"

	cases := []struct {
		scope    string
		wantPath string
	}{
		{
			scope:    "global",
			wantPath: filepath.Join(layout.SharedCache, toolName),
		},
		{
			scope:    "",
			wantPath: filepath.Join(layout.SharedCache, toolName),
		},
		{
			scope:    "profile",
			wantPath: filepath.Join(layout.Root, "profiles", profile, "cache", toolName),
		},
		{
			scope:    "agent",
			wantPath: filepath.Join(layout.Root, "agents", agentName, "cache", toolName),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("scope=%q", tc.scope), func(t *testing.T) {
			got := storage.ToolCachePath(layout, toolName, tc.scope, agentName, profile)
			assert.Equal(t, tc.wantPath, got,
				"ToolCachePath with scope=%q should return the expected host path", tc.scope)
		})
	}

	// Also verify via BuildSpec that scope is plumbed through to the mount source.
	agentDef, ok := agent.Lookup(agentName)
	require.True(t, ok)

	require.NoError(t, layout.EnsureDirectories(agentName, profile))
	require.NoError(t, layout.EnsureAgentData(profile, agentDef.DataDirs, agentDef.DataFiles))

	plat := platform.Info{UID: os.Getuid(), GID: os.Getgid(), Hostname: "testhost", Username: "testuser"}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("spec_scope=%q", tc.scope), func(t *testing.T) {
			cfg := config.Config{
				Image: "alpine:latest",
				Tools: map[string]config.ToolDef{
					toolName: {
						Type:       "custom",
						Install:    "true",
						DataDir:    true,
						CacheScope: tc.scope,
						EnvVar:     "MYTOOL_DIR",
					},
				},
			}

			spec, err := container.BuildSpec(container.BuildParams{
				Config:   cfg,
				Agent:    agentDef,
				Profile:  profile,
				Layout:   layout,
				Platform: plat,
				HomeDir:  "/home/user",
			})
			require.NoError(t, err)

			wantTarget := "/usr/local/share/ai-shim/cache/" + toolName
			var foundMount *mount.Mount
			for i := range spec.Mounts {
				if spec.Mounts[i].Target == wantTarget {
					foundMount = &spec.Mounts[i]
					break
				}
			}
			require.NotNil(t, foundMount,
				"spec should have a mount for target %q", wantTarget)
			assert.Equal(t, tc.wantPath, foundMount.Source,
				"mount source for scope=%q should be at the expected host path", tc.scope)

			// Cleanup the created host dir.
			t.Cleanup(func() { os.RemoveAll(foundMount.Source) })
		})
	}
}

// TestToolDataDir_UVPersistence is a two-launch integration test proving that the
// uv tool cache persists across separate container runs via the data_dir bind mount.
//
// Launch 1 installs uv and writes a marker file into $UV_CACHE_DIR.
// Launch 2 verifies the marker file is still there without reinstalling.
func TestToolDataDir_UVPersistence(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow uv persistence test")
	}

	// Use a Docker-accessible directory under project tmp/ so the bind mount is
	// reachable by the Docker daemon (important for DinD/CI environments).
	root := dockerTempDir(t)
	layout := storage.NewLayout(root)

	agentDef, ok := agent.Lookup("opencode")
	require.True(t, ok)

	require.NoError(t, layout.EnsureDirectories(agentDef.Name, "default"))
	require.NoError(t, layout.EnsureAgentData("default", agentDef.DataDirs, agentDef.DataFiles))

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

	// Determine the host-side cache path once (both launches share it).
	uvCacheHost := storage.ToolCachePath(layout, "uv", "global", agentDef.Name, "default")
	t.Cleanup(func() { os.RemoveAll(uvCacheHost) })

	containerTarget := "/usr/local/share/ai-shim/cache/uv"
	markerInContainer := containerTarget + "/.marker"

	// --- Launch 1: install uv and write the marker file ---
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

		// Replace the entrypoint: keep the tool provisioning script that sets
		// UV_CACHE_DIR and installs uv, then run the verification command.
		toolScript := extractToolScript(t, spec.Entrypoint[2])
		script := fmt.Sprintf(`%s
export PATH="$HOME/.local/bin:$PATH"
uv --version || { echo "ERROR: uv not available after install"; exit 1; }
echo "uv-installed" > %s
`, toolScript, markerInContainer)

		spec.Entrypoint = []string{"sh", "-c", script}
		spec.Cmd = nil
		spec.TTY = false
		spec.Stdin = false

		result, err := runner.Run(ctx, spec)
		require.NoError(t, err, "launch 1 should not error")
		assert.Equal(t, 0, result.ExitCode, "launch 1 should exit 0 (uv installed and marker written)")
	}

	// Verify the marker file is present on the host before launch 2.
	markerHost := filepath.Join(uvCacheHost, ".marker")
	data, err := os.ReadFile(markerHost)
	require.NoError(t, err, "marker file should exist on host after launch 1")
	assert.Equal(t, "uv-installed\n", string(data), "marker file should contain expected content")

	// --- Launch 2: verify the marker file persists ---
	{
		ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
		defer cancel()

		runner, err := container.NewRunner(ctx)
		require.NoError(t, err)
		defer runner.Close()

		spec, err := container.BuildSpec(container.BuildParams{
			Config:   cfg,
			Agent:    agentDef,
			Profile:  "default",
			Layout:   layout,
			Platform: plat,
			HomeDir:  "/home/user",
		})
		require.NoError(t, err)

		script := fmt.Sprintf(`test -f %s && cat %s`, markerInContainer, markerInContainer)
		spec.Entrypoint = []string{"sh", "-c", script}
		spec.Cmd = nil
		spec.TTY = false
		spec.Stdin = false

		result, err := runner.Run(ctx, spec)
		require.NoError(t, err, "launch 2 should not error")
		assert.Equal(t, 0, result.ExitCode,
			"launch 2 should exit 0 — marker file should persist across container launches")
	}
}

// TestToolDataDir_NVMPersistence is a two-launch integration test proving that the
// nvm installation (including node binaries) persists across separate container runs.
//
// Launch 1 installs nvm + node 22 and writes a marker file into $NVM_DIR.
// Launch 2 verifies node is still on PATH and the marker file still exists.
func TestToolDataDir_NVMPersistence(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow nvm persistence test")
	}

	root := dockerTempDir(t)
	layout := storage.NewLayout(root)

	agentDef, ok := agent.Lookup("opencode")
	require.True(t, ok)

	require.NoError(t, layout.EnsureDirectories(agentDef.Name, "default"))
	require.NoError(t, layout.EnsureAgentData("default", agentDef.DataDirs, agentDef.DataFiles))

	nvmInstall := `if [ ! -d "$NVM_DIR" ]; then
  curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | NVM_DIR="$NVM_DIR" bash
fi
bash -c ". $NVM_DIR/nvm.sh && nvm install 22 && nvm alias default 22" || true
export PATH="$NVM_DIR/versions/node/$(ls $NVM_DIR/versions/node/ | tail -1)/bin:$PATH"`

	cfg := config.Config{
		Image: container.DefaultImage,
		Tools: map[string]config.ToolDef{
			"nvm": {
				Type:    "custom",
				DataDir: true,
				EnvVar:  "NVM_DIR",
				Install: nvmInstall,
			},
		},
	}

	plat := platform.Detect()

	nvmCacheHost := storage.ToolCachePath(layout, "nvm", "", agentDef.Name, "default")
	t.Cleanup(func() { os.RemoveAll(nvmCacheHost) })

	containerTarget := "/usr/local/share/ai-shim/cache/nvm"
	markerInContainer := containerTarget + "/.marker"

	// --- Launch 1: install nvm + node and write the marker ---
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

		toolScript := extractToolScript(t, spec.Entrypoint[2])
		script := fmt.Sprintf(`%s
export PATH="$NVM_DIR/versions/node/$(ls $NVM_DIR/versions/node/ | tail -1)/bin:$PATH"
node --version || { echo "ERROR: node not available after nvm install"; exit 1; }
echo "node-ok" > %s
`, toolScript, markerInContainer)

		spec.Entrypoint = []string{"sh", "-c", script}
		spec.Cmd = nil
		spec.TTY = false
		spec.Stdin = false

		result, err := runner.Run(ctx, spec)
		require.NoError(t, err, "launch 1 should not error")
		assert.Equal(t, 0, result.ExitCode, "launch 1 should exit 0 (nvm+node installed and marker written)")
	}

	// Verify marker on host.
	markerHost := filepath.Join(nvmCacheHost, ".marker")
	data, err := os.ReadFile(markerHost)
	require.NoError(t, err, "marker file should exist on host after launch 1")
	assert.Equal(t, "node-ok\n", string(data), "marker file should contain expected content")

	// --- Launch 2: verify marker persists and node is still usable ---
	{
		ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
		defer cancel()

		runner, err := container.NewRunner(ctx)
		require.NoError(t, err)
		defer runner.Close()

		spec, err := container.BuildSpec(container.BuildParams{
			Config:   cfg,
			Agent:    agentDef,
			Profile:  "default",
			Layout:   layout,
			Platform: plat,
			HomeDir:  "/home/user",
		})
		require.NoError(t, err)

		// NVM_DIR is set by the tool provisioning preamble; we just need to
		// add the node bin dir to PATH and verify the marker.
		toolScript := extractToolScript(t, spec.Entrypoint[2])
		script := fmt.Sprintf(`%s
export PATH="$NVM_DIR/versions/node/$(ls $NVM_DIR/versions/node/ | tail -1)/bin:$PATH"
test -f %s && node --version
`, toolScript, markerInContainer)

		spec.Entrypoint = []string{"sh", "-c", script}
		spec.Cmd = nil
		spec.TTY = false
		spec.Stdin = false

		result, err := runner.Run(ctx, spec)
		require.NoError(t, err, "launch 2 should not error")
		assert.Equal(t, 0, result.ExitCode,
			"launch 2 should exit 0 — nvm cache and marker should persist across container launches")
	}
}

// extractToolScript extracts the tool-provisioning preamble from the full
// entrypoint script. It returns all lines up to (but not including) the first
// line that begins with the agent install section. This preserves the
// "export ENV_VAR=..." and custom install lines for use in override scripts.
//
// If the separator cannot be found it returns the full script unchanged so
// tests degrade gracefully rather than silently drop the preamble.
func extractToolScript(t *testing.T, fullScript string) string {
	t.Helper()
	// The agent install section starts after the tool provisioning block.
	// We look for the cache/update-interval logic that follows the tools block.
	// A reliable separator is the "# Agent install" comment or the first
	// occurrence of the update-interval / last-update logic.
	separators := []string{
		"# Check last update",
		"last_update=",
		"update_interval=",
		"exec ",
	}
	for _, sep := range separators {
		if idx := strings.Index(fullScript, sep); idx >= 0 {
			return fullScript[:idx]
		}
	}
	// Fallback: return full script; the test may still pass or fail clearly.
	return fullScript
}
