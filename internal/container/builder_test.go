package container

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func defaultBuildParams() BuildParams {
	return BuildParams{
		Config:  config.Config{},
		Agent:   agent.Definition{Name: "claude-code", InstallType: "custom", Package: "curl -fsSL https://claude.ai/install.sh | bash", Binary: "claude", DataDirs: []string{".claude"}, DataFiles: []string{".claude.json"}},
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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Equal(t, DefaultImage, spec.Image)
	assert.Equal(t, DefaultHostname, spec.Hostname)
}

func TestBuildSpec_ConfigOverridesImage(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Image = "custom/image:latest"
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Equal(t, "custom/image:latest", spec.Image)
}

func TestBuildSpec_ConfigOverridesHostname(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Hostname = "my-host"
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Equal(t, "my-host", spec.Hostname)
}

func TestBuildSpec_ConfigEnv(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Env = map[string]string{
		"FOO": "bar",
	}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Contains(t, spec.Env, "FOO=bar")
}

func TestBuildSpec_GPU(t *testing.T) {
	t.Run("nil GPU defaults to false", func(t *testing.T) {
		p := defaultBuildParams()
		spec, err := BuildSpec(p)
		require.NoError(t, err)
		assert.False(t, spec.GPU)
	})

	t.Run("GPU enabled", func(t *testing.T) {
		p := defaultBuildParams()
		p.Config.GPU = testutil.BoolPtr(true)
		spec, err := BuildSpec(p)
		require.NoError(t, err)
		assert.True(t, spec.GPU)
	})

	t.Run("GPU disabled", func(t *testing.T) {
		p := defaultBuildParams()
		p.Config.GPU = testutil.BoolPtr(false)
		spec, err := BuildSpec(p)
		require.NoError(t, err)
		assert.False(t, spec.GPU)
	})
}

func TestBuildSpec_User(t *testing.T) {
	p := defaultBuildParams()
	p.Platform.UID = 501
	p.Platform.GID = 20
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Equal(t, "501:20", spec.User)
}

func TestBuildSpec_Labels(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "gemini-cli"
	p.Profile = "work"
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Equal(t, "true", spec.Labels["ai-shim"])
	assert.Equal(t, "gemini-cli", spec.Labels["ai-shim.agent"])
	assert.Equal(t, "work", spec.Labels["ai-shim.profile"])

	// Workspace labels should always be set
	assert.NotEmpty(t, spec.Labels[LabelWorkspace], "workspace hash label must be set")
	assert.NotEmpty(t, spec.Labels[LabelWorkspaceDir], "workspace dir label must be set")
}

func TestBuildSpec_PersistentMatchesTTY(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	// Persistent flag and label must be consistent with the TTY detection.
	tty := IsTTY()
	assert.Equal(t, tty, spec.Persistent, "Persistent must match IsTTY()")
	assert.Equal(t, tty, spec.TTY, "TTY must match IsTTY()")
	if tty {
		assert.Equal(t, "true", spec.Labels[LabelPersistent])
	} else {
		assert.Empty(t, spec.Labels[LabelPersistent])
	}
}

func TestBuildSpec_RequiredMountsPresent(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false) // shared mode for full home
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	// Invalid ports should be skipped
	assert.Empty(t, spec.Ports)
}

func TestBuildSpec_Entrypoint(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	require.Len(t, spec.Entrypoint, 3)
	assert.Equal(t, "sh", spec.Entrypoint[0])
	assert.Equal(t, "-c", spec.Entrypoint[1])
	assert.Contains(t, spec.Entrypoint[2], "exec claude")
}

func TestBuildSpec_WorkingDir(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Contains(t, spec.WorkingDir, "/workspace/")
}

