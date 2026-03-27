package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-shim/ai-shim/internal/storage"
)

const currentProfileFile = ".current-profile"

// SwitchProfile writes the given profile name as the current default profile.
func SwitchProfile(layout storage.Layout, profile string) error {
	if profile == "" {
		return fmt.Errorf("profile name cannot be empty")
	}

	// Validate profile name: no slashes, no path traversal
	if strings.Contains(profile, "/") || strings.Contains(profile, "\\") || profile == "." || profile == ".." {
		return fmt.Errorf("invalid profile name: %q", profile)
	}

	markerPath := filepath.Join(layout.ConfigDir, currentProfileFile)

	// Ensure config dir exists
	if err := os.MkdirAll(layout.ConfigDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	if err := os.WriteFile(markerPath, []byte(profile+"\n"), 0644); err != nil {
		return fmt.Errorf("writing current profile: %w", err)
	}

	return nil
}

// CurrentProfile reads the current default profile from the marker file.
// Returns "default" if the marker file does not exist or is empty.
func CurrentProfile(layout storage.Layout) string {
	markerPath := filepath.Join(layout.ConfigDir, currentProfileFile)

	data, err := os.ReadFile(markerPath)
	if err != nil {
		return "default"
	}

	profile := strings.TrimSpace(string(data))
	if profile == "" {
		return "default"
	}

	return profile
}
