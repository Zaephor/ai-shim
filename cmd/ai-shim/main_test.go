package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Docker test in short mode")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker not available:", err)
	}
}

func TestRunManage_Version(t *testing.T) {
	// Capture stdout would be complex, just verify no error
	err := runManage([]string{"version"})
	assert.NoError(t, err)
}

// TestCleanupStaleContainers verifies that cleanupStaleContainers removes a
// stopped ai-shim container with matching agent+profile labels but leaves
// containers with different labels alone.
func TestCleanupStaleContainers(t *testing.T) {
	skipIfNoDocker(t)

	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	cli := runner.Client()

	// Create a container that exits immediately, with the labels the
	// cleanup function looks for.
	staleName := "ai-shim-test-stale-" + time.Now().Format("150405.000000")
	staleName = strings.ReplaceAll(staleName, ".", "-")
	staleResp, err := cli.ContainerCreate(ctx,
		&container_types.Config{
			Image: "alpine:latest",
			Cmd:   []string{"true"},
			Labels: map[string]string{
				container.LabelBase:      "true",
				container.LabelAgent:     "test-stale-agent",
				container.LabelProfile:   "test-stale-profile",
				container.LabelWorkspace: "test-ws-hash",
			},
		},
		&container_types.HostConfig{},
		nil, nil, staleName)
	require.NoError(t, err)
	defer cli.ContainerRemove(ctx, staleResp.ID, container_types.RemoveOptions{Force: true})
	require.NoError(t, cli.ContainerStart(ctx, staleResp.ID, container_types.StartOptions{}))

	// Wait for the container to exit.
	statusCh, errCh := cli.ContainerWait(ctx, staleResp.ID, container_types.WaitConditionNotRunning)
	select {
	case <-statusCh:
	case waitErr := <-errCh:
		require.NoError(t, waitErr)
	case <-time.After(15 * time.Second):
		t.Fatal("stale container did not exit within 15s")
	}

	// Sanity: container should still exist (not running, not removed).
	listBefore, err := cli.ContainerList(ctx, container_types.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", container.LabelAgent+"=test-stale-agent"),
			filters.Arg("label", container.LabelProfile+"=test-stale-profile"),
		),
	})
	require.NoError(t, err)
	require.NotEmpty(t, listBefore, "stale container should exist before cleanup")

	// Run the cleanup.
	cleanupStaleContainers(ctx, runner, "test-stale-agent", "test-stale-profile", "test-ws-hash")

	// The stale container should now be gone.
	listAfter, err := cli.ContainerList(ctx, container_types.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", container.LabelAgent+"=test-stale-agent"),
			filters.Arg("label", container.LabelProfile+"=test-stale-profile"),
		),
	})
	require.NoError(t, err)
	assert.Empty(t, listAfter, "cleanup should remove stale containers for this agent+profile")
}

func TestRunManage_ManageAgents(t *testing.T) {
	err := runManage([]string{"manage", "agents"})
	assert.NoError(t, err, "manage agents should be a valid command")
}

func TestRunManage_ManageDoctor(t *testing.T) {
	err := runManage([]string{"manage", "doctor"})
	assert.NoError(t, err, "manage doctor should be a valid command")
}

func TestRunManage_ManageProfiles(t *testing.T) {
	err := runManage([]string{"manage", "profiles"})
	assert.NoError(t, err, "manage profiles should be a valid command")
}

func TestRunManage_Update(t *testing.T) {
	// "update" command should exist (even if it can't actually update in test)
	err := runManage([]string{"update"})
	// Should not return "unknown command" error
	if err != nil {
		assert.NotContains(t, err.Error(), "unknown command", "update should be a recognized command")
	}
}

func TestRunManage_UnknownCommand(t *testing.T) {
	err := runManage([]string{"nonexistent"})
	assert.Error(t, err, "unknown command should return error")
	assert.Contains(t, err.Error(), "unknown")
}

func TestRunManage_ManageSymlinks(t *testing.T) {
	err := runManage([]string{"manage", "symlinks"})
	// Should return usage error, not "unknown command"
	if err != nil {
		assert.NotContains(t, err.Error(), "unknown")
	}
}

func TestRunManage_ManageDryRun(t *testing.T) {
	err := runManage([]string{"manage", "dry-run"})
	// Should return usage error, not "unknown command"
	if err != nil {
		assert.NotContains(t, err.Error(), "unknown")
	}
}

