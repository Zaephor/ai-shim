package install

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateEntrypoint_NPM(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "@google/gemini-cli",
		Binary:      "gemini",
		AgentArgs:   []string{"--verbose"},
	})
	assert.Contains(t, script, "npm install -g @google/gemini-cli")
	assert.Contains(t, script, "exec gemini --verbose")
	assert.NotContains(t, script, "2>/dev/null")
	assert.Contains(t, script, "ERROR: npm install failed")
	assert.Contains(t, script, "echo \"Installing @google/gemini-cli via npm...\"")
}

func TestGenerateEntrypoint_NPMWithVersion(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "@google/gemini-cli",
		Binary:      "gemini",
		Version:     "1.2.3",
	})
	assert.Contains(t, script, "npm install -g @google/gemini-cli@1.2.3")
}

func TestGenerateEntrypoint_UV(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
	})
	assert.Contains(t, script, "uv tool install aider-chat")
	assert.Contains(t, script, "exec aider")
	assert.NotContains(t, script, "2>/dev/null")
	assert.Contains(t, script, "ERROR: uv install failed")
	assert.Contains(t, script, "echo \"Installing aider-chat via uv...\"")
}

func TestGenerateEntrypoint_UVWithVersion(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
		Version:     "0.50.0",
	})
	assert.Contains(t, script, "uv tool install aider-chat==0.50.0")
}

func TestGenerateEntrypoint_Custom(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "custom",
		Package:     "curl -fsSL https://claude.ai/install.sh | bash",
		Binary:      "claude",
		AgentArgs:   []string{"--dangerously-skip-permissions"},
	})
	assert.Contains(t, script, "curl -fsSL https://claude.ai/install.sh | bash")
	assert.Contains(t, script, "exec claude --dangerously-skip-permissions")
}

func TestGenerateEntrypoint_ShellQuoting(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test",
		AgentArgs:   []string{"--msg", "hello world"},
	})
	assert.Contains(t, script, "'hello world'")
}

func TestGenerateEntrypoint_StartsWithShebang(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test",
		Binary:      "test",
	})
	assert.True(t, len(script) >= 11 && script[:11] == "#!/bin/sh\ns")
}

func TestGenerateEntrypoint_PinnedVersion(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test",
		Version:     "2.0.0",
		AgentName:   "test-agent",
	})
	assert.True(t, strings.Contains(script, "INSTALLED_VERSION="))
	assert.True(t, strings.Contains(script, "cat \"$INSTALLED_VERSION\""))
	assert.True(t, strings.Contains(script, "2.0.0"))
}

func TestGenerateEntrypoint_UpdateIntervalAlways(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType:    "npm",
		Package:        "test-pkg",
		Binary:         "test",
		AgentName:      "test-agent",
		UpdateInterval: 0,
	})
	assert.True(t, strings.Contains(script, "reinstall every launch"))
}

func TestGenerateEntrypoint_UpdateIntervalNever(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType:    "npm",
		Package:        "test-pkg",
		Binary:         "test",
		AgentName:      "test-agent",
		UpdateInterval: -1,
	})
	assert.True(t, strings.Contains(script, "update_interval=never"))
}

func TestGenerateEntrypoint_UpdateIntervalTimed(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType:    "npm",
		Package:        "test-pkg",
		Binary:         "test",
		AgentName:      "test-agent",
		UpdateInterval: 86400,
	})
	assert.True(t, strings.Contains(script, "elapsed"))
	assert.True(t, strings.Contains(script, "86400"))
}

func TestGenerateEntrypoint_NPMCaching(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test",
		AgentName:   "test-agent",
	})
	assert.True(t, strings.Contains(script, "NPM_CONFIG_PREFIX="))
	assert.True(t, strings.Contains(script, "NPM_CONFIG_CACHE="))
	assert.True(t, strings.Contains(script, "/usr/local/share/ai-shim/agents/test-agent"))
}

func TestGenerateEntrypoint_UVBootstrap(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
		AgentName:   "test-agent",
	})
	assert.True(t, strings.Contains(script, "curl -LsSf https://astral.sh/uv/install.sh | sh"))
}

func TestGenerateEntrypoint_UVCaching(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
		AgentName:   "test-agent",
	})
	assert.True(t, strings.Contains(script, "UV_TOOL_DIR="))
	assert.True(t, strings.Contains(script, "UV_TOOL_BIN_DIR="))
	assert.True(t, strings.Contains(script, "/usr/local/share/ai-shim/agents/test-agent"))
}

func TestGenerateEntrypoint_CustomPATH(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "custom",
		Package:     "curl -fsSL https://example.com/install.sh | bash",
		Binary:      "example",
	})
	assert.True(t, strings.Contains(script, "$HOME/.local/bin"))
	assert.True(t, strings.Contains(script, "$HOME/.cargo/bin"))
}

func TestGenerateEntrypoint_CustomSetPlusE(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "custom",
		Package:     "curl -fsSL https://example.com/install.sh | bash",
		Binary:      "example",
	})
	assert.True(t, strings.Contains(script, "set +e"))
	// Verify set -e comes after the install command
	setPlusE := strings.Index(script, "set +e")
	installCmd := strings.Index(script, "curl -fsSL https://example.com/install.sh | bash")
	setMinusE := strings.LastIndex(script, "set -e")
	assert.True(t, setPlusE < installCmd, "set +e should come before install command")
	assert.True(t, installCmd < setMinusE, "set -e should come after install command")
}

func TestGenerateEntrypoint_PostInstall(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test",
		AgentName:   "test-agent",
	})
	assert.True(t, strings.Contains(script, "$LAST_UPDATE"))
	assert.True(t, strings.Contains(script, "$INSTALLED_VERSION"))
	assert.True(t, strings.Contains(script, "date +%s"))
}
