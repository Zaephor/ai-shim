package container

import (
	"strings"
	"testing"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultBuildParams() BuildParams {
	return BuildParams{
		Config:  config.Config{},
		Agent:   agent.Definition{Name: "claude-code", InstallType: "custom", Package: "curl -fsSL https://claude.ai/install.sh | bash", Binary: "claude"},
		Profile: "default",
		Layout:  storage.NewLayout("/tmp/ai-shim-test"),
		Platform: platform.Info{
			DockerSocket: "/var/run/docker.sock",
			UID:          1000,
			GID:          1000,
			Username:     "testuser",
			Hostname:     "testhost",
		},
		Args: nil,
	}
}

func TestBuildSpec_DefaultImageAndHostname(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)

	assert.Equal(t, DefaultImage, spec.Image)
	assert.Equal(t, DefaultHostname, spec.Hostname)
}

func TestBuildSpec_ConfigOverridesImage(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Image = "custom/image:latest"
	spec := BuildSpec(p)

	assert.Equal(t, "custom/image:latest", spec.Image)
}

func TestBuildSpec_ConfigOverridesHostname(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Hostname = "my-host"
	spec := BuildSpec(p)

	assert.Equal(t, "my-host", spec.Hostname)
}

func TestBuildSpec_ConfigEnv(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Env = map[string]string{
		"FOO": "bar",
	}
	spec := BuildSpec(p)

	require.Len(t, spec.Env, 1)
	assert.Equal(t, "FOO=bar", spec.Env[0])
}

func TestBuildSpec_GPU(t *testing.T) {
	t.Run("nil GPU defaults to false", func(t *testing.T) {
		p := defaultBuildParams()
		spec := BuildSpec(p)
		assert.False(t, spec.GPU)
	})

	t.Run("GPU enabled", func(t *testing.T) {
		p := defaultBuildParams()
		p.Config.GPU = testutil.BoolPtr(true)
		spec := BuildSpec(p)
		assert.True(t, spec.GPU)
	})

	t.Run("GPU disabled", func(t *testing.T) {
		p := defaultBuildParams()
		p.Config.GPU = testutil.BoolPtr(false)
		spec := BuildSpec(p)
		assert.False(t, spec.GPU)
	})
}

func TestBuildSpec_User(t *testing.T) {
	p := defaultBuildParams()
	p.Platform.UID = 501
	p.Platform.GID = 20
	spec := BuildSpec(p)

	assert.Equal(t, "501:20", spec.User)
}

func TestBuildSpec_Labels(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "gemini-cli"
	p.Profile = "work"
	spec := BuildSpec(p)

	assert.Equal(t, "true", spec.Labels["ai-shim"])
	assert.Equal(t, "gemini-cli", spec.Labels["ai-shim.agent"])
	assert.Equal(t, "work", spec.Labels["ai-shim.profile"])
}

func TestBuildSpec_RequiredMountsPresent(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false) // shared mode for full home
	spec := BuildSpec(p)

	mountTargets := make(map[string]string)
	for _, m := range spec.Mounts {
		mountTargets[m.Target] = m.Source
	}

	// Shared bin mount
	assert.Contains(t, mountTargets, "/usr/local/share/ai-shim/bin")
	assert.Equal(t, "/tmp/ai-shim-test/shared/bin", mountTargets["/usr/local/share/ai-shim/bin"])

	// Profile home mount (shared mode)
	assert.Contains(t, mountTargets, "/home/user")
	assert.Equal(t, "/tmp/ai-shim-test/profiles/default/home", mountTargets["/home/user"])

	// Agent bin mount
	assert.Contains(t, mountTargets, "/usr/local/share/ai-shim/agents/claude-code/bin")

	// Agent cache mount
	assert.Contains(t, mountTargets, "/usr/local/share/ai-shim/agents/claude-code/cache")
}

