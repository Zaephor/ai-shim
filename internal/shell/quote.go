package shell

import "strings"

// Quote returns a shell-safe representation of s.
// If s contains no special characters, it is returned as-is.
// Otherwise, it is wrapped in single quotes with internal single quotes escaped.
func Quote(s string) string {
	if strings.ContainsRune(s, 0) {
		return "''"
	}
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
