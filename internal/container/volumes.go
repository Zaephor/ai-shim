package container

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Zaephor/ai-shim/internal/security"
)

// Volume is a validated user-configured bind mount.
type Volume struct {
	Source   string
	Target   string
	ReadOnly bool
}

// ParseVolume parses and validates a "source:target[:mode]" volume entry.
// Mode is "ro" or "rw"; when omitted the mount is writable.
func ParseVolume(vol string) (Volume, error) {
	parts := strings.Split(vol, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return Volume{}, fmt.Errorf("volume %q: expected source:target or source:target:ro|rw", vol)
	}
	var v Volume
	if len(parts) == 3 {
		switch parts[2] {
		case "ro":
			v.ReadOnly = true
		case "rw":
		default:
			return Volume{}, fmt.Errorf("volume %q: unknown mode %q (expected ro or rw)", vol, parts[2])
		}
	}
	if parts[0] == "" || parts[1] == "" {
		return Volume{}, fmt.Errorf("volume %q: source and target must not be empty", vol)
	}
	if err := security.ValidateVolumePath(parts[0]); err != nil {
		return Volume{}, fmt.Errorf("volume %q: %w", vol, err)
	}
	// Validate target path — reject traversal attempts.
	target := filepath.Clean(parts[1])
	if !filepath.IsAbs(target) || strings.Contains(target, "..") {
		return Volume{}, fmt.Errorf("volume %q: target must be absolute path without traversal", vol)
	}
	v.Source = parts[0]
	v.Target = target
	return v, nil
}

// ResolveVolumes parses the configured volume list. Invalid entries are
// returned as errors and left out. When several entries share a target,
// the last one wins (later config tiers override earlier ones) and each
// dropped entry is described in overridden.
func ResolveVolumes(vols []string) (resolved []Volume, errs []error, overridden []string) {
	type entry struct {
		raw string
		vol Volume
	}
	var parsed []entry
	last := map[string]int{}
	for _, raw := range vols {
		v, err := ParseVolume(raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		last[v.Target] = len(parsed)
		parsed = append(parsed, entry{raw: raw, vol: v})
	}
	for i, e := range parsed {
		if j := last[e.vol.Target]; j != i {
			overridden = append(overridden, fmt.Sprintf("volume %q overridden by %q (same target)", e.raw, parsed[j].raw))
			continue
		}
		resolved = append(resolved, e.vol)
	}
	return resolved, errs, overridden
}
