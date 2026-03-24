package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/storage"
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
