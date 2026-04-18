package parse

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Memory parses a human-readable memory string (e.g. "512m", "2g", "4g") into bytes.
// Supported suffixes: k (KiB), m (MiB), g (GiB). No suffix = bytes.
func Memory(s string) (int64, error) {
	original := s
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
	result := val * float64(multiplier)
	if result > float64(math.MaxInt64) {
		return 0, fmt.Errorf("memory value %q overflows int64", original)
	}
	return int64(result), nil
}
