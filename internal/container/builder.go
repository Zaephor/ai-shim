package container

import (
	"fmt"
	"os"
	"strings"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/provision"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/workspace"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

const (
	DefaultImage    = "ghcr.io/catthehacker/ubuntu:act-24.04"
	DefaultHostname = "ai-shim"
)

// BuildParams holds all inputs needed to build a ContainerSpec.
type BuildParams struct {
	Config   config.Config
	Agent    agent.Definition
	Profile  string
	Layout   storage.Layout
	Platform platform.Info
	Args     []string
}

// BuildSpec creates a ContainerSpec from the resolved parameters.
func BuildSpec(p BuildParams) ContainerSpec {
	image := p.Config.Image
	if image == "" {
		image = DefaultImage
	}

	hostname := p.Config.Hostname
	if hostname == "" {
		hostname = DefaultHostname
	}

	user := fmt.Sprintf("%d:%d", p.Platform.UID, p.Platform.GID)

	labels := map[string]string{
		"ai-shim":         "true",
		"ai-shim.agent":   p.Agent.Name,
		"ai-shim.profile": p.Profile,
	}

	pwd, _ := os.Getwd()
	workdir := workspace.ContainerWorkdir(p.Platform.Hostname, pwd)

	mounts := buildMounts(p, pwd, workdir)

	// Cross-agent access mounts
	isolated := true
	if p.Config.Isolated != nil {
		isolated = *p.Config.Isolated
	}
	crossMounts := CrossAgentMounts(p.Layout, p.Agent.Name, p.Config.AllowAgents, isolated)
	mounts = append(mounts, crossMounts...)

	// Tool provisioning script
	var toolScript string
	if len(p.Config.Tools) > 0 {
		provTools := make(map[string]provision.ToolDef)
		for name, td := range p.Config.Tools {
			provTools[name] = provision.ToolDef{
				Type: td.Type, URL: td.URL, Binary: td.Binary,
				Files: td.Files, Package: td.Package,
				Install: td.Install, Checksum: td.Checksum,
			}
		}
		toolScript = provision.GenerateInstallScript(provTools, "/usr/local/share/ai-shim/bin")
	}

	// Package installation script
	var packageScript string
	if len(p.Config.Packages) > 0 {
		packageScript = "apt-get update -qq && apt-get install -y -qq " + strings.Join(p.Config.Packages, " ") + " 2>/dev/null\n"
	}

	// Merge config args with passthrough args
	allArgs := append(p.Config.Args, p.Args...)

	entrypoint := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType: p.Agent.InstallType,
		Package:     p.Agent.Package,
		Binary:      p.Agent.Binary,
		Version:     p.Config.Version,
		AgentArgs:   allArgs,
	})

	// Prepend tool and package scripts to entrypoint
	fullScript := toolScript + packageScript + entrypoint

	env := buildEnv(p.Config.Env)

	ports, exposedPorts := parsePorts(p.Config.Ports)

	gpu := false
	if p.Config.GPU != nil {
		gpu = *p.Config.GPU
	}

	tty := isTTY()

	return ContainerSpec{
		Image:        image,
		Hostname:     hostname,
		Env:          env,
		Mounts:       mounts,
		WorkingDir:   workdir,
		Entrypoint:   []string{"sh", "-c", fullScript},
		User:         user,
		Labels:       labels,
		Ports:        ports,
		ExposedPorts: exposedPorts,
		TTY:          tty,
		Stdin:        tty,
		GPU:          gpu,
	}
}

func buildMounts(p BuildParams, pwd, workdir string) []mount.Mount {
	mounts := []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: p.Layout.SharedBin,
			Target: "/usr/local/share/ai-shim/bin",
		},
		{
			Type:   mount.TypeBind,
			Source: p.Layout.AgentBin(p.Agent.Name),
			Target: "/usr/local/share/ai-shim/agents/" + p.Agent.Name + "/bin",
		},
		{
			Type:   mount.TypeBind,
			Source: p.Layout.AgentCache(p.Agent.Name),
			Target: "/usr/local/share/ai-shim/agents/" + p.Agent.Name + "/cache",
		},
		{
			Type:   mount.TypeBind,
			Source: p.Layout.ProfileHome(p.Profile),
			Target: "/home/user",
		},
		{
			Type:   mount.TypeBind,
			Source: pwd,
			Target: workdir,
		},
	}

	// Custom volumes from config
	for _, vol := range p.Config.Volumes {
		parts := strings.SplitN(vol, ":", 2)
		if len(parts) == 2 {
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: parts[0],
				Target: parts[1],
			})
		}
	}

	return mounts
}

func buildEnv(envMap map[string]string) []string {
	if len(envMap) == 0 {
		return nil
	}
	env := make([]string, 0, len(envMap))
	for k, v := range envMap {
		env = append(env, k+"="+v)
	}
	return env
}

func parsePorts(ports []string) (nat.PortMap, nat.PortSet) {
	if len(ports) == 0 {
		return nil, nil
	}
	portMap := nat.PortMap{}
	portSet := nat.PortSet{}
	for _, p := range ports {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			continue
		}
		hostPort := parts[0]
		containerPort := parts[1]
		port, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			continue
		}
		portMap[port] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: hostPort},
		}
		portSet[port] = struct{}{}
	}
	return portMap, portSet
}

func isTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// boolPtr is a helper for tests.
func boolPtr(b bool) *bool {
	return &b
}
