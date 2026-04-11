package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"context"
	"time"

	"github.com/ai-shim/ai-shim/internal/color"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/testutil"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
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

func TestListProfiles_WithRuntimeOnly(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "work"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "personal"), 0755))

	layout := storage.NewLayout(root)
	output, err := ListProfiles(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "work")
	assert.Contains(t, output, "personal")
	assert.NotContains(t, output, "not yet launched")
}

func TestListProfiles_ConfigDefined(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config", "profiles")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "dev.yaml"), []byte("image: ubuntu\n"), 0644))

	layout := storage.NewLayout(root)
	output, err := ListProfiles(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "dev")
	assert.Contains(t, output, "not yet launched")
}

func TestListProfiles_MixedConfigAndRuntime(t *testing.T) {
	root := t.TempDir()
	// Runtime profile (launched)
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "work"), 0755))
	// Config-defined profile (not yet launched)
	configDir := filepath.Join(root, "config", "profiles")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "dev.yaml"), []byte("image: ubuntu\n"), 0644))
	// Config-defined AND launched
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "work.yaml"), []byte("image: ubuntu\n"), 0644))

	layout := storage.NewLayout(root)
	output, err := ListProfiles(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "work\n")        // launched, no annotation
	assert.Contains(t, output, "dev  (not yet") // config-only, annotated
}

func TestListProfilesJSON_MixedConfigAndRuntime(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "profiles", "work"), 0755))
	configDir := filepath.Join(root, "config", "profiles")
	require.NoError(t, os.MkdirAll(configDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "dev.yaml"), []byte("image: ubuntu\n"), 0644))

	layout := storage.NewLayout(root)
	output, err := ListProfilesJSON(layout)
	require.NoError(t, err)

	var entries []ProfileEntry
	require.NoError(t, json.Unmarshal([]byte(output), &entries))
	assert.Len(t, entries, 2)

	// Entries should be sorted
	assert.Equal(t, "dev", entries[0].Name)
	assert.False(t, entries[0].Launched)
	assert.Equal(t, "work", entries[1].Name)
	assert.True(t, entries[1].Launched)
}

func TestShowLogs_NoLogFile(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	output, err := ShowLogs(layout, "", "", 50)
	require.NoError(t, err)
	assert.Contains(t, output, "No logs found")
}

func TestShowLogs_WithEntries(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	logContent := "2026-03-30T20:00:00Z action=launch agent=claude-code profile=work container=c1 image=ubuntu\n" +
		"2026-03-30T20:01:00Z action=exit container=c1 exit_code=0\n" +
		"2026-03-30T20:05:00Z action=launch agent=aider profile=default container=c2 image=ubuntu\n"
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "ai-shim.log"), []byte(logContent), 0644))

	layout := storage.NewLayout(root)

	// Show all
	output, err := ShowLogs(layout, "", "", 50)
	require.NoError(t, err)
	assert.Contains(t, output, "agent=claude-code")
	assert.Contains(t, output, "agent=aider")

	// Filter by agent
	output, err = ShowLogs(layout, "claude-code", "", 50)
	require.NoError(t, err)
	assert.Contains(t, output, "agent=claude-code")
	assert.NotContains(t, output, "agent=aider")

	// Filter by agent+profile
	output, err = ShowLogs(layout, "aider", "default", 50)
	require.NoError(t, err)
	assert.Contains(t, output, "agent=aider")
	assert.NotContains(t, output, "agent=claude-code")
}

func TestShowLogs_TailN(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	var lines string
	for i := 0; i < 10; i++ {
		lines += fmt.Sprintf("2026-03-30T20:%02d:00Z action=launch agent=a%d profile=p container=c%d image=img\n", i, i, i)
	}
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "ai-shim.log"), []byte(lines), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowLogs(layout, "", "", 3)
	require.NoError(t, err)
	resultLines := strings.Split(strings.TrimSpace(output), "\n")
	assert.Len(t, resultLines, 3)
	assert.Contains(t, resultLines[0], "agent=a7") // last 3 lines
}

