package parse

import (
	"fmt"
	"strconv"
	"strings"
)

// Memory parses a human-readable memory string (e.g. "512m", "2g", "4g") into bytes.
// Supported suffixes: k (KiB), m (MiB), g (GiB). No suffix = bytes.
func Memory(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty memory value")
	}
	var multiplier int64 = 1
	if strings.HasSuffix(s, "g") {
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "m") {
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	} else if strings.HasSuffix(s, "k") {
		multiplier = 1024
		s = s[:len(s)-1]
	}
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid memory value %q: %w", s, err)
	}
	if val < 0 {
		return 0, fmt.Errorf("memory value must be positive: %s", s)
	}
	return int64(val * float64(multiplier)), nil
}