func TestBuildSpec_CustomVolumesFromConfig(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Volumes = []string{"/host/data:/container/data", "/host/logs:/container/logs"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	entrypoint := spec.Entrypoint[2] // the shell script
	assert.Contains(t, entrypoint, "act", "tool provisioning should be in entrypoint")
	assert.Contains(t, entrypoint, "curl", "tool download should be in entrypoint")
}

func TestBuildSpec_CrossAgentMountsIsolated(t *testing.T) {
	p := defaultBuildParams()
	p.Config.AllowAgents = []string{"gemini-cli"}
	p.Config.Isolated = testutil.BoolPtr(true)
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	targets := map[string]bool{}
	for _, m := range spec.Mounts {
		targets[m.Target] = true
	}
	assert.True(t, targets["/usr/local/share/ai-shim/agents/gemini-cli/bin"], "allowed agent bin should be mounted")
	// Agent data dirs are accessible via the profile home mount
	assert.True(t, targets["/home/user"], "profile home should be mounted")
}

func TestBuildSpec_CrossAgentMountsNonIsolated(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false)
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	// Should have mounts for agents other than the primary
	hasOtherAgentBin := false
	for _, m := range spec.Mounts {
		if strings.Contains(m.Target, "/usr/local/share/ai-shim/agents/gemini-cli/") {
			hasOtherAgentBin = true
		}
	}
	assert.True(t, hasOtherAgentBin, "non-isolated mode should mount other agent bins")
}

// TestBuildSpec_ProfileHomeMountedInAllModes verifies that the profile home
// directory is always bind-mounted at homeDir, regardless of isolation mode.
// Without this, the home directory inside the container would be unwritable
// (image default), breaking git config, npm cache, and other tools.
func TestBuildSpec_ProfileHomeMountedInAllModes(t *testing.T) {
	for _, isolated := range []bool{true, false} {
		name := "shared"
		if isolated {
			name = "isolated"
		}
		t.Run(name, func(t *testing.T) {
			p := defaultBuildParams()
			p.Config.Isolated = testutil.BoolPtr(isolated)
			spec, err := BuildSpec(p)
			require.NoError(t, err)

			var homeMount *mount.Mount
			for i, m := range spec.Mounts {
				if m.Target == "/home/user" {
					homeMount = &spec.Mounts[i]
					break
				}
			}
			require.NotNil(t, homeMount, "profile home must be mounted at /home/user in %s mode", name)
			assert.Equal(t, mount.TypeBind, homeMount.Type)
			assert.Contains(t, homeMount.Source, "profiles/",
				"mount source should be the profile home directory")
		})
	}
}

func TestBuildSpec_IsolatedExcludesOtherAgentData(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(true)
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	for _, m := range spec.Mounts {
		assert.NotContains(t, m.Target, ".gemini", "isolated mode should not mount other agent data dirs")
		assert.NotContains(t, m.Target, ".aider", "isolated mode should not mount other agent data dirs")
	}
}

func TestBuildSpec_PackagesInEntrypoint(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Packages = []string{"tmux", "jq"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "tmux", "packages should be installed in entrypoint")
	assert.Contains(t, entrypoint, "jq", "packages should be installed in entrypoint")
	assert.Contains(t, entrypoint, "apt-get", "should use apt-get for package installation")
}

func TestBuildSpec_CustomHomeDir(t *testing.T) {
	p := defaultBuildParams()
	p.HomeDir = "/home/runner"
	p.Config.Isolated = testutil.BoolPtr(false) // shared mode for full home mount
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	// Profile home should mount at custom home dir
	hasCustomHome := false
	for _, m := range spec.Mounts {
		if m.Target == "/home/runner" {
			hasCustomHome = true
		}
	}
	assert.True(t, hasCustomHome, "profile home should mount to custom home dir in isolated mode")
}

func TestBuildSpec_DefaultHomeDir(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false) // shared mode for full home mount
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	// Should only have the base mounts, no custom ones
	for _, m := range spec.Mounts {
		assert.NotEqual(t, "no-colon-here", m.Source, "malformed volume should be skipped")
	}
}

func TestBuildSpec_ContainerName(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "claude-code"
	p.Profile = "work"
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec1, err := BuildSpec(p)
	require.NoError(t, err)
	spec2, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)
	// WorkingDir should start with /workspace/ and have a hash
	assert.True(t, len(spec.WorkingDir) > len("/workspace/"), "workdir should have hash suffix")
}