func TestStripDockerLogHeaders(t *testing.T) {
	// Build a Docker multiplexed log frame: stdout(1), 3 padding, 4-byte size, payload
	payload := []byte("hello world\n")
	frame := make([]byte, 8+len(payload))
	frame[0] = 1 // stdout
	frame[4] = byte(len(payload) >> 24)
	frame[5] = byte(len(payload) >> 16)
	frame[6] = byte(len(payload) >> 8)
	frame[7] = byte(len(payload))
	copy(frame[8:], payload)

	result := stripDockerLogHeaders(frame)
	assert.Equal(t, payload, result)
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
	testutil.SkipIfNoDocker(t)
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

func TestCreateSymlink_RejectsInvalidProfile(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	cases := []string{"日本語", "Pro File", "-leading", ".leading", "profile/slash"}
	for _, profile := range cases {
		_, err := CreateSymlink("claude-code", profile, dir, shimPath)
		assert.Error(t, err, "should reject profile %q", profile)
	}
}

func TestCreateSymlink_RejectsInvalidAgent(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	cases := []string{"日本語", "Pro Agent", "-leading", ".leading", "agent/x", ""}
	for _, agent := range cases {
		_, err := CreateSymlink(agent, "work", dir, shimPath)
		assert.Error(t, err, "should reject agent %q", agent)
	}
}

func TestCreateSymlink_AcceptsDottedProfile(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	path, err := CreateSymlink("claude-code", "my.profile", dir, shimPath)
	require.NoError(t, err)
	assert.Contains(t, path, "claude-code_my.profile")
}

func TestCreateSymlink_RejectsTooLongProfile(t *testing.T) {
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	long := ""
	for i := 0; i < 64; i++ {
		long += "a"
	}
	_, err := CreateSymlink("claude-code", long, dir, shimPath)
	assert.Error(t, err)
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

func TestResolveSymlinkDir_ExplicitWins(t *testing.T) {
	// Explicit argument beats both config and the home-dir default.
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("symlink_dir: /this-should-be-ignored\n"),
		0644,
	))
	override := t.TempDir()

	got, err := ResolveSymlinkDir(configDir, override)
	require.NoError(t, err)
	assert.Equal(t, override, got)
}

func TestResolveSymlinkDir_FromConfig(t *testing.T) {
	// With no explicit argument, fall back to default.yaml's symlink_dir.
	configDir := t.TempDir()
	target := filepath.Join(t.TempDir(), "custom-bin")
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("symlink_dir: "+target+"\n"),
		0644,
	))

	got, err := ResolveSymlinkDir(configDir, "")
	require.NoError(t, err)
	assert.Equal(t, target, got)
}

func TestResolveSymlinkDir_ExpandsTilde(t *testing.T) {
	// Config values like `~/.local/bin` should expand to $HOME/.local/bin.
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("symlink_dir: ~/scratch/agents\n"),
		0644,
	))

	got, err := ResolveSymlinkDir(configDir, "")
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "scratch/agents"), got)
}

func TestResolveSymlinkDir_DefaultsToHomeLocalBin(t *testing.T) {
	// With no explicit arg and no config, the default is $HOME/.local/bin.
	configDir := t.TempDir() // no default.yaml written

	got, err := ResolveSymlinkDir(configDir, "")
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, ".local", "bin"), got)
}

func TestResolveSymlinkDir_ExplicitArgExpandsTilde(t *testing.T) {
	// Explicit CLI arg should also expand a leading ~.
	configDir := t.TempDir()

	got, err := ResolveSymlinkDir(configDir, "~/bin")
	require.NoError(t, err)
	home, _ := os.UserHomeDir()
	assert.Equal(t, filepath.Join(home, "bin"), got)
}

func TestCreateSymlink_PermissionErrorHintsSudo(t *testing.T) {
	// Create a read-only target directory and attempt to install a
	// symlink into it. The returned error must mention sudo so the user
	// has a clear next step.
	if os.Getuid() == 0 {
		t.Skip("running as root; cannot test permission-denied symlink")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0555)) // r-xr-xr-x: no writes
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	shimPath := filepath.Join(t.TempDir(), "ai-shim")
	require.NoError(t, os.WriteFile(shimPath, []byte(""), 0755))

	_, err := CreateSymlink("claude-code", "default", dir, shimPath)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "sudo",
		"permission-denied error should suggest sudo or a user-writable symlink_dir")
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

