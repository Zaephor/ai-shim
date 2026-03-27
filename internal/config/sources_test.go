package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeSources_ScalarLastWins(t *testing.T) {
	tiers := []namedConfig{
		{name: "default.yaml", config: Config{Image: "base-image"}},
		{name: "agent:claude", config: Config{Image: "agent-image"}},
		{name: "env", config: Config{}},
	}
	sources := computeSources(tiers)
	assert.Equal(t, "agent:claude", sources.Source("image"))
}

func TestComputeSources_EnvOverrides(t *testing.T) {
	tiers := []namedConfig{
		{name: "default.yaml", config: Config{Image: "base"}},
		{name: "env", config: Config{Image: "env-override"}},
	}
	sources := computeSources(tiers)
	assert.Equal(t, "env", sources.Source("image"))
}

func TestComputeSources_UnsetField(t *testing.T) {
	tiers := []namedConfig{
		{name: "default.yaml", config: Config{Image: "base"}},
	}
	sources := computeSources(tiers)
	assert.Equal(t, "", sources.Source("version"), "unset field should have empty source")
}

func TestComputeSources_AllFieldTypes(t *testing.T) {
	trueVal := true
	tiers := []namedConfig{
		{name: "default.yaml", config: Config{
			Image:    "img",
			Hostname: "host",
			Version:  "1.0",
			DIND:     &trueVal,
			Env:      map[string]string{"K": "V"},
			Volumes:  []string{"/a:/b"},
			Tools:    map[string]ToolDef{"t": {Type: "binary-download"}},
			MCPServers: map[string]MCPServerDef{"s": {Command: "cmd"}},
		}},
	}
	sources := computeSources(tiers)
	for _, field := range []string{"image", "hostname", "version", "dind", "env", "volumes", "tools", "mcp_servers"} {
		assert.Equal(t, "default.yaml", sources.Source(field), "field %s should be from default.yaml", field)
	}
}

func TestFormatSource(t *testing.T) {
	sources := NewConfigSources()
	sources.Fields["image"] = "default.yaml"

	assert.Equal(t, " (from default.yaml)", sources.FormatSource("image"))
	assert.Equal(t, "", sources.FormatSource("version"), "unset field should return empty string")
}

func TestResolveWithSources_FullTierTracking(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeFile(t, filepath.Join(dir, "default.yaml"), `
image: "ubuntu:24.04"
hostname: "default-host"
`)
	writeFile(t, filepath.Join(dir, "agents", "claude.yaml"), `
hostname: "claude-host"
env:
  CLAUDE: "yes"
`)
	writeFile(t, filepath.Join(dir, "profiles", "work.yaml"), `
volumes:
  - "/work:/work"
`)
	writeFile(t, filepath.Join(dir, "agent-profiles", "claude_work.yaml"), `
version: "2.0"
`)

	cfg, sources, err := ResolveWithSources(dir, "claude", "work")
	require.NoError(t, err)

	assert.Equal(t, "ubuntu:24.04", cfg.Image)
	assert.Equal(t, "default.yaml", sources.Source("image"))

	assert.Equal(t, "claude-host", cfg.Hostname)
	assert.Equal(t, "agent:claude", sources.Source("hostname"))

	assert.Equal(t, "2.0", cfg.Version)
	assert.Equal(t, "agent-profile:claude_work", sources.Source("version"))

	assert.Equal(t, "profile:work", sources.Source("volumes"))
	assert.Equal(t, "agent:claude", sources.Source("env"))
}

func TestResolveWithSources_EnvOverride(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeFile(t, filepath.Join(dir, "default.yaml"), `image: "base"`)

	t.Setenv("AI_SHIM_IMAGE", "env-image")

	cfg, sources, err := ResolveWithSources(dir, "test", "test")
	require.NoError(t, err)

	assert.Equal(t, "env-image", cfg.Image)
	assert.Equal(t, "env", sources.Source("image"))
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}
