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
// Falls back to the current working directory if HOME is not set.
func DefaultRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// Fallback to current directory
		if pwd, err := os.Getwd(); err == nil {
			return filepath.Join(pwd, ".ai-shim")
		}
		return ".ai-shim"
	}
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

// ToolCachePath returns the host path for a tool's persistent cache directory.
// The cacheScope controls where the directory lives:
//   - "" or "global": ~/.ai-shim/shared/cache/{tool-name}/
//   - "profile":      ~/.ai-shim/profiles/{profile}/cache/{tool-name}/
//   - "agent":        ~/.ai-shim/agents/{agent}/cache/{tool-name}/
func ToolCachePath(layout Layout, toolName, cacheScope, agent, profile string) string {
	switch cacheScope {
	case "profile":
		return filepath.Join(layout.Root, "profiles", profile, "cache", toolName)
	case "agent":
		return filepath.Join(layout.Root, "agents", agent, "cache", toolName)
	default: // "" or "global"
		return filepath.Join(layout.SharedCache, toolName)
	}
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

// EnsureAgentData pre-creates agent data directories and files under the profile
// home directory. This ensures correct ownership (host user, not root) before
// Docker bind-mounts them. dataDirs are created as directories, dataFiles are
// created as empty files (with parent directories).
func (l Layout) EnsureAgentData(profile string, dataDirs, dataFiles []string) error {
	home := l.ProfileHome(profile)
	for _, dir := range dataDirs {
		if err := os.MkdirAll(filepath.Join(home, dir), 0755); err != nil {
			return err
		}
	}
	for _, file := range dataFiles {
		path := filepath.Join(home, file)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
		// Create empty file if it doesn't exist, preserve existing content
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.WriteFile(path, nil, 0644); err != nil {
				return err
			}
		}
	}
	return nil
}