func TestRemoveSymlink_NonexistentPath(t *testing.T) {
	err := RemoveSymlink(filepath.Join(t.TempDir(), "does-not-exist"))
	assert.Error(t, err)
}

func TestRemoveSymlink_NotASymlink(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "regular-file")
	require.NoError(t, os.WriteFile(regularFile, []byte("data"), 0644))

	err := RemoveSymlink(regularFile)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a symlink")
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
	testutil.SkipIfNoDocker(t)
	output, err := Status()
	require.NoError(t, err)
	// May have 0 containers or some - just verify it doesn't error
	assert.NotEmpty(t, output)
}

func TestStatus_Format(t *testing.T) {
	testutil.SkipIfNoDocker(t)
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
dind_tls: true
isolated: false
security_profile: strict
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
		"test-image",        // image
		"test-host",         // hostname
		"1.0.0",             // version
		"KEY=***",           // env (sensitive key, masked)
		"--flag",            // args
		"/a:/b",             // volumes
		"8080:80",           // ports
		"tmux",              // packages
		"dind:",             // dind toggle
		"gpu:",              // gpu toggle
		"network_scope:",    // network scope
		"dind_hostname:",    // dind hostname
		"mirror.example",    // dind mirrors
		"dind_cache:",       // dind cache
		"dind_tls:",         // dind tls
		"isolated:",         // isolation
		"security_profile:", // security profile
		"gemini-cli",        // allow_agents
		"2g",                // resources memory
		"1.0",               // resources cpus
		"1g",                // dind_resources memory
		"mcp_servers:",      // mcp servers section
		"filesystem",        // mcp server name
		"act",               // tools
		"Test User",         // git name
		"test@example.com",  // git email
		"var1=val1",         // variables
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

func TestBackupProfile_AutoGeneratedPath(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	profileDir := layout.ProfileHome("test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "data.txt"), []byte("hello"), 0644))

	// Change to temp dir so auto-generated file lands there
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(root))
	defer os.Chdir(origDir)

	err := BackupProfile(layout, "test", "")
	require.NoError(t, err)

	// Should have created a file matching ai-shim-backup-test-*.tar.gz
	entries, _ := os.ReadDir(root)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "ai-shim-backup-test-") && strings.HasSuffix(e.Name(), ".tar.gz") {
			found = true
		}
	}
	assert.True(t, found, "auto-generated backup file should exist")
}

// TestScanAIShimSymlinks_DetectsBroken creates a tmp directory containing
// one healthy ai-shim symlink and one broken one, and verifies the scanner
// classifies them correctly.
func TestScanAIShimSymlinks_DetectsBroken(t *testing.T) {
	dir := t.TempDir()

	// Create a real "ai-shim" binary file as the valid target.
	validTarget := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(validTarget, []byte("#!/bin/sh\n"), 0755))

	// Healthy symlink: claude-code -> ai-shim
	healthy := filepath.Join(dir, "claude-code")
	require.NoError(t, os.Symlink(validTarget, healthy))

	// Broken symlink: opencode_work -> /nonexistent/ai-shim
	brokenLink := filepath.Join(dir, "opencode_work")
	require.NoError(t, os.Symlink("/nonexistent/path/to/ai-shim", brokenLink))

	// Unrelated broken symlink — must be ignored.
	require.NoError(t, os.Symlink("/nonexistent/random-tool", filepath.Join(dir, "random-tool")))

	valid, broken := scanAIShimSymlinks([]string{dir})

	// Healthy link should be detected.
	foundHealthy := false
	for _, v := range valid {
		if v == healthy {
			foundHealthy = true
		}
	}
	assert.True(t, foundHealthy, "healthy ai-shim symlink should be reported as valid: %v", valid)

	// Broken link should be detected.
	require.Len(t, broken, 1, "expected exactly 1 broken ai-shim symlink, got: %v", broken)
	assert.Equal(t, brokenLink, broken[0].Path)
	assert.Contains(t, broken[0].Target, "ai-shim")
}

