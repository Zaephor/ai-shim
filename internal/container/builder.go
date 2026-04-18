package container

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"sort"
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
	// Pwd is the host-side working directory to bind-mount as the
	// container's workspace. When empty, BuildSpec falls back to
	// os.Getwd() — that fallback is only correct when BuildSpec is
	// invoked by a process running on the same host the sidecar will
	// see. In nested invocations (a process inside an agent container
	// calling BuildSpec for an inner container) os.Getwd() returns the
	// agent-internal view, not the host path, so callers must pin Pwd
	// at the outermost layer and pass it through.
	Pwd string
}

// BuildSpec creates a ContainerSpec from the resolved parameters.
//
// Returns an error if a required host-side directory (e.g. a tool's
// persistent cache for a tool with data_dir:true) cannot be created.
// Callers must refuse to start the container in that case — running
// with a missing persistent mount silently drops the tool's state onto
// an ephemeral layer.
func BuildSpec(p BuildParams) (ContainerSpec, error) {
	image := p.Config.GetImage()
	hostname := p.Config.GetHostname()

	user := fmt.Sprintf("%d:%d", p.Platform.UID, p.Platform.GID)

	labels := map[string]string{
		LabelBase:    "true",
		LabelAgent:   p.Agent.Name,
		LabelProfile: p.Profile,
		LabelRole:    "agent",
	}

	// Workspace labels are set after wsHash is computed below.

	homeDir := p.HomeDir
	if homeDir == "" {
		homeDir = "/home/user"
	}

	pwd := p.Pwd
	if pwd == "" {
		got, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: cannot determine working directory: %v\n", err)
			got = "/tmp"
		}
		pwd = got
	}
	workdir := workspace.ContainerWorkdir(p.Platform.Hostname, pwd)

	wsHash := workspace.HashPath(p.Platform.Hostname, pwd)
	name := generateContainerName(p.Agent.Name, p.Profile, wsHash)

	labels[LabelWorkspace] = wsHash
	labels[LabelWorkspaceDir] = pwd

	mounts, mountsErr := buildMounts(p, pwd, workdir, homeDir)
	if mountsErr != nil {
		return ContainerSpec{}, mountsErr
	}

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
	packageScript := generatePackageScript(p.Config.Packages)

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

	// Load env_file first so the env: map can override on conflict.
	var fileVars map[string]string
	if p.Config.EnvFile != "" {
		envPath := p.Config.EnvFile
		if strings.HasPrefix(envPath, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				envPath = home + envPath[1:]
			}
		}
		var err error
		fileVars, err = config.ParseEnvFile(envPath)
		if err != nil {
			return ContainerSpec{}, fmt.Errorf("loading env_file: %w", err)
		}
	}

	env := buildEnv(p.Config.Env)

	// Prepend file vars — map-based env was already built above, so it wins
	// for any overlapping keys when Docker merges the slice (last wins).
	if len(fileVars) > 0 {
		fileEnv := make([]string, 0, len(fileVars))
		for k, v := range fileVars {
			fileEnv = append(fileEnv, k+"="+v)
		}
		env = append(fileEnv, env...)
	}

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

	// MCP server config as env var (JSON format for agent consumption).
	// Pass the YAML declaration order so the emitted JSON lists servers in
	// the order the user wrote them rather than Go map-iteration order.
	if len(p.Config.MCPServers) > 0 {
		env = append(env, "MCP_SERVERS="+mcpServersJSON(p.Config.MCPServers, p.Config.MCPServersOrder))
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
	}, nil
}

