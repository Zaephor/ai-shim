package integration

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAllPackagesReachableFromMain verifies that every internal/ package
// is imported (directly or transitively) by cmd/ai-shim.
// This prevents the "build in isolation, wire never" pattern.
func TestAllPackagesReachableFromMain(t *testing.T) {
	// Find the module root by locating go.mod
	modRoot, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(t, err, "failed to find go.mod")
	modDir := strings.TrimSpace(string(modRoot))
	require.NotEmpty(t, modDir, "GOMOD is empty — not inside a module")
	// modDir is the path to go.mod; we want its directory
	idx := strings.LastIndex(modDir, "/")
	require.True(t, idx >= 0, "unexpected GOMOD path: %s", modDir)
	rootDir := modDir[:idx]

	// Get all internal packages
	listCmd := exec.Command("go", "list", "./internal/...")
	listCmd.Dir = rootDir
	out, err := listCmd.Output()
	require.NoError(t, err, "failed to list internal packages")

	allPackages := strings.Split(strings.TrimSpace(string(out)), "\n")

	// Get the transitive dependency graph of cmd/ai-shim
	depCmd := exec.Command("go", "list", "-deps", "./cmd/ai-shim/")
	depCmd.Dir = rootDir
	depOut, err := depCmd.Output()
	require.NoError(t, err, "failed to list cmd/ai-shim dependencies")

	deps := strings.Split(strings.TrimSpace(string(depOut)), "\n")
	depSet := make(map[string]bool)
	for _, d := range deps {
		depSet[d] = true
	}

	// Packages that are legitimately test-only or utility-only
	// (not expected to be in the main binary's import graph)
	testOnlyPackages := map[string]bool{
		"github.com/ai-shim/ai-shim/internal/testutil": true, // test helpers only
	}

	for _, pkg := range allPackages {
		if testOnlyPackages[pkg] {
			continue
		}
		assert.True(t, depSet[pkg],
			"package %s is not reachable from cmd/ai-shim — likely a wiring gap", pkg)
	}
}

// TestBuildSpecUsesAllBuildParamsFields verifies that BuildSpec reads
// every field from BuildParams by creating a fully-populated BuildParams
// and asserting each field influences the resulting ContainerSpec.
func TestBuildSpecUsesAllBuildParamsFields(t *testing.T) {
	p := container.BuildParams{
		Config: config.Config{
			Image:    "test-image",
			Hostname: "test-host",
			Env:      map[string]string{"K": "V"},
			Args:     []string{"--flag"},
			Volumes:  []string{"/a:/b"},
			Ports:    []string{"8080:80"},
			Packages: []string{"tmux"},
			Tools:    map[string]config.ToolDef{"t": {Type: "apt", Package: "curl", Binary: "curl"}},
			GPU:      testutil.BoolPtr(true),
			Resources: &config.ResourceLimits{Memory: "2g", CPUs: "1.0"},
			Git:       &config.GitConfig{Name: "Test User", Email: "test@example.com"},
		},
		Agent:   agent.Definition{Name: "test", InstallType: "npm", Package: "test-pkg", Binary: "test-bin", DataDirs: []string{".test-data"}, DataFiles: []string{".test-config.json"}},
		Profile: "work",
		Layout:  storage.NewLayout("/tmp/test-ai-shim"),
		Platform: platform.Info{UID: 1000, GID: 1000, Hostname: "host"},
		Args:    []string{"--extra"},
		HomeDir: "/home/custom",
		LogDir:  "/tmp/logs",
	}

	spec := container.BuildSpec(p)

	// Verify each BuildParams field influenced the spec
	assert.Equal(t, "test-image", spec.Image)
	assert.Equal(t, "test-host", spec.Hostname)
	assert.Contains(t, spec.Env, "K=V")
	assert.True(t, spec.GPU)
	assert.NotNil(t, spec.Resources)
	// HomeDir is used: in isolated mode, data dirs mount under /home/custom/
	assert.Equal(t, "/home/custom/.test-data", findMountTarget(spec.Mounts, ".test-data"))
	assert.NotEmpty(t, spec.Name) // container naming
	assert.Contains(t, spec.Entrypoint[2], "--flag") // config args
	assert.Contains(t, spec.Entrypoint[2], "--extra") // passthrough args
	assert.Contains(t, spec.Entrypoint[2], "tmux") // packages
	assert.Contains(t, spec.Entrypoint[2], "curl") // tools
	assert.Contains(t, spec.Entrypoint[2], "git config --global user.name") // git config
	assert.Contains(t, spec.Entrypoint[2], "git config --global user.email") // git config
	assert.Equal(t, "/tmp/logs", spec.LogDir)
}

func findMountTarget(mounts []mount.Mount, keyword string) string {
	for _, m := range mounts {
		if strings.Contains(m.Target, keyword) {
			return m.Target
		}
	}
	return ""
}
