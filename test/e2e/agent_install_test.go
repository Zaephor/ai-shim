package e2e

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_AllAgents_InstallAndLaunch verifies that every built-in agent can be
// installed inside the default container image and that the resulting binary
// is callable. Agents will likely fail with auth/config errors — that's expected.
// The test proves the install pipeline works end-to-end.
func TestE2E_AllAgents_InstallAndLaunch(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow E2E agent install tests")
	}
	if runtime.GOOS == "darwin" {
		t.Skip("skipping all-agent install on macOS — already covered by Linux e2e, too slow via Colima")
	}

	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Pull the default image once
	err = runner.EnsureImage(ctx, container.DefaultImage)
	require.NoError(t, err, "must be able to pull default image for agent tests")

	for name, def := range agent.All() {
		t.Run(name, func(t *testing.T) {
			// Generate the real install script
			entrypoint := install.GenerateEntrypoint(install.EntrypointParams{
				InstallType: def.InstallType,
				Package:     def.Package,
				Binary:      def.Binary,
				AgentName:   name,
			})

			// Replace "exec <binary>" at the end with verification logic.
			// The entrypoint ends with "exec <binary>\n".
			// We keep the install part but replace the exec with our checks.
			execLine := "exec " + def.Binary
			installPart := entrypoint
			if idx := strings.LastIndex(installPart, execLine); idx >= 0 {
				installPart = installPart[:idx]
			}

			// Remove "set -e" from the install part. Custom install scripts
			// (claude-code, goose) may fail in post-install config steps even
			// though the binary was installed successfully. We handle errors
			// explicitly via exit code 99 for missing binaries.
			installPart = strings.Replace(installPart, "set -e\n", "set +e\n", 1)

			verifyScript := fmt.Sprintf(`%s
# Add common install locations to PATH (custom installers often use ~/.local/bin)
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"

# Verify the binary was installed
echo "Checking for %s binary..."
if ! command -v %s >/dev/null 2>&1; then
    echo "FAIL: %s binary not found after install"
    ls -la /usr/local/bin/ 2>/dev/null | head -20 || true
    ls -la "$HOME/.local/bin/" 2>/dev/null | head -20 || true
    exit 99
fi
echo "SUCCESS: %s installed at $(command -v %s)"

# Try to get version or help (brief attempt, expect failure without config)
echo "Attempting to run %s..."
timeout 15 %s --version 2>&1 || timeout 15 %s --help 2>&1 || timeout 15 %s 2>&1 || true
echo "Agent execution completed (exit from agent is expected without config)"
exit 0
`, installPart, def.Binary, def.Binary, def.Binary, def.Binary, def.Binary, def.Binary, def.Binary, def.Binary, def.Binary)

			// For uv-based agents, prepend uv installation since the default
			// image may not have uv pre-installed.
			if def.InstallType == "uv" {
				uvInstall := `echo "Installing uv..."
curl -LsSf https://astral.sh/uv/install.sh | sh
export PATH="$HOME/.local/bin:$HOME/.cargo/bin:$PATH"
`
				verifyScript = strings.Replace(verifyScript, "set +e\n", "set +e\n"+uvInstall, 1)
			}

			exitCode, err := runner.Run(ctx, container.ContainerSpec{
				Image:      container.DefaultImage,
				Entrypoint: []string{"sh", "-c", verifyScript},
				Labels:     map[string]string{container.LabelBase: "test"},
				Name:       fmt.Sprintf("e2e-install-%s-%s", name, randomTestSuffix()),
			})
			require.NoError(t, err, "%s: container execution error", name)
			assert.NotEqual(t, 99, exitCode, "%s: binary not found after install (install failed)", name)
		})
	}
}

// randomTestSuffix returns a short suffix for unique container names in tests.
func randomTestSuffix() string {
	return fmt.Sprintf("%d", time.Now().UnixNano()%100000)
}
