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
	assert.Contains(t, script, "/opt/bin/mybin")
	assert.Contains(t, script, "chmod +x")
	assert.Contains(t, script, "if [ ! -f")
}

func TestGenerateInstallScript_BinaryDownloadWithChecksum(t *testing.T) {
	tools := map[string]ToolDef{
		"mybin": {Type: "binary-download", URL: "https://example.com/bin", Binary: "mybin", Checksum: "abc123"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "sha256sum")
	assert.Contains(t, script, "abc123")
}

func TestGenerateInstallScript_TarExtract(t *testing.T) {
	tools := map[string]ToolDef{
		"act": {Type: "tar-extract", URL: "https://example.com/act.tar.gz", Binary: "act"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "tar xz")
	assert.Contains(t, script, "act")
}

func TestGenerateInstallScript_TarExtractSelective(t *testing.T) {
	tools := map[string]ToolDef{
		"tool": {Type: "tar-extract-selective", URL: "https://example.com/t.tar.gz", Binary: "tool", Files: []string{"lib.so"}},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "tool")
	assert.Contains(t, script, "lib.so")
}

func TestGenerateInstallScript_Apt(t *testing.T) {
	tools := map[string]ToolDef{
		"tmux": {Type: "apt", Package: "tmux", Binary: "tmux"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "apt-get install")
	assert.Contains(t, script, "tmux")
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

func TestGenerateInstallScript_UnknownType(t *testing.T) {
	tools := map[string]ToolDef{
		"mytool": {Type: "unknown-type", Binary: "mytool"},
	}
	script := GenerateInstallScript(tools, "/opt/bin")
	assert.Contains(t, script, "ERROR: unknown tool type: unknown-type for tool mytool")
}

func TestVerifyChecksum_Match(t *testing.T) {
	data := []byte("hello world")
	// SHA256 of "hello world"
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	assert.True(t, verifyChecksum(expected, data))
}

func TestVerifyChecksum_Mismatch(t *testing.T) {
	data := []byte("hello world")
	assert.False(t, verifyChecksum("wrong", data))
}