func TestBuildSpec_PortParsing(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Ports = []string{"8080:80", "3000:3000"}
	spec := BuildSpec(p)

	require.NotNil(t, spec.Ports)
	require.NotNil(t, spec.ExposedPorts)

	// Check port 80/tcp is mapped
	bindings, ok := spec.Ports["80/tcp"]
	require.True(t, ok, "port 80/tcp should be in port map")
	require.Len(t, bindings, 1)
	assert.Equal(t, "8080", bindings[0].HostPort)

	// Check port 3000/tcp is mapped
	bindings, ok = spec.Ports["3000/tcp"]
	require.True(t, ok, "port 3000/tcp should be in port map")
	require.Len(t, bindings, 1)
	assert.Equal(t, "3000", bindings[0].HostPort)
}

func TestBuildSpec_PortParsingInvalid(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Ports = []string{"invalid-port"}
	spec := BuildSpec(p)

	// Invalid ports should be skipped
	assert.Empty(t, spec.Ports)
}

func TestBuildSpec_Entrypoint(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)

	require.Len(t, spec.Entrypoint, 3)
	assert.Equal(t, "sh", spec.Entrypoint[0])
	assert.Equal(t, "-c", spec.Entrypoint[1])
	assert.Contains(t, spec.Entrypoint[2], "exec claude")
}

func TestBuildSpec_WorkingDir(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)

	assert.Contains(t, spec.WorkingDir, "/workspace/")
}

func TestBuildSpec_CustomVolumesFromConfig(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/host/data:/container/data", "/host/logs:/container/logs"}
	spec := BuildSpec(p)

	targets := map[string]bool{}
	for _, m := range spec.Mounts {
		targets[m.Target] = true
	}
	assert.True(t, targets["/container/data"], "custom volume /host/data:/container/data should be mounted")
	assert.True(t, targets["/container/logs"], "custom volume /host/logs:/container/logs should be mounted")
}

func TestBuildSpec_ToolProvisioningInEntrypoint(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Tools = map[string]config.ToolDef{
		"act": {Type: "binary-download", URL: "https://example.com/act", Binary: "act"},
	}
	spec := BuildSpec(p)

	entrypoint := spec.Entrypoint[2] // the shell script
	assert.Contains(t, entrypoint, "act", "tool provisioning should be in entrypoint")
	assert.Contains(t, entrypoint, "curl", "tool download should be in entrypoint")
}

func TestBuildSpec_CrossAgentMountsIsolated(t *testing.T) {
	p := defaultBuildParams()
	p.Config.AllowAgents = []string{"gemini-cli"}
	p.Config.Isolated = testutil.BoolPtr(true)
	spec := BuildSpec(p)

	targets := map[string]bool{}
	for _, m := range spec.Mounts {
		targets[m.Target] = true
	}
	assert.True(t, targets["/usr/local/share/ai-shim/agents/gemini-cli/bin"], "allowed agent bin should be mounted")
	assert.True(t, targets["/home/user/.gemini"], "allowed agent data dir should be mounted")
}

func TestBuildSpec_CrossAgentMountsNonIsolated(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false)
	spec := BuildSpec(p)

	// Should have mounts for agents other than the primary
	hasOtherAgentBin := false
	for _, m := range spec.Mounts {
		if strings.Contains(m.Target, "/usr/local/share/ai-shim/agents/gemini-cli/") {
			hasOtherAgentBin = true
		}
	}
	assert.True(t, hasOtherAgentBin, "non-isolated mode should mount other agent bins")
}

func TestBuildSpec_IsolatedMountsOnlyAgentData(t *testing.T) {
	p := defaultBuildParams()
	// Default is isolated=true
	spec := BuildSpec(p)

	targets := map[string]bool{}
	for _, m := range spec.Mounts {
		targets[m.Target] = true
	}
	// Should have claude's data dir, not the full home
	assert.True(t, targets["/home/user/.claude"], "primary agent data dir should be mounted")
	assert.True(t, targets["/home/user/.claude.json"], "primary agent data file should be mounted")
	assert.False(t, targets["/home/user"], "full home should NOT be mounted in isolated mode")
}

