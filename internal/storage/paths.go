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

func NewLayout(root string) Layout {
	return Layout{
		Root:        root,
		ConfigDir:   filepath.Join(root, "config"),
		SharedBin:   filepath.Join(root, "shared", "bin"),
		SharedCache: filepath.Join(root, "shared", "cache"),
	}
}

func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-shim")
}

func (l Layout) AgentBin(agent string) string {
	return filepath.Join(l.Root, "agents", agent, "bin")
}

func (l Layout) AgentCache(agent string) string {
	return filepath.Join(l.Root, "agents", agent, "cache")
}

func (l Layout) ProfileHome(profile string) string {
	return filepath.Join(l.Root, "profiles", profile, "home")
}

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
