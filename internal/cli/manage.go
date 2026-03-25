package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/storage"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
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
	cfg, err := config.Resolve(layout.ConfigDir, agentName, profile)
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Resolved config for %s_%s:\n\n", agentName, profile))

	if cfg.Image != "" {
		b.WriteString(fmt.Sprintf("  image:    %s\n", cfg.Image))
	}
	if cfg.Hostname != "" {
		b.WriteString(fmt.Sprintf("  hostname: %s\n", cfg.Hostname))
	}
	if cfg.Version != "" {
		b.WriteString(fmt.Sprintf("  version:  %s\n", cfg.Version))
	}

	if len(cfg.Env) > 0 {
		b.WriteString("  env:\n")
		for k, v := range cfg.Env {
			b.WriteString(fmt.Sprintf("    %s=%s\n", k, v))
		}
	}

	if len(cfg.Args) > 0 {
		b.WriteString(fmt.Sprintf("  args:     %s\n", strings.Join(cfg.Args, " ")))
	}

	if len(cfg.Volumes) > 0 {
		b.WriteString("  volumes:\n")
		for _, v := range cfg.Volumes {
			b.WriteString(fmt.Sprintf("    %s\n", v))
		}
	}

	if len(cfg.Ports) > 0 {
		b.WriteString("  ports:\n")
		for _, p := range cfg.Ports {
			b.WriteString(fmt.Sprintf("    %s\n", p))
		}
	}

	return b.String(), nil
}

// Doctor runs diagnostic checks and returns results.
func Doctor() string {
	var b strings.Builder
	b.WriteString("ai-shim doctor\n\n")

	// Check Docker
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		b.WriteString("  Docker client:  FAIL (" + err.Error() + ")\n")
	} else {
		defer cli.Close()
		if _, err := cli.Ping(ctx); err != nil {
			b.WriteString("  Docker daemon:  FAIL (" + err.Error() + ")\n")
		} else {
			info, _ := cli.Info(ctx)
			b.WriteString(fmt.Sprintf("  Docker daemon:  OK (server %s)\n", info.ServerVersion))
		}
	}

	// Check storage root
	layout := storage.NewLayout(storage.DefaultRoot())
	b.WriteString(fmt.Sprintf("  Storage root:   %s\n", layout.Root))

	// Check config dir
	b.WriteString(fmt.Sprintf("  Config dir:     %s\n", layout.ConfigDir))

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

	image := cfg.Image
	if image == "" {
		image = container.DefaultImage
	}
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = container.DefaultHostname
	}

	b.WriteString(fmt.Sprintf("  Image:     %s\n", image))
	b.WriteString(fmt.Sprintf("  Hostname:  %s\n", hostname))

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

	dind := "disabled"
	if cfg.DIND != nil && *cfg.DIND {
		dind = "enabled"
	}
	b.WriteString(fmt.Sprintf("  DIND:      %s\n", dind))

	gpu := "disabled"
	if cfg.GPU != nil && *cfg.GPU {
		gpu = "enabled"
	}
	b.WriteString(fmt.Sprintf("  GPU:       %s\n", gpu))

	return b.String(), nil
}

// CleanupResult holds the results of a cleanup operation.
type CleanupResult struct {
	Removed []string
	Failed  []string
}

// Cleanup finds and removes orphaned ai-shim containers.
func Cleanup() (CleanupResult, error) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return CleanupResult{}, fmt.Errorf("connecting to Docker: %w", err)
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "ai-shim=true")),
	})
	if err != nil {
		return CleanupResult{}, fmt.Errorf("listing containers: %w", err)
	}

	var removed []string
	var failed []string
	for _, c := range containers {
		if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
			name := c.ID[:12]
			if len(c.Names) > 0 {
				name = c.Names[0]
			}
			failed = append(failed, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		name := c.ID[:12]
		if len(c.Names) > 0 {
			name = c.Names[0]
		}
		removed = append(removed, name)
	}

	return CleanupResult{Removed: removed, Failed: failed}, nil
}

// Status returns a formatted list of running ai-shim containers.
func Status() (string, error) {
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return "", fmt.Errorf("connecting to docker: %w", err)
	}
	defer cli.Close()

	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "ai-shim=true")),
	})
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	if len(containers) == 0 {
		return "No running ai-shim containers.\n", nil
	}

	var b strings.Builder
	b.WriteString("Running ai-shim containers:\n\n")
	b.WriteString(fmt.Sprintf("  %-40s %-15s %-10s %s\n", "NAME", "AGENT", "PROFILE", "STATUS"))
	b.WriteString(fmt.Sprintf("  %-40s %-15s %-10s %s\n", "----", "-----", "-------", "------"))

	for _, c := range containers {
		name := c.ID[:12]
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		agent := c.Labels["ai-shim.agent"]
		profile := c.Labels["ai-shim.profile"]
		status := c.Status

		b.WriteString(fmt.Sprintf("  %-40s %-15s %-10s %s\n", name, agent, profile, status))
	}
	return b.String(), nil
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