func TestBuildSpec_ContainerNameUnder63Chars(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "claude-code"
	p.Profile = "work"
	spec, err := BuildSpec(p)
	require.NoError(t, err)
	assert.True(t, len(spec.Name) <= 63, "container name should not exceed Docker's 63-char limit, got %d: %s", len(spec.Name), spec.Name)
}

func TestBuildSpec_ContainerNameLongInputs(t *testing.T) {
	p := defaultBuildParams()
	p.Agent.Name = "my-extremely-long-custom-agent-name-that-is-ridiculous"
	p.Profile = "production-with-extra-context-for-no-reason"
	spec, err := BuildSpec(p)
	require.NoError(t, err)
	// Name must stay within Docker's limits regardless of input length
	assert.True(t, len(spec.Name) <= 128,
		"container name should fit Docker's 128-char limit even with long inputs, got %d: %s",
		len(spec.Name), spec.Name)
	// Name should still contain agent and profile for identification
	assert.Contains(t, spec.Name, p.Agent.Name[:10],
		"container name should contain at least part of agent name")
}

func TestGenerateContainerName_Format(t *testing.T) {
	name := generateContainerName("agent", "profile", "abc12345")
	parts := strings.Split(name, "-")
	// Should have at least agent-profile-hash-suffix
	assert.True(t, len(parts) >= 4, "name should have multiple segments: %s", name)
	assert.True(t, strings.HasPrefix(name, "agent-profile-"), "name should start with agent-profile: %s", name)
}

func TestParsePorts_InvalidFormat(t *testing.T) {
	portMap, portSet := parsePorts([]string{"invalid-port", "8080:80"})
	// Valid port should still be parsed
	require.NotNil(t, portMap)
	_, ok := portMap["80/tcp"]
	assert.True(t, ok, "valid port should still be parsed")
	assert.Len(t, portSet, 1, "only valid port should be in set")
}

// TestParsePorts_MalformedWarnsOnStderr verifies that parsePorts emits a
// warning to stderr for each skipped port mapping rather than silently dropping
// it. Without a warning, the user configures port forwarding and gets no
// feedback that it was ignored.
func TestParsePorts_MalformedWarnsOnStderr(t *testing.T) {
	// Capture stderr.
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	parsePorts([]string{"invalid-port", "8080:80"})

	w.Close()
	os.Stderr = origStderr
	var buf strings.Builder
	_, _ = io.Copy(&buf, r)

	assert.Contains(t, buf.String(), "ai-shim: skipping invalid port", "should warn about malformed port")
	assert.Contains(t, buf.String(), "invalid-port", "warning should include the malformed port string")
}

func TestBuildSpec_NoResourceLimits(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)
	assert.Nil(t, spec.Resources, "resources should be nil when not configured")
}

func TestBuildSpec_WithResourceLimits(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Resources = &config.ResourceLimits{Memory: "4g", CPUs: "2.0"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)
	require.NotNil(t, spec.Resources)
	assert.Equal(t, "4g", spec.Resources.Memory)
	assert.Equal(t, "2.0", spec.Resources.CPUs)
}

func TestBuildSpec_GitConfigInEntrypoint(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Git = &config.GitConfig{Name: "Test User", Email: "test@example.com"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "git config --global user.name", "should set git user.name")
	assert.Contains(t, entrypoint, "Test User", "should contain the configured name")
	assert.Contains(t, entrypoint, "git config --global user.email", "should set git user.email")
	assert.Contains(t, entrypoint, "test@example.com", "should contain the configured email")
}

func TestBuildSpec_GitConfigPartial(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Git = &config.GitConfig{Name: "Only Name"}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, "git config --global user.name", "should set git user.name")
	assert.NotContains(t, entrypoint, "git config --global user.email", "should not set email when not configured")
}

func TestBuildSpec_NoGitConfig(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

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
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	for _, e := range spec.Env {
		assert.False(t, strings.HasPrefix(e, "MCP_SERVERS="), "MCP_SERVERS should not be set when no MCP servers configured")
	}
}

func TestMCPServersJSON(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"test": {Command: "cmd", Args: []string{"arg1"}, Env: map[string]string{"K": "V"}},
	}
	result := mcpServersJSON(servers, nil)
	assert.Contains(t, result, `"command":"cmd"`)
	assert.Contains(t, result, `"args":["arg1"]`)
	assert.Contains(t, result, `"K":"V"`)
}

