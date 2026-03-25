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
	assert.Equal(t, filepath.Join(root, "agents", "claude", "bin"), layout.AgentBin("claude"))
	assert.Equal(t, filepath.Join(root, "agents", "claude", "cache"), layout.AgentCache("claude"))
}

func TestLayout_ProfilePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	assert.Equal(t, filepath.Join(root, "profiles", "work", "home"), layout.ProfileHome("work"))
}

func TestLayout_EnsureAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)
	err := layout.EnsureDirectories("claude", "work")
	require.NoError(t, err)
	for _, dir := range []string{
		layout.ConfigDir,
		filepath.Join(layout.ConfigDir, "agents"),
		filepath.Join(layout.ConfigDir, "profiles"),
		filepath.Join(layout.ConfigDir, "agent-profiles"),
		layout.SharedBin,
		layout.SharedCache,
		layout.AgentBin("claude"),
		layout.AgentCache("claude"),
		layout.ProfileHome("work"),
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

func TestLayout_EnsureDirectories_Idempotent(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)

	// Call twice -- should not error on second call
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))
}