func TestBuildSpec_SharedModeMountsFullHome(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false)
	spec := BuildSpec(p)

	targets := map[string]bool{}
	for _, m := range spec.Mounts {
		targets[m.Target] = true
	}
	assert.True(t, targets["/home/user"], "full home should be mounted in shared mode")
}

func TestBuildSpec_IsolatedExcludesOtherAgentData(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(true)
	spec := BuildSpec(p)

	for _, m := range spec.Mounts {
		assert.NotContains(t, m.Target, ".gemini", "isolated mode should not mount other agent data dirs")
		assert.NotContains(t, m.Target, ".aider", "isolated mode should not mount other agent data dirs")
	}
}

func TestBuildSpec_PackagesInEntrypoint(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Packages = []string{"tmux", "jq"}
	spec := BuildSpec(p)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "tmux", "packages should be installed in entrypoint")
	assert.Contains(t, entrypoint, "jq", "packages should be installed in entrypoint")
	assert.Contains(t, entrypoint, "apt-get", "should use apt-get for package installation")
}

func TestBuildSpec_CustomHomeDir(t *testing.T) {
	p := defaultBuildParams()
	p.HomeDir = "/home/runner"
	p.Config.Isolated = testutil.BoolPtr(false) // shared mode for full home mount
	spec := BuildSpec(p)

	hasRunnerHome := false
	for _, m := range spec.Mounts {
		if m.Target == "/home/runner" {
			hasRunnerHome = true
		}
	}
	assert.True(t, hasRunnerHome, "profile home should mount to custom home dir")
}

func TestBuildSpec_CustomHomeDir_Isolated(t *testing.T) {
	p := defaultBuildParams()
	p.HomeDir = "/home/runner"
	// Default isolated mode
	spec := BuildSpec(p)

	// Data dirs should be under custom home
	hasCustomHomeData := false
	for _, m := range spec.Mounts {
		if strings.HasPrefix(m.Target, "/home/runner/.") {
			hasCustomHomeData = true
		}
	}
	assert.True(t, hasCustomHomeData, "isolated data dirs should use custom home dir")
}

func TestBuildSpec_DefaultHomeDir(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false) // shared mode for full home mount
	spec := BuildSpec(p)

	hasDefaultHome := false
	for _, m := range spec.Mounts {
		if m.Target == "/home/user" {
			hasDefaultHome = true
		}
	}
	assert.True(t, hasDefaultHome, "should default to /home/user when HomeDir not set")
}

func TestBuildSpec_ConfigArgsAndPassthroughMerged(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Args = []string{"--no-telemetry"}
	p.Args = []string{"--verbose"}
	spec := BuildSpec(p)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "--no-telemetry", "config args should be in entrypoint")
	assert.Contains(t, entrypoint, "--verbose", "passthrough args should be in entrypoint")
}

func TestBuildSpec_VolumeValidationSkipsTraversal(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{
		"/host/safe:/container/safe",
		"/host/../etc/shadow:/container/etc",
		"/etc/passwd:/container/passwd",
		"/host/ok:/container/ok",
	}
	spec := BuildSpec(p)

	targets := map[string]bool{}
	for _, m := range spec.Mounts {
		targets[m.Target] = true
	}
	assert.True(t, targets["/container/safe"], "safe volume should be mounted")
	assert.True(t, targets["/container/ok"], "safe volume should be mounted")
	assert.False(t, targets["/container/etc"], "traversal volume should be skipped")
	assert.False(t, targets["/container/passwd"], "sensitive path volume should be skipped")
}

func TestBuildSpec_VolumeValidationSkipsMalformed(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"no-colon-here"}
	spec := BuildSpec(p)

	// Should only have the base mounts, no custom ones
	for _, m := range spec.Mounts {
		assert.NotEqual(t, "no-colon-here", m.Source, "malformed volume should be skipped")
	}
}

