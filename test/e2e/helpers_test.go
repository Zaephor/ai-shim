package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/cli"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/stretchr/testify/require"
)

// Per-container test timeouts. These bound each test's Runner.Run call so a
// hung container surfaces as a clear "context deadline exceeded" failure
// rather than blocking the whole test binary until the global -timeout fires.
//
// Linux containers finish in well under a minute; macOS via Colima is ~5x
// slower but still well within these budgets. Adjust if Colima regresses.
const (
	// buildAndRunTimeout bounds buildAndRun's per-container lifetime.
	// Used by journey tests that run the real agent entrypoint.
	buildAndRunTimeout = 5 * time.Minute

	// quickRunTimeout bounds short single-command containers (hostname
	// checks, mount checks, echo, etc.). These finish in seconds on Linux.
	quickRunTimeout = 2 * time.Minute

	// installRunTimeout bounds agent-install and tool-verification runs.
	// These are legitimately slow on Colima — TestDefaultImage_HasRequiredTools
	// was observed at 395s on macOS CI — so the budget has to be generous.
	installRunTimeout = 10 * time.Minute
)

// setupJourneyLayout creates a temp root under the Docker-accessible project
// tmp/ directory, initializes the ai-shim directory structure via cli.Init(),
// and returns the resulting storage Layout.
func setupJourneyLayout(t *testing.T) storage.Layout {
	t.Helper()
	root := dockerTempDir(t)
	layout := storage.NewLayout(root)
	require.NoError(t, cli.Init(layout), "cli.Init should succeed")
	return layout
}

// buildAndRun builds a ContainerSpec from config and agent, replaces the final
// "exec <binary>" line in the generated entrypoint with verifyCmd, runs the
// container, and captures stdout via a marker file in the bind-mounted cache
// directory. Returns (stdout, exitCode).
// specOverride allows tests to tweak the ContainerSpec before running.
type specOverride func(*container.ContainerSpec)

func buildAndRun(t *testing.T, layout storage.Layout, agentName, profile string, cfg config.Config, verifyCmd string, overrides ...specOverride) (string, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), buildAndRunTimeout)
	defer cancel()

	def, ok := agent.Lookup(agentName)
	require.True(t, ok, "agent %q must exist in registry", agentName)

	require.NoError(t, layout.EnsureDirectories(agentName, profile))
	require.NoError(t, layout.EnsureAgentData(profile, def.DataDirs, def.DataFiles))

	plat := platform.Detect()

	spec := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    def,
		Profile:  profile,
		Layout:   layout,
		Platform: plat,
		HomeDir:  "/home/user",
	})

	// Modify the entrypoint: keep install/cache logic but replace exec line
	// with verifyCmd that writes output to a marker file.
	entryScript := spec.Entrypoint[2] // "sh", "-c", <script>
	execLine := fmt.Sprintf("exec %s", def.Binary)
	if idx := strings.LastIndex(entryScript, execLine); idx >= 0 {
		entryScript = entryScript[:idx]
	}

	// Use the agent cache dir as output channel — it's bind-mounted.
	// Wrap the ENTIRE script (install logic + verifyCmd) so all output
	// (install messages, cache-hit messages, verifyCmd output) is captured.
	markerTarget := fmt.Sprintf("/usr/local/share/ai-shim/agents/%s/cache/.journey-output", agentName)
	modifiedScript := fmt.Sprintf(`exec > %s 2>&1
%s
# Journey test: run verifyCmd
%s
`, markerTarget, entryScript, verifyCmd)

	spec.Entrypoint = []string{"sh", "-c", modifiedScript}
	spec.Cmd = nil
	// Disable TTY/stdin for test capture
	spec.TTY = false
	spec.Stdin = false

	// Apply any test-specific overrides to the spec.
	for _, fn := range overrides {
		fn(&spec)
	}

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Ensure image is available
	require.NoError(t, runner.EnsureImage(ctx, spec.Image))

	exitCode, err := runner.Run(ctx, spec)
	require.NoError(t, err, "container Run should not return an error")

	// Read captured output from the marker file on the host side.
	markerHost := filepath.Join(layout.AgentCache(agentName), ".journey-output")
	data, readErr := os.ReadFile(markerHost)
	output := ""
	if readErr == nil {
		output = string(data)
	}

	return output, exitCode
}

// buildAndRunRaw is like buildAndRun but returns the full entrypoint script
// without modification, for inspection tests that don't need to run a container.
func entrypointScript(t *testing.T, layout storage.Layout, agentName, profile string, cfg config.Config) string {
	t.Helper()

	def, ok := agent.Lookup(agentName)
	require.True(t, ok)

	updateInterval, _ := config.ParseUpdateInterval(cfg.UpdateInterval)

	return install.GenerateEntrypoint(install.EntrypointParams{
		InstallType:    def.InstallType,
		Package:        def.Package,
		Binary:         def.Binary,
		Version:        cfg.Version,
		AgentName:      agentName,
		UpdateInterval: updateInterval,
	})
}
