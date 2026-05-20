package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProfileDockerDev verifies that configs/examples/profiles/docker-dev.yaml
// parses without warnings and produces a valid container spec with DIND enabled,
// cache enabled, and the expected tools (lazydocker, jq, yq).
func TestProfileDockerDev(t *testing.T) {
	path := filepath.Join(projectRoot(), "configs", "examples", "profiles", "docker-dev.yaml")

	cfg, warnings, err := config.LoadFileStrict(path)
	require.NoError(t, err, "LoadFileStrict should succeed for docker-dev.yaml")
	require.Empty(t, warnings, "docker-dev.yaml should have no unknown-key warnings")

	// DIND and cache flags
	assert.True(t, cfg.IsDINDEnabled(), "docker-dev should have DIND enabled")
	assert.True(t, cfg.IsCacheEnabled(), "docker-dev should have dind_cache enabled")

	// Tools
	require.Len(t, cfg.Tools, 3, "docker-dev should have exactly 3 tools")
	assert.Equal(t, "tar-extract", cfg.Tools["lazydocker"].Type)
	assert.Equal(t, "binary-download", cfg.Tools["jq"].Type)
	assert.Equal(t, "binary-download", cfg.Tools["yq"].Type)

	// Build a container spec and verify it has mounts and an entrypoint.
	spec := buildSpecFromConfig(t, &cfg, "docker-dev")

	assert.NotEmpty(t, spec.Mounts, "spec should have mounts")
	require.Len(t, spec.Entrypoint, 3, "entrypoint should be [sh, -c, <script>]")
	assert.NotEmpty(t, spec.Entrypoint[2], "entrypoint script should not be empty")
}

// TestProfilePython verifies that configs/examples/profiles/python.yaml parses
// correctly, has the expected tools (uv with data_dir, ruff as tar-extract,
// pyright, pre-commit), and that BuildSpec produces a spec with the UV_CACHE_DIR
// export and a bind mount targeting the uv cache path.
func TestProfilePython(t *testing.T) {
	path := filepath.Join(projectRoot(), "configs", "examples", "profiles", "python.yaml")

	cfg, warnings, err := config.LoadFileStrict(path)
	require.NoError(t, err, "LoadFileStrict should succeed for python.yaml")
	require.Empty(t, warnings, "python.yaml should have no unknown-key warnings")

	// uv — persistent cache tool
	uv, ok := cfg.Tools["uv"]
	require.True(t, ok, "python profile should have a uv tool")
	assert.Equal(t, "custom", uv.Type)
	assert.True(t, uv.DataDir, "uv should have data_dir=true")
	assert.Equal(t, "UV_CACHE_DIR", uv.EnvVar, "uv should export UV_CACHE_DIR")

	// ruff — tar-extract tool
	ruff, ok := cfg.Tools["ruff"]
	require.True(t, ok, "python profile should have a ruff tool")
	assert.Equal(t, "tar-extract", ruff.Type)

	// pyright and pre-commit — custom tools
	_, ok = cfg.Tools["pyright"]
	assert.True(t, ok, "python profile should have a pyright tool")
	_, ok = cfg.Tools["pre-commit"]
	assert.True(t, ok, "python profile should have a pre-commit tool")

	// Build a spec and verify wiring.
	spec := buildSpecFromConfig(t, &cfg, "python")

	require.Len(t, spec.Entrypoint, 3, "entrypoint should be [sh, -c, <script>]")
	script := spec.Entrypoint[2]

	// The entrypoint should contain the UV_CACHE_DIR export pointing at the
	// container-side cache mount.
	assert.Contains(t, script, `export UV_CACHE_DIR="/usr/local/share/ai-shim/cache/uv"`,
		"entrypoint should export UV_CACHE_DIR pointing at the cache mount path")

	// Verify a bind mount targets the uv cache path.
	wantTarget := "/usr/local/share/ai-shim/cache/uv"
	var found *mount.Mount
	for i := range spec.Mounts {
		if spec.Mounts[i].Target == wantTarget {
			found = &spec.Mounts[i]
			break
		}
	}
	require.NotNil(t, found, "spec should have a mount targeting %q", wantTarget)
	assert.Equal(t, mount.TypeBind, found.Type, "uv cache mount should be TypeBind")

	// Cleanup the host-side cache directory created by BuildSpec.
	t.Cleanup(func() { os.RemoveAll(found.Source) })
}

