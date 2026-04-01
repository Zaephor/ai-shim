package cli

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/color"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/dind"
	"github.com/ai-shim/ai-shim/internal/docker"
	"github.com/ai-shim/ai-shim/internal/parse"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/storage"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	network_types "github.com/docker/docker/api/types/network"
	volume_types "github.com/docker/docker/api/types/volume"
)

// dockerTimeout is the deadline for Docker API calls in management commands
// (status, cleanup, doctor). These should complete quickly — a hung daemon
// shouldn't block the CLI forever.
const dockerTimeout = 30 * time.Second

// ListAgents returns a formatted list of all built-in agents.
func ListAgents() string {
	var b strings.Builder
	b.WriteString("Built-in agents:\n")
	for _, name := range agent.Names() {
		def, _ := agent.Lookup(name)
		fmt.Fprintf(&b, "  %-15s  %-8s  %s\n", name, def.InstallType, def.Binary)
	}
	return b.String()
}

// ListProfiles returns a formatted list of profiles found in storage.
// It shows both config-defined profiles (from config/profiles/*.yaml) and
// runtime profiles (from profiles/*/). Config-defined profiles that haven't
// been launched yet are marked as "(not yet launched)".
func ListProfiles(layout storage.Layout) (string, error) {
	// Collect config-defined profiles from config/profiles/*.yaml
	configProfiles := listConfigProfiles(layout.ConfigDir)

	// Collect runtime profiles (directories under profiles/)
	runtimeProfiles, err := readDirNames(layout.Root, "profiles")
	if err != nil {
		return "", err
	}

	// Build a merged set: all config profiles + any runtime-only profiles
	seen := make(map[string]bool)
	runtimeSet := make(map[string]bool)
	for _, name := range runtimeProfiles {
		runtimeSet[name] = true
		seen[name] = true
	}
	for _, name := range configProfiles {
		seen[name] = true
	}

	if len(seen) == 0 {
		return "No profiles found.\n\nCreate a profile config at: " + filepath.Join(layout.ConfigDir, "profiles") + "/<name>.yaml\n", nil
	}

	// Collect sorted names
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString("Profiles:\n")
	for _, name := range names {
		if runtimeSet[name] {
			fmt.Fprintf(&b, "  %s\n", name)
		} else {
			fmt.Fprintf(&b, "  %s  (not yet launched)\n", name)
		}
	}
	return b.String(), nil
}

// listConfigProfiles reads profile names from config/profiles/*.yaml files.
func listConfigProfiles(configDir string) []string {
	profileDir := filepath.Join(configDir, "profiles")
	entries, err := os.ReadDir(profileDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if ext := filepath.Ext(name); ext == ".yaml" || ext == ".yml" {
			names = append(names, strings.TrimSuffix(name, ext))
		}
	}
	return names
}

