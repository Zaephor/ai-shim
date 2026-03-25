package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/storage"
)

// IsFirstRun checks if ai-shim has been set up.
func IsFirstRun(layout storage.Layout) bool {
	_, err := os.Stat(layout.ConfigDir)
	return os.IsNotExist(err)
}

// Init creates the initial ai-shim directory structure and example configs.
func Init(layout storage.Layout) error {
	// Create all config directories
	dirs := []string{
		layout.ConfigDir,
		filepath.Join(layout.ConfigDir, "agents"),
		filepath.Join(layout.ConfigDir, "profiles"),
		filepath.Join(layout.ConfigDir, "agent-profiles"),
		layout.SharedBin,
		layout.SharedCache,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory %s: %w", dir, err)
		}
	}

	// Write default config
	defaultConfig := `# ai-shim default configuration
# See: https://github.com/ai-shim/ai-shim

# Container image (default: ghcr.io/catthehacker/ubuntu:act-24.04)
# image: "ghcr.io/catthehacker/ubuntu:act-24.04"

# Container hostname (default: ai-shim)
# hostname: "ai-shim"

# Environment variables injected into the container
# env:
#   MY_API_KEY: "your-key-here"

# Template variables (not injected, used for templating)
# variables:
#   llm_host: "my-host:8080"
`
	configPath := filepath.Join(layout.ConfigDir, "default.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := os.WriteFile(configPath, []byte(defaultConfig), 0644); err != nil {
			return fmt.Errorf("writing default config: %w", err)
		}
	}

	// Seed example agent config
	agentExample := filepath.Join(layout.ConfigDir, "agents", "claude-code.yaml")
	if _, err := os.Stat(agentExample); os.IsNotExist(err) {
		os.WriteFile(agentExample, []byte(`# Claude Code agent configuration
# Uncomment and customize as needed
# env:
#   ANTHROPIC_API_KEY: "your-key-here"
# args:
#   - "--no-telemetry"
`), 0644)
	}

	// Seed example profile config
	profileExample := filepath.Join(layout.ConfigDir, "profiles", "work.yaml")
	if _, err := os.Stat(profileExample); os.IsNotExist(err) {
		os.WriteFile(profileExample, []byte(`# Work profile configuration
# This profile's home directory is mounted as the container's home
# Uncomment and customize as needed
# env:
#   EDITOR: "vim"
# volumes:
#   - "/host/path:/container/path"
`), 0644)
	}

	return nil
}

// PrintFirstRunHelp prints setup instructions for new users.
func PrintFirstRunHelp(layout storage.Layout) {
	fmt.Fprintf(os.Stderr, `ai-shim: first run detected

To get started:
  1. Initialize:     ai-shim init
  2. Create symlink:  ai-shim manage symlinks create claude-code work
  3. Launch:          ./claude-code_work

Configuration:  %s
Storage:        %s

Available agents:
`, layout.ConfigDir, layout.Root)

	for _, name := range agent.Names() {
		def, _ := agent.Lookup(name)
		fmt.Fprintf(os.Stderr, "  %-15s %s\n", name, def.Binary)
	}
	fmt.Fprintln(os.Stderr)
}