// IsTTY reports whether stdin is connected to a terminal.
func IsTTY() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func buildMounts(p BuildParams, pwd, workdir, homeDir string) ([]mount.Mount, error) {
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
			// Refuse to proceed: silently dropping the mount would run
			// the tool against an empty ephemeral layer, losing the
			// user's persisted state with no actionable feedback.
			return nil, fmt.Errorf("creating tool cache dir %s for tool %q: %w", hostPath, name, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: hostPath,
			Target: "/usr/local/share/ai-shim/cache/" + name,
		})
	}

	return mounts, nil
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
//
// When `order` is non-empty, entries are emitted in that sequence so the JSON
// preserves the YAML declaration order captured by Config.MCPServersOrder.
// Any keys present in the map but not in `order` are appended alphabetically
// so nothing is silently dropped. When `order` is empty, all keys are emitted
// in alphabetical order for deterministic output.
func mcpServersJSON(servers map[string]config.MCPServerDef, order []string) string {
	type mcpEntry struct {
		Command string            `json:"command"`
		Args    []string          `json:"args,omitempty"`
		Env     map[string]string `json:"env,omitempty"`
	}
	if len(servers) == 0 {
		return "{}"
	}
	// Build the final key sequence: honored-order keys first (skipping any
	// not present in the map), then any leftover map keys alphabetically so
	// nothing is silently dropped.
	seen := make(map[string]bool, len(servers))
	keys := make([]string, 0, len(servers))
	for _, k := range order {
		if _, ok := servers[k]; !ok {
			continue
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	leftover := make([]string, 0, len(servers))
	for k := range servers {
		if !seen[k] {
			leftover = append(leftover, k)
		}
	}
	sort.Strings(leftover)
	keys = append(keys, leftover...)

	var buf strings.Builder
	buf.WriteByte('{')
	for i, name := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		srv := servers[name]
		entry := mcpEntry{
			Command: srv.Command,
			Args:    srv.Args,
			Env:     srv.Env,
		}
		nameJSON, err := json.Marshal(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to marshal MCP server name %q: %v\n", name, err)
			return "{}"
		}
		valJSON, err := json.Marshal(entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to marshal MCP server %q: %v\n", name, err)
			return "{}"
		}
		buf.Write(nameJSON)
		buf.WriteByte(':')
		buf.Write(valJSON)
	}
	buf.WriteByte('}')
	return buf.String()
}

// generatePackageScript builds the shell snippet that installs apt packages
// inside the container. Returns an empty string when packages is empty.
//
// apt-get requires root. Images that run the agent as a non-root UID
// (e.g. ghcr.io/catthehacker/ubuntu runs as `ubuntu`) would have the
// install fail silently-ish with a permission error, leaving the user
// confused about why their declared packages weren't actually
// installed. Detect the effective UID at runtime and route through
// passwordless sudo when available; fail loudly with a clear hint
// when neither path is possible.
func generatePackageScript(packages []string) string {
	if len(packages) == 0 {
		return ""
	}
	quoted := make([]string, len(packages))
	for i, pkg := range packages {
		quoted[i] = shell.Quote(pkg)
	}
	pkgs := strings.Join(quoted, " ")
	var sb strings.Builder
	fmt.Fprintf(&sb, "echo \"Installing packages: %s\"\n", pkgs)
	sb.WriteString("if [ \"$(id -u)\" = \"0\" ]; then\n")
	fmt.Fprintf(&sb, "  apt-get update -qq && apt-get install -y -qq %s || { echo \"ERROR: package installation failed\"; exit 1; }\n", pkgs)
	sb.WriteString("elif command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then\n")
	fmt.Fprintf(&sb, "  sudo apt-get update -qq && sudo apt-get install -y -qq %s || { echo \"ERROR: package installation failed\"; exit 1; }\n", pkgs)
	sb.WriteString("else\n")
	fmt.Fprintf(&sb, "  echo \"ERROR: profile requests apt packages (%s) but the container is running as uid $(id -u) without passwordless sudo.\" >&2\n", pkgs)
	sb.WriteString("  echo \"       apt-get requires root. Options:\" >&2\n")
	sb.WriteString("  echo \"         - use a base image that runs as root, or that grants passwordless sudo to this user\" >&2\n")
	sb.WriteString("  echo \"         - rewrite these deps as self-contained tools: entries (binary-download / tar-extract / custom)\" >&2\n")
	sb.WriteString("  exit 1\n")
	sb.WriteString("fi\n")
	return sb.String()
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

// WarmEntrypoint rewrites a ContainerSpec's entrypoint so the provisioning
// and install scripts run to completion but the final `exec <agent-binary>`
// is replaced with a no-op exit. The spec is also made non-persistent and
// non-interactive so the container auto-removes after the warm run.
//
// Precondition: spec.Entrypoint must be ["sh", "-c", <script>] as produced
// by BuildSpec. Returns an error if the entrypoint shape is unexpected or
// the exec line cannot be found.
func WarmEntrypoint(spec *ContainerSpec) error {
	if len(spec.Entrypoint) != 3 || spec.Entrypoint[0] != "sh" || spec.Entrypoint[1] != "-c" {
		return fmt.Errorf("unexpected entrypoint shape (len=%d)", len(spec.Entrypoint))
	}
	script := spec.Entrypoint[2]
	idx := strings.LastIndex(script, "\nexec ")
	if idx < 0 {
		return fmt.Errorf("entrypoint script has no exec line to replace")
	}
	// Replace everything from the exec line to the end with a no-op.
	spec.Entrypoint[2] = script[:idx] + "\nexit 0\n"

	// Make the container ephemeral and non-interactive.
	spec.Persistent = false
	spec.TTY = false
	spec.Stdin = false
	spec.Reattach = false
	return nil
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
