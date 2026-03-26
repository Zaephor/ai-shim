package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "ghcr.io/catthehacker/ubuntu:act-24.04"
hostname: "ai-shim"
variables:
  llm_host: "default-host:8080"
env:
  LLM_ENDPOINT: "{{ .llm_host }}"
`)

	writeYAML(t, filepath.Join(dir, "agents", "claude.yaml"), `
env:
  LLM_ENDPOINT: "https://{{ .llm_host }}/v1"
  CLAUDE_SPECIFIC: "yes"
`)

	writeYAML(t, filepath.Join(dir, "profiles", "work.yaml"), `
volumes:
  - "/work/shared:/shared"
`)

	writeYAML(t, filepath.Join(dir, "agent-profiles", "claude_work.yaml"), `
hostname: "claude-work"
args:
  - "--profile=work"
`)

	return dir
}

func writeYAML(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func TestResolve_FullTierMerge(t *testing.T) {
	configDir := setupConfigDir(t)

	cfg, err := Resolve(configDir, "claude", "work")
	require.NoError(t, err)

	assert.Equal(t, "claude-work", cfg.Hostname, "agent-profile tier wins for hostname")
	assert.Equal(t, "ghcr.io/catthehacker/ubuntu:act-24.04", cfg.Image, "default image preserved")
	assert.Equal(t, "https://default-host:8080/v1", cfg.Env["LLM_ENDPOINT"], "agent tier overrides and templates resolve")
	assert.Equal(t, "yes", cfg.Env["CLAUDE_SPECIFIC"], "agent-specific env carried through")
	assert.Equal(t, []string{"/work/shared:/shared"}, cfg.Volumes, "profile volumes present")
	assert.Equal(t, []string{"--profile=work"}, cfg.Args, "agent-profile args present")
}

func TestResolve_MissingTiers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "ubuntu:24.04"
`)

	cfg, err := Resolve(dir, "nonexistent", "noprofile")
	require.NoError(t, err)
	assert.Equal(t, "ubuntu:24.04", cfg.Image, "default still applies")
}

func TestResolve_EnvVarOverride(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "default-image"
hostname: "default-host"
`)

	t.Setenv("AI_SHIM_IMAGE", "env-image")

	cfg, err := Resolve(dir, "test", "test")
	require.NoError(t, err)

	assert.Equal(t, "env-image", cfg.Image, "env var overrides image")
	assert.Equal(t, "default-host", cfg.Hostname, "non-overridden field preserved")
}

func TestResolve_EnvVarOverridesAllTiers(t *testing.T) {
	configDir := setupConfigDir(t)

	// default.yaml has image "ghcr.io/catthehacker/ubuntu:act-24.04"
	// Set env var to override
	t.Setenv("AI_SHIM_IMAGE", "override-image:latest")
	t.Setenv("AI_SHIM_GPU", "1")

	cfg, err := Resolve(configDir, "claude", "work")
	require.NoError(t, err)

	assert.Equal(t, "override-image:latest", cfg.Image, "env var should override all tiers")
	assert.True(t, cfg.IsGPUEnabled(), "AI_SHIM_GPU=1 should enable GPU")
}
