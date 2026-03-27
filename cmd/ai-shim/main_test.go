package main

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunManage_Version(t *testing.T) {
	// Capture stdout would be complex, just verify no error
	err := runManage([]string{"version"})
	assert.NoError(t, err)
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
