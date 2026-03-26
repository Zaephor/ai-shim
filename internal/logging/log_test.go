package logging

import (
	"bytes"
	"io"
	"os"
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

func TestDebug_OutputsWhenVerbose(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "1")
	Init()

	// Capture stderr
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	Debug("test %s %d", "hello", 42)

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "test hello 42")
	assert.Contains(t, output, "[debug]")
}

func TestDebugEnv_MasksSecretsInOutput(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "1")
	Init()

	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	DebugEnv(map[string]string{
		"SAFE_VAR": "visible",
		"API_KEY":  "sk-secret-123",
	})

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "visible", "safe values should be shown")
	assert.Contains(t, output, "***", "secrets should be masked")
	assert.NotContains(t, output, "sk-secret-123", "secret values must not appear")
}

func TestDebug_NoOutputWhenNotVerbose(t *testing.T) {
	t.Setenv("AI_SHIM_VERBOSE", "0")
	Init()

	// Should not panic when not verbose
	Debug("test %s", "message")
	DebugEnv(map[string]string{"FOO": "bar"})
}
