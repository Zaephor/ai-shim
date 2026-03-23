package invocation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseName_AgentAndProfile(t *testing.T) {
	agent, profile, err := ParseName("claude_work")
	require.NoError(t, err)
	assert.Equal(t, "claude", agent)
	assert.Equal(t, "work", profile)
}

func TestParseName_AgentWithDashes(t *testing.T) {
	agent, profile, err := ParseName("claude-code_work")
	require.NoError(t, err)
	assert.Equal(t, "claude-code", agent)
	assert.Equal(t, "work", profile)
}

func TestParseName_ProfileWithDashes(t *testing.T) {
	agent, profile, err := ParseName("gemini_my-profile")
	require.NoError(t, err)
	assert.Equal(t, "gemini", agent)
	assert.Equal(t, "my-profile", profile)
}

func TestParseName_AgentOnly(t *testing.T) {
	agent, profile, err := ParseName("claude")
	require.NoError(t, err)
	assert.Equal(t, "claude", agent)
	assert.Equal(t, "default", profile)
}

func TestParseName_MultipleUnderscores(t *testing.T) {
	agent, profile, err := ParseName("claude_work_extra")
	require.NoError(t, err)
	assert.Equal(t, "claude", agent)
	assert.Equal(t, "work_extra", profile)
}

func TestParseName_Empty(t *testing.T) {
	_, _, err := ParseName("")
	assert.Error(t, err)
}