func TestBuildSpec_ContainerName(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "claude-code"
	p.Profile = "work"
	spec := BuildSpec(p)

	assert.NotEmpty(t, spec.Name, "container should have a name")
	assert.Contains(t, spec.Name, "claude-code-work-", "name should contain agent-profile prefix")
	// Name format: agent-profile-workspacehash-randomsuffix
	parts := strings.Split(spec.Name, "-")
	// claude-code = 2 parts, work = 1, hash = 1, suffix = 1 => at least 5
	assert.True(t, len(parts) >= 5, "name should have at least 5 dash-separated parts")
}

func TestBuildSpec_ContainerNameDeterministicPrefix(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "gemini-cli"
	p.Profile = "personal"
	spec1 := BuildSpec(p)
	spec2 := BuildSpec(p)

	// Prefix should be the same (deterministic), but suffix differs (random)
	prefix1 := spec1.Name[:strings.LastIndex(spec1.Name, "-")]
	prefix2 := spec2.Name[:strings.LastIndex(spec2.Name, "-")]
	assert.Equal(t, prefix1, prefix2, "name prefix should be deterministic")
	assert.NotEqual(t, spec1.Name, spec2.Name, "full name should differ due to random suffix")
}

func TestRandomSuffix_Length(t *testing.T) {
	s := randomSuffix(8)
	assert.Len(t, s, 8, "suffix should be exactly 8 characters")
	assert.Regexp(t, "^[0-9a-f]+$", s, "suffix should be hex characters only")
}

func TestRandomSuffix_Unique(t *testing.T) {
	s1 := randomSuffix(8)
	s2 := randomSuffix(8)
	assert.NotEqual(t, s1, s2, "two calls should produce different suffixes")
}

func TestGenerateContainerName_SuffixLength(t *testing.T) {
	name := generateContainerName("agent", "profile", "hash")
	// Format: agent-profile-hash-<8 hex chars>
	parts := strings.Split(name, "-")
	suffix := parts[len(parts)-1]
	assert.Len(t, suffix, 8, "container name suffix should be 8 hex chars")
}

func TestBuildSpec_WorkdirUsesRealPwd(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)
	// WorkingDir should start with /workspace/ and have a hash
	assert.True(t, len(spec.WorkingDir) > len("/workspace/"), "workdir should have hash suffix")
}

func TestBuildSpec_ContainerNameUnder63Chars(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "claude-code"
	p.Profile = "work"
	spec := BuildSpec(p)
	assert.True(t, len(spec.Name) <= 63, "container name should not exceed Docker's 63-char limit, got %d: %s", len(spec.Name), spec.Name)
}

func TestParsePorts_InvalidFormat(t *testing.T) {
	portMap, portSet := parsePorts([]string{"invalid-port", "8080:80"})
	// Valid port should still be parsed
	require.NotNil(t, portMap)
	_, ok := portMap["80/tcp"]
	assert.True(t, ok, "valid port should still be parsed")
	assert.Len(t, portSet, 1, "only valid port should be in set")
}

func TestBuildSpec_NoResourceLimits(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)
	assert.Nil(t, spec.Resources, "resources should be nil when not configured")
}

func TestBuildSpec_WithResourceLimits(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Resources = &config.ResourceLimits{Memory: "4g", CPUs: "2.0"}
	spec := BuildSpec(p)
	require.NotNil(t, spec.Resources)
	assert.Equal(t, "4g", spec.Resources.Memory)
	assert.Equal(t, "2.0", spec.Resources.CPUs)
}

