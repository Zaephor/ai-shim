package container

import (
	"crypto/rand"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/provision"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/workspace"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"
)

const (
	DefaultImage    = config.DefaultImage
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
	HomeDir string // container-side home directory (from image inspect)
	LogDir  string // directory for exit logs (empty = no logging)
}

// BuildSpec creates a ContainerSpec from the resolved parameters.
func BuildSpec(p BuildParams) ContainerSpec {
	image := p.Config.GetImage()
	hostname := p.Config.GetHostname()

	user := fmt.Sprintf("%d:%d", p.Platform.UID, p.Platform.GID)

	labels := map[string]string{
		LabelBase:    "true",
		LabelAgent:   p.Agent.Name,
		LabelProfile: p.Profile,
	}

	homeDir := p.HomeDir
	if homeDir == "" {
		homeDir = "/home/user"
	}

	pwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: cannot determine working directory: %v\n", err)
		pwd = "/tmp"
	}
	workdir := workspace.ContainerWorkdir(p.Platform.Hostname, pwd)

	wsHash := workspace.HashPath(p.Platform.Hostname, pwd)
	name := generateContainerName(p.Agent.Name, p.Profile, wsHash)

	mounts := buildMounts(p, pwd, workdir, homeDir)

	// Cross-agent access mounts
	crossMounts := CrossAgentMounts(p.Layout, p.Agent.Name, p.Config.AllowAgents, p.Config.IsIsolated())
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
		packageScript = "echo \"Installing packages: " + strings.Join(p.Config.Packages, " ") + "\"\napt-get update -qq && apt-get install -y -qq " + strings.Join(p.Config.Packages, " ") + " || { echo \"ERROR: package installation failed\"; exit 1; }\n"
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

	// Git config setup (global, so it doesn't leak into bind-mounted repos)
	var gitScript string
	if p.Config.Git != nil {
		if p.Config.Git.Name != "" {
			gitScript += fmt.Sprintf("git config --global user.name %s\n", shellQuote(p.Config.Git.Name))
		}
		if p.Config.Git.Email != "" {
			gitScript += fmt.Sprintf("git config --global user.email %s\n", shellQuote(p.Config.Git.Email))
		}
	}

	// Prepend tool, package, and git scripts to entrypoint
	fullScript := toolScript + packageScript + gitScript + entrypoint

	env := buildEnv(p.Config.Env)

	ports, exposedPorts := parsePorts(p.Config.Ports)

	gpu := p.Config.IsGPUEnabled()

	// Resource limits (optional)
	var resources *ResourceLimits
	if p.Config.Resources != nil {
		resources = &ResourceLimits{
			Memory: p.Config.Resources.Memory,
			CPUs:   p.Config.Resources.CPUs,
		}
	}

	tty := isTTY()

	return ContainerSpec{
		Name:         name,
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
		Resources:    resources,
		LogDir:       p.LogDir,
	}
}

func buildMounts(p BuildParams, pwd, workdir, homeDir string) []mount.Mount {
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
			Target: homeDir,
		},
		{
			Type:   mount.TypeBind,
			Source: pwd,
			Target: workdir,
		},
	}

	// Custom volumes from config (validated)
	for _, vol := range p.Config.Volumes {
		parts := strings.SplitN(vol, ":", 2)
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "ai-shim: skipping malformed volume %q (expected source:target)\n", vol)
			continue
		}
		if err := security.ValidateVolumePath(parts[0]); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: skipping invalid volume %s: %v\n", vol, err)
			continue
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: parts[0],
			Target: parts[1],
		})
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

func generateContainerName(agentName, profile, workspaceHash string) string {
	suffix := randomSuffix(4)
	return fmt.Sprintf("%s-%s-%s-%s", agentName, profile, workspaceHash, suffix)
}

func randomSuffix(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based suffix if crypto/rand fails
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n]
	}
	return fmt.Sprintf("%x", b)[:n]
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// ValidateConfigVolumes checks all volume mount paths for security issues.
func ValidateConfigVolumes(volumes []string) []error {
	var errs []error
	for _, vol := range volumes {
		parts := strings.SplitN(vol, ":", 2)
		if len(parts) < 2 {
			continue
		}
		if err := security.ValidateVolumePath(parts[0]); err != nil {
			errs = append(errs, fmt.Errorf("volume %s: %w", vol, err))
		}
	}
	return errs
}