// TestBackupProfile_FreeBytesPositive verifies the unix freeBytes helper
// returns a non-zero free-space value for a real temp dir.
func TestBackupProfile_FreeBytesPositive(t *testing.T) {
	free, err := freeBytes(t.TempDir())
	require.NoError(t, err)
	assert.Greater(t, free, uint64(0), "free space should be > 0 on a writable temp dir")
}

// TestBackupProfile_PartialCleanupOnFailure verifies the partial archive is
// removed if tar fails. We simulate failure by making the output path point
// at a directory that does not exist (a parent directory missing causes tar
// to fail before writing meaningful data).
func TestBackupProfile_PartialCleanupOnFailure(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	profileDir := layout.ProfileHome("test")
	require.NoError(t, os.MkdirAll(profileDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(profileDir, "data.txt"), []byte("hi"), 0644))

	// Output path inside a directory that doesn't exist — tar will fail
	// when it tries to create the file.
	bogusOut := filepath.Join(root, "no-such-dir", "backup.tar.gz")
	err := BackupProfile(layout, "test", bogusOut)
	require.Error(t, err)

	// The bogus path must not exist after the failure.
	_, statErr := os.Stat(bogusOut)
	assert.True(t, os.IsNotExist(statErr), "partial archive should be cleaned up on failure")
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

func TestDiskUsage_PerProfile(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Create two profiles with data
	for _, profile := range []string{"work", "personal"} {
		home := layout.ProfileHome(profile)
		require.NoError(t, os.MkdirAll(home, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(home, "data.txt"), []byte("content"), 0644))
	}

	output, err := DiskUsage(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "Per-profile:")
	assert.Contains(t, output, "work")
	assert.Contains(t, output, "personal")
}

func TestDiskUsage_MissingDirs(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	// Don't create any dirs — all should show "(not found)"

	output, err := DiskUsage(layout)
	require.NoError(t, err)
	assert.Contains(t, output, "(not found)")
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

	// Restarting should be yellow
	assert.Contains(t, colorizeStatus(c, "Restarting (1) 2 seconds ago"), "\033[33m")

	// Unknown status should be uncolored
	assert.Equal(t, "Removing", colorizeStatus(c, "Removing"))

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

func TestDryRun_ShowsAllFields(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
hostname: "test"
packages:
  - tmux
network_scope: profile
dind_hostname: my-dind
dind: true
gpu: true
dind_gpu: true
dind_mirrors:
  - https://mirror.example.com
dind_cache: true
dind_tls: true
isolated: false
allow_agents:
  - gemini-cli
mcp_servers:
  filesystem:
    command: npx
tools:
  act:
    type: binary-download
    url: https://example.com/act
    binary: act
git:
  name: "Test User"
  email: "test@example.com"
security_profile: strict
`), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", nil)
	require.NoError(t, err)

	expectedSubstrings := []string{
		"Packages:",
		"tmux",
		"Network:",
		"DIND Host:",
		"DIND Mirrors:",
		"mirror.example.com",
		"DIND Cache:",
		"DIND TLS:",
		"Isolated:",
		"Allow Agents:",
		"gemini-cli",
		"MCP Servers:",
		"filesystem",
		"Tools:",
		"act",
		"Git:",
		"Test User",
		"test@example.com",
		"Security:",
		"strict",
	}
	for _, sub := range expectedSubstrings {
		assert.Contains(t, output, sub, "DryRun should display: %s", sub)
	}
}

func TestDryRun_ShowsRemainingFields(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
version: "2.1.0"
variables:
  my_var: my_value
volumes:
  - /host:/container
ports:
  - "8080:80"
args:
  - "--flag"
update_interval: "7d"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", []string{"--extra-arg"})
	require.NoError(t, err)

	for _, sub := range []string{
		"Version:",
		"2.1.0",
		"Variables:",
		"my_var=my_value",
		"Volumes:",
		"/host:/container",
		"Ports:",
		"8080:80",
		"Default args:",
		"--flag",
		"Passthrough:",
		"--extra-arg",
		"Update:",
		"7d",
	} {
		assert.Contains(t, output, sub, "DryRun should display: %s", sub)
	}
}

func TestListAgents_ContainsAllBuiltins(t *testing.T) {
	output := ListAgents()
	agents := []string{
		"claude-code", "gemini-cli", "qwen-code", "codex",
		"pi", "gsd", "aider", "goose", "opencode",
	}
	for _, name := range agents {
		assert.Contains(t, output, name, "ListAgents should contain %s", name)
	}
}

func TestListAgents_ShowsInstallType(t *testing.T) {
	output := ListAgents()
	assert.Contains(t, output, "npm")
	assert.Contains(t, output, "uv")
	assert.Contains(t, output, "custom")
}

func TestDiskUsage_ReturnsOutput(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	output, err := DiskUsage(layout)
	require.NoError(t, err)
	assert.NotEmpty(t, output)
}

func TestDiskUsageJSON_ValidJSON(t *testing.T) {
	layout := storage.NewLayout(t.TempDir())
	output, err := DiskUsageJSON(layout)
	require.NoError(t, err)
	assert.NotEmpty(t, output)

	// Verify it's valid JSON
	var result map[string]interface{}
	err = json.Unmarshal([]byte(output), &result)
	assert.NoError(t, err, "DiskUsageJSON should return valid JSON")
}

func TestDoctor_ContainsChecks(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	output := Doctor()
	assert.Contains(t, output, "Docker")
}

func TestDryRun_ShowsUpdateInterval(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte("update_interval: \"7d\"\n"), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", nil)
	require.NoError(t, err)
	assert.Contains(t, output, "7d")
}

func TestShowConfig_SecurityProfile(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
hostname: "test"
security_profile: strict
`), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfig(layout, "claude-code", "work")
	require.NoError(t, err)
	assert.Contains(t, output, "security_profile: strict")
}

func TestStatus_NoContainers(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	output, err := StatusWithColor(false)
	require.NoError(t, err)
	assert.Contains(t, output, "No running ai-shim containers")
}

func TestStatusJSON_NoContainers(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	output, err := StatusJSON()
	require.NoError(t, err)
	assert.Equal(t, "[]\n", output)
}

func TestCleanup_NoOrphans(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	result, err := Cleanup()
	require.NoError(t, err)
	// Nothing to clean if no ai-shim containers exist
	assert.Empty(t, result.Failed)
}

func TestContainerDisplayName_WithNames(t *testing.T) {
	c := container_types.Summary{
		Names: []string{"/my-container"},
		ID:    "abc123def456abcdef",
	}
	assert.Equal(t, "my-container", containerDisplayName(c))
}

func TestContainerDisplayName_NoNames(t *testing.T) {
	c := container_types.Summary{
		ID: "abc123def456abcdef",
	}
	assert.Equal(t, "abc123def456", containerDisplayName(c))
}

func TestShowConfig_MasksSensitiveEnvVars(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
env:
  ANTHROPIC_API_KEY: "sk-ant-secret123"
  SAFE_VAR: "visible"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowConfig(layout, "claude-code", "work")
	require.NoError(t, err)
	assert.Contains(t, output, "ANTHROPIC_API_KEY=***", "sensitive env var value should be masked")
	assert.Contains(t, output, "SAFE_VAR=visible", "non-sensitive env var should remain visible")
	assert.NotContains(t, output, "sk-ant-secret123", "secret value must not appear in output")
}

func TestDryRun_MasksSensitiveEnvVars(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
env:
  ANTHROPIC_API_KEY: "sk-ant-secret123"
  SAFE_VAR: "visible"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "work", nil)
	require.NoError(t, err)
	assert.Contains(t, output, "ANTHROPIC_API_KEY=***", "sensitive env var value should be masked in dry-run")
	assert.Contains(t, output, "SAFE_VAR=visible", "non-sensitive env var should remain visible in dry-run")
	assert.NotContains(t, output, "sk-ant-secret123", "secret value must not appear in dry-run output")
}

