package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ai-shim/ai-shim/internal/color"
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

func TestShowConfig_SourceAnnotations(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "ubuntu:24.04"
`), 0644))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "agents", "claude.yaml"), []byte(`
hostname: "claude-host"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfig(layout, "claude", "work")
	require.NoError(t, err)

	assert.Contains(t, output, "(from default.yaml)", "image source annotation")
	assert.Contains(t, output, "(from agent:claude)", "hostname source annotation")
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

func TestDoctor_ShowsImagePinningStatus(t *testing.T) {
	output := Doctor()
	assert.Contains(t, output, "Image pinning:")
	assert.Contains(t, output, "agent image:")
	assert.Contains(t, output, "dind image:")
	assert.Contains(t, output, "cache image:")
	// Default images are tag-based
	assert.Contains(t, output, "tag, default")
}

func TestImagePinLabel(t *testing.T) {
	assert.Equal(t, "tag, default", imagePinLabel("ubuntu:24.04", true))
	assert.Equal(t, "tag", imagePinLabel("ubuntu:24.04", false))
	hash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	assert.Equal(t, "pinned, default", imagePinLabel("ubuntu@sha256:"+hash, true))
	assert.Equal(t, "pinned", imagePinLabel("ubuntu@sha256:"+hash, false))
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

func TestStatus_Format(t *testing.T) {
	output, err := Status()
	require.NoError(t, err)
	// Output should have headers or "No running" message
	assert.True(t, strings.Contains(output, "NAME") || strings.Contains(output, "No running"),
		"should have table headers or empty message")
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

func TestShowConfig_CoversAllConfigFields(t *testing.T) {
	// Create a config with ALL fields populated
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	// Write a config that sets every field
	fullConfig := `
image: "test-image"
hostname: "test-host"
version: "1.0.0"
env:
  KEY: "value"
variables:
  var1: "val1"
args:
  - "--flag"
volumes:
  - "/a:/b"
ports:
  - "8080:80"
packages:
  - tmux
dind: true
dind_gpu: true
gpu: true
network_scope: global
dind_hostname: my-dind
dind_mirrors:
  - https://mirror.example.com
dind_cache: true
isolated: false
allow_agents:
  - gemini-cli
resources:
  memory: "2g"
  cpus: "1.0"
dind_resources:
  memory: "1g"
  cpus: "0.5"
mcp_servers:
  filesystem:
    command: npx
    args:
      - "-y"
      - "@modelcontextprotocol/server-filesystem"
    env:
      MCP_ROOT: "/workspace"
tools:
  act:
    type: binary-download
    url: https://example.com/act
    binary: act
git:
  name: "Test User"
  email: "test@example.com"
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(fullConfig), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfig(layout, "test", "test")
	require.NoError(t, err)

	// Every config concept should appear in output
	expectedSubstrings := []string{
		"test-image",     // image
		"test-host",      // hostname
		"1.0.0",          // version
		"KEY=value",      // env
		"--flag",         // args
		"/a:/b",          // volumes
		"8080:80",        // ports
		"tmux",           // packages
		"dind:",          // dind toggle
		"gpu:",           // gpu toggle
		"network_scope:", // network scope
		"dind_hostname:", // dind hostname
		"mirror.example", // dind mirrors
		"dind_cache:",    // dind cache
		"isolated:",      // isolation
		"gemini-cli",     // allow_agents
		"2g",             // resources memory
		"1.0",            // resources cpus
		"1g",             // dind_resources memory
		"mcp_servers:",   // mcp servers section
		"filesystem",     // mcp server name
		"act",            // tools
		"Test User",      // git name
		"test@example.com", // git email
	}

	for _, sub := range expectedSubstrings {
		assert.Contains(t, output, sub, "ShowConfig should display: %s", sub)
	}

	// dind_gpu must be displayed explicitly
	assert.Contains(t, output, "dind_gpu:", "ShowConfig should display dind_gpu field")
}

func TestBackupProfile_NonExistent(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	err := BackupProfile(layout, "nonexistent", "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestBackupProfile_Success(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	profileDir := layout.ProfileHome("test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "data.txt"), []byte("hello"), 0644))

	backupPath := filepath.Join(root, "backup.tar.gz")
	err := BackupProfile(layout, "test", backupPath)
	require.NoError(t, err)

	info, err := os.Stat(backupPath)
	require.NoError(t, err)
	assert.True(t, info.Size() > 0, "backup should not be empty")
}

func TestRestoreProfile_InvalidArchive(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	err := RestoreProfile(layout, "test", "/nonexistent/archive.tar.gz")
	assert.Error(t, err)
}

func TestBackupAndRestore_RoundTrip(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Create profile with content
	profileDir := layout.ProfileHome("test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "marker.txt"), []byte("test-data"), 0644))

	// Backup
	backupPath := filepath.Join(root, "backup.tar.gz")
	err := BackupProfile(layout, "test", backupPath)
	require.NoError(t, err)

	// Verify backup file exists
	_, err = os.Stat(backupPath)
	require.NoError(t, err)

	// Delete profile
	os.RemoveAll(profileDir)

	// Restore
	err = RestoreProfile(layout, "test", backupPath)
	require.NoError(t, err)

	// Verify content restored
	data, err := os.ReadFile(filepath.Join(profileDir, "marker.txt"))
	require.NoError(t, err)
	assert.Equal(t, "test-data", string(data))
}

func TestDiskUsage(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	os.MkdirAll(layout.SharedBin, 0755)
	os.MkdirAll(layout.ConfigDir, 0755)
	os.WriteFile(filepath.Join(layout.SharedBin, "test"), []byte("data"), 0644)

	output, err := DiskUsage(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "Shared")
	assert.Contains(t, output, "Total")
}

func TestFormatBytes(t *testing.T) {
	assert.Equal(t, "0 B", formatBytes(0))
	assert.Equal(t, "500 B", formatBytes(500))
	assert.Equal(t, "1.0 KB", formatBytes(1024))
	assert.Equal(t, "1.5 MB", formatBytes(1572864))
	assert.Equal(t, "2.0 GB", formatBytes(2147483648))
}

func TestDoctor_NoColorNoANSI(t *testing.T) {
	t.Setenv("AI_SHIM_NO_COLOR", "1")
	output := DoctorWithColor(false)
	assert.NotContains(t, output, "\033[", "should not contain ANSI codes when color disabled")
	// Should still have content
	assert.Contains(t, output, "ai-shim doctor")
}

func TestDoctor_WithColor(t *testing.T) {
	output := DoctorWithColor(true)
	// Should contain ANSI codes for OK or FAIL
	assert.Contains(t, output, "\033[", "should contain ANSI codes when color enabled")
}

func TestColorizeStatus(t *testing.T) {
	c := color.New(true)
	noColor := color.New(false)

	// Up status should be green
	assert.Contains(t, colorizeStatus(c, "Up 2 hours"), "\033[32m")

	// Exited status should be red
	assert.Contains(t, colorizeStatus(c, "Exited (1) 5 minutes ago"), "\033[31m")

	// Created should be yellow
	assert.Contains(t, colorizeStatus(c, "Created"), "\033[33m")

	// No color should have no ANSI
	assert.NotContains(t, colorizeStatus(noColor, "Up 2 hours"), "\033[")
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

func TestAgentVersions(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Create bin dir for one agent
	os.MkdirAll(layout.AgentBin("claude-code"), 0755)

	output := AgentVersions(layout)
	assert.Contains(t, output, "Installed agent versions:")
	assert.Contains(t, output, "claude-code")
	assert.Contains(t, output, "not installed") // most agents won't have bins
}

func TestAgentVersions_WithBinary(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Create a fake binary
	binDir := layout.AgentBin("claude-code")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "claude"), []byte("fake"), 0755)

	output := AgentVersions(layout)
	assert.Contains(t, output, "claude-code")
	// Binary exists but won't actually run, so it should show version unknown or error
}

func TestReinstall_Success(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Create bin dir with a file
	binDir := layout.AgentBin("claude-code")
	os.MkdirAll(binDir, 0755)
	os.WriteFile(filepath.Join(binDir, "claude"), []byte("binary"), 0755)

	err := Reinstall(layout, "claude-code")
	require.NoError(t, err)

	// Bin dir should still exist but be empty
	entries, err := os.ReadDir(binDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "bin dir should be empty after reinstall")
}

func TestReinstall_UnknownAgent(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := Reinstall(layout, "nonexistent-agent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown agent")
}

func TestReinstall_NotInstalled(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := Reinstall(layout, "claude-code")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not installed")
}

func TestShowConfig_NetworkRulesAndSecurityProfile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
hostname: "test"
network_rules:
  allowed_hosts:
    - api.example.com
  allowed_ports:
    - "443"
security_profile: strict
`), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfig(layout, "claude-code", "work")
	require.NoError(t, err)
	assert.Contains(t, output, "network_rules:")
	assert.Contains(t, output, "api.example.com")
	assert.Contains(t, output, "443")
	assert.Contains(t, output, "security_profile: strict")
}
