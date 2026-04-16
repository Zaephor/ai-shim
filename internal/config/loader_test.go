package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFile_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	content := []byte(`
image: "ubuntu:24.04"
hostname: "test-host"
env:
  FOO: "bar"
  BAZ: "qux"
variables:
  my_var: "my_value"
volumes:
  - "/host:/container"
args:
  - "--flag"
`)
	require.NoError(t, os.WriteFile(path, content, 0644))
	cfg, err := LoadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "ubuntu:24.04", cfg.Image)
	assert.Equal(t, "test-host", cfg.Hostname)
	assert.Equal(t, "bar", cfg.Env["FOO"])
	assert.Equal(t, "qux", cfg.Env["BAZ"])
	assert.Equal(t, "my_value", cfg.Variables["my_var"])
	assert.Equal(t, []string{"/host:/container"}, cfg.Volumes)
	assert.Equal(t, []string{"--flag"}, cfg.Args)
}

func TestLoadFile_Missing(t *testing.T) {
	cfg, err := LoadFile("/nonexistent/path.yaml")
	assert.NoError(t, err, "missing file should not error, returns empty config")
	assert.Equal(t, "", cfg.Image)
}

func TestLoadFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))
	cfg, err := LoadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, "", cfg.Image)
}

func TestLoadFile_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("invalid: yaml: [unterminated"), 0644))
	_, err := LoadFile(path)
	assert.Error(t, err, "malformed YAML should return error")
}

func TestLoadFileStrict_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte("image: test:latest\nhostname: test\n"), 0644))
	cfg, warnings, err := LoadFileStrict(path)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "test:latest", cfg.Image)
}

func TestLoadFileStrict_UnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte("image: test:latest\nimagee: typo\nunknown_field: value\n"), 0644))
	cfg, warnings, err := LoadFileStrict(path)
	require.NoError(t, err)
	assert.Equal(t, "test:latest", cfg.Image, "valid fields should still be loaded")
	assert.NotEmpty(t, warnings, "unknown keys should produce warnings")
	found := false
	for _, w := range warnings {
		if assert.Contains(t, w, "not found in type") {
			found = true
		}
	}
	assert.True(t, found, "warnings should mention unknown field")
}

func TestLoadFileStrict_MissingFile(t *testing.T) {
	cfg, warnings, err := LoadFileStrict("/nonexistent/path.yaml")
	assert.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "", cfg.Image)
}

func TestLoadFileStrict_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))
	cfg, warnings, err := LoadFileStrict(path)
	assert.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "", cfg.Image)
}

func TestLoadFileStrict_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("invalid: yaml: [unterminated"), 0644))
	_, _, err := LoadFileStrict(path)
	assert.Error(t, err)
}

func TestLoadFileStrict_CommentOnlyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	// File with only comments — yaml.Decoder returns io.EOF, should be treated as empty
	require.NoError(t, os.WriteFile(path, []byte("# This is a comment\n# Another comment\n"), 0644))
	cfg, warnings, err := LoadFileStrict(path)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, "", cfg.Image)
}

func TestLoadFileStrict_CommentedKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	// Commented-out keys should NOT produce warnings
	require.NoError(t, os.WriteFile(path, []byte("image: test:latest\n# imagee: typo\n"), 0644))
	_, warnings, err := LoadFileStrict(path)
	require.NoError(t, err)
	assert.Empty(t, warnings, "commented keys should not produce warnings")
}

// TestLoadFile_MCPServersOrderPreserved verifies that the declaration order of
// mcp_servers in YAML is captured in Config.MCPServersOrder, even though the
// Go map iteration itself is non-deterministic. Mirrors tools ordering.
func TestLoadFile_MCPServersOrderPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yaml")
	content := []byte(`
mcp_servers:
  zeta:
    command: "z-cmd"
  alpha:
    command: "a-cmd"
  middle:
    command: "m-cmd"
  beta:
    command: "b-cmd"
`)
	require.NoError(t, os.WriteFile(path, content, 0644))
	cfg, err := LoadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"zeta", "alpha", "middle", "beta"}, cfg.MCPServersOrder)
}

// TestLoadFileStrict_MCPServersOrderPreserved verifies strict-mode loader also
// populates MCPServersOrder alongside yaml.Unmarshal.
func TestLoadFileStrict_MCPServersOrderPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yaml")
	content := []byte(`
mcp_servers:
  first:
    command: "f"
  second:
    command: "s"
  third:
    command: "t"
`)
	require.NoError(t, os.WriteFile(path, content, 0644))
	cfg, warnings, err := LoadFileStrict(path)
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{"first", "second", "third"}, cfg.MCPServersOrder)
}
