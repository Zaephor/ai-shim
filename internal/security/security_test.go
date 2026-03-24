package security

import (
	"testing"
)

// --- Secret Masking Tests ---

func TestMaskSecrets_KeyPatterns(t *testing.T) {
	env := map[string]string{
		"API_KEY":          "some-value",
		"SECRET_TOKEN":     "another-value",
		"MY_PASSWORD":      "pass123",
		"AWS_CREDENTIAL":   "cred-abc",
		"AUTH_HEADER":      "Bearer xyz",
		"DATABASE_SECRET":  "db-secret",
		"NORMAL_VAR":       "safe-value",
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
		"HOME":     "/home/user",
		"PATH":     "/usr/bin:/usr/local/bin",
		"LANG":     "en_US.UTF-8",
		"EDITOR":   "vim",
		"TERM":     "xterm-256color",
	}

	masked := MaskSecrets(env)

	for key, val := range env {
		if masked[key] != val {
			t.Errorf("expected %s to pass through as %q, got %q", key, val, masked[key])
		}
	}
}