func TestMCPServersJSON_Empty(t *testing.T) {
	result := mcpServersJSON(map[string]config.MCPServerDef{}, nil)
	assert.Equal(t, "{}", result)
}

func TestMCPServersJSON_OmitsEmptyFields(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"minimal": {Command: "echo"},
	}
	result := mcpServersJSON(servers, nil)
	assert.Contains(t, result, `"command":"echo"`)
	assert.NotContains(t, result, `"args"`, "nil args should be omitted")
	assert.NotContains(t, result, `"env"`, "nil env should be omitted")
}

func TestMCPServersJSON_MultipleServers(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"fs":  {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem"}},
		"git": {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-git"}},
	}
	result := mcpServersJSON(servers, nil)
	assert.Contains(t, result, `"fs"`)
	assert.Contains(t, result, `"git"`)
}

// TestMCPServersJSON_HonorsOrder verifies that when an order slice is
// provided, mcpServersJSON emits entries in declaration order (not map
// iteration order).
func TestMCPServersJSON_HonorsOrder(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"zeta":  {Command: "z"},
		"alpha": {Command: "a"},
		"mid":   {Command: "m"},
	}
	order := []string{"zeta", "alpha", "mid"}
	result := mcpServersJSON(servers, order)
	zIdx := strings.Index(result, `"zeta"`)
	aIdx := strings.Index(result, `"alpha"`)
	mIdx := strings.Index(result, `"mid"`)
	require.True(t, zIdx >= 0 && aIdx >= 0 && mIdx >= 0, "all keys present: %s", result)
	assert.Less(t, zIdx, aIdx, "zeta should precede alpha per declaration order")
	assert.Less(t, aIdx, mIdx, "alpha should precede mid per declaration order")
}

// TestMCPServersJSON_AlphabeticalFallback verifies that with an empty order
// slice, mcpServersJSON falls back to sorted alphabetical order for
// deterministic output.
func TestMCPServersJSON_AlphabeticalFallback(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"zeta":  {Command: "z"},
		"alpha": {Command: "a"},
		"mid":   {Command: "m"},
	}
	result := mcpServersJSON(servers, nil)
	aIdx := strings.Index(result, `"alpha"`)
	mIdx := strings.Index(result, `"mid"`)
	zIdx := strings.Index(result, `"zeta"`)
	require.True(t, aIdx >= 0 && mIdx >= 0 && zIdx >= 0, "all keys present: %s", result)
	assert.Less(t, aIdx, mIdx, "alpha should precede mid alphabetically")
	assert.Less(t, mIdx, zIdx, "mid should precede zeta alphabetically")
}

// TestMCPServersJSON_OrderKeyMissing verifies that if the order slice references
// a key not in the map, it is skipped (no crash, no empty entry).
func TestMCPServersJSON_OrderKeyMissing(t *testing.T) {
	servers := map[string]config.MCPServerDef{
		"a": {Command: "a"},
	}
	order := []string{"ghost", "a"}
	result := mcpServersJSON(servers, order)
	assert.Contains(t, result, `"a"`)
	assert.NotContains(t, result, `"ghost"`)
}

func TestBuildSpec_SecurityProfileDefault(t *testing.T) {
	p := defaultBuildParams()
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Empty(t, spec.SecurityOpt, "default profile should not set SecurityOpt")
	assert.Empty(t, spec.CapDrop, "default profile should not drop capabilities")
}

func TestBuildSpec_SecurityProfileStrict(t *testing.T) {
	p := defaultBuildParams()
	p.Config.SecurityProfile = "strict"
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Contains(t, spec.SecurityOpt, "no-new-privileges:true")
	assert.Contains(t, spec.CapDrop, "ALL")
}

func TestBuildSpec_SecurityProfileNone(t *testing.T) {
	p := defaultBuildParams()
	p.Config.SecurityProfile = "none"
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	assert.Contains(t, spec.SecurityOpt, "seccomp=unconfined")
	assert.Empty(t, spec.CapDrop)
}