// --- formatBytes boundary tests ---

func TestFormatBytes_AllBoundaries(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{1, "1 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1048575, "1024.0 KB"}, // 1 MB - 1 byte, still KB
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%d", tt.input), func(t *testing.T) {
			got := formatBytes(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// --- containerDisplayName tests ---

func TestContainerDisplayName_MultipleNames(t *testing.T) {
	c := container_types.Summary{
		Names: []string{"/first-name", "/second-name"},
		ID:    "abc123def456abcdef",
	}
	assert.Equal(t, "first-name", containerDisplayName(c))
}

func TestContainerDisplayName_EmptyNames(t *testing.T) {
	c := container_types.Summary{
		Names: []string{},
		ID:    "abc123def456abcdef",
	}
	assert.Equal(t, "abc123def456", containerDisplayName(c))
}

// --- stripDockerLogHeaders edge case tests ---

func TestStripDockerLogHeaders_EmptyInput(t *testing.T) {
	result := stripDockerLogHeaders(nil)
	assert.Nil(t, result)

	result = stripDockerLogHeaders([]byte{})
	assert.Nil(t, result)
}

func TestStripDockerLogHeaders_TruncatedHeader(t *testing.T) {
	// Less than 8 bytes — not a valid frame, should return nil
	result := stripDockerLogHeaders([]byte{1, 0, 0, 0, 0})
	assert.Nil(t, result)
}

func TestStripDockerLogHeaders_MultipleFrames(t *testing.T) {
	buildFrame := func(streamType byte, payload []byte) []byte {
		frame := make([]byte, 8+len(payload))
		frame[0] = streamType
		frame[4] = byte(len(payload) >> 24)
		frame[5] = byte(len(payload) >> 16)
		frame[6] = byte(len(payload) >> 8)
		frame[7] = byte(len(payload))
		copy(frame[8:], payload)
		return frame
	}

	p1 := []byte("hello ")
	p2 := []byte("world\n")
	data := append(buildFrame(1, p1), buildFrame(1, p2)...)
	result := stripDockerLogHeaders(data)
	assert.Equal(t, []byte("hello world\n"), result)
}

func TestStripDockerLogHeaders_StderrFrame(t *testing.T) {
	payload := []byte("error output\n")
	frame := make([]byte, 8+len(payload))
	frame[0] = 2 // stderr stream type
	frame[4] = byte(len(payload) >> 24)
	frame[5] = byte(len(payload) >> 16)
	frame[6] = byte(len(payload) >> 8)
	frame[7] = byte(len(payload))
	copy(frame[8:], payload)

	result := stripDockerLogHeaders(frame)
	assert.Equal(t, payload, result)
}

func TestStripDockerLogHeaders_OversizedFrameHeader(t *testing.T) {
	// Header says 100 bytes but only 5 bytes of data follow — should clamp
	frame := make([]byte, 8+5)
	frame[0] = 1
	frame[7] = 100 // claims 100 bytes
	copy(frame[8:], []byte("short"))

	result := stripDockerLogHeaders(frame)
	assert.Equal(t, []byte("short"), result)
}

// --- ShowLogs filter matches nothing ---

func TestShowLogs_FilterMatchesNothing(t *testing.T) {
	root := t.TempDir()
	logDir := filepath.Join(root, "logs")
	require.NoError(t, os.MkdirAll(logDir, 0755))

	logContent := "2026-03-30T20:00:00Z action=launch agent=claude-code profile=work container=c1 image=ubuntu\n"
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "ai-shim.log"), []byte(logContent), 0644))

	layout := storage.NewLayout(root)
	output, err := ShowLogs(layout, "nonexistent-agent", "nonexistent-profile", 50)
	require.NoError(t, err)
	assert.Contains(t, output, "No logs found for agent=nonexistent-agent")
}

