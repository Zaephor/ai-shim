package container

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/install"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/provision"
	"github.com/Zaephor/ai-shim/internal/security"
	"github.com/Zaephor/ai-shim/internal/shell"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/Zaephor/ai-shim/internal/workspace"
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

	// Workspace labels are set after wsHash is computed below.

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

	labels[LabelWorkspace] = wsHash
	labels[LabelWorkspaceDir] = pwd

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
				DataDir: td.DataDir, CacheScope: td.CacheScope, EnvVar: td.EnvVar,
			}
		}
		toolScript = provision.GenerateInstallScript(p.Config.ToolsOrder, provTools, "/usr/local/share/ai-shim/bin")
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

	updateInterval, _ := config.ParseUpdateInterval(p.Config.UpdateInterval)

	entrypoint := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType:    p.Agent.InstallType,
		Package:        p.Agent.Package,
		Binary:         p.Agent.Binary,
		Version:        p.Config.Version,
		AgentArgs:      allArgs,
		AgentName:      p.Agent.Name,
		UpdateInterval: updateInterval,
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

	// Set HOME so git config --global and other tools find the right home.
	env = append(env, "HOME="+homeDir)

	// Pass through host terminal type so container apps render correctly.
	// Without this, Docker defaults to TERM=xterm which lacks 256-color
	// support and causes rendering issues.
	// Normalize screen/tmux TERM values (e.g. screen.xterm-256color →
	// xterm-256color) because the multiplexer-prefixed terminfo entries
	// rarely exist inside container images.
	if term := os.Getenv("TERM"); term != "" {
		if after, ok := strings.CutPrefix(term, "screen."); ok {
			term = after
		} else if after, ok := strings.CutPrefix(term, "tmux."); ok {
			term = after
		}
		env = append(env, "TERM="+term)
	}
	if val := os.Getenv("COLORTERM"); val != "" {
		env = append(env, "COLORTERM="+val)
	}

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

	tty := IsTTY()

	// TTY sessions are persistent (support detach/reattach).
	persistent := tty
	if persistent {
		labels[LabelPersistent] = "true"
	}

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
		Persistent:   persistent,
	}
}

// IsTTY reports whether stdin is connected to a terminal.
func IsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

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

	// Always mount the profile home at homeDir so the home directory
	// is writable (needed for git config, npm, etc). In isolated mode,
	// agent data dirs overlay on top and only the current agent's bins
	// are accessible.
	mounts = append(mounts, mount.Mount{
		Type:   mount.TypeBind,
		Source: profileHome,
		Target: homeDir,
	})

	if p.Config.IsIsolated() {
		// Isolated mode: only current agent's bins + allowed agents' bins
		for _, name := range p.Config.AllowAgents {
			if name == p.Agent.Name {
				continue
			}
			if _, ok := agent.Lookup(name); ok {
				mounts = append(mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: p.Layout.AgentBin(name),
					Target: "/usr/local/share/ai-shim/agents/" + name + "/bin",
				})
			} else {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: allow_agents references unknown agent %q (skipped)\n", name)
			}
		}
	} else {
		// Shared mode: all agents' bins accessible
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

	// Tool cache mounts for tools with data_dir enabled
	for name, td := range p.Config.Tools {
		if !td.DataDir {
			continue
		}
		hostPath := storage.ToolCachePath(p.Layout, name, td.CacheScope, p.Agent.Name, p.Profile)
		if err := os.MkdirAll(hostPath, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to create tool cache dir %s: %v\n", hostPath, err)
			continue
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: hostPath,
			Target: "/usr/local/share/ai-shim/cache/" + name,
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
			fmt.Fprintf(os.Stderr, "ai-shim: skipping invalid port %q: expected hostPort:containerPort format\n", p)
			continue
		}
		hostPort := parts[0]
		containerPort := parts[1]
		port, err := nat.NewPort("tcp", containerPort)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: skipping invalid port %q: %v\n", p, err)
			continue
		}
		portMap[port] = []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: hostPort},
		}
		portSet[port] = struct{}{}
	}
	return portMap, portSet
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
