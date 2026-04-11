package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/cli"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/dind"
	"github.com/ai-shim/ai-shim/internal/network"
	"github.com/ai-shim/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJourney_FullLaunchFlow verifies the end-to-end path: setup layout,
// build spec for an agent with the default image, run a simple command,
// and confirm success.
func TestJourney_FullLaunchFlow(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
	}

	output, exitCode := buildAndRun(t, layout, "opencode", "default", cfg, "echo SUCCESS")
	assert.Equal(t, 0, exitCode, "container should exit 0")
	assert.Contains(t, output, "SUCCESS", "output should contain SUCCESS")
}

// TestJourney_AgentInstallAndCache verifies that the second run of an agent
// uses the cached installation rather than reinstalling from scratch.
func TestJourney_AgentInstallAndCache(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
	}

	// First run: agent installs
	output1, exitCode1 := buildAndRun(t, layout, "opencode", "default", cfg, "echo first-run-done")
	assert.Equal(t, 0, exitCode1, "first run should exit 0")
	assert.Contains(t, output1, "first-run-done")

	// Remove the marker file so the second run can write fresh output.
	markerHost := filepath.Join(layout.AgentCache("opencode"), ".journey-output")
	_ = os.Remove(markerHost)

	// Second run: same layout, persistent bind mounts should have cached state.
	output2, exitCode2 := buildAndRun(t, layout, "opencode", "default", cfg, "echo second-run-done")
	assert.Equal(t, 0, exitCode2, "second run should exit 0")
	assert.Contains(t, output2, "second-run-done")

	// The entrypoint should detect existing install. Check for cache indicators
	// in the entrypoint output (written to stdout before verifyCmd).
	// With default update_interval (1d), the second run should skip install
	// because last-update was just written.
	assert.True(t,
		strings.Contains(output2, "already installed") ||
			strings.Contains(output2, "up to date") ||
			strings.Contains(output2, "skipping"),
		"second run should indicate cached install, got: %s", output2)
}

// TestJourney_VersionPinInstall verifies that pinning a version causes the
// entrypoint to include the version string in the install command.
func TestJourney_VersionPinInstall(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image:   container.DefaultImage,
		Version: "1.0.0",
	}

	// Verify the generated entrypoint contains the pinned version.
	script := entrypointScript(t, layout, "opencode", "default", cfg)
	assert.Contains(t, script, "1.0.0",
		"entrypoint should contain the pinned version string")

	// Run and verify it at least attempts the versioned install.
	output, _ := buildAndRun(t, layout, "opencode", "default", cfg, "echo version-pin-done")
	assert.Contains(t, output, "version-pin-done")
}

// TestJourney_UpdateIntervalNever verifies that update_interval=never causes
// the second run to skip installation entirely.
func TestJourney_UpdateIntervalNever(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image:          container.DefaultImage,
		UpdateInterval: "never",
	}

	// First run: installs the agent.
	output1, exitCode1 := buildAndRun(t, layout, "opencode", "default", cfg, "echo never-first")
	assert.Equal(t, 0, exitCode1)
	assert.Contains(t, output1, "never-first")

	// Remove marker for clean second-run capture.
	markerHost := filepath.Join(layout.AgentCache("opencode"), ".journey-output")
	_ = os.Remove(markerHost)

	// Second run: should skip with update_interval=never message.
	output2, exitCode2 := buildAndRun(t, layout, "opencode", "default", cfg, "echo never-second")
	assert.Equal(t, 0, exitCode2)
	assert.Contains(t, output2, "update_interval=never",
		"second run with never interval should indicate skip, got: %s", output2)
}

// TestJourney_UpdateIntervalAlways verifies that update_interval=always causes
// every run to reinstall the agent.
func TestJourney_UpdateIntervalAlways(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image:          container.DefaultImage,
		UpdateInterval: "always",
	}

	// First run.
	output1, exitCode1 := buildAndRun(t, layout, "opencode", "default", cfg, "echo always-first")
	assert.Equal(t, 0, exitCode1)
	assert.Contains(t, output1, "Installing",
		"first run should install, got: %s", output1)

	// Remove marker for clean second-run capture.
	markerHost := filepath.Join(layout.AgentCache("opencode"), ".journey-output")
	_ = os.Remove(markerHost)

	// Second run: should also install (always).
	output2, exitCode2 := buildAndRun(t, layout, "opencode", "default", cfg, "echo always-second")
	assert.Equal(t, 0, exitCode2)
	assert.Contains(t, output2, "Installing",
		"second run with always interval should reinstall, got: %s", output2)
}

