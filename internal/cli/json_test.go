package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/testutil"
)

func TestIsJSONMode(t *testing.T) {
	t.Run("disabled by default", func(t *testing.T) {
		assert.False(t, IsJSONMode())
	})

	t.Run("enabled with AI_SHIM_JSON=1", func(t *testing.T) {
		t.Setenv("AI_SHIM_JSON", "1")
		assert.True(t, IsJSONMode())
	})

	t.Run("not enabled with AI_SHIM_JSON=0", func(t *testing.T) {
		t.Setenv("AI_SHIM_JSON", "0")
		assert.False(t, IsJSONMode())
	})
}

func TestMarshalJSON(t *testing.T) {
	result, err := MarshalJSON(map[string]string{"key": "value"})
	require.NoError(t, err)
	assert.Contains(t, result, `"key"`)
	assert.Contains(t, result, `"value"`)
	// Should end with newline
	assert.True(t, result[len(result)-1] == '\n')
}

func TestMarshalJSON_EmptyMap(t *testing.T) {
	result, err := MarshalJSON(map[string]string{})
	require.NoError(t, err)
	assert.Equal(t, "{}\n", result)
}

func TestMarshalJSON_EmptySlice(t *testing.T) {
	result, err := MarshalJSON([]string{})
	require.NoError(t, err)
	assert.Equal(t, "[]\n", result)
}

func TestMarshalJSON_NilInput(t *testing.T) {
	result, err := MarshalJSON(nil)
	require.NoError(t, err)
	assert.Equal(t, "null\n", result)
}

func TestMarshalJSON_ErrorHandling(t *testing.T) {
	// Channels cannot be marshaled to JSON
	_, err := MarshalJSON(make(chan int))
	assert.Error(t, err)
}

func TestListAgentsJSON(t *testing.T) {
	output, err := ListAgentsJSON()
	require.NoError(t, err)

	var agents []AgentEntry
	require.NoError(t, json.Unmarshal([]byte(output), &agents))
	assert.NotEmpty(t, agents)

	// Check that known agents are present
	names := make(map[string]bool)
	for _, a := range agents {
		names[a.Name] = true
		assert.NotEmpty(t, a.InstallType)
		assert.NotEmpty(t, a.Binary)
	}
	assert.True(t, names["claude-code"], "should include claude-code")
	assert.True(t, names["aider"], "should include aider")
}

func TestListProfilesJSON(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		layout := storage.NewLayout(t.TempDir())
		output, err := ListProfilesJSON(layout)
		require.NoError(t, err)

		var profiles []ProfileEntry
		require.NoError(t, json.Unmarshal([]byte(output), &profiles))
		assert.Empty(t, profiles)
	})

	t.Run("with profiles", func(t *testing.T) {
		root := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "work"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "personal"), 0755))

		layout := storage.NewLayout(root)
		output, err := ListProfilesJSON(layout)
		require.NoError(t, err)

		var profiles []ProfileEntry
		require.NoError(t, json.Unmarshal([]byte(output), &profiles))
		assert.Len(t, profiles, 2)
		// Both are runtime-only (launched)
		for _, p := range profiles {
			assert.True(t, p.Launched)
		}
	})
}

func TestShowConfigJSON(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "ubuntu:24.04"
hostname: "test"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfigJSON(layout, "claude-code", "work")
	require.NoError(t, err)

	// Should be valid JSON
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.Equal(t, "ubuntu:24.04", result["image"])
	assert.Equal(t, "test", result["hostname"])
}

func TestDoctorJSON(t *testing.T) {
	output, err := DoctorJSON()
	require.NoError(t, err)

	var result DoctorResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	// Docker status should be "ok" or "fail"
	assert.Contains(t, []string{"ok", "fail"}, result.Docker.Status)
	assert.NotEmpty(t, result.StorageRoot)
	assert.NotEmpty(t, result.ConfigDir)
	assert.Len(t, result.ImagePinning, 3, "should have agent, dind, cache pinning")
}

func TestStatusJSON(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	output, err := StatusJSON()
	require.NoError(t, err)

	var entries []StatusEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	// May be empty array if no containers running
	assert.NotNil(t, entries)
}

func TestDiskUsageJSON(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	os.MkdirAll(layout.SharedBin, 0755)
	os.MkdirAll(layout.ConfigDir, 0755)
	os.WriteFile(filepath.Join(layout.SharedBin, "test"), []byte("data"), 0644)

	output, err := DiskUsageJSON(layout)
	require.NoError(t, err)

	var result DiskUsageResult
	require.NoError(t, json.Unmarshal([]byte(output), &result))
	assert.NotEmpty(t, result.Directories)
	assert.GreaterOrEqual(t, result.Total, int64(0))
}
