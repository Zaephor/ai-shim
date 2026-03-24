// Package security provides secret masking, path validation, and safe directory checks.
package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// sensitiveKeyPatterns lists substrings that mark an env var name as sensitive.
var sensitiveKeyPatterns = []string{
	"KEY",
	"SECRET",
	"TOKEN",
	"PASSWORD",
	"CREDENTIAL",
	"AUTH",
}

// sensitiveValuePrefixes lists prefixes that indicate a secret API key value.
var sensitiveValuePrefixes = []string{
	"sk-ant-",
	"sk-",
	"gsk_",
}

// MaskSecrets replaces sensitive values in a key=value map with "***".
// Masks based on key name patterns (*KEY*, *SECRET*, *TOKEN*, *PASSWORD*, *CREDENTIAL*, *AUTH*)
// and value patterns (sk-ant-*, sk-*, gsk_*, etc.)
func MaskSecrets(env map[string]string) map[string]string {
	result := make(map[string]string, len(env))
	for k, v := range env {
		if isSensitiveKey(k) || isSensitiveValue(v) {
			result[k] = "***"
		} else {
			result[k] = v
		}
	}
	return result
}

func isSensitiveKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, pattern := range sensitiveKeyPatterns {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}

func isSensitiveValue(value string) bool {
	for _, prefix := range sensitiveValuePrefixes {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}

// blockedVolumePaths lists host paths that must not be volume-mounted.
var blockedVolumePaths = []string{
	"/etc",
	"/proc",
	"/sys",
	"/dev",
	"/var/run",
}

// ValidateVolumePath checks that a volume mount source is safe.
// Returns error if path contains traversal or targets sensitive directories.
func ValidateVolumePath(path string) error {
	cleaned := filepath.Clean(path)

	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("path traversal not allowed: %s", path)
	}

	// Special exception: /var/run/docker.sock is allowed.
	if cleaned == "/var/run/docker.sock" {
		return nil
	}

	for _, blocked := range blockedVolumePaths {
		if cleaned == blocked || strings.HasPrefix(cleaned, blocked+"/") {
			return fmt.Errorf("mounting sensitive path not allowed: %s", cleaned)
		}
	}

	return nil
}

// blockedWorkingDirs lists directories that are too dangerous to mount as a working directory.
var blockedWorkingDirs = []string{
	"/",
	"/etc",
	"/var",
	"/usr",
	"/bin",
	"/sbin",
	"/proc",
	"/sys",
	"/dev",
}

// ValidateWorkingDirectory checks that the current directory is safe to mount.
// Returns error for dangerous directories like /, $HOME root, /etc.
func ValidateWorkingDirectory(dir string) error {
	cleaned := filepath.Clean(dir)

	for _, blocked := range blockedWorkingDirs {
		if cleaned == blocked {
			return fmt.Errorf("refusing to run from dangerous directory: %s", cleaned)
		}
	}

	// Block exact $HOME but allow subdirectories.
	home := os.Getenv("HOME")
	if home != "" {
		home = filepath.Clean(home)
		if cleaned == home {
			return fmt.Errorf("refusing to run from home directory root: %s", cleaned)
		}
	}

	return nil
}
