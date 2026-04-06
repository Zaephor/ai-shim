package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"github.com/ai-shim/ai-shim/internal/invocation"
	"github.com/ai-shim/ai-shim/internal/storage"
)

// switchProfileSeq disambiguates temp marker filenames across concurrent
// in-process callers (which would otherwise share the same PID).
var switchProfileSeq uint64

const currentProfileFile = ".current-profile"

// SwitchProfile writes the given profile name as the current default profile.
// The profile name must satisfy invocation.ValidateProfileName.
func SwitchProfile(layout storage.Layout, profile string) error {
	if err := invocation.ValidateProfileName(profile); err != nil {
		return err
	}

	markerPath := filepath.Join(layout.ConfigDir, currentProfileFile)

	// Ensure config dir exists
	if err := os.MkdirAll(layout.ConfigDir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	// Atomic write: stage the new contents in a per-pid temp file inside the
	// same directory, then rename(2) onto the marker. POSIX rename is atomic
	// and guarantees that concurrent readers always observe either the old
	// or the new file — never a truncated/partially written one. Two
	// concurrent SwitchProfile callers thus end with one well-formed marker
	// (last writer wins), with no possibility of corruption.
	seq := atomic.AddUint64(&switchProfileSeq, 1)
	tmpPath := fmt.Sprintf("%s.tmp.%d.%d", markerPath, os.Getpid(), seq)
	if err := os.WriteFile(tmpPath, []byte(profile+"\n"), 0644); err != nil {
		return fmt.Errorf("writing current profile: %w", err)
	}
	if err := os.Rename(tmpPath, markerPath); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming current profile marker: %w", err)
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