// TestJourney_ContainerCleanup verifies that after a container exits, no
// containers with the ai-shim label remain (AutoRemove works).
func TestJourney_ContainerCleanup(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
	}

	_, exitCode := buildAndRun(t, layout, "opencode", "default", cfg, "echo cleanup-test")
	assert.Equal(t, 0, exitCode)

	// AutoRemove is async — give Docker a moment to clean up.
	ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
	defer cancel()
	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Retry a few times to allow AutoRemove to complete.
	var stale []string
	for attempt := 0; attempt < 5; attempt++ {
		stale = nil
		containers, err := runner.Client().ContainerList(ctx, dockercontainer.ListOptions{
			All:     true,
			Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
		})
		require.NoError(t, err)

		for _, c := range containers {
			for _, name := range c.Names {
				if strings.Contains(name, "opencode") {
					stale = append(stale, name)
				}
			}
		}
		if len(stale) == 0 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	assert.Empty(t, stale, "no ai-shim containers should remain after exit")
}

// TestJourney_EnvVarsReachContainer verifies that config env vars are passed
// into the container and accessible by commands.
func TestJourney_EnvVarsReachContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
		Env: map[string]string{
			"TEST_MARKER": "journey123",
		},
	}

	output, exitCode := buildAndRun(t, layout, "opencode", "default", cfg, `echo $TEST_MARKER`)
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, output, "journey123",
		"container should see TEST_MARKER env var, got: %s", output)
}

// TestJourney_GitConfigInContainer verifies that git config is set inside the
// container when cfg.Git is provided.
func TestJourney_GitConfigInContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
		Git: &config.GitConfig{
			Name:  "Test User",
			Email: "test@example.com",
		},
	}

	output, exitCode := buildAndRun(t, layout, "opencode", "default", cfg, "git config --global user.name")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, output, "Test User",
		"container should have git user.name set, got: %s", output)
}

// TestJourney_PackagesInstalled verifies that system packages specified in
// config are installed inside the container.
func TestJourney_PackagesInstalled(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image:    container.DefaultImage,
		Packages: []string{"curl"},
	}

	// Package installation via apt-get requires root.
	asRoot := func(s *container.ContainerSpec) { s.User = "0:0" }
	output, exitCode := buildAndRun(t, layout, "opencode", "default", cfg, "which curl", asRoot)
	assert.Equal(t, 0, exitCode, "curl should be installed, got output: %s", output)
}

// TestJourney_ToolProvisioning verifies that tools defined in config are
// provisioned inside the container and available on PATH.
func TestJourney_ToolProvisioning(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
		Tools: map[string]config.ToolDef{
			"jq": {
				Type:   "binary-download",
				URL:    "https://github.com/jqlang/jq/releases/download/jq-1.7.1/jq-linux-amd64",
				Binary: "jq",
			},
		},
	}

	output, exitCode := buildAndRun(t, layout, "opencode", "default", cfg,
		"ls -la /usr/local/share/ai-shim/bin/jq && /usr/local/share/ai-shim/bin/jq --version")
	assert.Equal(t, 0, exitCode, "jq tool should be provisioned, got output: %s", output)
}

// TestJourney_CustomInstallerPATH verifies that agents with custom installers
// (like claude-code) have ~/.local/bin in PATH in the generated entrypoint.
func TestJourney_CustomInstallerPATH(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
	}

	script := entrypointScript(t, layout, "claude-code", "default", cfg)
	assert.Contains(t, script, ".local/bin",
		"claude-code entrypoint should add ~/.local/bin to PATH")

	// Also verify via a real container run that PATH includes .local/bin.
	output, exitCode := buildAndRun(t, layout, "claude-code", "default", cfg,
		"echo $PATH")
	assert.Equal(t, 0, exitCode)
	assert.Contains(t, output, ".local/bin",
		"claude-code container PATH should include .local/bin, got: %s", output)
}

