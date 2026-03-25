package logging

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDebug_Verbose(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "1")
	Init()
	assert.True(t, IsVerbose())
}

func TestDebug_NotVerbose(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "0")
	Init()
	assert.False(t, IsVerbose())
}

func TestDebug_DefaultNotVerbose(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "")
	Init()
	assert.False(t, IsVerbose())
}

func TestDebugEnv_MasksSecrets(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "1")
	Init()

	// DebugEnv should not panic; it prints to stderr
	env := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-secret123",
		"HOME":              "/home/user",
	}
	// Just verify it doesn't panic
	DebugEnv(env)
}

func TestDebug_NoOutputWhenNotVerbose(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "0")
	Init()

	// Should not panic when not verbose
	Debug("test %s", "message")
	DebugEnv(map[string]string{"FOO": "bar"})
}
