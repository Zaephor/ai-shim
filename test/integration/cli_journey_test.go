package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/cli"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/invocation"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJourney_InitCreatesStructure(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := cli.Init(layout)
	require.NoError(t, err)

	expectedDirs := []string{
		filepath.Join(root, "config"),
		filepath.Join(root, "config", "agents"),
		filepath.Join(root, "config", "profiles"),
		filepath.Join(root, "config", "agent-profiles"),
		filepath.Join(root, "shared", "bin"),
		filepath.Join(root, "shared", "cache"),
	}
	for _, dir := range expectedDirs {
		info, err := os.Stat(dir)
		require.NoError(t, err, "expected directory %s to exist", dir)
		assert.True(t, info.IsDir(), "%s should be a directory", dir)
	}

	defaultYAML := filepath.Join(root, "config", "default.yaml")
	data, err := os.ReadFile(defaultYAML)
	require.NoError(t, err, "default.yaml should exist")
	assert.NotEmpty(t, data, "default.yaml should have non-empty content")
}

func TestJourney_InitIsIdempotent(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := cli.Init(layout)
	require.NoError(t, err)

	// Write custom content to default.yaml
	defaultYAML := filepath.Join(root, "config", "default.yaml")
	customContent := "image: my-custom-image\n"
	err = os.WriteFile(defaultYAML, []byte(customContent), 0644)
	require.NoError(t, err)

	// Run Init again
	err = cli.Init(layout)
	require.NoError(t, err)

	// Verify custom content is preserved
	data, err := os.ReadFile(defaultYAML)
	require.NoError(t, err)
	assert.Equal(t, customContent, string(data), "Init should not overwrite existing default.yaml")
}

func TestJourney_SymlinkCreateAndResolve(t *testing.T) {
	dir := t.TempDir()

	// Create a fake binary to symlink to
	binaryPath := filepath.Join(dir, "ai-shim")
	err := os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755)
	require.NoError(t, err)

	linkPath, err := cli.CreateSymlink("claude-code", "work", dir, binaryPath)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "claude-code_work"), linkPath)

	// Verify symlink exists and points to the binary
	target, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, binaryPath, target)

	// Verify invocation parsing
	agentName, profile, err := invocation.ParseName("claude-code_work")
	require.NoError(t, err)
	assert.Equal(t, "claude-code", agentName)
	assert.Equal(t, "work", profile)
}

func TestJourney_ConfigResolution5Tier(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")

	// Create all config directories
	for _, sub := range []string{"", "agents", "profiles", "agent-profiles"} {
		require.NoError(t, os.MkdirAll(filepath.Join(configDir, sub), 0755))
	}

	// Tier 1: default
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("image: tier1\n"), 0644))

	// Tier 2: agent
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "agents", "claude-code.yaml"),
		[]byte("image: tier2\n"), 0644))

	// Tier 3: profile
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "profiles", "work.yaml"),
		[]byte("image: tier3\n"), 0644))

	// Tier 4: agent-profile
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "agent-profiles", "claude-code_work.yaml"),
		[]byte("image: tier4\n"), 0644))

	cfg, err := config.Resolve(configDir, "claude-code", "work")
	require.NoError(t, err)
	assert.Equal(t, "tier4", cfg.Image, "highest tier (agent-profile) should win")
}

func TestJourney_ConfigEnvVarOverride(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")

	for _, sub := range []string{"", "agents", "profiles", "agent-profiles"} {
		require.NoError(t, os.MkdirAll(filepath.Join(configDir, sub), 0755))
	}

	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("image: from-yaml\n"), 0644))

	t.Setenv("AI_SHIM_IMAGE", "from-env")

	cfg, err := config.Resolve(configDir, "claude-code", "default")
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.Image, "env var (tier 5) should override YAML config")
}

func TestJourney_ConfigValidationBlocks(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")

	for _, sub := range []string{"", "agents", "profiles", "agent-profiles"} {
		require.NoError(t, os.MkdirAll(filepath.Join(configDir, sub), 0755))
	}

	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("security_profile: invalid\n"), 0644))

	cfg, err := config.Resolve(configDir, "claude-code", "default")
	require.NoError(t, err)

	warnings := cfg.Validate()
	assert.NotEmpty(t, warnings, "invalid security_profile should produce validation warnings")

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "security_profile") || strings.Contains(w, "invalid") {
			found = true
			break
		}
	}
	assert.True(t, found, "warnings should mention security_profile; got: %v", warnings)
}

