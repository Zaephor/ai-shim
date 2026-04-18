package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	assert.Equal(t, root, layout.Root)
	assert.Equal(t, filepath.Join(root, "config"), layout.ConfigDir)
	assert.Equal(t, filepath.Join(root, "shared", "bin"), layout.SharedBin)
	assert.Equal(t, filepath.Join(root, "shared", "cache"), layout.SharedCache)
}

func TestLayout_AgentPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	agentBin, err := layout.AgentBin("claude")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "agents", "claude", "bin"), agentBin)
	agentCache, err := layout.AgentCache("claude")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "agents", "claude", "cache"), agentCache)
}

func TestLayout_ProfilePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	profileHome, err := layout.ProfileHome("work")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "profiles", "work", "home"), profileHome)
}

func TestLayout_EnsureAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	err := layout.EnsureDirectories("claude", "work")
	require.NoError(t, err)
	agentBin, err := layout.AgentBin("claude")
	require.NoError(t, err)
	agentCache, err := layout.AgentCache("claude")
	require.NoError(t, err)
	profileHome, err := layout.ProfileHome("work")
	require.NoError(t, err)
	for _, dir := range []string{
		layout.ConfigDir,
		filepath.Join(layout.ConfigDir, "agents"),
		filepath.Join(layout.ConfigDir, "profiles"),
		filepath.Join(layout.ConfigDir, "agent-profiles"),
		layout.SharedBin,
		layout.SharedCache,
		agentBin,
		agentCache,
		profileHome,
	} {
		_, err := os.Stat(dir)
		assert.NoError(t, err, "directory should exist: %s", dir)
	}
}

func TestDefaultRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".ai-shim"), DefaultRoot())
}

func TestDefaultRoot_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	root := DefaultRoot()
	// Should not be empty or just "/.ai-shim"
	assert.NotEqual(t, "/.ai-shim", root, "should not use root filesystem when HOME unset")
	assert.NotEmpty(t, root)
}

func TestLayout_EnsureDirectories_Idempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)

	// Call twice -- should not error on second call
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))
}

func TestLayout_EnsureAgentData_Dirs(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))

	err := layout.EnsureAgentData("work", []string{".claude", ".config/goose"}, nil)
	require.NoError(t, err)

	home, err := layout.ProfileHome("work")
	require.NoError(t, err)
	for _, dir := range []string{".claude", ".config/goose"} {
		info, err := os.Stat(filepath.Join(home, dir))
		require.NoError(t, err, "data dir should exist: %s", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)
	}
}

func TestLayout_EnsureAgentData_Files(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))

	err := layout.EnsureAgentData("work", nil, []string{".claude.json"})
	require.NoError(t, err)

	home, err := layout.ProfileHome("work")
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(home, ".claude.json"))
	require.NoError(t, err, ".claude.json should exist")
	assert.False(t, info.IsDir(), ".claude.json should be a file, not a directory")
}

func TestLayout_EnsureAgentData_PreservesExistingFiles(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))

	home, err := layout.ProfileHome("work")
	require.NoError(t, err)
	path := filepath.Join(home, ".claude.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"key":"value"}`), 0644))

	err = layout.EnsureAgentData("work", nil, []string{".claude.json"})
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, `{"key":"value"}`, string(data), "existing file content should be preserved")
}

func TestLayout_EnsureAgentData_Idempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))

	require.NoError(t, layout.EnsureAgentData("work", []string{".claude"}, []string{".claude.json"}))
	require.NoError(t, layout.EnsureAgentData("work", []string{".claude"}, []string{".claude.json"}))
}

func TestToolCachePath_Global(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	got, err := ToolCachePath(layout, "nvm", "global", "claude-code", "default")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "shared", "cache", "nvm"), got)
}

func TestToolCachePath_EmptyScopeDefaultsToGlobal(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	got, err := ToolCachePath(layout, "nvm", "", "claude-code", "default")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "shared", "cache", "nvm"), got)
}

func TestToolCachePath_Profile(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	got, err := ToolCachePath(layout, "gvm", "profile", "claude-code", "golang")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "profiles", "golang", "cache", "gvm"), got)
}

func TestToolCachePath_Agent(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	got, err := ToolCachePath(layout, "sdkman", "agent", "gemini-cli", "default")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "agents", "gemini-cli", "cache", "sdkman"), got)
}

func TestLayout_EnsureAgentData_NestedFileParentCreated(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))

	err := layout.EnsureAgentData("work", nil, []string{".config/agent/settings.json"})
	require.NoError(t, err)

	home, err := layout.ProfileHome("work")
	require.NoError(t, err)
	info, err := os.Stat(filepath.Join(home, ".config/agent/settings.json"))
	require.NoError(t, err)
	assert.False(t, info.IsDir())
}

func TestAgentBin_ValidName(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	got, err := layout.AgentBin("valid-name")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "agents", "valid-name", "bin"), got)
}

func TestAgentBin_TraversalRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	_, err := layout.AgentBin("../../etc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestAgentBin_SeparatorRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	_, err := layout.AgentBin("foo/bar")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestProfileHome_ValidName(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	got, err := layout.ProfileHome("my-profile")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "profiles", "my-profile", "home"), got)
}

func TestProfileHome_TraversalRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	_, err := layout.ProfileHome("../../etc")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestProfileHome_SeparatorRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	_, err := layout.ProfileHome("foo/bar")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestToolCachePath_TraversalRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	_, err := ToolCachePath(layout, "../../etc/passwd", "", "agent", "profile")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "path traversal")
}

func TestToolCachePath_EmptyNameRejected(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	_, err := ToolCachePath(layout, "", "", "agent", "profile")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}
