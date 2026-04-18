package integration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Zaephor/ai-shim/internal/config"
	"gopkg.in/yaml.v3"
)

func projectRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			d, _ := os.Getwd()
			return d
		}
		dir = parent
	}
}

// collectExampleFiles returns all YAML files under configs/examples that should
// be validated: profiles/*.yaml, default.yaml, and agents/*.yaml.
func collectExampleFiles(t *testing.T) []string {
	t.Helper()
	root := projectRoot()

	patterns := []string{
		filepath.Join(root, "configs", "examples", "profiles", "*.yaml"),
		filepath.Join(root, "configs", "examples", "default.yaml"),
		filepath.Join(root, "configs", "examples", "agents", "*.yaml"),
	}

	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("filepath.Glob(%q): %v", pattern, err)
		}
		files = append(files, matches...)
	}
	return files
}

// validToolTypes is the set of tool types supported by the provisioning system.
var validToolTypes = map[string]bool{
	"binary-download":       true,
	"tar-extract":           true,
	"tar-extract-selective": true,
	"apt":                   true,
	"go-install":            true,
	"custom":                true,
}

func TestProfileExamples_ValidYAML(t *testing.T) {
	files := collectExampleFiles(t)

	const minExpectedCount = 3 // sanity check: at least default + a few profiles
	if len(files) < minExpectedCount {
		t.Fatalf("expected at least %d example files, got %d", minExpectedCount, len(files))
	}

	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("reading %s: %v", f, err)
			}

			var cfg config.Config
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("YAML parse error in %s: %v", f, err)
			}
		})
	}
}

func TestProfileExamples_ValidToolTypes(t *testing.T) {
	files := collectExampleFiles(t)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}

		var cfg config.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("YAML parse error in %s: %v", f, err)
		}

		for toolName, tool := range cfg.Tools {
			if !validToolTypes[tool.Type] {
				t.Errorf("file %s: tool %q has invalid type %q", filepath.Base(f), toolName, tool.Type)
			}
		}
	}
}

func TestProfileExamples_ToolsHaveRequiredFields(t *testing.T) {
	files := collectExampleFiles(t)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}

		var cfg config.Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			t.Fatalf("YAML parse error in %s: %v", f, err)
		}

		base := filepath.Base(f)
		for toolName, tool := range cfg.Tools {
			switch tool.Type {
			case "binary-download":
				if tool.URL == "" {
					t.Errorf("file %s: tool %q (binary-download) missing URL", base, toolName)
				}
				if tool.Binary == "" {
					t.Errorf("file %s: tool %q (binary-download) missing Binary", base, toolName)
				}
			case "tar-extract":
				if tool.URL == "" {
					t.Errorf("file %s: tool %q (tar-extract) missing URL", base, toolName)
				}
				if tool.Binary == "" {
					t.Errorf("file %s: tool %q (tar-extract) missing Binary", base, toolName)
				}
			case "custom":
				if tool.Install == "" {
					t.Errorf("file %s: tool %q (custom) missing Install", base, toolName)
				}
			case "apt":
				if tool.Package == "" {
					t.Errorf("file %s: tool %q (apt) missing Package", base, toolName)
				}
			case "go-install":
				if tool.Package == "" {
					t.Errorf("file %s: tool %q (go-install) missing Package", base, toolName)
				}
			}
		}
	}
}

func TestProfileExamples_StrictYAML(t *testing.T) {
	files := collectExampleFiles(t)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			cfg, warnings, err := config.LoadFileStrict(f)
			if err != nil {
				t.Fatalf("strict load error in %s: %v", f, err)
			}
			for _, w := range warnings {
				t.Errorf("unknown key in %s: %s", filepath.Base(f), w)
			}
			_ = cfg
		})
	}
}

func TestProfileExamples_PassValidation(t *testing.T) {
	files := collectExampleFiles(t)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			cfg, err := config.LoadFile(f)
			if err != nil {
				t.Fatalf("load error in %s: %v", f, err)
			}
			errs := cfg.Validate()
			for _, e := range errs {
				t.Errorf("validation error in %s: %s", filepath.Base(f), e)
			}
		})
	}
}

func TestProfileExamples_NoDuplicateNames(t *testing.T) {
	files := collectExampleFiles(t)

	seen := make(map[string]string) // basename -> full path
	for _, f := range files {
		base := filepath.Base(f)
		if prev, ok := seen[base]; ok {
			t.Errorf("duplicate basename %q:\n  %s\n  %s", base, prev, f)
		}
		seen[base] = f
	}
}
