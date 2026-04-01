package logging

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
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

func TestLogLaunch_WritesLogFile(t *testing.T) {
	logDir := t.TempDir()
	LogLaunch(logDir, "claude-code", "work", "claude-code-work-abc123", "ubuntu:24.04")

	content, err := os.ReadFile(filepath.Join(logDir, "ai-shim.log"))
	assert.NoError(t, err)
	assert.Contains(t, string(content), "action=launch")
	assert.Contains(t, string(content), "agent=claude-code")
	assert.Contains(t, string(content), "profile=work")
	assert.Contains(t, string(content), "container=claude-code-work-abc123")
	assert.Contains(t, string(content), "image=ubuntu:24.04")
}

func TestLogExit_WritesLogFile(t *testing.T) {
	logDir := t.TempDir()
	LogExit(logDir, "claude-code-work-abc123", 0)

	content, err := os.ReadFile(filepath.Join(logDir, "ai-shim.log"))
	assert.NoError(t, err)
	assert.Contains(t, string(content), "action=exit")
	assert.Contains(t, string(content), "container=claude-code-work-abc123")
	assert.Contains(t, string(content), "exit_code=0")
}

func TestLogLaunch_AppendsMultipleEntries(t *testing.T) {
	logDir := t.TempDir()
	LogLaunch(logDir, "agent1", "p1", "c1", "img1")
	LogLaunch(logDir, "agent2", "p2", "c2", "img2")
	LogExit(logDir, "c1", 0)

	content, err := os.ReadFile(filepath.Join(logDir, "ai-shim.log"))
	assert.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	assert.Len(t, lines, 3)
}

func TestLogLaunch_EmptyLogDir(t *testing.T) {
	// Should not panic when logDir is empty
	LogLaunch("", "agent", "profile", "container", "image")
	LogExit("", "container", 1)
}
