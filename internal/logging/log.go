package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ai-shim/ai-shim/internal/security"
)

var verbose bool

// Init checks AI_SHIM_VERBOSE environment variable.
func Init() {
	verbose = os.Getenv("AI_SHIM_VERBOSE") == "1"
}

// IsVerbose returns whether verbose logging is enabled.
func IsVerbose() bool {
	return verbose
}

// Debug prints a debug message to stderr if verbose is enabled.
func Debug(format string, args ...interface{}) {
	if !verbose {
		return
	}
	fmt.Fprintf(os.Stderr, "ai-shim: [debug] "+format+"\n", args...)
}

// DebugEnv prints environment variables with secrets masked.
func DebugEnv(env map[string]string) {
	if !verbose {
		return
	}
	masked := security.MaskSecrets(env)
	for k, v := range masked {
		fmt.Fprintf(os.Stderr, "ai-shim: [debug]   %s=%s\n", k, v)
	}
}

// LogLaunch writes a launch event to the persistent log file.
// Each line: timestamp action=launch agent=<name> profile=<profile> container=<name> image=<image>
func LogLaunch(logDir, agent, profile, containerName, image string) {
	appendLog(logDir, fmt.Sprintf("action=launch agent=%s profile=%s container=%s image=%s",
		agent, profile, containerName, image))
}

// LogExit writes an exit event to the persistent log file.
// Each line: timestamp action=exit container=<name> exit_code=<code>
func LogExit(logDir, containerName string, exitCode int) {
	appendLog(logDir, fmt.Sprintf("action=exit container=%s exit_code=%d",
		containerName, exitCode))
}

// appendLog writes a timestamped line to the ai-shim.log file in logDir.
func appendLog(logDir, message string) {
	if logDir == "" {
		return
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}
	logFile := filepath.Join(logDir, "ai-shim.log")
	entry := fmt.Sprintf("%s %s\n", time.Now().Format(time.RFC3339), message)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = f.WriteString(entry)
}