// ShowConfig returns the fully resolved config for an agent+profile combination.
func ShowConfig(layout storage.Layout, agentName, profile string) (string, error) {
	cfg, sources, err := config.ResolveWithSources(layout.ConfigDir, agentName, profile)
	if err != nil {
		return "", err
	}

	src := func(field string) string {
		return sources.FormatSource(field)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Resolved config for %s_%s:\n\n", agentName, profile)

	if cfg.Image != "" {
		fmt.Fprintf(&b, "  image:    %s%s\n", cfg.GetImage(), src("image"))
	}
	if cfg.Hostname != "" {
		fmt.Fprintf(&b, "  hostname: %s%s\n", cfg.GetHostname(), src("hostname"))
	}
	if cfg.Version != "" {
		fmt.Fprintf(&b, "  version:  %s%s\n", cfg.Version, src("version"))
	}
	if cfg.UpdateInterval != "" {
		fmt.Fprintf(&b, "  update_interval: %s%s\n", cfg.UpdateInterval, src("update_interval"))
	}

	if len(cfg.Variables) > 0 {
		b.WriteString("  variables:\n")
		for k, v := range cfg.Variables {
			fmt.Fprintf(&b, "    %s=%s\n", k, v)
		}
	}

	if len(cfg.Env) > 0 {
		fmt.Fprintf(&b, "  env:%s\n", src("env"))
		masked := security.MaskSecrets(cfg.Env)
		for k := range cfg.Env {
			fmt.Fprintf(&b, "    %s=%s\n", k, masked[k])
		}
	}

	if len(cfg.Args) > 0 {
		fmt.Fprintf(&b, "  args:     %s%s\n", strings.Join(cfg.Args, " "), src("args"))
	}

	if len(cfg.Volumes) > 0 {
		fmt.Fprintf(&b, "  volumes:%s\n", src("volumes"))
		for _, v := range cfg.Volumes {
			fmt.Fprintf(&b, "    %s\n", v)
		}
	}

	if len(cfg.Ports) > 0 {
		fmt.Fprintf(&b, "  ports:%s\n", src("ports"))
		for _, p := range cfg.Ports {
			fmt.Fprintf(&b, "    %s\n", p)
		}
	}

	if len(cfg.Packages) > 0 {
		fmt.Fprintf(&b, "  packages:%s\n", src("packages"))
		for _, p := range cfg.Packages {
			fmt.Fprintf(&b, "    - %s\n", p)
		}
	}

	b.WriteString(formatBoolFieldSrc("dind", cfg.DIND, false, src("dind")))
	b.WriteString(formatBoolFieldSrc("gpu", cfg.GPU, false, src("gpu")))
	b.WriteString(formatBoolFieldSrc("dind_gpu", cfg.DINDGpu, false, src("dind_gpu")))

	if cfg.NetworkScope != "" {
		fmt.Fprintf(&b, "  network_scope: %s%s\n", cfg.NetworkScope, src("network_scope"))
	}
	if cfg.DINDHostname != "" {
		fmt.Fprintf(&b, "  dind_hostname: %s%s\n", cfg.DINDHostname, src("dind_hostname"))
	}

	if cfg.Resources != nil {
		fmt.Fprintf(&b, "  resources:%s\n", src("resources"))
		if cfg.Resources.Memory != "" {
			fmt.Fprintf(&b, "    memory: %s\n", cfg.Resources.Memory)
		}
		if cfg.Resources.CPUs != "" {
			fmt.Fprintf(&b, "    cpus:   %s\n", cfg.Resources.CPUs)
		}
	}
	if cfg.DINDResources != nil {
		fmt.Fprintf(&b, "  dind_resources:%s\n", src("dind_resources"))
		if cfg.DINDResources.Memory != "" {
			fmt.Fprintf(&b, "    memory: %s\n", cfg.DINDResources.Memory)
		}
		if cfg.DINDResources.CPUs != "" {
			fmt.Fprintf(&b, "    cpus:   %s\n", cfg.DINDResources.CPUs)
		}
	}

	if len(cfg.DINDMirrors) > 0 {
		fmt.Fprintf(&b, "  dind_mirrors:%s\n", src("dind_mirrors"))
		for _, m := range cfg.DINDMirrors {
			fmt.Fprintf(&b, "    - %s\n", m)
		}
	}

	b.WriteString(formatBoolFieldSrc("dind_cache", cfg.DINDCache, false, src("dind_cache")))
	b.WriteString(formatBoolFieldSrc("dind_tls", cfg.DINDTLS, false, src("dind_tls")))
	b.WriteString(formatBoolFieldSrc("isolated", cfg.Isolated, true, src("isolated")))

	if len(cfg.AllowAgents) > 0 {
		fmt.Fprintf(&b, "  allow_agents:%s\n", src("allow_agents"))
		for _, a := range cfg.AllowAgents {
			fmt.Fprintf(&b, "    - %s\n", a)
		}
	}

	if len(cfg.MCPServers) > 0 {
		fmt.Fprintf(&b, "  mcp_servers:%s\n", src("mcp_servers"))
		for name, srv := range cfg.MCPServers {
			fmt.Fprintf(&b, "    %s: %s\n", name, srv.Command)
		}
	}

	if len(cfg.Tools) > 0 {
		fmt.Fprintf(&b, "  tools:%s\n", src("tools"))
		for name, tool := range cfg.Tools {
			fmt.Fprintf(&b, "    %s: (type=%s)\n", name, tool.Type)
		}
	}

	if cfg.SecurityProfile != "" {
		fmt.Fprintf(&b, "  security_profile: %s%s\n", cfg.SecurityProfile, src("security_profile"))
	}

	if cfg.Git != nil && (cfg.Git.Name != "" || cfg.Git.Email != "") {
		fmt.Fprintf(&b, "  git:%s\n", src("git"))
		if cfg.Git.Name != "" {
			fmt.Fprintf(&b, "    name:  %s\n", cfg.Git.Name)
		}
		if cfg.Git.Email != "" {
			fmt.Fprintf(&b, "    email: %s\n", cfg.Git.Email)
		}
	}

	return b.String(), nil
}

// Doctor runs diagnostic checks and returns results.
func Doctor() string {
	return DoctorWithColor(color.Enabled())
}

// DoctorWithColor runs diagnostic checks with explicit color control.
func DoctorWithColor(useColor bool) string {
	c := color.New(useColor)
	var b strings.Builder
	b.WriteString("ai-shim doctor\n\n")

	// Check Docker
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()
	cli, err := docker.NewClientNoPing()
	if err != nil {
		fmt.Fprintf(&b, "  Docker client:  %s (%s)\n", c.Red("FAIL"), err.Error())
	} else {
		defer func() { _ = cli.Close() }()
		if _, err := cli.Ping(ctx); err != nil {
			fmt.Fprintf(&b, "  Docker daemon:  %s (%s)\n", c.Red("FAIL"), err.Error())
		} else {
			info, _ := cli.Info(ctx)
			fmt.Fprintf(&b, "  Docker daemon:  %s (server %s)\n", c.Green("OK"), info.ServerVersion)
		}

		// Check default image
		_, imgErr := cli.ImageInspect(ctx, container.DefaultImage)
		if imgErr != nil {
			fmt.Fprintf(&b, "  Default image:  %s (%s) — will be pulled on first use\n", c.Yellow("NOT CACHED"), container.DefaultImage)
		} else {
			fmt.Fprintf(&b, "  Default image:  %s (%s)\n", c.Green("OK"), container.DefaultImage)
		}
	}

	// Check storage root
	layout := storage.NewLayout(storage.DefaultRoot())
	fmt.Fprintf(&b, "  Storage root:   %s\n", layout.Root)

	// Check config dir
	fmt.Fprintf(&b, "  Config dir:     %s\n", layout.ConfigDir)

	// Config validation
	b.WriteString("\n  Config files:\n")
	configFiles := []struct {
		name string
		path string
	}{
		{"default.yaml", filepath.Join(layout.ConfigDir, "default.yaml")},
	}
	// Also check agent and profile configs that exist
	for _, subdir := range []string{"agents", "profiles", "agent-profiles"} {
		dirPath := filepath.Join(layout.ConfigDir, subdir)
		entries, _ := os.ReadDir(dirPath)
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".yaml") {
				configFiles = append(configFiles, struct {
					name string
					path string
				}{subdir + "/" + e.Name(), filepath.Join(dirPath, e.Name())})
			}
		}
	}
	configOK := true
	for _, cf := range configFiles {
		_, warnings, loadErr := config.LoadFileStrict(cf.path)
		if loadErr != nil {
			fmt.Fprintf(&b, "    %s: %s (%v)\n", cf.name, c.Red("ERROR"), loadErr)
			configOK = false
		} else if len(warnings) > 0 {
			fmt.Fprintf(&b, "    %s: %s (unknown keys)\n", cf.name, c.Yellow("WARNING"))
			for _, w := range warnings {
				fmt.Fprintf(&b, "      %s\n", w)
			}
			configOK = false
		}
	}
	if configOK {
		fmt.Fprintf(&b, "    all configs: %s (%d files checked)\n", c.Green("OK"), len(configFiles))
	}

	// Image pinning status
	b.WriteString("\n  Image pinning:\n")
	fmt.Fprintf(&b, "    agent image: %s (%s)\n", container.DefaultImage, imagePinLabel(container.DefaultImage, true))
	fmt.Fprintf(&b, "    dind image:  %s (%s)\n", dind.DefaultImage, imagePinLabel(dind.DefaultImage, true))
	fmt.Fprintf(&b, "    cache image: %s (%s)\n", dind.CacheImage, imagePinLabel(dind.CacheImage, true))

	return b.String()
}

