package config

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestExampleProfilesParseAndValidate enumerates every YAML under
// configs/examples/profiles and asserts it parses via LoadFileStrict. It also
// runs Config.Validate and `bash -n` over each tool's install script (when
// bash is on PATH). Validation output and strict-decode unknown-field
// warnings are surfaced via t.Logf so schema drift is visible without
// failing CI. Parse errors and install-script syntax errors are hard
// failures.
func TestExampleProfilesParseAndValidate(t *testing.T) {
	runExampleDirCheck(t, "../../configs/examples/profiles")
}

// TestExampleAgentsParseAndValidate is the same sweep for agent templates.
func TestExampleAgentsParseAndValidate(t *testing.T) {
	runExampleDirCheck(t, "../../configs/examples/agents")
}

func runExampleDirCheck(t *testing.T, relDir string) {
	t.Helper()
	absDir, err := filepath.Abs(relDir)
	if err != nil {
		t.Fatalf("abs(%q): %v", relDir, err)
	}
	if _, err := os.Stat(absDir); err != nil {
		t.Fatalf("example dir not found: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(absDir, "*.yaml"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no example YAMLs found under %s", absDir)
	}

	for _, path := range matches {
		path := path
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			cfg, warnings, err := LoadFileStrict(path)
			if err != nil {
				t.Fatalf("LoadFileStrict: %v", err)
			}
			for _, w := range warnings {
				t.Logf("strict-decode warning: %s", w)
			}
			for _, w := range cfg.Validate() {
				t.Logf("validate warning: %s", w)
			}

			// Per-tool install-script syntax check. Table-driven sub-tests
			// so failures name the offending tool.
			for toolName, td := range cfg.Tools {
				toolName, td := toolName, td
				if td.Install == "" {
					continue
				}
				t.Run("install/"+toolName, func(t *testing.T) {
					bashPath, err := exec.LookPath("bash")
					if err != nil {
						t.Skipf("bash not on PATH: %v", err)
					}
					cmd := exec.Command(bashPath, "-n")
					cmd.Stdin = strings.NewReader(td.Install)
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("bash -n on install script failed: %v\noutput: %s\nscript:\n%s", err, out, td.Install)
					}
				})
			}
		})
	}
}