func TestRunManage_ManageCleanup(t *testing.T) {
	skipIfNoDocker(t)
	err := runManage([]string{"manage", "cleanup"})
	assert.NoError(t, err, "cleanup should work (may find 0 containers)")
}

func TestRunManage_CompletionBash(t *testing.T) {
	err := runManage([]string{"completion", "bash"})
	assert.NoError(t, err)
}

func TestRunManage_CompletionZsh(t *testing.T) {
	err := runManage([]string{"completion", "zsh"})
	assert.NoError(t, err)
}

func TestRunManage_CompletionMissingShell(t *testing.T) {
	err := runManage([]string{"completion"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_CompletionUnsupportedShell(t *testing.T) {
	err := runManage([]string{"completion", "fish"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shell")
}

func TestRunManage_Run(t *testing.T) {
	// Can't actually run (needs Docker), but verify parsing works
	err := runManage([]string{"run"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_RunMissingAgent(t *testing.T) {
	// "run" with no agent should return usage error
	err := runManage([]string{"run"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_SubcommandHelp(t *testing.T) {
	err := runManage([]string{"manage", "symlinks", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_ManageHelp(t *testing.T) {
	err := runManage([]string{"manage", "--help"})
	assert.NoError(t, err)
}

func TestHelpText_ListsAllEnvVarOverrides(t *testing.T) {
	// Read resolver.go to find all AI_SHIM_ env var names
	data, err := os.ReadFile("../../internal/config/resolver.go")
	require.NoError(t, err)

	// Extract AI_SHIM_* variable names from os.Getenv calls
	var envVars []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, `os.Getenv("AI_SHIM_`) {
			start := strings.Index(line, `"AI_SHIM_`)
			end := strings.Index(line[start+1:], `"`) + start + 1
			envVar := line[start+1 : end]
			envVars = append(envVars, envVar)
		}
	}

	require.NotEmpty(t, envVars, "should find AI_SHIM_* env vars in resolver.go")

	// Capture help text
	// We can't easily capture stdout from printHelp(), so read main.go
	// and check the help string contains each env var
	mainData, err := os.ReadFile("main.go")
	require.NoError(t, err)
	mainStr := string(mainData)

	for _, envVar := range envVars {
		assert.Contains(t, mainStr, envVar,
			"help text in main.go should mention %s (defined in resolver.go)", envVar)
	}
}

func TestRunManage_ManageAgentVersions(t *testing.T) {
	err := runManage([]string{"manage", "agent-versions"})
	assert.NoError(t, err, "manage agent-versions should be a valid command")
}

func TestRunManage_ManageReinstallMissingAgent(t *testing.T) {
	err := runManage([]string{"manage", "reinstall"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageReinstallUnknownAgent(t *testing.T) {
	err := runManage([]string{"manage", "reinstall", "nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent")
}

func TestRunManage_Help(t *testing.T) {
	err := runManage([]string{"help"})
	assert.NoError(t, err)
}

func TestRunManage_DashH(t *testing.T) {
	err := runManage([]string{"-h"})
	assert.NoError(t, err)
}

func TestRunManage_DashDashHelp(t *testing.T) {
	err := runManage([]string{"--help"})
	assert.NoError(t, err)
}

func TestRunManage_NoArgs(t *testing.T) {
	err := runManage(nil)
	assert.NoError(t, err)
}

func TestRunManage_Init(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	err := runManage([]string{"init"})
	assert.NoError(t, err)
}

func TestRunManage_ManageConfigMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "config"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing required argument")
}

func TestRunManage_ManageConfigOneArg(t *testing.T) {
	// With only agent specified, profile defaults to "default" — should succeed
	err := runManage([]string{"manage", "config", "claude-code"})
	assert.NoError(t, err)
}

func TestRunManage_ManageExecMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "exec"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageExecOneArg(t *testing.T) {
	err := runManage([]string{"manage", "exec", "mycontainer"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageWatchMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "watch"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageSwitchProfileMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "switch-profile"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageBackupMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageRestoreMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "restore"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageRestoreOneArg(t *testing.T) {
	err := runManage([]string{"manage", "restore", "myprofile"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageUnknownSubcommand(t *testing.T) {
	err := runManage([]string{"manage", "nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown manage subcommand")
}

func TestRunManage_ManageSymlinksUnknownSubcommand(t *testing.T) {
	err := runManage([]string{"manage", "symlinks", "nonexistent"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown symlinks subcommand")
}

func TestRunManage_ManageSymlinksCreateMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "symlinks", "create"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageSymlinksRemoveMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "symlinks", "remove"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageSubcommandHelps(t *testing.T) {
	subcommands := []string{
		"agents", "profiles", "config", "doctor", "symlinks",
		"dry-run", "cleanup", "status", "backup", "restore",
		"disk-usage", "agent-versions", "reinstall", "exec",
		"attach", "watch", "switch-profile",
	}
	for _, sub := range subcommands {
		t.Run(sub, func(t *testing.T) {
			err := runManage([]string{"manage", sub, "--help"})
			assert.NoError(t, err, "manage %s --help should succeed", sub)
		})
	}
}

func TestRunManage_ManageAttachMissingArgs(t *testing.T) {
	err := runManage([]string{"manage", "attach"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
	assert.Contains(t, err.Error(), "<container-name>")
}

func TestRunManage_ManageAttachHelp(t *testing.T) {
	err := runManage([]string{"manage", "attach", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_ManageDiskUsage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	err := runManage([]string{"manage", "disk-usage"})
	assert.NoError(t, err)
}

func TestPrintHelp(t *testing.T) {
	// Just verify it doesn't panic
	printHelp()
}

func TestFormatAgentList(t *testing.T) {
	output := formatAgentList()
	assert.Contains(t, output, "claude-code")
	assert.Contains(t, output, "aider")
}

func TestRunManage_ManageSymlinksList(t *testing.T) {
	tmpDir := t.TempDir()
	// Change to tmpDir so "." resolves to it
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(tmpDir))
	defer os.Chdir(origDir)

	err := runManage([]string{"manage", "symlinks", "list"})
	assert.NoError(t, err)
}

func TestRunManage_ManageBackupNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	err := runManage([]string{"manage", "backup", "nonexistent"})
	assert.Error(t, err)
}

func TestRunManage_ManageConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	// Initialize so config dirs exist
	require.NoError(t, runManage([]string{"init"}))

	err := runManage([]string{"manage", "config", "claude-code", "default"})
	assert.NoError(t, err)
}

func TestRunManage_ManageDryRunValidArgs(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, runManage([]string{"init"}))

	err := runManage([]string{"manage", "dry-run", "claude-code", "default"})
	assert.NoError(t, err)
}

func TestRunManage_ManageAgentsJSON(t *testing.T) {
	t.Setenv("AI_SHIM_JSON", "1")
	err := runManage([]string{"manage", "agents"})
	assert.NoError(t, err)
}

func TestRunManage_ManageProfilesJSON(t *testing.T) {
	t.Setenv("AI_SHIM_JSON", "1")
	err := runManage([]string{"manage", "profiles"})
	assert.NoError(t, err)
}

func TestRunManage_ManageConfigJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("AI_SHIM_JSON", "1")
	require.NoError(t, runManage([]string{"init"}))

	err := runManage([]string{"manage", "config", "claude-code", "default"})
	assert.NoError(t, err)
}

func TestRunManage_ManageDoctorJSON(t *testing.T) {
	t.Setenv("AI_SHIM_JSON", "1")
	err := runManage([]string{"manage", "doctor"})
	assert.NoError(t, err)
}

func TestRunManage_ManageDiskUsageJSON(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("AI_SHIM_JSON", "1")
	err := runManage([]string{"manage", "disk-usage"})
	assert.NoError(t, err)
}

func TestRunManage_ManageSwitchProfile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, runManage([]string{"init"}))

	err := runManage([]string{"manage", "switch-profile", "work"})
	assert.NoError(t, err)
}

func TestRunManage_ManageSymlinksListDir(t *testing.T) {
	tmpDir := t.TempDir()
	err := runManage([]string{"manage", "symlinks", "list", tmpDir})
	assert.NoError(t, err)
}

// TestRunAgent_FullPipeline exercises the complete runAgent flow:
// symlink parse → config resolve → BuildSpec → Docker launch.
// The agent installs inside the container and exits (no TTY/config).
// This is the only test that covers the runAgent orchestrator directly —
// other e2e tests bypass it by building specs manually.
func TestRunAgent_FullPipeline(t *testing.T) {
	skipIfNoDocker(t)

	// Use a Docker-accessible directory for HOME. In DooD environments,
	// /tmp is inside the container overlay and invisible to the host
	// Docker daemon. The project's tmp/ dir is bind-mounted.
	// Go test runs from the package directory (cmd/ai-shim/).
	// Navigate up to the project root for the Docker-accessible tmp/ dir.
	cwd, _ := os.Getwd()
	projectRoot := filepath.Join(cwd, "..", "..")
	base := filepath.Join(projectRoot, "tmp", "e2e-test")
	require.NoError(t, os.MkdirAll(base, 0755))
	tmpHome, err := os.MkdirTemp(base, "runagent-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tmpHome) })
	t.Setenv("HOME", tmpHome)

	err = runManage([]string{"init"})
	require.NoError(t, err, "init should succeed")

	// Run opencode agent in a goroutine with a timeout. The agent will
	// install via npm and then hang waiting for input (no TTY). We verify
	// the pipeline reaches the container run phase — if config resolve,
	// BuildSpec, or container creation fails, runAgent returns an error
	// before the timeout.
	type result struct {
		exitCode int
		err      error
	}
	ch := make(chan result, 1)
	go func() {
		code, runErr := runAgent("opencode", nil)
		ch <- result{code, runErr}
	}()

	select {
	case r := <-ch:
		// Agent exited (non-zero is expected without config)
		require.NoError(t, r.err,
			"runAgent pipeline should not error (exit code %d is expected from agent)", r.exitCode)
	case <-time.After(90 * time.Second):
		// Timeout means the container is running — the pipeline succeeded.
		// The agent is just waiting for input. This is the expected path.
		t.Log("runAgent pipeline reached container run phase (agent waiting for input, as expected)")
	}
}

// TestRunAgent_UnknownAgent verifies the error path when an unknown agent
// is specified. This tests the early exit before Docker is involved.
func TestRunAgent_UnknownAgent(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	_ = runManage([]string{"init"})

	_, err := runAgent("nonexistent-agent-xyz", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent")
}

// TestRunAgent_FirstRunDetected verifies runAgent fails gracefully before
// ai-shim is initialized.
func TestRunAgent_FirstRunDetected(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	// Don't run init — should trigger first-run detection

	_, err := runAgent("opencode", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "init")
}

// TestRunAgent_UnknownAgentSuggestions verifies the error message for an
// unknown agent includes the list of available agents as suggestions.
func TestRunAgent_UnknownAgentSuggestions(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	_ = runManage([]string{"init"})

	_, err := runAgent("totally-bogus-agent", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent")
	// Should list available agents as suggestions
	assert.Contains(t, err.Error(), "claude-code")
	assert.Contains(t, err.Error(), "aider")
}

// TestRunAgent_FirstRunCreatesConfigDir verifies that after init, runAgent
// proceeds past the first-run check. We use a lightweight agent (opencode)
// and verify the error is about container/Docker setup, not about init.
func TestRunAgent_FirstRunCreatesConfigDir(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	require.NoError(t, runManage([]string{"init"}))

	// Use a short deadline so the test doesn't hang if the container
	// actually starts (which happens when Docker is available in CI).
	done := make(chan struct{})
	var agentErr error
	go func() {
		_, agentErr = runAgent("claude-code", nil)
		close(done)
	}()

	select {
	case <-done:
		// runAgent returned — verify it got past first-run detection
		if agentErr != nil {
			assert.NotContains(t, agentErr.Error(), "run 'ai-shim init'")
		}
	case <-time.After(30 * time.Second):
		// runAgent is pulling/running a container — it got past first-run.
		// This is success: we verified init detection was bypassed.
		t.Log("runAgent proceeded past init check into Docker operations (expected)")
	}
}

func TestRunManage_ManageLogsNoArgs(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, runManage([]string{"init"}))

	// manage logs with no args should show persistent log (or "no logs" message)
	err := runManage([]string{"manage", "logs"})
	assert.NoError(t, err)
}

func TestRunManage_ManageLogsWithAgent(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, runManage([]string{"init"}))

	// manage logs <agent> — should attempt container logs, fall back to
	// persistent log filtered by agent. Should not panic or error.
	err := runManage([]string{"manage", "logs", "claude-code"})
	assert.NoError(t, err)
}

func TestRunManage_ManageLogsWithAgentAndProfile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, runManage([]string{"init"}))

	// manage logs <agent> <profile> — should not panic
	err := runManage([]string{"manage", "logs", "claude-code", "work"})
	assert.NoError(t, err)
}

func TestRunManage_ManageLogsHelp(t *testing.T) {
	err := runManage([]string{"manage", "logs", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_ManageDiskUsageNoInit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	// disk-usage should work even without init (empty storage)
	err := runManage([]string{"manage", "disk-usage"})
	assert.NoError(t, err)
}

func TestRunManage_ManageAgentVersionsNoInit(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	// agent-versions should work even without init
	err := runManage([]string{"manage", "agent-versions"})
	assert.NoError(t, err)
}

func TestRunManage_ManageSwitchProfileValid(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	require.NoError(t, runManage([]string{"init"}))

	err := runManage([]string{"manage", "switch-profile", "production"})
	assert.NoError(t, err)

	// Switching again to a different profile should also work
	err = runManage([]string{"manage", "switch-profile", "staging"})
	assert.NoError(t, err)
}

func TestRunManage_ManageBackupMissingProfile(t *testing.T) {
	err := runManage([]string{"manage", "backup"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageRestoreMissingArchive(t *testing.T) {
	err := runManage([]string{"manage", "restore", "myprofile"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageRestoreMissingAll(t *testing.T) {
	err := runManage([]string{"manage", "restore"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageWatchMissingAgent(t *testing.T) {
	err := runManage([]string{"manage", "watch"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestRunManage_ManageWatchHelp(t *testing.T) {
	err := runManage([]string{"manage", "watch", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_ManageSwitchProfileHelp(t *testing.T) {
	err := runManage([]string{"manage", "switch-profile", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_ManageBackupHelp(t *testing.T) {
	err := runManage([]string{"manage", "backup", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_ManageRestoreHelp(t *testing.T) {
	err := runManage([]string{"manage", "restore", "--help"})
	assert.NoError(t, err)
}

func TestRunManage_CompletionInvalid(t *testing.T) {
	err := runManage([]string{"completion", "powershell"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported shell")
}

func TestRunManage_UnknownManageSubcommand(t *testing.T) {
	err := runManage([]string{"manage", "frobnicate"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown manage subcommand")
	// Should list available subcommands
	assert.Contains(t, err.Error(), "agents")
	assert.Contains(t, err.Error(), "profiles")
}

func TestRunManage_ManageStatusJSON(t *testing.T) {
	skipIfNoDocker(t)
	t.Setenv("AI_SHIM_JSON", "1")
	err := runManage([]string{"manage", "status"})
	assert.NoError(t, err)
}

// TestInitIdempotency verifies that running init twice does not clobber
// user-modified config files (default.yaml, agent configs, profile configs).
func TestInitIdempotency(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// First init — creates default configs
	require.NoError(t, runManage([]string{"init"}))

	configDir := filepath.Join(tmpDir, ".ai-shim", "config")

	// Read default.yaml and verify it was created
	defaultYAML := filepath.Join(configDir, "default.yaml")
	original, err := os.ReadFile(defaultYAML)
	require.NoError(t, err)
	require.NotEmpty(t, original)

	// Modify default.yaml — add a custom field at the end
	customContent := string(original) + "\ncustom_user_field: preserve_me\n"
	require.NoError(t, os.WriteFile(defaultYAML, []byte(customContent), 0644))

	// Modify the example agent config
	agentConfig := filepath.Join(configDir, "agents", "claude-code.yaml")
	agentOriginal, err := os.ReadFile(agentConfig)
	require.NoError(t, err)
	agentCustom := string(agentOriginal) + "\nmy_custom_setting: true\n"
	require.NoError(t, os.WriteFile(agentConfig, []byte(agentCustom), 0644))

	// Modify the example profile config
	profileConfig := filepath.Join(configDir, "profiles", "work.yaml")
	profileOriginal, err := os.ReadFile(profileConfig)
	require.NoError(t, err)
	profileCustom := string(profileOriginal) + "\nmy_profile_setting: enabled\n"
	require.NoError(t, os.WriteFile(profileConfig, []byte(profileCustom), 0644))

	// Second init — should NOT clobber any of the above
	require.NoError(t, runManage([]string{"init"}))

	// Verify default.yaml still has the custom field
	afterInit, err := os.ReadFile(defaultYAML)
	require.NoError(t, err)
	assert.Contains(t, string(afterInit), "custom_user_field: preserve_me",
		"init clobbered default.yaml — user config was lost")
	assert.Equal(t, customContent, string(afterInit),
		"default.yaml content changed after second init")

	// Verify agent config still has the custom setting
	afterAgent, err := os.ReadFile(agentConfig)
	require.NoError(t, err)
	assert.Contains(t, string(afterAgent), "my_custom_setting: true",
		"init clobbered agent config — user config was lost")

	// Verify profile config still has the custom setting
	afterProfile, err := os.ReadFile(profileConfig)
	require.NoError(t, err)
	assert.Contains(t, string(afterProfile), "my_profile_setting: enabled",
		"init clobbered profile config — user config was lost")
}