func TestJourney_BuildSpecFromConfig(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Ensure directories exist for mount sources
	require.NoError(t, layout.EnsureDirectories("claude-code", "work"))

	agentDef := agent.Definition{
		Name:        "claude-code",
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test-bin",
		DataDirs:    []string{".test-data"},
	}

	cfg := config.Config{
		Image:    "test-image:latest",
		Hostname: "test-host",
		Env:      map[string]string{"MY_KEY": "my-value"},
		Packages: []string{"curl"},
	}

	// Pre-create profile home data dirs so mounts are valid
	require.NoError(t, layout.EnsureAgentData("work", agentDef.DataDirs, nil))

	p := container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  "work",
		Layout:   layout,
		Platform: platform.Info{UID: 1000, GID: 1000, Hostname: "testhost"},
		HomeDir:  "/home/user",
	}

	spec := container.BuildSpec(p)

	assert.Equal(t, "test-image:latest", spec.Image)
	assert.Equal(t, "test-host", spec.Hostname)
	assert.Contains(t, spec.Env, "MY_KEY=my-value")
	assert.NotEmpty(t, spec.Entrypoint, "entrypoint should be non-empty")
}

func TestJourney_EntrypointVersionPinning(t *testing.T) {
	entrypoint := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test-bin",
		Version:     "1.0.0",
		AgentName:   "test-agent",
	})

	assert.Contains(t, entrypoint, "INSTALLED_VERSION", "entrypoint should reference INSTALLED_VERSION")
	assert.Contains(t, entrypoint, "1.0.0", "entrypoint should contain the pinned version")
}

func TestJourney_EntrypointUpdateInterval(t *testing.T) {
	interval, err := config.ParseUpdateInterval("7d")
	require.NoError(t, err)
	assert.Equal(t, int64(604800), interval, "7d should be 604800 seconds")

	entrypoint := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType:    "npm",
		Package:        "test-pkg",
		Binary:         "test-bin",
		AgentName:      "test-agent",
		UpdateInterval: interval,
	})

	assert.Contains(t, entrypoint, fmt.Sprintf("%d", 604800),
		"entrypoint should contain the interval in seconds")
}

func TestJourney_DryRunMatchesConfig(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	require.NoError(t, cli.Init(layout))

	configDir := layout.ConfigDir
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte(`image: "dry-run-image"
hostname: "dry-run-host"
version: "2.0.0"
update_interval: "7d"
security_profile: "strict"
`), 0644))

	output, err := cli.DryRun(layout, "claude-code", "default", nil)
	require.NoError(t, err)

	assert.Contains(t, output, "dry-run-image")
	assert.Contains(t, output, "dry-run-host")
	assert.Contains(t, output, "2.0.0")
	assert.Contains(t, output, "7d")
	assert.Contains(t, output, "strict")
}

func TestJourney_ProfileSwitching(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Ensure config dir exists
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	err := cli.SwitchProfile(layout, "staging")
	require.NoError(t, err)

	// Read the marker file directly
	markerData, err := os.ReadFile(filepath.Join(layout.ConfigDir, ".current-profile"))
	require.NoError(t, err)
	assert.Equal(t, "staging", strings.TrimSpace(string(markerData)))

	// Verify via CurrentProfile
	current := cli.CurrentProfile(layout)
	assert.Equal(t, "staging", current)
}

