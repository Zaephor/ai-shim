package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