// CreateSymlink creates a symlink for an agent+profile combination.
func CreateSymlink(agent, profile, targetDir string, shimPath string) (string, error) {
	name := agent
	if profile != "" && profile != "default" {
		name = agent + "_" + profile
	}
	linkPath := filepath.Join(targetDir, name)

	if _, err := os.Lstat(linkPath); err == nil {
		return "", fmt.Errorf("symlink %s already exists", linkPath)
	}

	if err := os.Symlink(shimPath, linkPath); err != nil {
		return "", fmt.Errorf("creating symlink: %w", err)
	}
	return linkPath, nil
}

// ListSymlinks finds all symlinks pointing to ai-shim in a directory.
func ListSymlinks(dir string, shimPath string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var links []string
	for _, e := range entries {
		if e.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(filepath.Join(dir, e.Name()))
			if err == nil && (target == shimPath || filepath.Base(target) == "ai-shim") {
				links = append(links, e.Name())
			}
		}
	}
	return links, nil
}

// RemoveSymlink removes a symlink if it points to ai-shim.
func RemoveSymlink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("%s is not a symlink", path)
	}
	return os.Remove(path)
}

// DryRun returns a formatted representation of the container spec that would be created.
func DryRun(layout storage.Layout, agentName, profile string, args []string) (string, error) {
	cfg, err := config.Resolve(layout.ConfigDir, agentName, profile)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Dry run for %s_%s:\n\n", agentName, profile)

	fmt.Fprintf(&b, "  Image:     %s\n", cfg.GetImage())
	fmt.Fprintf(&b, "  Hostname:  %s\n", cfg.GetHostname())

	if cfg.Version != "" {
		fmt.Fprintf(&b, "  Version:   %s\n", cfg.Version)
	}
	if cfg.UpdateInterval != "" {
		fmt.Fprintf(&b, "  Update:    %s\n", cfg.UpdateInterval)
	} else {
		b.WriteString("  Update:    1d (default)\n")
	}

	if len(cfg.Variables) > 0 {
		b.WriteString("  Variables:\n")
		for k, v := range cfg.Variables {
			fmt.Fprintf(&b, "    %s=%s\n", k, v)
		}
	}

	if len(cfg.Env) > 0 {
		b.WriteString("  Env:\n")
		masked := security.MaskSecrets(cfg.Env)
		for k := range cfg.Env {
			fmt.Fprintf(&b, "    %s=%s\n", k, masked[k])
		}
	}

	if len(cfg.Volumes) > 0 {
		b.WriteString("  Volumes:\n")
		for _, v := range cfg.Volumes {
			fmt.Fprintf(&b, "    %s\n", v)
		}
	}

	if len(cfg.Ports) > 0 {
		b.WriteString("  Ports:\n")
		for _, p := range cfg.Ports {
			fmt.Fprintf(&b, "    %s\n", p)
		}
	}

	if len(cfg.Args) > 0 {
		fmt.Fprintf(&b, "  Default args: %s\n", strings.Join(cfg.Args, " "))
	}
	if len(args) > 0 {
		fmt.Fprintf(&b, "  Passthrough:  %s\n", strings.Join(args, " "))
	}

	if len(cfg.Packages) > 0 {
		b.WriteString("  Packages:\n")
		for _, p := range cfg.Packages {
			fmt.Fprintf(&b, "    - %s\n", p)
		}
	}

	b.WriteString(formatEnabledField("DIND", cfg.DIND))
	b.WriteString(formatEnabledField("GPU", cfg.GPU))
	b.WriteString(formatEnabledField("DIND GPU", cfg.DINDGpu))

	if cfg.NetworkScope != "" {
		fmt.Fprintf(&b, "  Network:   %s\n", cfg.NetworkScope)
	}
	if cfg.DINDHostname != "" {
		fmt.Fprintf(&b, "  DIND Host: %s\n", cfg.DINDHostname)
	}

	if cfg.Resources != nil {
		b.WriteString("  Resources:\n")
		if cfg.Resources.Memory != "" {
			fmt.Fprintf(&b, "    memory: %s\n", cfg.Resources.Memory)
		}
		if cfg.Resources.CPUs != "" {
			fmt.Fprintf(&b, "    cpus:   %s\n", cfg.Resources.CPUs)
		}
	}
	if cfg.DINDResources != nil {
		b.WriteString("  DIND Resources:\n")
		if cfg.DINDResources.Memory != "" {
			fmt.Fprintf(&b, "    memory: %s\n", cfg.DINDResources.Memory)
		}
		if cfg.DINDResources.CPUs != "" {
			fmt.Fprintf(&b, "    cpus:   %s\n", cfg.DINDResources.CPUs)
		}
	}

	if len(cfg.DINDMirrors) > 0 {
		b.WriteString("  DIND Mirrors:\n")
		for _, m := range cfg.DINDMirrors {
			fmt.Fprintf(&b, "    - %s\n", m)
		}
	}

	b.WriteString(formatEnabledField("DIND Cache", cfg.DINDCache))
	b.WriteString(formatEnabledField("DIND TLS", cfg.DINDTLS))
	b.WriteString(formatEnabledField("Isolated", cfg.Isolated))

	if len(cfg.AllowAgents) > 0 {
		b.WriteString("  Allow Agents:\n")
		for _, a := range cfg.AllowAgents {
			fmt.Fprintf(&b, "    - %s\n", a)
		}
	}

	if len(cfg.MCPServers) > 0 {
		b.WriteString("  MCP Servers:\n")
		for name, srv := range cfg.MCPServers {
			fmt.Fprintf(&b, "    %s: %s\n", name, srv.Command)
		}
	}

	if len(cfg.Tools) > 0 {
		b.WriteString("  Tools:\n")
		for name, tool := range cfg.Tools {
			fmt.Fprintf(&b, "    %s: (type=%s)\n", name, tool.Type)
		}
	}

	if cfg.Git != nil && (cfg.Git.Name != "" || cfg.Git.Email != "") {
		b.WriteString("  Git:\n")
		if cfg.Git.Name != "" {
			fmt.Fprintf(&b, "    name:  %s\n", cfg.Git.Name)
		}
		if cfg.Git.Email != "" {
			fmt.Fprintf(&b, "    email: %s\n", cfg.Git.Email)
		}
	}

	if cfg.SecurityProfile != "" {
		fmt.Fprintf(&b, "  Security:  %s\n", cfg.SecurityProfile)
	}

	return b.String(), nil
}