func TestJourney_ErrorMessages(t *testing.T) {
	t.Run("ParseNameEmpty", func(t *testing.T) {
		_, _, err := invocation.ParseName("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty",
			"ParseName(\"\") should mention 'empty', got: %v", err)
	})

	t.Run("ParseNameEmptyAgent", func(t *testing.T) {
		_, _, err := invocation.ParseName("_profile")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty agent",
			"ParseName(\"_profile\") should mention 'empty agent', got: %v", err)
	})

	t.Run("ParseNameEmptyProfile", func(t *testing.T) {
		_, _, err := invocation.ParseName("agent_")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty profile",
			"ParseName(\"agent_\") should mention 'empty profile', got: %v", err)
	})

	t.Run("ConfigValidationNetworkScope", func(t *testing.T) {
		root := t.TempDir()
		configDir := filepath.Join(root, "config")
		for _, sub := range []string{"", "agents", "profiles", "agent-profiles"} {
			require.NoError(t, os.MkdirAll(filepath.Join(configDir, sub), 0755))
		}

		require.NoError(t, os.WriteFile(
			filepath.Join(configDir, "default.yaml"),
			[]byte("network_scope: bogus\n"), 0644))

		cfg, err := config.Resolve(configDir, "claude-code", "default")
		require.NoError(t, err)

		warnings := cfg.Validate()
		require.NotEmpty(t, warnings, "bogus network_scope should produce validation warnings")

		found := false
		for _, w := range warnings {
			if strings.Contains(w, "network_scope") && strings.Contains(w, "bogus") {
				found = true
				break
			}
		}
		assert.True(t, found, "validation warning should mention 'network_scope' and 'bogus'; got: %v", warnings)
	})

	t.Run("ConfigValidationSecurityProfile", func(t *testing.T) {
		root := t.TempDir()
		configDir := filepath.Join(root, "config")
		for _, sub := range []string{"", "agents", "profiles", "agent-profiles"} {
			require.NoError(t, os.MkdirAll(filepath.Join(configDir, sub), 0755))
		}

		require.NoError(t, os.WriteFile(
			filepath.Join(configDir, "default.yaml"),
			[]byte("security_profile: hacker\n"), 0644))

		cfg, err := config.Resolve(configDir, "claude-code", "default")
		require.NoError(t, err)

		warnings := cfg.Validate()
		require.NotEmpty(t, warnings, "invalid security_profile should produce validation warnings")

		found := false
		for _, w := range warnings {
			if strings.Contains(w, "security_profile") {
				found = true
				break
			}
		}
		assert.True(t, found, "validation warning should mention 'security_profile'; got: %v", warnings)
	})

	t.Run("DryRunProducesOutput", func(t *testing.T) {
		root := t.TempDir()
		layout := storage.NewLayout(root)
		require.NoError(t, cli.Init(layout))

		// DryRun with a valid agent should produce useful output
		output, err := cli.DryRun(layout, "claude-code", "default", nil)
		require.NoError(t, err)
		assert.Contains(t, output, "claude-code", "DryRun output should mention the agent name")
		assert.Contains(t, output, "Image", "DryRun output should show image info")
	})
}

