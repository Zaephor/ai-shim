package container

import (
	"strings"
	"testing"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
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
	spec := BuildSpec(p)

	mountTargets := make(map[string]string)
	for _, m := range spec.Mounts {
		mountTargets[m.Target] = m.Source
	}

	// Shared bin mount
	assert.Contains(t, mountTargets, "/usr/local/share/ai-shim/bin")
	assert.Equal(t, "/tmp/ai-shim-test/shared/bin", mountTargets["/usr/local/share/ai-shim/bin"])

	// Profile home mount
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
}

func TestBuildSpec_CrossAgentMountsNonIsolated(t *testing.T) {
	p := defaultBuildParams()
	p.Config.Isolated = testutil.BoolPtr(false)
	spec := BuildSpec(p)

	// Should have mounts for agents other than the primary
	hasOtherAgent := false
	for _, m := range spec.Mounts {
		if strings.Contains(m.Target, "/usr/local/share/ai-shim/agents/gemini-cli/") {
			hasOtherAgent = true
		}
	}
	assert.True(t, hasOtherAgent, "non-isolated mode should mount other agent bins")
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
	spec := BuildSpec(p)

	hasRunnerHome := false
	for _, m := range spec.Mounts {
		if m.Target == "/home/runner" {
			hasRunnerHome = true
		}
	}
	assert.True(t, hasRunnerHome, "profile home should mount to custom home dir")
}

func TestBuildSpec_DefaultHomeDir(t *testing.T) {
	p := defaultBuildParams()
	// HomeDir empty = default
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

func TestBuildSpec_DINDSocketMount(t *testing.T) {
	p := defaultBuildParams()
	p.DINDSocketVolume = "test-dind-socket-vol"
	spec := BuildSpec(p)

	hasDINDMount := false
	for _, m := range spec.Mounts {
		if m.Target == "/var/run/dind" && m.Source == "test-dind-socket-vol" && m.Type == mount.TypeVolume {
			hasDINDMount = true
		}
	}
	assert.True(t, hasDINDMount, "should mount DIND socket volume")
	assert.Contains(t, spec.Env, "DOCKER_HOST=unix:///var/run/dind/docker.sock", "should set DOCKER_HOST to socket path")
}

func TestBuildSpec_NoDINDSocketMount(t *testing.T) {
	p := defaultBuildParams()
	// DINDSocketVolume not set
	spec := BuildSpec(p)

	for _, m := range spec.Mounts {
		assert.NotEqual(t, "/var/run/dind", m.Target, "should not mount DIND socket when not configured")
	}
	for _, e := range spec.Env {
		assert.NotContains(t, e, "DOCKER_HOST", "should not set DOCKER_HOST when DIND not enabled")
	}
}

func TestRandomSuffix_Length(t *testing.T) {
	s := randomSuffix(4)
	assert.Len(t, s, 4, "suffix should be exactly 4 characters")
	assert.Regexp(t, "^[0-9a-f]+$", s, "suffix should be hex characters only")
}

func TestRandomSuffix_Unique(t *testing.T) {
	s1 := randomSuffix(4)
	s2 := randomSuffix(4)
	assert.NotEqual(t, s1, s2, "two calls should produce different suffixes")
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

