package logging

import (
	"fmt"
	"os"

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
