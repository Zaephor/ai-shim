package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookup_BuiltinAgent(t *testing.T) {
	def, ok := Lookup("claude-code")
	require.True(t, ok)
	assert.Equal(t, "claude", def.Binary)
	assert.Equal(t, "custom", def.InstallType)
	assert.Contains(t, def.DataDirs, ".claude")
	assert.Contains(t, def.DataFiles, ".claude.json")
}

func TestLookup_NotFound(t *testing.T) {
	_, ok := Lookup("nonexistent")
	assert.False(t, ok)
}

func TestAllAgents_HaveDataDirs(t *testing.T) {
	for name, def := range All() {
		assert.NotEmpty(t, def.DataDirs, "%s should have at least one data dir", name)
	}
}

func TestAll_ContainsAllAgents(t *testing.T) {
	all := All()
	for _, name := range []string{"claude-code", "gemini-cli", "qwen-code", "codex", "pi", "gsd", "aider", "goose", "opencode"} {
		_, ok := all[name]
		assert.True(t, ok, "missing agent: %s", name)
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	all := All()
	all["test"] = Definition{Name: "test"}
	_, ok := Lookup("test")
	assert.False(t, ok)
}

func TestNames_Sorted(t *testing.T) {
	names := Names()
	assert.Equal(t, len(All()), len(names))
	for i := 1; i < len(names); i++ {
		assert.True(t, names[i-1] < names[i])
	}
}

func TestInstallTypes(t *testing.T) {
	tests := []struct{ agent, installType string }{
		{"claude-code", "custom"},
		{"gemini-cli", "npm"},
		{"aider", "uv"},
		{"goose", "custom"},
		{"opencode", "npm"},
	}
	for _, tt := range tests {
		def, ok := Lookup(tt.agent)
		require.True(t, ok)
		assert.Equal(t, tt.installType, def.InstallType)
	}
}