// CleanupResult holds the results of a cleanup operation.
type CleanupResult struct {
	RemovedContainers []string
	RemovedNetworks   []string
	RemovedVolumes    []string
	Failed            []string
}

// Cleanup finds and removes orphaned ai-shim containers, networks, and volumes.
func Cleanup() (CleanupResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return CleanupResult{}, err
	}
	defer func() { _ = cli.Close() }()

	var result CleanupResult

	// Clean orphaned containers
	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
	})
	if err != nil {
		return CleanupResult{}, fmt.Errorf("listing containers: %w", err)
	}

	for _, c := range containers {
		name := containerDisplayName(c)
		if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
			result.Failed = append(result.Failed, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		result.RemovedContainers = append(result.RemovedContainers, name)
	}

	// Clean orphaned networks
	networks, err := cli.NetworkList(ctx, network_types.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
	})
	if err == nil {
		for _, n := range networks {
			if err := cli.NetworkRemove(ctx, n.ID); err != nil {
				result.Failed = append(result.Failed, fmt.Sprintf("network %s: %v", n.Name, err))
			} else {
				result.RemovedNetworks = append(result.RemovedNetworks, n.Name)
			}
		}
	}

	// Clean orphaned volumes
	volumes, err := cli.VolumeList(ctx, volume_types.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
	})
	if err == nil {
		for _, v := range volumes.Volumes {
			if err := cli.VolumeRemove(ctx, v.Name, true); err != nil {
				result.Failed = append(result.Failed, fmt.Sprintf("volume %s: %v", v.Name, err))
			} else {
				result.RemovedVolumes = append(result.RemovedVolumes, v.Name)
			}
		}
	}

	return result, nil
}

