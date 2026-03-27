package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadCustomAgents_WithAgentDef(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))

	content := `
agent_def:
  install_type: npm
  package: my-custom-agent
  binary: myagent
  data_dirs:
    - ".myagent"
  data_files:
    - ".myagent.json"
env:
  MY_KEY: "value"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "myagent.yaml"), []byte(content), 0644))

	customs := LoadCustomAgents(dir)
	require.NotNil(t, customs)
	def, ok := customs["myagent"]
	require.True(t, ok)
	assert.Equal(t, "myagent", def.Name)
	assert.Equal(t, "npm", def.InstallType)
	assert.Equal(t, "my-custom-agent", def.Package)
	assert.Equal(t, "myagent", def.Binary)
	assert.Equal(t, []string{".myagent"}, def.DataDirs)
	assert.Equal(t, []string{".myagent.json"}, def.DataFiles)
}

func TestLoadCustomAgents_NoAgentDef(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))

	// A normal agent config file without agent_def should be ignored
	content := `
env:
  MY_KEY: "value"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "claude.yaml"), []byte(content), 0644))

	customs := LoadCustomAgents(dir)
	assert.Nil(t, customs, "files without agent_def should not produce custom agents")
}

func TestLoadCustomAgents_MissingDir(t *testing.T) {
	customs := LoadCustomAgents("/nonexistent/path")
	assert.Nil(t, customs)
}

func TestLoadCustomAgents_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "empty.yaml"), []byte(""), 0644))

	customs := LoadCustomAgents(dir)
	assert.Nil(t, customs)
}

func TestSetCustomAgents_OverrideBuiltin(t *testing.T) {
	// Save and restore customs
	orig := customs
	defer func() { customs = orig }()

	SetCustomAgents(map[string]Definition{
		"claude-code": {
			Name:        "claude-code",
			InstallType: "npm",
			Package:     "custom-claude",
			Binary:      "custom-claude",
			DataDirs:    []string{".custom-claude"},
		},
	})

	def, ok := Lookup("claude-code")
	require.True(t, ok)
	assert.Equal(t, "custom-claude", def.Binary, "custom agent should override built-in")
	assert.Equal(t, "npm", def.InstallType)
}

func TestSetCustomAgents_NewAgent(t *testing.T) {
	orig := customs
	defer func() { customs = orig }()

	SetCustomAgents(map[string]Definition{
		"new-agent": {
			Name:        "new-agent",
			InstallType: "pip",
			Package:     "new-agent-pkg",
			Binary:      "newagent",
			DataDirs:    []string{".newagent"},
		},
	})

	def, ok := Lookup("new-agent")
	require.True(t, ok)
	assert.Equal(t, "newagent", def.Binary)

	// Should appear in Names()
	names := Names()
	found := false
	for _, n := range names {
		if n == "new-agent" {
			found = true
			break
		}
	}
	assert.True(t, found, "new-agent should appear in Names()")

	// Should appear in All()
	all := All()
	_, ok = all["new-agent"]
	assert.True(t, ok, "new-agent should appear in All()")
}

func TestSetCustomAgents_Nil(t *testing.T) {
	orig := customs
	defer func() { customs = orig }()

	SetCustomAgents(map[string]Definition{
		"test": {Name: "test"},
	})
	SetCustomAgents(nil)

	_, ok := Lookup("test")
	assert.False(t, ok, "nil should clear custom agents")
}

func TestValidateDataPath_Valid(t *testing.T) {
	valid := []string{".claude", ".config/goose", "data", ".claude.json", "a/b/c"}
	for _, p := range valid {
		assert.NoError(t, ValidateDataPath(p), "should accept %q", p)
	}
}

func TestValidateDataPath_Empty(t *testing.T) {
	err := ValidateDataPath("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestValidateDataPath_Absolute(t *testing.T) {
	err := ValidateDataPath("/etc/passwd")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "absolute")
}

func TestValidateDataPath_Traversal(t *testing.T) {
	traversals := []string{
		"../../etc/passwd",
		"../secret",
		"..",
	}
	for _, p := range traversals {
		err := ValidateDataPath(p)
		assert.Error(t, err, "should reject %q", p)
		assert.Contains(t, err.Error(), "traversal", "error for %q should mention traversal", p)
	}
}

func TestLoadCustomAgents_FiltersBadPaths(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))

	content := `
agent_def:
  install_type: npm
  package: evil-agent
  binary: evil
  data_dirs:
    - "../../etc"
    - ".evil"
    - "/absolute/path"
  data_files:
    - "../../../etc/passwd"
    - ".evil.json"
`
	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "evil.yaml"), []byte(content), 0644))

	customs := LoadCustomAgents(dir)
	require.NotNil(t, customs)
	def, ok := customs["evil"]
	require.True(t, ok)
	// Only safe paths should remain
	assert.Equal(t, []string{".evil"}, def.DataDirs)
	assert.Equal(t, []string{".evil.json"}, def.DataFiles)
}

func TestLoadCustomAgents_SkipsNonYAML(t *testing.T) {
	dir := t.TempDir()
	agentsDir := filepath.Join(dir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))

	require.NoError(t, os.WriteFile(filepath.Join(agentsDir, "readme.txt"), []byte("not yaml"), 0644))

	customs := LoadCustomAgents(dir)
	assert.Nil(t, customs)
}