// TestProfileCiTesting verifies that configs/examples/profiles/ci-testing.yaml
// parses correctly with DIND, cache, resource limits (main container and DIND
// sidecar), and the expected tools (act, gh, jq, yq). BuildSpec should produce
// a spec with DIND-related configuration.
func TestProfileCiTesting(t *testing.T) {
	path := filepath.Join(projectRoot(), "configs", "examples", "profiles", "ci-testing.yaml")

	cfg, warnings, err := config.LoadFileStrict(path)
	require.NoError(t, err, "LoadFileStrict should succeed for ci-testing.yaml")
	require.Empty(t, warnings, "ci-testing.yaml should have no unknown-key warnings")

	// DIND and cache
	assert.True(t, cfg.IsDINDEnabled(), "ci-testing should have DIND enabled")
	assert.True(t, cfg.IsCacheEnabled(), "ci-testing should have dind_cache enabled")

	// Resource limits
	require.NotNil(t, cfg.Resources, "ci-testing should define resources")
	assert.Equal(t, "8g", cfg.Resources.Memory)
	assert.Equal(t, "4.0", cfg.Resources.CPUs)

	require.NotNil(t, cfg.DINDResources, "ci-testing should define dind_resources")
	assert.Equal(t, "4g", cfg.DINDResources.Memory)
	assert.Equal(t, "2.0", cfg.DINDResources.CPUs)

	// Tools
	for _, name := range []string{"act", "gh", "jq", "yq"} {
		_, ok := cfg.Tools[name]
		assert.True(t, ok, "ci-testing should have tool %q", name)
	}

	// Build a spec and verify DIND-related config.
	spec := buildSpecFromConfig(t, &cfg, "ci-testing")

	assert.NotEmpty(t, spec.Mounts, "spec should have mounts")
	require.Len(t, spec.Entrypoint, 3, "entrypoint should be [sh, -c, <script>]")
	assert.NotEmpty(t, spec.Entrypoint[2], "entrypoint script should not be empty")

	// The spec should carry the resource limits.
	require.NotNil(t, spec.Resources, "spec should have resource limits")
	assert.Equal(t, "8g", spec.Resources.Memory)
	assert.Equal(t, "4.0", spec.Resources.CPUs)
}

// buildSpecFromConfig is a test helper that constructs a BuildParams from the
// given config and profile name, calls BuildSpec, and returns the resulting
// ContainerSpec. It uses a temp directory for the storage layout and the
// "opencode" agent definition.
func buildSpecFromConfig(t *testing.T, cfg *config.Config, profile string) container.ContainerSpec {
	t.Helper()

	root := t.TempDir()
	layout := storage.NewLayout(root)

	agentDef, ok := agent.Lookup("opencode")
	require.True(t, ok, "opencode agent must exist in registry")

	require.NoError(t, layout.EnsureDirectories(agentDef.Name, profile))
	require.NoError(t, layout.EnsureAgentData(profile, agentDef.DataDirs, agentDef.DataFiles))

	plat := platform.Info{UID: os.Getuid(), GID: os.Getgid(), Hostname: "testhost", Username: "testuser"}

	spec, err := container.BuildSpec(container.BuildParams{
		Config:   *cfg,
		Agent:    agentDef,
		Profile:  profile,
		Layout:   layout,
		Platform: plat,
		HomeDir:  "/home/user",
	})
	require.NoError(t, err, "BuildSpec should succeed for profile %q", profile)

	return spec
}