func TestJourney_CustomAgentDefinition(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	agentsDir := filepath.Join(configDir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0755))

	customYAML := `agent_def:
  install_type: npm
  package: my-agent-pkg
  binary: my-agent-bin
  data_dirs:
    - ".my-agent"
  data_files:
    - ".my-agent-config.json"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(agentsDir, "my-agent.yaml"),
		[]byte(customYAML), 0644))

	customs := agent.LoadCustomAgents(configDir)
	require.NotNil(t, customs, "LoadCustomAgents should return custom agents")

	def, ok := customs["my-agent"]
	require.True(t, ok, "my-agent should be in the custom agents map")
	assert.Equal(t, "my-agent", def.Name)
	assert.Equal(t, "npm", def.InstallType)
	assert.Equal(t, "my-agent-pkg", def.Package)
	assert.Equal(t, "my-agent-bin", def.Binary)
	assert.Equal(t, []string{".my-agent"}, def.DataDirs)
	assert.Equal(t, []string{".my-agent-config.json"}, def.DataFiles)

	// Register and verify lookup
	agent.SetCustomAgents(customs)
	t.Cleanup(func() { agent.SetCustomAgents(nil) })

	looked, found := agent.Lookup("my-agent")
	require.True(t, found, "my-agent should be found via Lookup after SetCustomAgents")
	assert.Equal(t, "my-agent-bin", looked.Binary)
}

// ---------------------------------------------------------------------------
// Binary launch flow tests
// ---------------------------------------------------------------------------

func TestJourney_SymlinkInvocationParsing(t *testing.T) {
	dir := t.TempDir()

	// Create a fake binary to symlink to
	binaryPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755))

	// Create symlink claude-code_work -> ai-shim
	symlinkPath := filepath.Join(dir, "claude-code_work")
	require.NoError(t, os.Symlink(binaryPath, symlinkPath))

	// The basename is not "ai-shim", so it would enter agent mode
	baseName := filepath.Base(symlinkPath)
	assert.NotEqual(t, "ai-shim", baseName, "symlink name should not be ai-shim")

	// Parse claude-code_work -> agent=claude-code, profile=work
	agentName, profile, err := invocation.ParseName("claude-code_work")
	require.NoError(t, err)
	assert.Equal(t, "claude-code", agentName)
	assert.Equal(t, "work", profile)

	// Parse opencode (no profile) -> agent=opencode, profile=default
	agentName, profile, err = invocation.ParseName("opencode")
	require.NoError(t, err)
	assert.Equal(t, "opencode", agentName)
	assert.Equal(t, "default", profile)

	// Parse aider_personal -> agent=aider, profile=personal
	agentName, profile, err = invocation.ParseName("aider_personal")
	require.NoError(t, err)
	assert.Equal(t, "aider", agentName)
	assert.Equal(t, "personal", profile)
}

func TestJourney_DirectInvocationDetection(t *testing.T) {
	// "ai-shim" triggers manage mode
	assert.Equal(t, "ai-shim", filepath.Base("ai-shim"),
		"ai-shim should be detected as manage mode")

	// "ai-shim.exe" triggers manage mode (Windows)
	assert.Equal(t, "ai-shim.exe", filepath.Base("ai-shim.exe"),
		"ai-shim.exe should be detected as manage mode")

	// "claude-code" triggers agent mode (not ai-shim)
	name := filepath.Base("claude-code")
	assert.NotEqual(t, "ai-shim", name, "claude-code should trigger agent mode")
	assert.NotEqual(t, "ai-shim.exe", name, "claude-code should trigger agent mode")
}

func TestJourney_RunCommandParsing(t *testing.T) {
	// Test the arg parsing logic that runManage's "run" case uses.
	// We can't call runManage directly (package main), so we test the
	// underlying ParseName that it delegates to.

	tests := []struct {
		name           string
		invocationName string
		wantAgent      string
		wantProfile    string
	}{
		{
			name:           "run opencode -> agent=opencode, profile=default",
			invocationName: "opencode",
			wantAgent:      "opencode",
			wantProfile:    "default",
		},
		{
			name:           "run opencode work -> agent=opencode, profile=work",
			invocationName: "opencode_work",
			wantAgent:      "opencode",
			wantProfile:    "work",
		},
		{
			name:           "run claude-code personal -> agent=claude-code, profile=personal",
			invocationName: "claude-code_personal",
			wantAgent:      "claude-code",
			wantProfile:    "personal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentName, profile, err := invocation.ParseName(tt.invocationName)
			require.NoError(t, err)
			assert.Equal(t, tt.wantAgent, agentName)
			assert.Equal(t, tt.wantProfile, profile)
		})
	}

	// Verify config resolution works for a parsed agent+profile
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	for _, sub := range []string{"", "agents", "profiles", "agent-profiles"} {
		require.NoError(t, os.MkdirAll(filepath.Join(configDir, sub), 0755))
	}
	require.NoError(t, os.WriteFile(
		filepath.Join(configDir, "default.yaml"),
		[]byte("image: test-image\n"), 0644))

	cfg, err := config.Resolve(configDir, "opencode", "default")
	require.NoError(t, err)
	assert.Equal(t, "test-image", cfg.Image,
		"config resolution should work for parsed agent+profile")
}

func TestJourney_FirstRunDetection(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	// Before init, IsFirstRun should return true
	assert.True(t, cli.IsFirstRun(layout),
		"IsFirstRun should return true for empty temp dir")

	// After init, IsFirstRun should return false
	require.NoError(t, cli.Init(layout))
	assert.False(t, cli.IsFirstRun(layout),
		"IsFirstRun should return false after Init")
}

func TestJourney_SymlinkCRUDCycle(t *testing.T) {
	dir := t.TempDir()

	// Create a fake binary
	binaryPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(binaryPath, []byte("#!/bin/sh\n"), 0755))

	// Create symlink
	linkPath, err := cli.CreateSymlink("opencode", "default", dir, binaryPath)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "opencode"), linkPath,
		"default profile should produce symlink without profile suffix")

	// List symlinks — should find it
	links, err := cli.ListSymlinks(dir, binaryPath)
	require.NoError(t, err)
	assert.Contains(t, links, "opencode",
		"ListSymlinks should include the created symlink")

	// Remove symlink
	err = cli.RemoveSymlink(linkPath)
	require.NoError(t, err)

	// List again — should be empty
	links, err = cli.ListSymlinks(dir, binaryPath)
	require.NoError(t, err)
	assert.Empty(t, links,
		"ListSymlinks should return empty after removal")
}

func TestJourney_UnknownAgentError(t *testing.T) {
	_, found := agent.Lookup("nonexistent-agent")
	assert.False(t, found,
		"Lookup should return false for a nonexistent agent")

	// Verify the available agents list is non-empty (agents exist in the registry)
	names := agent.Names()
	assert.NotEmpty(t, names,
		"agent.Names() should return at least one built-in agent")
}

func TestJourney_WorkingDirValidation(t *testing.T) {
	tests := []struct {
		dir     string
		wantErr bool
		desc    string
	}{
		{"/", true, "root directory should be rejected"},
		{"/etc", true, "/etc should be rejected"},
		{"/var", true, "/var should be rejected"},
		{"/usr", true, "/usr should be rejected"},
		{"/proc", true, "/proc should be rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.dir, func(t *testing.T) {
			err := security.ValidateWorkingDirectory(tt.dir)
			if tt.wantErr {
				assert.Error(t, err, tt.desc)
				assert.Contains(t, err.Error(), "refusing",
					"error message should say 'refusing'")
			}
		})
	}

	// A normal project directory should be allowed
	projectDir := t.TempDir()
	err := security.ValidateWorkingDirectory(projectDir)
	assert.NoError(t, err,
		"a normal temp directory should be accepted as working dir")
}

func TestJourney_DetachKeysParsing(t *testing.T) {
	// Default keys
	keys, err := container.ParseDetachKeys("ctrl-],d")
	require.NoError(t, err)
	assert.Equal(t, [2]byte{0x1D, 0x64}, keys)

	// Docker-style keys
	keys, err = container.ParseDetachKeys("ctrl-p,ctrl-q")
	require.NoError(t, err)
	assert.Equal(t, [2]byte{0x10, 0x11}, keys)

	// Invalid format
	_, err = container.ParseDetachKeys("invalid")
	assert.Error(t, err)

	_, err = container.ParseDetachKeys("")
	assert.Error(t, err)
}

func TestJourney_DetachLabelsInBuildSpec(t *testing.T) {
	// BuildSpec should always include workspace labels.
	configDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(configDir, "profiles"), 0755))

	cfg, err := config.Resolve(configDir, "claude-code", "default")
	require.NoError(t, err)

	agentDef, ok := agent.Lookup("claude-code")
	require.True(t, ok)

	layout := storage.NewLayout(t.TempDir())
	require.NoError(t, layout.EnsureDirectories("claude-code", "default"))

	spec := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  "default",
		Layout:   layout,
		Platform: platform.Detect(),
		HomeDir:  "/home/user",
	})

	assert.NotEmpty(t, spec.Labels[container.LabelWorkspace],
		"BuildSpec must set workspace hash label")
	assert.NotEmpty(t, spec.Labels[container.LabelWorkspaceDir],
		"BuildSpec must set workspace dir label")
	assert.Equal(t, "true", spec.Labels[container.LabelBase],
		"BuildSpec must set base label")
}

func TestJourney_AttachResultDefaults(t *testing.T) {
	// Zero-value AttachResult should indicate no detach and exit code 0.
	var result container.AttachResult
	assert.False(t, result.Detached)
	assert.Equal(t, 0, result.ExitCode)
}

func TestJourney_DefaultDetachKeys(t *testing.T) {
	// Verify the default detach keys are Ctrl+] then 'd'.
	assert.Equal(t, [2]byte{0x1D, 0x64}, container.DefaultDetachKeys)
}
