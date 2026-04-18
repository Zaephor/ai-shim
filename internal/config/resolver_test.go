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

func TestResolve_DINDTLSEnvVarOverride(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))
	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	t.Setenv("AI_SHIM_DIND_TLS", "1")

	cfg, err := Resolve(dir, "test", "test")
	require.NoError(t, err)
	assert.True(t, cfg.IsDINDTLSEnabled(), "AI_SHIM_DIND_TLS=1 should enable TLS")
}

func TestResolve_GitEnvVarOverrides(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))
	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	t.Setenv("AI_SHIM_GIT_NAME", "Env User")
	t.Setenv("AI_SHIM_GIT_EMAIL", "env@example.com")

	cfg, err := Resolve(dir, "test", "test")
	require.NoError(t, err)
	require.NotNil(t, cfg.Git)
	assert.Equal(t, "Env User", cfg.Git.Name)
	assert.Equal(t, "env@example.com", cfg.Git.Email)
}

func TestResolve_AllEnvVarOverrides(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))
	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	t.Setenv("AI_SHIM_VERSION", "1.2.3")
	t.Setenv("AI_SHIM_DIND", "1")
	t.Setenv("AI_SHIM_DIND_GPU", "true")
	t.Setenv("AI_SHIM_NETWORK_SCOPE", "profile")
	t.Setenv("AI_SHIM_DIND_HOSTNAME", "custom-dind")
	t.Setenv("AI_SHIM_DIND_CACHE", "1")
	t.Setenv("AI_SHIM_SECURITY_PROFILE", "strict")
	t.Setenv("AI_SHIM_UPDATE_INTERVAL", "7d")

	cfg, err := Resolve(dir, "test", "test")
	require.NoError(t, err)

	assert.Equal(t, "1.2.3", cfg.Version)
	assert.True(t, cfg.IsDINDEnabled())
	assert.True(t, cfg.IsDINDGPUEnabled())
	assert.Equal(t, "profile", cfg.NetworkScope)
	assert.Equal(t, "custom-dind", cfg.DINDHostname)
	assert.True(t, cfg.IsCacheEnabled())
	assert.Equal(t, "strict", cfg.SecurityProfile)
	assert.Equal(t, "7d", cfg.UpdateInterval)
}

func TestResolve_UnknownKeysStillResolves(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	// Config with a valid field AND an unknown field
	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "test:latest"
imagee: "typo"
`)

	// Resolve should succeed — unknown keys produce warnings, not errors
	cfg, err := Resolve(dir, "test", "test")
	require.NoError(t, err)
	assert.Equal(t, "test:latest", cfg.Image, "valid fields should still be loaded")
}

// --- Profile extends tests ---

func TestResolve_ExtendsBasicInheritance(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	// Parent profile has tool jq
	writeYAML(t, filepath.Join(dir, "profiles", "parent.yaml"), `
tools:
  jq:
    type: binary-download
    url: "https://example.com/jq"
    binary: jq
image: "parent-image"
`)

	// Child profile extends parent, adds tool ruff, overrides image
	writeYAML(t, filepath.Join(dir, "profiles", "child.yaml"), `
extends: parent
tools:
  ruff:
    type: binary-download
    url: "https://example.com/ruff"
    binary: ruff
image: "child-image"
`)

	cfg, err := Resolve(dir, "test", "child")
	require.NoError(t, err)

	// Child image overrides parent
	assert.Equal(t, "child-image", cfg.Image, "child image should override parent")

	// Both tools present
	require.Contains(t, cfg.Tools, "jq", "parent tool inherited")
	require.Contains(t, cfg.Tools, "ruff", "child tool present")
	assert.Equal(t, "jq", cfg.Tools["jq"].Binary)
	assert.Equal(t, "ruff", cfg.Tools["ruff"].Binary)

	// Extends should not leak into final config
	assert.Empty(t, cfg.Extends, "extends should be cleared from merged result")
}

func TestResolve_ExtendsChain(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	// Grandparent: tool-a
	writeYAML(t, filepath.Join(dir, "profiles", "grandparent.yaml"), `
tools:
  tool-a:
    type: binary-download
    url: "https://example.com/a"
    binary: a
`)

	// Parent extends grandparent, adds tool-b
	writeYAML(t, filepath.Join(dir, "profiles", "parent.yaml"), `
extends: grandparent
tools:
  tool-b:
    type: binary-download
    url: "https://example.com/b"
    binary: b
`)

	// Child extends parent, adds tool-c
	writeYAML(t, filepath.Join(dir, "profiles", "child.yaml"), `
extends: parent
tools:
  tool-c:
    type: binary-download
    url: "https://example.com/c"
    binary: c
`)

	cfg, err := Resolve(dir, "test", "child")
	require.NoError(t, err)

	require.Contains(t, cfg.Tools, "tool-a", "grandparent tool inherited")
	require.Contains(t, cfg.Tools, "tool-b", "parent tool inherited")
	require.Contains(t, cfg.Tools, "tool-c", "child tool present")
}

func TestResolve_ExtendsCircularDetection(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	// A extends B, B extends A
	writeYAML(t, filepath.Join(dir, "profiles", "alpha.yaml"), `
extends: beta
image: "alpha"
`)
	writeYAML(t, filepath.Join(dir, "profiles", "beta.yaml"), `
extends: alpha
image: "beta"
`)

	_, err := Resolve(dir, "test", "alpha")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular")
}

func TestResolve_ExtendsMissing(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	writeYAML(t, filepath.Join(dir, "profiles", "child.yaml"), `
extends: nonexistent
image: "child"
`)

	_, err := Resolve(dir, "test", "child")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
}

func TestResolve_ExtendsToolsOrder(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), "image: test\n")

	// Parent declares tools in order [a, b]
	writeYAML(t, filepath.Join(dir, "profiles", "parent.yaml"), `
tools:
  tool-a:
    type: binary-download
    url: "https://example.com/a"
    binary: a
  tool-b:
    type: binary-download
    url: "https://example.com/b"
    binary: b
`)

	// Child extends parent, adds tool-c
	writeYAML(t, filepath.Join(dir, "profiles", "child.yaml"), `
extends: parent
tools:
  tool-c:
    type: binary-download
    url: "https://example.com/c"
    binary: c
`)

	cfg, err := Resolve(dir, "test", "child")
	require.NoError(t, err)

	assert.Equal(t, []string{"tool-a", "tool-b", "tool-c"}, cfg.ToolsOrder,
		"parent tools order preserved, child tool appended")
}