func TestBuildSpec_GitConfigInEntrypoint(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Git = &config.GitConfig{Name: "Test User", Email: "test@example.com"}
	spec := BuildSpec(p)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "git config --global user.name", "should set git user.name")
	assert.Contains(t, entrypoint, "Test User", "should contain the configured name")
	assert.Contains(t, entrypoint, "git config --global user.email", "should set git user.email")
	assert.Contains(t, entrypoint, "test@example.com", "should contain the configured email")
}

func TestBuildSpec_GitConfigPartial(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Git = &config.GitConfig{Name: "Only Name"}
	spec := BuildSpec(p)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "git config --global user.name", "should set git user.name")
	assert.NotContains(t, entrypoint, "git config --global user.email", "should not set email when not configured")
}

func TestBuildSpec_NoGitConfig(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)

	entrypoint := spec.Entrypoint[2]
	assert.NotContains(t, entrypoint, "git config --global", "should not set git config when not configured")
}

func TestBuildSpec_MCPServersEnvVar(t *testing.T) {
	p := defaultBuildParams()
	p.Config.MCPServers = map[string]config.MCPServerDef{
		"filesystem": {
			Command: "npx",
			Args:    []string{"-y", "@modelcontextprotocol/server-filesystem"},
			Env:     map[string]string{"MCP_ROOT": "/workspace"},
		},
	}
	spec := BuildSpec(p)

	var mcpEnv string
	for _, e := range spec.Env {
		if strings.HasPrefix(e, "MCP_SERVERS=") {
			mcpEnv = e
			break
		}
	}
	require.NotEmpty(t, mcpEnv, "MCP_SERVERS env var should be set")
	assert.Contains(t, mcpEnv, "filesystem")
	assert.Contains(t, mcpEnv, "npx")
	assert.Contains(t, mcpEnv, "@modelcontextprotocol/server-filesystem")
}

func TestBuildSpec_NoMCPServers(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)

	for _, e := range spec.Env {
		assert.False(t, strings.HasPrefix(e, "MCP_SERVERS="), "MCP_SERVERS should not be set when no MCP servers configured")
	}
}

func TestMCPServersJSON(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"test": {Command: "cmd", Args: []string{"arg1"}, Env: map[string]string{"K": "V"}},
	}
	result := mcpServersJSON(servers)
	assert.Contains(t, result, `"command":"cmd"`)
	assert.Contains(t, result, `"args":["arg1"]`)
	assert.Contains(t, result, `"K":"V"`)
}

func TestBuildSpec_SecurityProfileDefault(t *testing.T) {
	p := defaultBuildParams()
	spec := BuildSpec(p)

	assert.Empty(t, spec.SecurityOpt, "default profile should not set SecurityOpt")
	assert.Empty(t, spec.CapDrop, "default profile should not drop capabilities")
}

func TestBuildSpec_SecurityProfileStrict(t *testing.T) {
	p := defaultBuildParams()
	p.Config.SecurityProfile = "strict"
	spec := BuildSpec(p)

	assert.Contains(t, spec.SecurityOpt, "no-new-privileges:true")
	assert.Contains(t, spec.CapDrop, "ALL")
}

func TestBuildSpec_SecurityProfileNone(t *testing.T) {
	p := defaultBuildParams()
	p.Config.SecurityProfile = "none"
	spec := BuildSpec(p)

	assert.Contains(t, spec.SecurityOpt, "seccomp=unconfined")
	assert.Empty(t, spec.CapDrop)
}

func TestResolveSecurityProfile(t *testing.T) {
	tests := []struct {
		profile     string
		wantSecOpt  []string
		wantCapDrop []string
	}{
		{"", nil, nil},
		{"default", nil, nil},
		{"strict", []string{"no-new-privileges:true"}, []string{"ALL"}},
		{"none", []string{"seccomp=unconfined"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.profile, func(t *testing.T) {
			secOpt, capDrop := resolveSecurityProfile(tt.profile)
			assert.Equal(t, tt.wantSecOpt, secOpt)
			assert.Equal(t, tt.wantCapDrop, capDrop)
		})
	}
}