// Status returns a formatted list of running ai-shim containers.
func Status() (string, error) {
	return StatusWithColor(color.Enabled())
}

// StatusWithColor returns a formatted list of running ai-shim containers
// with explicit color control.
func StatusWithColor(useColor bool) (string, error) {
	col := color.New(useColor)
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = cli.Close() }()

	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
	})
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	if len(containers) == 0 {
		return "No running ai-shim containers.\n", nil
	}

	var b strings.Builder
	b.WriteString("Running ai-shim containers:\n\n")
	fmt.Fprintf(&b, "  %-35s %-15s %-10s %-25s %s\n", "NAME", "AGENT", "PROFILE", "IMAGE", "STATUS")
	fmt.Fprintf(&b, "  %-35s %-15s %-10s %-25s %s\n", "----", "-----", "-------", "-----", "------")

	for _, c := range containers {
		name := containerDisplayName(c)
		agentLabel := c.Labels[container.LabelAgent]
		profile := c.Labels[container.LabelProfile]

		// Mark cache and DIND containers
		if c.Labels[container.LabelCache] == "true" {
			agentLabel = "(cache)"
			profile = ""
		} else if strings.HasSuffix(name, "-dind") {
			agentLabel = agentLabel + " (dind)"
		}

		image := c.Image
		if len(image) > 25 {
			image = image[:22] + "..."
		}

		status := colorizeStatus(col, c.Status)
		fmt.Fprintf(&b, "  %-35s %-15s %-10s %-25s %s\n", name, agentLabel, profile, image, status)
	}
	return b.String(), nil
}

// colorizeStatus applies color to container status strings.
func colorizeStatus(c color.Colorer, status string) string {
	lower := strings.ToLower(status)
	switch {
	case strings.HasPrefix(lower, "up"):
		return c.Green(status)
	case strings.HasPrefix(lower, "exited"):
		return c.Red(status)
	case strings.Contains(lower, "created"), strings.Contains(lower, "restarting"):
		return c.Yellow(status)
	default:
		return status
	}
}

// BackupProfile creates a tar.gz archive of a profile's home directory.
func BackupProfile(layout storage.Layout, profile, outputPath string) error {
	profileDir := layout.ProfileHome(profile)
	if _, err := os.Stat(profileDir); os.IsNotExist(err) {
		return fmt.Errorf("profile %q does not exist", profile)
	}

	if outputPath == "" {
		outputPath = fmt.Sprintf("ai-shim-backup-%s-%s.tar.gz", profile, time.Now().Format("20060102-150405"))
	}

	cmd := exec.Command("tar", "czf", outputPath, "-C", filepath.Dir(profileDir), filepath.Base(profileDir))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating backup: %s: %w", string(output), err)
	}

	return nil
}

// RestoreProfile extracts a tar.gz archive into a profile's home directory.
func RestoreProfile(layout storage.Layout, profile, archivePath string) error {
	profileDir := layout.ProfileHome(profile)
	if err := os.MkdirAll(filepath.Dir(profileDir), 0755); err != nil {
		return fmt.Errorf("creating profile directory: %w", err)
	}

	cmd := exec.Command("tar", "xzf", archivePath, "-C", filepath.Dir(profileDir))
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restoring backup: %s: %w", string(output), err)
	}

	return nil
}

