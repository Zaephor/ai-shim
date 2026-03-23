package install

import (
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
