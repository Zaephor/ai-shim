package container

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/provision"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/shell"
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
	HomeDir  string // container-side home directory (from image inspect)
	LogDir   string // directory for exit logs (empty = no logging)
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
		quoted := make([]string, len(p.Config.Packages))
		for i, pkg := range p.Config.Packages {
			quoted[i] = shell.Quote(pkg)
		}
		packageScript = "echo \"Installing packages: " + strings.Join(quoted, " ") + "\"\napt-get update -qq && apt-get install -y -qq " + strings.Join(quoted, " ") + " || { echo \"ERROR: package installation failed\"; exit 1; }\n"
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
			gitScript += fmt.Sprintf("git config --global user.name %s\n", shell.Quote(p.Config.Git.Name))
		}
		if p.Config.Git.Email != "" {
			gitScript += fmt.Sprintf("git config --global user.email %s\n", shell.Quote(p.Config.Git.Email))
		}
	}

	// Prepend tool, package, and git scripts to entrypoint
	fullScript := toolScript + packageScript + gitScript + entrypoint

	env := buildEnv(p.Config.Env)

	// MCP server config as env var (JSON format for agent consumption)
	if len(p.Config.MCPServers) > 0 {
		env = append(env, "MCP_SERVERS="+mcpServersJSON(p.Config.MCPServers))
	}

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

	// Security profile
	securityOpt, capDrop := resolveSecurityProfile(p.Config.SecurityProfile)

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
		SecurityOpt:  securityOpt,
		CapDrop:      capDrop,
		LogDir:       p.LogDir,
	}
}

// sharedHomeFiles are files in the home directory that are shared across all agents
// regardless of isolation mode, because they contain cross-agent configuration.
var sharedHomeFiles = []string{".gitconfig"}

func buildMounts(p BuildParams, pwd, workdir, homeDir string) []mount.Mount {
	profileHome := p.Layout.ProfileHome(p.Profile)

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
			Source: pwd,
			Target: workdir,
		},
	}

	if p.Config.IsIsolated() {
		// Isolated mode: mount only this agent's data dirs/files + allowed agents
		mounts = append(mounts, agentDataMounts(profileHome, homeDir, p.Agent)...)

		// Allowed agents: mount their bins and data
		for _, name := range p.Config.AllowAgents {
			if name == p.Agent.Name {
				continue
			}
			if def, ok := agent.Lookup(name); ok {
				mounts = append(mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: p.Layout.AgentBin(name),
					Target: "/usr/local/share/ai-shim/agents/" + name + "/bin",
				})
				mounts = append(mounts, agentDataMounts(profileHome, homeDir, def)...)
			}
		}

		// Shared home files (e.g. .gitconfig)
		mounts = append(mounts, sharedFileMounts(profileHome, homeDir)...)
	} else {
		// Shared mode: mount the entire profile home
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: profileHome,
			Target: homeDir,
		})

		// All agents' bins accessible
		for _, name := range agent.Names() {
			if name == p.Agent.Name {
				continue
			}
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: p.Layout.AgentBin(name),
				Target: "/usr/local/share/ai-shim/agents/" + name + "/bin",
			})
		}
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

// agentDataMounts creates bind mounts for an agent's data dirs and files.
func agentDataMounts(profileHome, homeDir string, def agent.Definition) []mount.Mount {
	var mounts []mount.Mount
	for _, dir := range def.DataDirs {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: filepath.Join(profileHome, dir),
			Target: filepath.Join(homeDir, dir),
		})
	}
	for _, file := range def.DataFiles {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: filepath.Join(profileHome, file),
			Target: filepath.Join(homeDir, file),
		})
	}
	return mounts
}

// sharedFileMounts creates bind mounts for files shared across all agents.
func sharedFileMounts(profileHome, homeDir string) []mount.Mount {
	var mounts []mount.Mount
	for _, file := range sharedHomeFiles {
		source := filepath.Join(profileHome, file)
		// Only mount if the file exists (shared files are optional)
		if _, err := os.Stat(source); err == nil {
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: source,
				Target: filepath.Join(homeDir, file),
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

func generateContainerName(agentName, profile, workspaceHash string) string {
	suffix := randomSuffix(8)
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

// mcpServersJSON serializes MCP server definitions to JSON for the MCP_SERVERS
// env var. The format matches what claude-code and other agents expect.
func mcpServersJSON(servers map[string]config.MCPServerDef) string {
	type mcpEntry struct {
		Command string            `json:"command"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
	}
	m := make(map[string]mcpEntry, len(servers))
	for name, srv := range servers {
		m[name] = mcpEntry{
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
		}
	}
	data, err := json.Marshal(m)
	if err != nil {
		// Should not happen with string maps, but be safe
		fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to marshal MCP servers: %v\n", err)
		return "{}"
	}
	return string(data)
}

// resolveSecurityProfile returns SecurityOpt and CapDrop based on the profile.
func resolveSecurityProfile(profile string) (securityOpt []string, capDrop []string) {
	switch profile {
	case "strict":
		securityOpt = []string{"no-new-privileges:true"}
		capDrop = []string{"ALL"}
	case "none":
		securityOpt = []string{"seccomp=unconfined"}
	default:
		// "default" or empty — Docker's default behavior, no changes
	}
	return
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