// DiskUsage returns a formatted report of ai-shim storage usage.
func DiskUsage(layout storage.Layout) (string, error) {
	var b strings.Builder
	b.WriteString("ai-shim disk usage:\n\n")

	dirs := []struct {
		name string
		path string
	}{
		{"Shared", filepath.Join(layout.Root, "shared")},
		{"Agents", filepath.Join(layout.Root, "agents")},
		{"Profiles", filepath.Join(layout.Root, "profiles")},
		{"Config", layout.ConfigDir},
		{"Logs", filepath.Join(layout.Root, "logs")},
	}

	var total int64
	for _, dir := range dirs {
		size, err := dirSize(dir.path)
		if err != nil {
			fmt.Fprintf(&b, "  %-12s  (not found)\n", dir.name)
			continue
		}
		total += size
		fmt.Fprintf(&b, "  %-12s  %s\n", dir.name, formatBytes(size))
	}
	fmt.Fprintf(&b, "\n  %-12s  %s\n", "Total", formatBytes(total))

	// Per-profile breakdown
	profilesDir := filepath.Join(layout.Root, "profiles")
	entries, _ := os.ReadDir(profilesDir)
	if len(entries) > 0 {
		b.WriteString("\nPer-profile:\n")
		for _, e := range entries {
			if e.IsDir() {
				size, _ := dirSize(filepath.Join(profilesDir, e.Name()))
				fmt.Fprintf(&b, "  %-20s  %s\n", e.Name(), formatBytes(size))
			}
		}
	}

	return b.String(), nil
}

func dirSize(path string) (int64, error) {
	var size int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMG"[exp])
}

// formatEnabledField formats a *bool config field as enabled/disabled for DryRun output.
func formatEnabledField(name string, val *bool) string {
	enabled := "disabled"
	if val != nil && *val {
		enabled = "enabled"
	}
	return fmt.Sprintf("  %-12s %s\n", name+":", enabled)
}

func formatBoolFieldSrc(name string, val *bool, defaultVal bool, srcAnnotation string) string {
	if val != nil {
		if *val {
			return fmt.Sprintf("  %-15s true%s\n", name+":", srcAnnotation)
		}
		return fmt.Sprintf("  %-15s false%s\n", name+":", srcAnnotation)
	}
	if defaultVal {
		return fmt.Sprintf("  %-15s true (default)\n", name+":")
	}
	return ""
}

// imagePinLabel returns a display label for an image's pinning status.
func imagePinLabel(image string, isDefault bool) string {
	pinned := parse.IsDigestPinned(image)
	label := "tag"
	if pinned {
		label = "pinned"
	}
	if isDefault {
		label += ", default"
	}
	return label
}

func containerDisplayName(c container_types.Summary) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID[:12]
}

// AgentVersions returns a formatted report of installed agent versions.
func AgentVersions(layout storage.Layout) string {
	var b strings.Builder
	b.WriteString("Installed agent versions:\n\n")

	for _, name := range agent.Names() {
		def, _ := agent.Lookup(name)
		binDir := layout.AgentBin(name)

		status := "not installed"
		entries, err := os.ReadDir(binDir)
		if err == nil && len(entries) > 0 {
			// Try to get version from the binary
			binaryPath := filepath.Join(binDir, def.Binary)
			if _, statErr := os.Stat(binaryPath); statErr == nil {
				// Try --version
				cmd := exec.Command(binaryPath, "--version")
				cmd.Env = []string{"HOME=/tmp", "PATH=" + binDir + ":/usr/bin:/bin"}
				out, err := cmd.Output()
				if err == nil {
					ver := strings.TrimSpace(string(out))
					// Take just the first line
					if idx := strings.IndexByte(ver, '\n'); idx >= 0 {
						ver = ver[:idx]
					}
					status = ver
				} else {
					status = "installed (version unknown)"
				}
			} else {
				status = "installed (binary not found in cache)"
			}
		}

		fmt.Fprintf(&b, "  %-15s  %s\n", name, status)
	}

	return b.String()
}

// Reinstall clears an agent's bin cache directory, forcing reinstall on next launch.
func Reinstall(layout storage.Layout, agentName string) error {
	if _, ok := agent.Lookup(agentName); !ok {
		return fmt.Errorf("unknown agent: %s", agentName)
	}

	binDir := layout.AgentBin(agentName)
	if _, err := os.Stat(binDir); os.IsNotExist(err) {
		return fmt.Errorf("agent %s is not installed (no bin directory)", agentName)
	}

	// Remove all files in the bin directory
	entries, err := os.ReadDir(binDir)
	if err != nil {
		return fmt.Errorf("reading bin directory: %w", err)
	}

	for _, entry := range entries {
		path := filepath.Join(binDir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("removing %s: %w", path, err)
		}
	}

	return nil
}

