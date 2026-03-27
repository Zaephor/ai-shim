package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ai-shim/ai-shim/internal/storage"
)

var validProfileName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// isValidProfileName returns true if the profile name contains only
// alphanumeric characters, hyphens, and underscores.
func isValidProfileName(name string) bool {
	return validProfileName.MatchString(name)
}

const currentProfileFile = ".current-profile"

// SwitchProfile writes the given profile name as the current default profile.
func SwitchProfile(layout storage.Layout, profile string) error {
	if profile == "" {
		return fmt.Errorf("profile name cannot be empty")
	}

	// Validate profile name: allowlist of safe characters only
	if !isValidProfileName(profile) {
		return fmt.Errorf("invalid profile name: %q (only alphanumeric, hyphens, and underscores allowed)", profile)
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
