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
