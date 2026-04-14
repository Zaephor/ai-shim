package install

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// findShellParser returns the path to a shell that can be invoked with -n
// to validate syntax without executing, preferring bash. Returns "" if none
// are available.
func findShellParser(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"bash", "sh"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// TestEntrypoint_ShellSyntax confirms every generated entrypoint script
// parses cleanly under `bash -n` (falling back to `sh -n`). This guards
// against regressions in entrypoint.go where the golden text could change
// shape in a syntactically invalid way without any existing test catching it.
func TestEntrypoint_ShellSyntax(t *testing.T) {
	shell := findShellParser(t)
	if shell == "" {
		t.Skipf("no bash or sh available in PATH; skipping shell syntax validation")
	}

	for name, params := range goldenCases {
		t.Run(name, func(t *testing.T) {
			script := GenerateEntrypoint(params)

			tmp, err := os.CreateTemp(t.TempDir(), name+"-*.sh")
			if err != nil {
				t.Fatalf("create temp: %v", err)
			}
			if _, err := tmp.WriteString(script); err != nil {
				t.Fatalf("write script: %v", err)
			}
			if err := tmp.Close(); err != nil {
				t.Fatalf("close temp: %v", err)
			}

			cmd := exec.Command(shell, "-n", tmp.Name())
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s -n rejected generated script (%s):\n%s\n---script---\n%s",
					filepath.Base(shell), err, out, script)
			}
		})
	}
}
