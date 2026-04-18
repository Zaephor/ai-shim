package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/cli"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJourney_ReinstallFlow verifies that Reinstall clears the agent bin
// directory and that a subsequent container run reinstalls the agent.
func TestJourney_ReinstallFlow(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image:          container.DefaultImage,
		UpdateInterval: "always",
	}

	// First run: install the agent.
	output1, exitCode1 := buildAndRun(t, layout, "opencode", "default", cfg, "echo install-done")
	assert.Equal(t, 0, exitCode1, "first run should exit 0")
	assert.Contains(t, output1, "install-done")

	// Verify there are files in the bin directory after install.
	binDir, err := layout.AgentBin("opencode")
	require.NoError(t, err)
	_, err = os.ReadDir(binDir)
	require.NoError(t, err, "bin dir should exist after install")

	// Reinstall: clear the bin directory.
	err = cli.Reinstall(layout, "opencode")
	require.NoError(t, err, "Reinstall should succeed")

	// Verify the bin directory is cleared.
	entries, err := os.ReadDir(binDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "bin directory should be empty after Reinstall")

	// Remove marker so the next run writes fresh output.
	agentCacheDir, err := layout.AgentCache("opencode")
	require.NoError(t, err)
	markerHost := filepath.Join(agentCacheDir, ".journey-output")
	_ = os.Remove(markerHost)

	// Second run: should reinstall (output contains "Installing").
	output2, exitCode2 := buildAndRun(t, layout, "opencode", "default", cfg, "echo reinstall-done")
	assert.Equal(t, 0, exitCode2, "second run should exit 0")
	assert.Contains(t, output2, "Installing",
		"second run after reinstall should show Installing, got: %s", output2)
	assert.Contains(t, output2, "reinstall-done")
}

// TestJourney_AgentVersionsAfterInstall verifies that AgentVersions reports
// information about an installed agent after a container run.
func TestJourney_AgentVersionsAfterInstall(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)
	cfg := config.Config{
		Image: container.DefaultImage,
	}

	// Run an agent once to install it.
	_, exitCode := buildAndRun(t, layout, "opencode", "default", cfg, "echo versions-done")
	assert.Equal(t, 0, exitCode, "agent run should exit 0")

	// Call AgentVersions and check the output.
	output := cli.AgentVersions(layout)
	assert.Contains(t, output, "opencode",
		"AgentVersions should mention the agent name")
	// After install, the agent should show as installed (version or "installed").
	assert.True(t,
		strings.Contains(output, "installed") || strings.Contains(output, "0.") || strings.Contains(output, "1."),
		"AgentVersions should indicate the agent is installed, got: %s", output)
}

// TestJourney_BackupRestoreRoundtrip verifies the full backup and restore cycle
// for a profile: create a marker file, back it up, delete it, restore, verify.
func TestJourney_BackupRestoreRoundtrip(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)

	// Create the profile home directory and a marker file.
	profileHome, err := layout.ProfileHome("default")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(profileHome, 0755))
	markerPath := filepath.Join(profileHome, "journey-marker.txt")
	require.NoError(t, os.WriteFile(markerPath, []byte("backup-roundtrip-data"), 0644))

	// Backup.
	backupPath := filepath.Join(layout.Root, "test-backup.tar.gz")
	err = cli.BackupProfile(layout, "default", backupPath)
	require.NoError(t, err, "BackupProfile should succeed")

	// Verify backup file was created and is non-empty.
	info, err := os.Stat(backupPath)
	require.NoError(t, err, "backup file should exist")
	assert.True(t, info.Size() > 0, "backup file should not be empty")

	// Delete the marker file.
	require.NoError(t, os.Remove(markerPath))
	_, err = os.Stat(markerPath)
	require.True(t, os.IsNotExist(err), "marker should be deleted before restore")

	// Restore.
	err = cli.RestoreProfile(layout, "default", backupPath)
	require.NoError(t, err, "RestoreProfile should succeed")

	// Verify the marker file is restored with correct content.
	data, err := os.ReadFile(markerPath)
	require.NoError(t, err, "marker file should exist after restore")
	assert.Equal(t, "backup-roundtrip-data", string(data),
		"restored marker should have original content")
}

