package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/storage"
)

func TestListAgents(t *testing.T) {
	output := ListAgents()
	assert.Contains(t, output, "claude-code")
	assert.Contains(t, output, "gemini-cli")
	assert.Contains(t, output, "aider")
	assert.Contains(t, output, "goose")
}

func TestListProfiles_Empty(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	output, err := ListProfiles(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "No profiles")
}

func TestListProfiles_WithProfiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "work"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "personal"), 0755))

	layout := storage.NewLayout(root)
	output, err := ListProfiles(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "work")
	assert.Contains(t, output, "personal")
}

func TestShowConfig(t *testing.T) {
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
	output, err := ShowConfig(layout, "claude-code", "work")
	require.NoError(t, err)
	assert.Contains(t, output, "ubuntu:24.04")
	assert.Contains(t, output, "test")
}

func TestDoctor(t *testing.T) {
	output := Doctor()
	assert.Contains(t, output, "ai-shim doctor")
	// Docker may or may not be available
	assert.Contains(t, output, "Docker")
}

func TestDoctor_ChecksDefaultImage(t *testing.T) {
	output := Doctor()
	// Should mention the default image regardless of whether it's cached
	assert.Contains(t, output, container.DefaultImage)
}

func TestCleanup_ReturnsResult(t *testing.T) {
	result, err := Cleanup()
	require.NoError(t, err)
	// Verify the result type has container, network, and volume fields.
	// With no orphaned resources these are nil slices, but the fields must exist.
	_ = result.RemovedContainers
	_ = result.RemovedNetworks
	_ = result.RemovedVolumes
	_ = result.Failed
}

func TestCreateSymlink(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	path, err := CreateSymlink("claude-code", "work", dir, shimPath)
	require.NoError(t, err)
	assert.Contains(t, path, "claude-code_work")

	// Verify symlink exists and points correctly
	target, err := os.Readlink(path)
	require.NoError(t, err)
	assert.Equal(t, shimPath, target)
}

func TestCreateSymlink_DefaultProfile(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	path, err := CreateSymlink("claude-code", "default", dir, shimPath)
	require.NoError(t, err)
	assert.Contains(t, path, "claude-code")
	assert.NotContains(t, path, "_default")
}

func TestCreateSymlink_AlreadyExists(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	_, err := CreateSymlink("claude-code", "work", dir, shimPath)
	require.NoError(t, err)

	_, err = CreateSymlink("claude-code", "work", dir, shimPath)
	assert.Error(t, err, "should fail if symlink already exists")
}

func TestListSymlinks(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))
	require.NoError(t, os.Symlink(shimPath, filepath.Join(dir, "claude-code_work")))
	require.NoError(t, os.Symlink(shimPath, filepath.Join(dir, "gemini_test")))

	links, err := ListSymlinks(dir, shimPath)
	require.NoError(t, err)
	assert.Len(t, links, 2)
}

func TestRemoveSymlink(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))
	linkPath := filepath.Join(dir, "test-link")
	require.NoError(t, os.Symlink(shimPath, linkPath))

	err := RemoveSymlink(linkPath)
	assert.NoError(t, err)
	_, err = os.Lstat(linkPath)
	assert.True(t, os.IsNotExist(err))
}

func TestDryRun_UsesDefaultImage(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	// Write a config with no image set so the default is used
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("hostname: \"\"\n"), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", nil)
	require.NoError(t, err)
	assert.Contains(t, output, container.DefaultImage, "should use container.DefaultImage when no image configured")
	assert.Contains(t, output, container.DefaultHostname, "should use container.DefaultHostname when no hostname configured")
}

func TestStatus(t *testing.T) {
	output, err := Status()
	require.NoError(t, err)
	// May have 0 containers or some - just verify it doesn't error
	assert.NotEmpty(t, output)
}

func TestDryRun(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("image: test:latest\nhostname: test\n"), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", []string{"--verbose"})
	require.NoError(t, err)
	assert.Contains(t, output, "test:latest")
	assert.Contains(t, output, "test")
	assert.Contains(t, output, "--verbose")
}

func TestShowConfig_ShowsAllFields(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
hostname: "test"
dind: true
gpu: false
network_scope: profile
dind_hostname: my-dind
packages:
  - tmux
`), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfig(layout, "claude-code", "work")
	require.NoError(t, err)
	assert.Contains(t, output, "dind:")
	assert.Contains(t, output, "gpu:")
	assert.Contains(t, output, "network_scope:")
	assert.Contains(t, output, "packages:")
}

func TestDryRun_ShowsResources(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
hostname: "test"
resources:
  memory: "4g"
  cpus: "2.0"
dind_resources:
  memory: "2g"
  cpus: "1.0"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", nil)
	require.NoError(t, err)
	assert.Contains(t, output, "Resources:")
	assert.Contains(t, output, "memory: 4g")
	assert.Contains(t, output, "cpus:   2.0")
	assert.Contains(t, output, "DIND Resources:")
	assert.Contains(t, output, "memory: 2g")
	assert.Contains(t, output, "cpus:   1.0")
}
