package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Zaephor/ai-shim/internal/storage"
)

func TestIsFirstRun_True(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	assert.True(t, IsFirstRun(layout))
}

func TestIsFirstRun_False(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))
	assert.False(t, IsFirstRun(layout))
}

func TestInit(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	err := Init(layout)
	require.NoError(t, err)
	assert.False(t, IsFirstRun(layout))

	// Verify default.yaml created
	_, err = os.Stat(filepath.Join(layout.ConfigDir, "default.yaml"))
	assert.NoError(t, err)
}

func TestInit_CreatesDirectories(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	require.NoError(t, Init(layout))

	expectedDirs := []string{
		layout.ConfigDir,
		filepath.Join(layout.ConfigDir, "agents"),
		filepath.Join(layout.ConfigDir, "profiles"),
		filepath.Join(layout.ConfigDir, "agent-profiles"),
		layout.SharedBin,
		layout.SharedCache,
	}
	for _, dir := range expectedDirs {
		info, err := os.Stat(dir)
		assert.NoError(t, err, "directory %s should exist", dir)
		if err == nil {
			assert.True(t, info.IsDir(), "%s should be a directory", dir)
		}
	}
}

func TestInit_SeedsExampleConfigs(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	err := Init(layout)
	require.NoError(t, err)

	// Check example configs were created
	_, err = os.Stat(filepath.Join(layout.ConfigDir, "agents", "claude-code.yaml"))
	assert.NoError(t, err, "should create example agent config")
	_, err = os.Stat(filepath.Join(layout.ConfigDir, "profiles", "work.yaml"))
	assert.NoError(t, err, "should create example profile config")
}

func TestInit_DoesNotOverwriteExistingConfig(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	existingContent := "# my custom config\nimage: custom:latest\n"
	configPath := filepath.Join(layout.ConfigDir, "default.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(existingContent), 0644))

	require.NoError(t, Init(layout))

	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, existingContent, string(data), "Init should not overwrite existing config")
}

func TestPrintFirstRunHelp(t *testing.T) {
	layout := storage.NewLayout(filepath.Join(t.TempDir(), ".ai-shim"))
	// Just verify it doesn't panic; output goes to stderr
	PrintFirstRunHelp(layout)
}
