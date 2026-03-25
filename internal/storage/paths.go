package storage

import (
	"os"
	"path/filepath"
)

// Layout holds all resolved paths for the ai-shim storage hierarchy.
type Layout struct {
	Root        string
	ConfigDir   string
	SharedBin   string
	SharedCache string
}

// NewLayout creates a Layout with all paths derived from the given root directory.
func NewLayout(root string) Layout {
	return Layout{
		Root:        root,
		ConfigDir:   filepath.Join(root, "config"),
		SharedBin:   filepath.Join(root, "shared", "bin"),
		SharedCache: filepath.Join(root, "shared", "cache"),
	}
}

// DefaultRoot returns the default ai-shim storage root (~/.ai-shim).
func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-shim")
}

// AgentBin returns the bin directory path for the given agent.
func (l Layout) AgentBin(agent string) string {
	return filepath.Join(l.Root, "agents", agent, "bin")
}

// AgentCache returns the cache directory path for the given agent.
func (l Layout) AgentCache(agent string) string {
	return filepath.Join(l.Root, "agents", agent, "cache")
}

// ProfileHome returns the home directory path for the given profile.
func (l Layout) ProfileHome(profile string) string {
	return filepath.Join(l.Root, "profiles", profile, "home")
}

// EnsureDirectories creates all required directories for the given agent and profile.
func (l Layout) EnsureDirectories(agent, profile string) error {
	dirs := []string{
		l.ConfigDir,
		filepath.Join(l.ConfigDir, "agents"),
		filepath.Join(l.ConfigDir, "profiles"),
		filepath.Join(l.ConfigDir, "agent-profiles"),
		l.SharedBin,
		l.SharedCache,
		l.AgentBin(agent),
		l.AgentCache(agent),
		l.ProfileHome(profile),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