// --- DryRun env masking with multiple key types ---

func TestDryRun_MasksMultipleKeyTypes(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "agent-profiles"), 0755))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "default.yaml"), []byte(`
image: "test:latest"
env:
  OPENAI_API_KEY: "sk-openai-secret"
  GEMINI_API_KEY: "AIza-gemini-secret"
  NORMAL_VAR: "plaintext"
`), 0644))

	layout := storage.NewLayout(root)
	output, err := DryRun(layout, "claude-code", "default", nil)
	require.NoError(t, err)
	assert.Contains(t, output, "OPENAI_API_KEY=***", "OPENAI key should be masked")
	assert.Contains(t, output, "GEMINI_API_KEY=***", "GEMINI key should be masked")
	assert.Contains(t, output, "NORMAL_VAR=plaintext", "non-sensitive var should be visible")
	assert.NotContains(t, output, "sk-openai-secret", "OpenAI secret must not appear")
	assert.NotContains(t, output, "AIza-gemini-secret", "Gemini secret must not appear")
}

func TestContainerLogs_Integration(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	containerName := "ai-shim-test-logs-" + fmt.Sprintf("%d", time.Now().UnixNano()%100000)

	// Create and start a container with ai-shim labels
	resp, err := cli.ContainerCreate(ctx, &container_types.Config{
		Image: "alpine:latest",
		Cmd:   []string{"echo", "hello"},
		Labels: map[string]string{
			container.LabelBase:    "true",
			container.LabelAgent:   "test-agent",
			container.LabelProfile: "test-profile",
		},
	}, nil, nil, nil, containerName)
	require.NoError(t, err)
	t.Cleanup(func() {
		cli.ContainerRemove(ctx, resp.ID, container_types.RemoveOptions{Force: true})
	})

	err = cli.ContainerStart(ctx, resp.ID, container_types.StartOptions{})
	require.NoError(t, err)

	// Wait for the container to finish
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, container_types.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-statusCh:
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for container to finish")
	}

	// Call ContainerLogs and verify output
	output, err := ContainerLogs("test-agent", "test-profile", 100)
	require.NoError(t, err)
	assert.Contains(t, output, containerName, "output should contain the container name")
	assert.Contains(t, output, "hello", "output should contain the echoed message")
}