func TestBuildSpec_ToolDataDirMount(t *testing.T) {
	root := t.TempDir()
	p := defaultBuildParams()
	p.Layout = storage.NewLayout(root)
	p.Config.Tools = map[string]config.ToolDef{
		"nvm": {
			Type:    "custom",
			Install: "echo hello",
			DataDir: true,
			EnvVar:  "NVM_DIR",
		},
	}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	// Should have a bind mount for the tool cache
	found := false
	for _, m := range spec.Mounts {
		if m.Target == "/usr/local/share/ai-shim/cache/nvm" {
			found = true
			assert.Equal(t, storage.ToolCachePath(p.Layout, "nvm", "", p.Agent.Name, p.Profile), m.Source)
		}
	}
	assert.True(t, found, "tool data_dir mount should be present at /usr/local/share/ai-shim/cache/nvm")
}

func TestBuildSpec_ToolDataDirMountProfileScope(t *testing.T) {
	root := t.TempDir()
	p := defaultBuildParams()
	p.Layout = storage.NewLayout(root)
	p.Config.Tools = map[string]config.ToolDef{
		"gvm": {
			Type:       "custom",
			Install:    "echo hello",
			DataDir:    true,
			EnvVar:     "GVM_ROOT",
			CacheScope: "profile",
		},
	}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	found := false
	for _, m := range spec.Mounts {
		if m.Target == "/usr/local/share/ai-shim/cache/gvm" {
			found = true
			expected := storage.ToolCachePath(p.Layout, "gvm", "profile", p.Agent.Name, p.Profile)
			assert.Equal(t, expected, m.Source)
		}
	}
	assert.True(t, found, "tool data_dir mount with profile scope should be present")
}

func TestBuildSpec_ToolNoDataDirNoMount(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Tools = map[string]config.ToolDef{
		"act": {Type: "binary-download", URL: "https://example.com/act", Binary: "act"},
	}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	for _, m := range spec.Mounts {
		assert.NotContains(t, m.Target, "/usr/local/share/ai-shim/cache/act",
			"tool without data_dir should not have a cache mount")
	}
}

func TestBuildSpec_ToolDataDirEnvVarInScript(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Tools = map[string]config.ToolDef{
		"nvm": {
			Type:    "custom",
			Install: "echo nvm_install",
			DataDir: true,
			EnvVar:  "NVM_DIR",
		},
	}
	spec, err := BuildSpec(p)
	require.NoError(t, err)

	entrypoint := spec.Entrypoint[2]
	assert.Contains(t, entrypoint, `export NVM_DIR="/usr/local/share/ai-shim/cache/nvm"`,
		"entrypoint should export the env var for the tool cache path")
}

// TestBuildSpec_ToolCacheMkdirFailureReturnsError verifies that when the
// tool-cache directory cannot be created on the host (e.g. because a file
// exists where a directory is expected), BuildSpec returns an error instead
// of silently dropping the mount. Starting the container with a broken
// persistent-cache mount is worse than refusing to start — the tool would
// run on an empty ephemeral layer and its state would be lost.
func TestBuildSpec_ToolCacheMkdirFailureReturnsError(t *testing.T) {
	root := t.TempDir()

	// Force the cache parent path to be a regular file so MkdirAll for a
	// directory underneath it fails with ENOTDIR. ToolCachePath for
	// "global" scope returns {root}/shared/cache/{tool}.
	sharedDir := root + "/shared"
	require.NoError(t, os.MkdirAll(sharedDir, 0755))
	// Create "cache" as a file to block MkdirAll on "cache/<tool>".
	f, err := os.Create(sharedDir + "/cache")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	p := defaultBuildParams()
	p.Layout = storage.NewLayout(root)
	p.Config.Tools = map[string]config.ToolDef{
		"broken-tool": {
			Type:    "custom",
			Install: "echo hello",
			DataDir: true,
			EnvVar:  "BROKEN_DIR",
			// Empty CacheScope → global → layout.SharedCache/{tool}.
		},
	}

	spec, err := BuildSpec(p)
	assert.Error(t, err, "BuildSpec must return an error when the tool cache dir cannot be created")
	assert.Zero(t, spec, "returned spec should be zero-valued on error")
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
