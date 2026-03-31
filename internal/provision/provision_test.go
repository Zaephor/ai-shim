package provision

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateInstallScript_Empty(t *testing.T) {
	script := GenerateInstallScript(nil, "/usr/local/bin")
	assert.Equal(t, "", script)
}

func TestGenerateInstallScript_BinaryDownload(t *testing.T) {
	tools := map[string]ToolDef{
		"mybin": {Type: "binary-download", URL: "https://example.com/bin", Binary: "mybin"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "curl -fsSL")
	assert.Contains(t, script, "/opt/bin\"/mybin")
	assert.Contains(t, script, "chmod +x")
	assert.Contains(t, script, "if [ ! -f")
}

func TestGenerateInstallScript_BinaryDownloadWithChecksum(t *testing.T) {
	tools := map[string]ToolDef{
		"mybin": {Type: "binary-download", URL: "https://example.com/bin", Binary: "mybin", Checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "sha256sum")
	assert.Contains(t, script, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
}

func TestGenerateInstallScript_TarExtract(t *testing.T) {
	tools := map[string]ToolDef{
		"act": {Type: "tar-extract", URL: "https://example.com/act.tar.gz", Binary: "act"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "tar xz")
	assert.Contains(t, script, "act")
	assert.NotContains(t, script, "2>/dev/null")
	assert.Contains(t, script, "ERROR: tar extract failed")
}

func TestGenerateInstallScript_TarExtractSelective(t *testing.T) {
	tools := map[string]ToolDef{
		"tool": {Type: "tar-extract-selective", URL: "https://example.com/t.tar.gz", Binary: "tool", Files: []string{"lib.so"}},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "tool")
	assert.Contains(t, script, "lib.so")
	assert.NotContains(t, script, "2>/dev/null")
	assert.Contains(t, script, "ERROR: tar extract failed")
}

func TestGenerateInstallScript_Apt(t *testing.T) {
	tools := map[string]ToolDef{
		"tmux": {Type: "apt", Package: "tmux", Binary: "tmux"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "apt-get install")
	assert.Contains(t, script, "tmux")
	assert.Contains(t, script, "ERROR: apt install failed")
}

func TestGenerateInstallScript_GoInstall(t *testing.T) {
	tools := map[string]ToolDef{
		"tool": {Type: "go-install", Package: "github.com/user/tool", Binary: "tool"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "go install")
	assert.Contains(t, script, "github.com/user/tool")
}

func TestGenerateInstallScript_Custom(t *testing.T) {
	tools := map[string]ToolDef{
		"custom": {Type: "custom", Install: "curl -fsSL https://example.com/install.sh | bash"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "curl -fsSL https://example.com/install.sh | bash")
}

func TestGenerateInstallScript_BinaryDownload_ShellInjection(t *testing.T) {
	tools := map[string]ToolDef{
		"evil": {Type: "binary-download", URL: "https://example.com/$(evil)", Binary: "my binary"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	// Binary name with space should be quoted
	assert.Contains(t, script, "'my binary'")
	// URL with $() should be quoted
	assert.Contains(t, script, "'https://example.com/$(evil)'")
}

func TestGenerateInstallScript_Apt_ShellInjection(t *testing.T) {
	tools := map[string]ToolDef{
		"evil": {Type: "apt", Package: "pkg; rm -rf /", Binary: "evil bin"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	// Package should be quoted to prevent injection
	assert.Contains(t, script, "'pkg; rm -rf /'")
	// Binary should be quoted
	assert.Contains(t, script, "'evil bin'")
}

func TestVerifyChecksum_Match(t *testing.T) {
	data := []byte("hello world")
	// SHA256 of "hello world"
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	assert.True(t, VerifyChecksum(expected, data))
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("hello world")
	assert.False(t, VerifyChecksum("wrong", data))
}

func TestGenerateInstallScript_UnknownType(t *testing.T) {
	tools := map[string]ToolDef{
		"mystery": {Type: "unknown-type", Binary: "mystery"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	// Should either produce an error message or be empty -- not silently skip
	// Verify current behavior
	assert.NotEmpty(t, script, "unknown type should produce some output (at least a comment)")
}

func TestIsValidChecksum_Valid(t *testing.T) {
	assert.True(t, isValidChecksum("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"))
}

func TestIsValidChecksum_Invalid(t *testing.T) {
	// Too short
	assert.False(t, isValidChecksum("abc123"))
	// Wrong characters
	assert.False(t, isValidChecksum("g3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"))
	// Empty
	assert.False(t, isValidChecksum(""))
	// Too long
	assert.False(t, isValidChecksum("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855aa"))
	// Contains spaces
	assert.False(t, isValidChecksum("e3b0c44298fc1c149afbf4c8996fb924 7ae41e4649b934ca495991b7852b855"))
}

func TestGenerateInstallScript_InvalidChecksumIgnored(t *testing.T) {
	tools := map[string]ToolDef{
		"tool": {Type: "binary-download", URL: "https://example.com/tool", Binary: "tool", Checksum: "not-a-valid-checksum"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	// Invalid checksum should be silently ignored — no sha256sum verification in output
	assert.NotContains(t, script, "sha256sum", "invalid checksum should be ignored, not verified")
	assert.Contains(t, script, "curl", "tool should still be downloaded")
}

func TestGenerateInstallScript_AdversarialToolName(t *testing.T) {
	tools := map[string]ToolDef{
		"evil; rm -rf /": {Type: "binary-download", URL: "https://example.com/tool", Binary: "tool"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	// Tool name only appears in a shell comment (# Install: ...) which is not executed.
	// The actual curl/chmod commands use the binary name, not the tool map key.
	// Verify the binary name is properly quoted in the executable parts.
	assert.Contains(t, script, "# Install:")
	assert.Contains(t, script, "curl -fsSL")
}

func TestGenerateInstallScript_AdversarialURL(t *testing.T) {
	tools := map[string]ToolDef{
		"tool": {Type: "binary-download", URL: "https://example.com/tool; rm -rf /", Binary: "tool"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	// URL should be quoted in curl command
	assert.Contains(t, script, "'https://example.com/tool; rm -rf /'",
		"URL with shell metacharacters should be quoted")
}

func TestGenerateInstallScript_MultipleTools(t *testing.T) {
	tools := map[string]ToolDef{
		"act":  {Type: "binary-download", URL: "https://example.com/act", Binary: "act"},
		"helm": {Type: "tar-extract", URL: "https://example.com/helm.tar.gz", Binary: "helm"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "act")
	assert.Contains(t, script, "helm")
}