// ListAgentsJSON returns agents as a JSON string.
func ListAgentsJSON() (string, error) {
	var entries []AgentEntry
	for _, name := range agent.Names() {
		def, _ := agent.Lookup(name)
		entries = append(entries, AgentEntry{
			Name:        name,
			InstallType: def.InstallType,
			Binary:      def.Binary,
		})
	}
	return MarshalJSON(entries)
}

// ProfileEntry is a JSON-serializable profile listing entry.
type ProfileEntry struct {
	Name     string `json:"name"`
	Launched bool   `json:"launched"`
}

// ListProfilesJSON returns profile info as a JSON string.
func ListProfilesJSON(layout storage.Layout) (string, error) {
	configProfiles := listConfigProfiles(layout.ConfigDir)
	runtimeProfiles, err := readDirNames(layout.Root, "profiles")
	if err != nil {
		return "", err
	}

	runtimeSet := make(map[string]bool)
	seen := make(map[string]bool)
	for _, name := range runtimeProfiles {
		runtimeSet[name] = true
		seen[name] = true
	}
	for _, name := range configProfiles {
		seen[name] = true
	}

	var entries []ProfileEntry
	var names []string
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entries = append(entries, ProfileEntry{Name: name, Launched: runtimeSet[name]})
	}
	if entries == nil {
		entries = []ProfileEntry{}
	}
	return MarshalJSON(entries)
}

// ShowConfigJSON returns the resolved config as JSON.
func ShowConfigJSON(layout storage.Layout, agentName, profile string) (string, error) {
	cfg, err := config.Resolve(layout.ConfigDir, agentName, profile)
	if err != nil {
		return "", err
	}
	return MarshalJSON(cfg)
}

// DoctorJSON runs diagnostic checks and returns results as JSON.
func DoctorJSON() (string, error) {
	result := DoctorResult{
		ImagePinning: []PinStatus{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()
	cli, err := docker.NewClientNoPing()
	if err != nil {
		result.Docker = DoctorCheck{Status: "fail", Detail: err.Error()}
	} else {
		defer func() { _ = cli.Close() }()
		if _, err := cli.Ping(ctx); err != nil {
			result.Docker = DoctorCheck{Status: "fail", Detail: err.Error()}
		} else {
			info, _ := cli.Info(ctx)
			result.Docker = DoctorCheck{Status: "ok", Detail: "server " + info.ServerVersion}
		}

		_, imgErr := cli.ImageInspect(ctx, container.DefaultImage)
		if imgErr != nil {
			result.DefaultImage = DoctorCheck{Status: "not_cached", Detail: container.DefaultImage}
		} else {
			result.DefaultImage = DoctorCheck{Status: "ok", Detail: container.DefaultImage}
		}
	}

	layout := storage.NewLayout(storage.DefaultRoot())
	result.StorageRoot = layout.Root
	result.ConfigDir = layout.ConfigDir

	result.ImagePinning = []PinStatus{
		{Label: "agent", Image: container.DefaultImage, Pinned: parse.IsDigestPinned(container.DefaultImage)},
		{Label: "dind", Image: dind.DefaultImage, Pinned: parse.IsDigestPinned(dind.DefaultImage)},
		{Label: "cache", Image: dind.CacheImage, Pinned: parse.IsDigestPinned(dind.CacheImage)},
	}

	return MarshalJSON(result)
}

// StatusJSON returns container status as a JSON string.
func StatusJSON() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = cli.Close() }()

	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", container.LabelBase+"=true")),
	})
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	entries := make([]StatusEntry, 0, len(containers))
	for _, c := range containers {
		name := containerDisplayName(c)
		agentLabel := c.Labels[container.LabelAgent]
		profile := c.Labels[container.LabelProfile]

		if c.Labels[container.LabelCache] == "true" {
			agentLabel = "(cache)"
			profile = ""
		} else if strings.HasSuffix(name, "-dind") {
			agentLabel = agentLabel + " (dind)"
		}

		entries = append(entries, StatusEntry{
			Name:    name,
			Agent:   agentLabel,
			Profile: profile,
			Image:   c.Image,
			Status:  c.Status,
		})
	}
	return MarshalJSON(entries)
}

