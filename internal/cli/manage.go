package cli

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/color"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/dind"
	"github.com/ai-shim/ai-shim/internal/docker"
	"github.com/ai-shim/ai-shim/internal/parse"
	"github.com/ai-shim/ai-shim/internal/storage"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	network_types "github.com/docker/docker/api/types/network"
	volume_types "github.com/docker/docker/api/types/volume"
)

// ListAgents returns a formatted list of all built-in agents.
func ListAgents() string {
	var b strings.Builder
	b.WriteString("Built-in agents:\n")
	for _, name := range agent.Names() {
		def, _ := agent.Lookup(name)
		b.WriteString(fmt.Sprintf("  %-15s  %-8s  %s\n", name, def.InstallType, def.Binary))
	}
	return b.String()
}

// ListProfiles returns a formatted list of profiles found in storage.
func ListProfiles(layout storage.Layout) (string, error) {
	// Read profiles directory
	entries, err := readDirNames(layout.Root, "profiles")
	if err != nil {
		return "", err
	}

	if len(entries) == 0 {
		return "No profiles found.\n", nil
	}

	var b strings.Builder
	b.WriteString("Profiles:\n")
	for _, name := range entries {
		b.WriteString(fmt.Sprintf("  %s\n", name))
	}
	return b.String(), nil
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
	b.WriteString(fmt.Sprintf("Resolved config for %s_%s:\n\n", agentName, profile))

	if cfg.Image != "" {
		b.WriteString(fmt.Sprintf("  image:    %s%s\n", cfg.GetImage(), src("image")))
	}
	if cfg.Hostname != "" {
		b.WriteString(fmt.Sprintf("  hostname: %s%s\n", cfg.GetHostname(), src("hostname")))
	}
	if cfg.Version != "" {
		b.WriteString(fmt.Sprintf("  version:  %s%s\n", cfg.Version, src("version")))
	}

	if len(cfg.Env) > 0 {
		b.WriteString(fmt.Sprintf("  env:%s\n", src("env")))
		for k, v := range cfg.Env {
			b.WriteString(fmt.Sprintf("    %s=%s\n", k, v))
		}
	}

	if len(cfg.Args) > 0 {
		b.WriteString(fmt.Sprintf("  args:     %s%s\n", strings.Join(cfg.Args, " "), src("args")))
	}

	if len(cfg.Volumes) > 0 {
		b.WriteString(fmt.Sprintf("  volumes:%s\n", src("volumes")))
		for _, v := range cfg.Volumes {
			b.WriteString(fmt.Sprintf("    %s\n", v))
		}
	}

	if len(cfg.Ports) > 0 {
		b.WriteString(fmt.Sprintf("  ports:%s\n", src("ports")))
		for _, p := range cfg.Ports {
			b.WriteString(fmt.Sprintf("    %s\n", p))
		}
	}

	if len(cfg.Packages) > 0 {
		b.WriteString(fmt.Sprintf("  packages:%s\n", src("packages")))
		for _, p := range cfg.Packages {
			b.WriteString(fmt.Sprintf("    - %s\n", p))
		}
	}

	b.WriteString(formatBoolFieldSrc("dind", cfg.DIND, false, src("dind")))
	b.WriteString(formatBoolFieldSrc("gpu", cfg.GPU, false, src("gpu")))
	b.WriteString(formatBoolFieldSrc("dind_gpu", cfg.DINDGpu, false, src("dind_gpu")))

	if cfg.NetworkScope != "" {
		b.WriteString(fmt.Sprintf("  network_scope: %s%s\n", cfg.NetworkScope, src("network_scope")))
	}
	if cfg.DINDHostname != "" {
		b.WriteString(fmt.Sprintf("  dind_hostname: %s%s\n", cfg.DINDHostname, src("dind_hostname")))
	}

	if cfg.Resources != nil {
		b.WriteString(fmt.Sprintf("  resources:%s\n", src("resources")))
		if cfg.Resources.Memory != "" {
			b.WriteString(fmt.Sprintf("    memory: %s\n", cfg.Resources.Memory))
		}
		if cfg.Resources.CPUs != "" {
			b.WriteString(fmt.Sprintf("    cpus:   %s\n", cfg.Resources.CPUs))
		}
	}
	if cfg.DINDResources != nil {
		b.WriteString(fmt.Sprintf("  dind_resources:%s\n", src("dind_resources")))
		if cfg.DINDResources.Memory != "" {
			b.WriteString(fmt.Sprintf("    memory: %s\n", cfg.DINDResources.Memory))
		}
		if cfg.DINDResources.CPUs != "" {
			b.WriteString(fmt.Sprintf("    cpus:   %s\n", cfg.DINDResources.CPUs))
		}
	}

	if len(cfg.DINDMirrors) > 0 {
		b.WriteString(fmt.Sprintf("  dind_mirrors:%s\n", src("dind_mirrors")))
		for _, m := range cfg.DINDMirrors {
			b.WriteString(fmt.Sprintf("    - %s\n", m))
		}
	}

	b.WriteString(formatBoolFieldSrc("dind_cache", cfg.DINDCache, false, src("dind_cache")))
	b.WriteString(formatBoolFieldSrc("dind_tls", cfg.DINDTLS, false, src("dind_tls")))
	b.WriteString(formatBoolFieldSrc("isolated", cfg.Isolated, true, src("isolated")))

	if len(cfg.AllowAgents) > 0 {
		b.WriteString(fmt.Sprintf("  allow_agents:%s\n", src("allow_agents")))
		for _, a := range cfg.AllowAgents {
			b.WriteString(fmt.Sprintf("    - %s\n", a))
		}
	}

	if len(cfg.MCPServers) > 0 {
		b.WriteString(fmt.Sprintf("  mcp_servers:%s\n", src("mcp_servers")))
		for name, srv := range cfg.MCPServers {
			b.WriteString(fmt.Sprintf("    %s: %s\n", name, srv.Command))
		}
	}

	if len(cfg.Tools) > 0 {
		b.WriteString(fmt.Sprintf("  tools:%s\n", src("tools")))
		for name, tool := range cfg.Tools {
			b.WriteString(fmt.Sprintf("    %s: (type=%s)\n", name, tool.Type))
		}
	}

	if cfg.NetworkRules != nil {
		b.WriteString(fmt.Sprintf("  network_rules:%s\n", src("network_rules")))
		if len(cfg.NetworkRules.AllowedHosts) > 0 {
			b.WriteString("    allowed_hosts:\n")
			for _, h := range cfg.NetworkRules.AllowedHosts {
				b.WriteString(fmt.Sprintf("      - %s\n", h))
			}
		}
		if len(cfg.NetworkRules.BlockedHosts) > 0 {
			b.WriteString("    blocked_hosts:\n")
			for _, h := range cfg.NetworkRules.BlockedHosts {
				b.WriteString(fmt.Sprintf("      - %s\n", h))
			}
		}
		if len(cfg.NetworkRules.AllowedPorts) > 0 {
			b.WriteString("    allowed_ports:\n")
			for _, p := range cfg.NetworkRules.AllowedPorts {
				b.WriteString(fmt.Sprintf("      - %s\n", p))
			}
		}
	}

	if cfg.SecurityProfile != "" {
		b.WriteString(fmt.Sprintf("  security_profile: %s%s\n", cfg.SecurityProfile, src("security_profile")))
	}

	if cfg.Git != nil && (cfg.Git.Name != "" || cfg.Git.Email != "") {
		b.WriteString(fmt.Sprintf("  git:%s\n", src("git")))
		if cfg.Git.Name != "" {
			b.WriteString(fmt.Sprintf("    name:  %s\n", cfg.Git.Name))
		}
		if cfg.Git.Email != "" {
			b.WriteString(fmt.Sprintf("    email: %s\n", cfg.Git.Email))
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
	ctx := context.Background()
	cli, err := docker.NewClientNoPing()
	if err != nil {
		b.WriteString(fmt.Sprintf("  Docker client:  %s (%s)\n", c.Red("FAIL"), err.Error()))
	} else {
		defer cli.Close()
		if _, err := cli.Ping(ctx); err != nil {
			b.WriteString(fmt.Sprintf("  Docker daemon:  %s (%s)\n", c.Red("FAIL"), err.Error()))
		} else {
			info, _ := cli.Info(ctx)
			b.WriteString(fmt.Sprintf("  Docker daemon:  %s (server %s)\n", c.Green("OK"), info.ServerVersion))
		}

		// Check default image
		_, _, imgErr := cli.ImageInspectWithRaw(ctx, container.DefaultImage)
		if imgErr != nil {
			b.WriteString(fmt.Sprintf("  Default image:  %s (%s) — will be pulled on first use\n", c.Yellow("NOT CACHED"), container.DefaultImage))
		} else {
			b.WriteString(fmt.Sprintf("  Default image:  %s (%s)\n", c.Green("OK"), container.DefaultImage))
		}
	}

	// Check storage root
	layout := storage.NewLayout(storage.DefaultRoot())
	b.WriteString(fmt.Sprintf("  Storage root:   %s\n", layout.Root))

	// Check config dir
	b.WriteString(fmt.Sprintf("  Config dir:     %s\n", layout.ConfigDir))

	// Image pinning status
	b.WriteString("\n  Image pinning:\n")
	b.WriteString(fmt.Sprintf("    agent image: %s (%s)\n", container.DefaultImage, imagePinLabel(container.DefaultImage, true)))
	b.WriteString(fmt.Sprintf("    dind image:  %s (%s)\n", dind.DefaultImage, imagePinLabel(dind.DefaultImage, true)))
	b.WriteString(fmt.Sprintf("    cache image: %s (%s)\n", dind.CacheImage, imagePinLabel(dind.CacheImage, true)))

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
	b.WriteString(fmt.Sprintf("Dry run for %s_%s:\n\n", agentName, profile))

	b.WriteString(fmt.Sprintf("  Image:     %s\n", cfg.GetImage()))
	b.WriteString(fmt.Sprintf("  Hostname:  %s\n", cfg.GetHostname()))

	if cfg.Version != "" {
		b.WriteString(fmt.Sprintf("  Version:   %s\n", cfg.Version))
	}

	if len(cfg.Env) > 0 {
		b.WriteString("  Env:\n")
		for k, v := range cfg.Env {
			b.WriteString(fmt.Sprintf("    %s=%s\n", k, v))
		}
	}

	if len(cfg.Volumes) > 0 {
		b.WriteString("  Volumes:\n")
		for _, v := range cfg.Volumes {
			b.WriteString(fmt.Sprintf("    %s\n", v))
		}
	}

	if len(cfg.Ports) > 0 {
		b.WriteString("  Ports:\n")
		for _, p := range cfg.Ports {
			b.WriteString(fmt.Sprintf("    %s\n", p))
		}
	}

	if len(cfg.Args) > 0 {
		b.WriteString(fmt.Sprintf("  Default args: %s\n", strings.Join(cfg.Args, " ")))
	}
	if len(args) > 0 {
		b.WriteString(fmt.Sprintf("  Passthrough:  %s\n", strings.Join(args, " ")))
	}

	b.WriteString(formatEnabledField("DIND", cfg.DIND))
	b.WriteString(formatEnabledField("GPU", cfg.GPU))
	b.WriteString(formatEnabledField("DIND GPU", cfg.DINDGpu))

	if cfg.Resources != nil {
		b.WriteString("  Resources:\n")
		if cfg.Resources.Memory != "" {
			b.WriteString(fmt.Sprintf("    memory: %s\n", cfg.Resources.Memory))
		}
		if cfg.Resources.CPUs != "" {
			b.WriteString(fmt.Sprintf("    cpus:   %s\n", cfg.Resources.CPUs))
		}
	}
	if cfg.DINDResources != nil {
		b.WriteString("  DIND Resources:\n")
		if cfg.DINDResources.Memory != "" {
			b.WriteString(fmt.Sprintf("    memory: %s\n", cfg.DINDResources.Memory))
		}
		if cfg.DINDResources.CPUs != "" {
			b.WriteString(fmt.Sprintf("    cpus:   %s\n", cfg.DINDResources.CPUs))
		}
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
	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return CleanupResult{}, err
	}
	defer cli.Close()

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
			if err := cli.NetworkRemove(ctx, n.ID); err == nil {
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
			if err := cli.VolumeRemove(ctx, v.Name, true); err == nil {
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
	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return "", err
	}
	defer cli.Close()

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
	b.WriteString(fmt.Sprintf("  %-35s %-15s %-10s %-25s %s\n", "NAME", "AGENT", "PROFILE", "IMAGE", "STATUS"))
	b.WriteString(fmt.Sprintf("  %-35s %-15s %-10s %-25s %s\n", "----", "-----", "-------", "-----", "------"))

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
		b.WriteString(fmt.Sprintf("  %-35s %-15s %-10s %-25s %s\n", name, agentLabel, profile, image, status))
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
			b.WriteString(fmt.Sprintf("  %-12s  (not found)\n", dir.name))
			continue
		}
		total += size
		b.WriteString(fmt.Sprintf("  %-12s  %s\n", dir.name, formatBytes(size)))
	}
	b.WriteString(fmt.Sprintf("\n  %-12s  %s\n", "Total", formatBytes(total)))

	// Per-profile breakdown
	profilesDir := filepath.Join(layout.Root, "profiles")
	entries, _ := os.ReadDir(profilesDir)
	if len(entries) > 0 {
		b.WriteString("\nPer-profile:\n")
		for _, e := range entries {
			if e.IsDir() {
				size, _ := dirSize(filepath.Join(profilesDir, e.Name()))
				b.WriteString(fmt.Sprintf("  %-20s  %s\n", e.Name(), formatBytes(size)))
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

// formatBoolFieldSrc is like formatBoolField but appends a source annotation.
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
	return fmt.Sprintf("  %-15s false (default)\n", name+":")
}

// formatBoolField formats a *bool config field with default annotation for ShowConfig output.
func formatBoolField(name string, val *bool, defaultVal bool) string {
	if val != nil {
		if *val {
			return fmt.Sprintf("  %-15s true\n", name+":")
		}
		return fmt.Sprintf("  %-15s false\n", name+":")
	}
	if defaultVal {
		return fmt.Sprintf("  %-15s true (default)\n", name+":")
	}
	return fmt.Sprintf("  %-15s false (default)\n", name+":")
}

// formatEnabledField formats a *bool config field as enabled/disabled for DryRun output.
func formatEnabledField(name string, val *bool) string {
	enabled := "disabled"
	if val != nil && *val {
		enabled = "enabled"
	}
	return fmt.Sprintf("  %-12s %s\n", name+":", enabled)
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

		b.WriteString(fmt.Sprintf("  %-15s  %s\n", name, status))
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