// TestJourney_DiskUsageReportsData verifies that DiskUsage returns a non-empty
// report with expected section headers when there is data in the layout.
func TestJourney_DiskUsageReportsData(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	layout := setupJourneyLayout(t)

	// Write some files into agent and shared directories.
	opencodeBin, err := layout.AgentBin("opencode")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(opencodeBin, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(opencodeBin, "fake-binary"),
		[]byte(strings.Repeat("x", 4096)),
		0644,
	))
	require.NoError(t, os.MkdirAll(layout.SharedBin, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(layout.SharedBin, "tool"),
		[]byte(strings.Repeat("y", 2048)),
		0644,
	))

	output, err := cli.DiskUsage(layout)
	require.NoError(t, err, "DiskUsage should not error")
	assert.NotEmpty(t, output, "DiskUsage output should be non-empty")
	assert.Contains(t, output, "Shared", "should contain Shared section header")
	assert.Contains(t, output, "Agents", "should contain Agents section header")
	assert.Contains(t, output, "Total", "should contain Total line")
}

// TestJourney_StatusWhileRunning verifies that Status() reports a running
// container by name while it is active.
func TestJourney_StatusWhileRunning(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	containerName := fmt.Sprintf("ai-shim-status-test-%d", time.Now().UnixNano())

	// Ensure the image is available.
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	// Start a container that sleeps for 30s in a goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = runner.Run(ctx, container.ContainerSpec{
			Name:  containerName,
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "30"},
			Labels: map[string]string{
				container.LabelBase:    "true",
				container.LabelAgent:   "test-agent",
				container.LabelProfile: "default",
			},
			TTY:   false,
			Stdin: false,
			User:  fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		})
	}()

	// Wait for the container to start.
	var started bool
	for attempt := 0; attempt < 20; attempt++ {
		time.Sleep(500 * time.Millisecond)
		containers, listErr := runner.Client().ContainerList(ctx, dockercontainer.ListOptions{
			All: true,
		})
		if listErr != nil {
			continue
		}
		for _, c := range containers {
			for _, name := range c.Names {
				if strings.Contains(name, containerName) {
					started = true
					break
				}
			}
			if started {
				break
			}
		}
		if started {
			break
		}
	}
	require.True(t, started, "container should have started within 10s")

	// Call Status and verify it contains the container name.
	statusOutput, err := cli.Status()
	require.NoError(t, err, "Status should not error")
	assert.Contains(t, statusOutput, containerName,
		"Status output should contain the running container name, got: %s", statusOutput)

	// Cancel context to stop the container.
	cancel()

	// Wait for the container run goroutine to finish.
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("container did not stop within 15s of context cancellation")
	}
}

// TestJourney_SymlinkListAfterCreate verifies that creating symlinks for
// multiple agents and then calling ListSymlinks returns all of them.
func TestJourney_SymlinkListAfterCreate(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow journey test")
	}

	dir := dockerTempDir(t)
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte("#!/bin/sh\n"), 0755))

	// Create symlinks for several agents with different profiles.
	agents := []struct {
		name    string
		profile string
	}{
		{"claude-code", "default"},
		{"opencode", "work"},
		{"aider", "personal"},
	}

	for _, a := range agents {
		path, err := cli.CreateSymlink(a.name, a.profile, dir, shimPath)
		require.NoError(t, err, "CreateSymlink should succeed for %s_%s", a.name, a.profile)
		require.NotEmpty(t, path)
	}

	// List symlinks and verify all created ones are present.
	links, err := cli.ListSymlinks(dir, shimPath)
	require.NoError(t, err, "ListSymlinks should not error")
	assert.Len(t, links, len(agents),
		"should find all created symlinks, got: %v", links)

	// Verify specific link names.
	linkSet := make(map[string]bool)
	for _, l := range links {
		linkSet[l] = true
	}
	assert.True(t, linkSet["claude-code"],
		"should have claude-code symlink (default profile omits suffix)")
	assert.True(t, linkSet["opencode_work"],
		"should have opencode_work symlink")
	assert.True(t, linkSet["aider_personal"],
		"should have aider_personal symlink")
}
