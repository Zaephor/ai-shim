package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	// IntervalAlways means reinstall on every launch.
	IntervalAlways = 0
	// IntervalNever means never auto-reinstall.
	IntervalNever = -1
	// IntervalDefault is 1 day (the default when update_interval is omitted).
	IntervalDefault = 86400
)

// ParseUpdateInterval parses an update interval string into seconds.
// Accepts: "always", "never", Go durations ("24h", "1h30m"), and day
// shorthand ("1d", "7d"). Returns seconds, or an error if unparseable.
// Empty string returns IntervalDefault (1 day).
func ParseUpdateInterval(s string) (int64, error) {
	s = strings.TrimSpace(s)
	switch s {
	case "":
		return IntervalDefault, nil
	case "always":
		return IntervalAlways, nil
	case "never":
		return IntervalNever, nil
	}

	// Handle day shorthand: "1d", "7d", "30d"
	if strings.HasSuffix(s, "d") {
		numStr := strings.TrimSuffix(s, "d")
		days, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid day interval %q: %w", s, err)
		}
		return int64(days * 86400), nil
	}

	// Fall through to Go duration parser ("24h", "1h30m", "30m")
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid update interval %q: %w", s, err)
	}
	return int64(d.Seconds()), nil
}
