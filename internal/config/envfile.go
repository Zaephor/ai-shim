package config

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ParseEnvFile reads a dotenv-style file and returns a map of key-value pairs.
// Format: one KEY=VALUE per line; # comments and blank lines are skipped.
// Lines may optionally start with "export ". Surrounding double or single
// quotes on values are stripped. No multiline values or variable interpolation.
func ParseEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("env_file: %w", err)
	}
	defer func() { _ = f.Close() }()

	result := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines and comments.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Strip optional "export " prefix.
		line = strings.TrimPrefix(line, "export ")

		// Split on first '='.
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue // malformed line, skip
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		// Strip matching surrounding quotes.
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') ||
				(value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		result[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("env_file: reading %s: %w", path, err)
	}
	return result, nil
}
