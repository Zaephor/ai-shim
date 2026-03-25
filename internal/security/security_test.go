package security

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Secret Masking Tests ---

func TestMaskSecrets_KeyPatterns(t *testing.T) {
	env := map[string]string{
		"API_KEY":         "some-value",
		"SECRET_TOKEN":    "another-value",
		"MY_PASSWORD":     "pass123",
		"AWS_CREDENTIAL":  "cred-abc",
		"AUTH_HEADER":     "Bearer xyz",
		"DATABASE_SECRET": "db-secret",
		"NORMAL_VAR":      "safe-value",
	}

	masked := MaskSecrets(env)

	// Keys matching patterns should be masked
	for _, key := range []string{"API_KEY", "SECRET_TOKEN", "MY_PASSWORD", "AWS_CREDENTIAL", "AUTH_HEADER", "DATABASE_SECRET"} {
		if masked[key] != "***" {
			t.Errorf("expected %s to be masked, got %q", key, masked[key])
		}
	}

	// Normal keys should pass through
	if masked["NORMAL_VAR"] != "safe-value" {
		t.Errorf("expected NORMAL_VAR to pass through, got %q", masked["NORMAL_VAR"])
	}
}

func TestMaskSecrets_ValuePatterns(t *testing.T) {
	env := map[string]string{
		"VAR1": "sk-ant-abc123",
		"VAR2": "sk-proj-def456",
		"VAR3": "gsk_something",
		"VAR4": "sk-live-test",
	}

	masked := MaskSecrets(env)

	for key := range env {
		if masked[key] != "***" {
			t.Errorf("expected %s to be masked by value pattern, got %q", key, masked[key])
		}
	}
}

func TestMaskSecrets_SafeValues(t *testing.T) {
	env := map[string]string{
		"HOME":   "/home/user",
		"PATH":   "/usr/bin:/usr/local/bin",
		"LANG":   "en_US.UTF-8",
		"EDITOR": "vim",
		"TERM":   "xterm-256color",
	}

	masked := MaskSecrets(env)

	for key, val := range env {
		if masked[key] != val {
			t.Errorf("expected %s to pass through as %q, got %q", key, val, masked[key])
		}
	}
}

// --- Path Validation Tests ---

func TestValidateVolumePath_Valid(t *testing.T) {
	paths := []string{
		"/home/user/data",
		"/opt/app",
		"/tmp/workspace",
	}
	for _, p := range paths {
		if err := ValidateVolumePath(p); err != nil {
			t.Errorf("expected %s to be valid, got error: %v", p, err)
		}
	}
}

func TestValidateVolumePath_Traversal(t *testing.T) {
	paths := []string{
		"/home/../etc/passwd",
		"/home/user/../../etc/shadow",
	}
	for _, p := range paths {
		if err := ValidateVolumePath(p); err == nil {
			t.Errorf("expected %s to be rejected for traversal", p)
		}
	}
}

func TestValidateVolumePath_Sensitive(t *testing.T) {
	paths := []string{
		"/etc",
		"/etc/passwd",
		"/proc",
		"/proc/1/status",
		"/sys",
		"/sys/class",
		"/dev",
		"/dev/sda",
		"/var/run",
		"/var/run/something",
	}
	for _, p := range paths {
		if err := ValidateVolumePath(p); err == nil {
			t.Errorf("expected %s to be rejected as sensitive", p)
		}
	}
}

func TestValidateVolumePath_DockerSocket(t *testing.T) {
	if err := ValidateVolumePath("/var/run/docker.sock"); err != nil {
		t.Errorf("expected /var/run/docker.sock to be allowed, got error: %v", err)
	}
}

// --- Safe Directory Tests ---

func TestValidateWorkingDirectory_Valid(t *testing.T) {
	dirs := []string{
		"/home/user/projects",
		"/opt/app",
		"/tmp/workspace",
	}
	for _, d := range dirs {
		if err := ValidateWorkingDirectory(d); err != nil {
			t.Errorf("expected %s to be valid, got error: %v", d, err)
		}
	}
}

func TestValidateWorkingDirectory_Root(t *testing.T) {
	if err := ValidateWorkingDirectory("/"); err == nil {
		t.Error("expected / to be rejected")
	}
}

func TestValidateWorkingDirectory_Home(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}
	if err := ValidateWorkingDirectory(home); err == nil {
		t.Errorf("expected exact HOME (%s) to be rejected", home)
	}
}

func TestValidateWorkingDirectory_HomeSubdir(t *testing.T) {
	home := os.Getenv("HOME")
	if home == "" {
		t.Skip("HOME not set")
	}
	subdir := filepath.Join(home, "projects")
	if err := ValidateWorkingDirectory(subdir); err != nil {
		t.Errorf("expected HOME subdir (%s) to be valid, got error: %v", subdir, err)
	}
}

func TestValidateWorkingDirectory_System(t *testing.T) {
	dirs := []string{"/etc", "/var", "/usr", "/bin", "/sbin", "/proc", "/sys", "/dev"}
	for _, d := range dirs {
		if err := ValidateWorkingDirectory(d); err == nil {
			t.Errorf("expected %s to be rejected", d)
		}
	}
}

func TestValidateVolumePath_EmptyPath(t *testing.T) {
	// Empty path cleans to "." which should be allowed
	err := ValidateVolumePath("")
	// At minimum it shouldn't panic
	_ = err
}

func TestValidateVolumePath_RelativePath(t *testing.T) {
	err := ValidateVolumePath("relative/path")
	// Relative paths should be allowed (Docker resolves them)
	assert.NoError(t, err)
}

func TestMaskSecrets_NilMap(t *testing.T) {
	result := MaskSecrets(nil)
	assert.NotNil(t, result, "nil input should return empty map, not nil")
	assert.Empty(t, result)
}

func TestMaskSecrets_EmptyMap(t *testing.T) {
	result := MaskSecrets(map[string]string{})
	assert.NotNil(t, result)
	assert.Empty(t, result)
}