// DiskUsageJSON returns disk usage as a JSON string.
func DiskUsageJSON(layout storage.Layout) (string, error) {
	dirs := []struct {
		name string
		path string
	}{
		{"Shared", filepath.Join(layout.Root, "shared")},
		{"Agents", filepath.Join(layout.Root, "agents")},
		{"Profiles", filepath.Join(layout.Root, "profiles")},
		{"Config", layout.ConfigDir},
		{"Logs", filepath.Join(layout.Root, "logs")},
	}

	result := DiskUsageResult{
		Directories: make([]DiskUsageEntry, 0, len(dirs)),
	}

	for _, dir := range dirs {
		size, err := dirSize(dir.path)
		if err != nil {
			result.Directories = append(result.Directories, DiskUsageEntry{
				Name:  dir.name,
				Path:  dir.path,
				Bytes: 0,
			})
			continue
		}
		result.Total += size
		result.Directories = append(result.Directories, DiskUsageEntry{
			Name:  dir.name,
			Path:  dir.path,
			Bytes: size,
		})
	}

	profilesDir := filepath.Join(layout.Root, "profiles")
	entries, _ := os.ReadDir(profilesDir)
	for _, e := range entries {
		if e.IsDir() {
			size, _ := dirSize(filepath.Join(profilesDir, e.Name()))
			result.Profiles = append(result.Profiles, DiskUsageEntry{
				Name:  e.Name(),
				Bytes: size,
			})
		}
	}

	return MarshalJSON(result)
}

// ShowLogs returns the persistent launch/exit log for all agents or a
// specific agent/profile. When agent is empty, it shows the full log.
// When agent is specified, it filters to lines matching that agent.
func ShowLogs(layout storage.Layout, agent, profile string, tailN int) (string, error) {
	logFile := filepath.Join(layout.Root, "logs", "ai-shim.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "No logs found. Logs are written after each agent launch.\n", nil
		}
		return "", err
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Filter by agent/profile if specified
	if agent != "" {
		var filtered []string
		agentMatch := "agent=" + agent
		profileMatch := "profile=" + profile
		for _, line := range lines {
			if !strings.Contains(line, agentMatch) {
				continue
			}
			if profile != "" && !strings.Contains(line, profileMatch) {
				continue
			}
			filtered = append(filtered, line)
		}
		lines = filtered
	}

	if len(lines) == 0 {
		if agent != "" {
			return fmt.Sprintf("No logs found for agent=%s profile=%s.\n", agent, profile), nil
		}
		return "No logs found.\n", nil
	}

	// Tail last N lines
	if tailN > 0 && tailN < len(lines) {
		lines = lines[len(lines)-tailN:]
	}

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// ContainerLogs fetches Docker logs for the most recent container matching
// the given agent and profile labels.
func ContainerLogs(agent, profile string, tailLines int) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dockerTimeout)
	defer cancel()

	cli, err := docker.NewClient(ctx)
	if err != nil {
		return "", fmt.Errorf("connecting to Docker: %w", err)
	}
	defer func() { _ = cli.Close() }()

	// Find containers matching agent/profile (include stopped containers)
	filterArgs := filters.NewArgs(
		filters.Arg("label", container.LabelBase+"=true"),
		filters.Arg("label", container.LabelAgent+"="+agent),
	)
	if profile != "" {
		filterArgs.Add("label", container.LabelProfile+"="+profile)
	}

	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	if len(containers) == 0 {
		hint := agent
		if profile != "" {
			hint += "/" + profile
		}
		return fmt.Sprintf("No containers found for %s.\nRun 'ai-shim manage status' to see active containers.\n", hint), nil
	}

	// Use the most recently created container
	target := containers[0]
	for _, c := range containers[1:] {
		if c.Created > target.Created {
			target = c
		}
	}

	tail := "100"
	if tailLines > 0 {
		tail = fmt.Sprintf("%d", tailLines)
	}

	reader, err := cli.ContainerLogs(ctx, target.ID, container_types.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		return "", fmt.Errorf("fetching logs: %w", err)
	}
	defer func() { _ = reader.Close() }()

	var buf strings.Builder
	containerName := target.ID[:12]
	if len(target.Names) > 0 {
		containerName = strings.TrimPrefix(target.Names[0], "/")
	}
	fmt.Fprintf(&buf, "Logs for container %s (status: %s):\n\n", containerName, target.Status)

	// Docker log stream has 8-byte header per frame; read raw for simplicity
	logBytes, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("reading logs: %w", err)
	}

	// Strip Docker multiplexed stream headers (8-byte prefix per frame)
	buf.Write(stripDockerLogHeaders(logBytes))
	return buf.String(), nil
}

// stripDockerLogHeaders removes the 8-byte Docker multiplexed log frame headers.
// Format: [stream_type(1)][0(3)][size(4)][payload(size)]
func stripDockerLogHeaders(data []byte) []byte {
	var result []byte
	for len(data) >= 8 {
		// Frame: 1 byte stream type, 3 padding, 4 byte big-endian size
		size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
		data = data[8:]
		if size > len(data) {
			size = len(data)
		}
		result = append(result, data[:size]...)
		data = data[size:]
	}
	return result
}

func readDirNames(root, subdir string) ([]string, error) {
	dirPath := filepath.Join(root, subdir)
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}
