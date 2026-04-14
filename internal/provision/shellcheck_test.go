package provision

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

// shellSyntaxCases covers every tool Type handled by generateToolInstall, plus
// a couple of orthogonal flags (checksum, Files, DataDir/EnvVar). These render
// the full set of distinct emission branches in GenerateInstallScript so that
// `bash -n` exercises each code path at least once.
var shellSyntaxCases = map[string]struct {
	order     []string
	tools     map[string]ToolDef
	targetDir string
}{
	"binary_download": {
		tools: map[string]ToolDef{
			"act": {Type: "binary-download", URL: "https://example.com/act", Binary: "act"},
		},
		targetDir: "/opt/bin",
	},
	"binary_download_with_checksum": {
		tools: map[string]ToolDef{
			"act": {
				Type:     "binary-download",
				URL:      "https://example.com/act",
				Binary:   "act",
				Checksum: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
			},
		},
		targetDir: "/opt/bin",
	},
	"tar_extract": {
		tools: map[string]ToolDef{
			"helm": {Type: "tar-extract", URL: "https://example.com/helm.tar.gz", Binary: "helm"},
		},
		targetDir: "/opt/bin",
	},
	"tar_extract_selective": {
		tools: map[string]ToolDef{
			"tool": {
				Type:   "tar-extract-selective",
				URL:    "https://example.com/t.tar.gz",
				Binary: "tool",
				Files:  []string{"lib.so", "helper"},
			},
		},
		targetDir: "/opt/bin",
	},
	"apt": {
		tools: map[string]ToolDef{
			"tmux": {Type: "apt", Package: "tmux", Binary: "tmux"},
		},
		targetDir: "/opt/bin",
	},
	"go_install": {
		tools: map[string]ToolDef{
			"tool": {Type: "go-install", Package: "github.com/user/tool", Binary: "tool"},
		},
		targetDir: "/opt/bin",
	},
	"custom": {
		tools: map[string]ToolDef{
			"claude": {
				Type:    "custom",
				Install: "curl -fsSL https://claude.ai/install.sh | bash",
				Binary:  "claude",
			},
		},
		targetDir: "/opt/bin",
	},
	"custom_with_datadir_envvar": {
		tools: map[string]ToolDef{
			"nvm": {
				Type:    "custom",
				Install: "curl -fsSL https://install.sh | bash",
				Binary:  "nvm",
				DataDir: true,
				EnvVar:  "NVM_DIR",
			},
		},
		targetDir: "/opt/bin",
	},
	"multiple_tools_ordered": {
		order: []string{"act", "helm", "tmux"},
		tools: map[string]ToolDef{
			"act":  {Type: "binary-download", URL: "https://example.com/act", Binary: "act"},
			"helm": {Type: "tar-extract", URL: "https://example.com/helm.tar.gz", Binary: "helm"},
			"tmux": {Type: "apt", Package: "tmux", Binary: "tmux"},
		},
		targetDir: "/opt/bin",
	},
}

// TestGenerateInstallScript_ShellSyntax confirms every rendered install
// script parses cleanly under `bash -n` (falling back to `sh -n`). It mirrors
// the install-package test; existing golden/assert tests only check substring
// membership, not that the script is a syntactically valid shell program.
func TestGenerateInstallScript_ShellSyntax(t *testing.T) {
	shell := findShellParser(t)
	if shell == "" {
		t.Skipf("no bash or sh available in PATH; skipping shell syntax validation")
	}

	for name, tc := range shellSyntaxCases {
		t.Run(name, func(t *testing.T) {
			script := GenerateInstallScript(tc.order, tc.tools, tc.targetDir)
			if script == "" {
				t.Fatalf("GenerateInstallScript returned empty script for %s", name)
			}

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
