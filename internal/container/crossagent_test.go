package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ai-shim/ai-shim/internal/storage"
)

func TestCrossAgentMounts_Isolated_NoAllow(t *testing.T) {
	layout := storage.NewLayout("/tmp/test-ai-shim")
	mounts := CrossAgentMounts(layout, "claude-code", nil, true)
	assert.Empty(t, mounts, "isolated with no allow_agents should produce no mounts")
}

func TestCrossAgentMounts_Isolated_WithAllow(t *testing.T) {
	layout := storage.NewLayout("/tmp/test-ai-shim")
	mounts := CrossAgentMounts(layout, "claude-code", []string{"gemini-cli"}, true)
	assert.NotEmpty(t, mounts)

	// Should have mounts for gemini bin and home paths
	hasGeminiBin := false
	for _, m := range mounts {
		if m.Target == "/opt/ai-shim/agents/gemini-cli/bin" {
			hasGeminiBin = true
		}
	}
	assert.True(t, hasGeminiBin, "should mount gemini bin")
}

func TestCrossAgentMounts_NonIsolated(t *testing.T) {
	layout := storage.NewLayout("/tmp/test-ai-shim")
	mounts := CrossAgentMounts(layout, "claude-code", nil, false)
	// Should have mounts for all agents except claude-code
	assert.NotEmpty(t, mounts)
}

func TestCrossAgentMounts_ExcludesPrimary(t *testing.T) {
	layout := storage.NewLayout("/tmp/test-ai-shim")
	mounts := CrossAgentMounts(layout, "claude-code", []string{"claude-code", "gemini-cli"}, true)
	for _, m := range mounts {
		assert.NotContains(t, m.Source, "/claude-code/", "primary agent should not appear in cross-agent mounts")
	}
}

func TestDetermineAccessibleAgents_Isolated(t *testing.T) {
	agents := determineAccessibleAgents("claude-code", []string{"gemini-cli", "aider"}, true)
	assert.Equal(t, []string{"gemini-cli", "aider"}, agents)
}

func TestDetermineAccessibleAgents_NonIsolated(t *testing.T) {
	agents := determineAccessibleAgents("claude-code", nil, false)
	assert.NotEmpty(t, agents)
	for _, a := range agents {
		assert.NotEqual(t, "claude-code", a)
	}
}