func TestCleanup_WithOrphanedContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	containerName := "ai-shim-test-cleanup-" + fmt.Sprintf("%d", time.Now().UnixNano()%100000)

	// Create a stopped container with ai-shim labels (simulate orphan)
	resp, err := cli.ContainerCreate(ctx, &container_types.Config{
		Image: "alpine:latest",
		Cmd:   []string{"true"},
		Labels: map[string]string{
			container.LabelBase:    "true",
			container.LabelAgent:   "test-agent",
			container.LabelProfile: "test-profile",
		},
	}, nil, nil, nil, containerName)
	require.NoError(t, err)

	// Verify the container exists
	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("id", resp.ID)),
	})
	require.NoError(t, err)
	require.Len(t, containers, 1, "orphaned container should exist before cleanup")

	// Run Cleanup
	result, err := Cleanup()
	require.NoError(t, err)

	// The orphaned container should have been removed
	found := false
	for _, name := range result.RemovedContainers {
		if name == containerName {
			found = true
			break
		}
	}
	assert.True(t, found, "cleanup result should include the orphaned container %s, got: %v", containerName, result.RemovedContainers)

	// Verify the container is gone
	containers, err = cli.ContainerList(ctx, container_types.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("id", resp.ID)),
	})
	require.NoError(t, err)
	assert.Empty(t, containers, "container should be removed after cleanup")
}

func TestStatusWithColor_ReturnsOutput(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	output, err := StatusWithColor(false)
	require.NoError(t, err, "StatusWithColor should not error when Docker is available")
	assert.True(t,
		strings.Contains(output, "No running") || strings.Contains(output, "Running"),
		"output should indicate container status, got: %s", output)
}

func TestDoctorWithColor_ChecksDocker(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	output := DoctorWithColor(false)
	assert.Contains(t, output, "Docker daemon", "should check Docker daemon")
	assert.Contains(t, output, "OK", "Docker should be OK since it is available")
	assert.Contains(t, output, "Storage root", "should report storage root")
}