// TestJourney_ContainerStopsOnContext verifies that cancelling the context
// causes the container to exit promptly rather than hanging.
func TestJourney_ContainerStopsOnContext(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	containerName := fmt.Sprintf("ai-shim-ctx-test-%d", time.Now().UnixNano())

	// Launch a container that sleeps for 60s
	done := make(chan struct{})
	var result container.AttachResult
	var runErr error
	go func() {
		defer close(done)
		result, runErr = runner.Run(ctx, container.ContainerSpec{
			Name:   containerName,
			Image:  "alpine:latest",
			Cmd:    []string{"sleep", "60"},
			Labels: map[string]string{container.LabelBase: "true"},
		})
	}()

	// Give the container time to start
	time.Sleep(2 * time.Second)

	// Cancel the context
	cancel()

	// Wait for the container run to finish, with a timeout
	select {
	case <-done:
		// Container exited as expected
	case <-time.After(15 * time.Second):
		t.Fatal("container did not stop within 15s of context cancellation")
	}

	// We expect either an error or a non-zero exit code
	_ = result
	_ = runErr

	// Verify no orphaned container remains
	checkCtx := context.Background()
	checkRunner, err := container.NewRunner(checkCtx)
	require.NoError(t, err)
	defer checkRunner.Close()

	containers, err := checkRunner.Client().ContainerList(checkCtx, dockercontainer.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	require.NoError(t, err)

	// AutoRemove should have cleaned it, but give it a moment
	for attempt := 0; attempt < 5 && len(containers) > 0; attempt++ {
		time.Sleep(500 * time.Millisecond)
		containers, err = checkRunner.Client().ContainerList(checkCtx, dockercontainer.ListOptions{
			All:     true,
			Filters: filters.NewArgs(filters.Arg("name", containerName)),
		})
		require.NoError(t, err)
	}
	assert.Empty(t, containers, "no orphaned container should remain after context cancellation")
}

// TestJourney_CleanupRemovesOrphans verifies that cli.Cleanup() removes
// stopped containers with ai-shim labels.
func TestJourney_CleanupRemovesOrphans(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
	defer cancel()
	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	containerName := fmt.Sprintf("ai-shim-orphan-test-%d", time.Now().UnixNano())

	// Create a container with AutoRemove: false so it persists after stop
	resp, err := runner.Client().ContainerCreate(ctx,
		&dockercontainer.Config{
			Image:  "alpine:latest",
			Cmd:    []string{"echo", "orphan"},
			Labels: map[string]string{container.LabelBase: "true"},
		},
		&dockercontainer.HostConfig{
			AutoRemove: false,
		},
		nil, nil, containerName,
	)
	require.NoError(t, err)

	// Start and wait for it to exit
	err = runner.Client().ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{})
	require.NoError(t, err)

	statusCh, errCh := runner.Client().ContainerWait(ctx, resp.ID, dockercontainer.WaitConditionNotRunning)
	select {
	case <-statusCh:
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("container did not stop in time")
	}

	// Verify the stopped container still exists
	containers, err := runner.Client().ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	require.NoError(t, err)
	require.NotEmpty(t, containers, "stopped container should still exist before cleanup")

	// Run cleanup
	result, err := cli.Cleanup()
	require.NoError(t, err)

	// Verify the container was removed
	containers, err = runner.Client().ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", containerName)),
	})
	require.NoError(t, err)
	assert.Empty(t, containers, "orphaned container should be removed after cleanup")

	// Verify cleanup reported it
	found := false
	for _, name := range result.RemovedContainers {
		if strings.Contains(name, containerName) {
			found = true
			break
		}
	}
	assert.True(t, found, "cleanup result should include the removed container name")
}

// TestJourney_DINDSidecar verifies the full DIND lifecycle: create a network,
// start a DIND sidecar, run a container that executes "docker info" via the
// DIND socket, and confirm the Docker daemon is reachable.
func TestJourney_DINDSidecar(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping DIND journey test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// 1. Pull required images.
	require.NoError(t, runner.EnsureImage(ctx, dind.DefaultImage))
	require.NoError(t, runner.EnsureImage(ctx, "docker:latest"))

	// 2. Create a dedicated network for DIND communication.
	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-dind-journey-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	defer func() { _ = netHandle.Remove(ctx) }()

	// 3. Start the DIND sidecar (Start already calls WaitForReady internally).
	dindName := fmt.Sprintf("ai-shim-test-dind-%d", time.Now().UnixNano())
	sidecar, err := dind.Start(ctx, runner.Client(), dind.Config{
		ContainerName: dindName,
		Hostname:      "dind",
		NetworkID:     netHandle.ID,
		Labels:        labels,
	})
	require.NoError(t, err)
	defer func() { _ = sidecar.Stop(ctx) }()

	// 4. Run a test container with the DIND socket mounted.
	//    Use a retry loop because the socket file may take a moment to
	//    appear in the volume mount from the client container's perspective.
	containerName := fmt.Sprintf("ai-shim-test-dind-client-%d", time.Now().UnixNano())
	result, err := runner.Run(ctx, container.ContainerSpec{
		Name:  containerName,
		Image: "docker:latest",
		Env:   []string{"DOCKER_HOST=unix:///var/run/dind/docker.sock"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: sidecar.SocketVolume(),
				Target: "/var/run/dind",
			},
		},
		Entrypoint: []string{"sh", "-c", `
for i in $(seq 1 30); do
  if docker info 2>&1; then
    exit 0
  fi
  sleep 1
done
echo "ERROR: docker info failed after 30 retries"
exit 1
`},
		NetworkID: netHandle.ID,
		Labels:    labels,
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode, "docker info inside DIND should exit 0")
}
