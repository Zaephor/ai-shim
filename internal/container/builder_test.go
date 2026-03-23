package container

import (
	"testing"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
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
		p.Config.GPU = boolPtr(true)
		spec := BuildSpec(p)
		assert.True(t, spec.GPU)
	})

	t.Run("GPU disabled", func(t *testing.T) {
		p := defaultBuildParams()
		p.Config.GPU = boolPtr(false)
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
