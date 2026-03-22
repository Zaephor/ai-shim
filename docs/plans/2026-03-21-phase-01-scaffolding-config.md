# Phase 1: Project Scaffolding & Core Config — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Set up Go module, project structure, CI foundation, git hooks, and the 5-tier YAML config system with merge semantics and template resolution.

**Architecture:** The config package is the core of ai-shim. It loads YAML files from `~/.ai-shim/config/` across 4 file tiers, merges them with defined semantics (scalars: last wins, maps: per-key replace, lists: append), applies `AI_SHIM_*` env var overrides as tier 5, then resolves `{{ .var }}` templates in string values. The result is a single resolved `Config` struct used by all downstream packages.

**Tech Stack:** Go 1.24+, `gopkg.in/yaml.v3`, Go `text/template`, `github.com/stretchr/testify` (assertions only)

---

### Task 1: Initialize Go Module & Directory Structure

**Files:**
- Create: `go.mod`
- Create: `cmd/ai-shim/main.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `.githooks/commit-msg`
- Create: `Makefile`
- Create: `.gitignore`

**Step 1: Initialize Go module**

Run:
```bash
cd /workspace/c21b3ead7222c3eb
go mod init github.com/ai-shim/ai-shim
```

**Step 2: Create directory structure**

Run:
```bash
mkdir -p cmd/ai-shim
mkdir -p internal/{config,agent,container,dind,install,provision,storage,workspace,selfupdate,platform,security}
```

**Step 3: Write minimal main.go**

Create `cmd/ai-shim/main.go`:
```go
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintf(os.Stderr, "ai-shim: not yet implemented\n")
	os.Exit(1)
}
```

**Step 4: Write .gitignore**

Create `.gitignore`:
```
# Binaries
ai-shim
*.exe
dist/

# Test
coverage.out

# IDE
.idea/
.vscode/
*.swp

# OS
.DS_Store
```

**Step 5: Write Makefile**

Create `Makefile`:
```makefile
.PHONY: build test lint clean setup

BINARY := ai-shim
MODULE := github.com/ai-shim/ai-shim

build:
	go build -o $(BINARY) ./cmd/ai-shim

test:
	go test ./... -v

test-short:
	go test ./... -v -short

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
	go clean -testcache

setup:
	git config core.hooksPath .githooks
```

**Step 6: Write conventional commit hook**

Create `.githooks/commit-msg`:
```bash
#!/usr/bin/env bash
set -euo pipefail

commit_msg_file="$1"
commit_msg=$(head -1 "$commit_msg_file")

# Conventional commit pattern: type(scope): description
# or: type: description
pattern='^(feat|fix|docs|style|refactor|perf|test|build|ci|chore|revert)(\(.+\))?: .+'

if ! echo "$commit_msg" | grep -qE "$pattern"; then
    echo "ERROR: Commit message does not follow Conventional Commits format."
    echo ""
    echo "Expected: <type>(<optional scope>): <description>"
    echo "Types: feat, fix, docs, style, refactor, perf, test, build, ci, chore, revert"
    echo ""
    echo "Got: $commit_msg"
    exit 1
fi
```

Run:
```bash
chmod +x .githooks/commit-msg
```

**Step 7: Activate git hooks and verify build**

Run:
```bash
make setup
make build
```
Expected: Binary `ai-shim` created, prints "not yet implemented" when run.

**Step 8: Commit**

```bash
git add go.mod cmd/ internal/ .githooks/ Makefile .gitignore
git commit -m "build: initialize Go module and project structure"
```

---

### Task 2: Config Types & YAML Loading

**Files:**
- Create: `internal/config/types.go`
- Create: `internal/config/loader.go`
- Create: `internal/config/loader_test.go`

**Step 1: Write the config types**

Create `internal/config/types.go`:
```go
package config

// Config represents the fully resolved configuration for an agent+profile invocation.
type Config struct {
	// Variables are template sources, not injected into the container.
	Variables map[string]string `yaml:"variables,omitempty"`

	// Env vars injected into the container. Supports templating via Variables.
	Env map[string]string `yaml:"env,omitempty"`

	// Container image.
	Image string `yaml:"image,omitempty"`

	// Container hostname.
	Hostname string `yaml:"hostname,omitempty"`

	// Agent version pin. Empty string means latest.
	Version string `yaml:"version,omitempty"`

	// Default args passed to the agent CLI.
	Args []string `yaml:"args,omitempty"`

	// Volume mounts beyond automatic storage mounts.
	Volumes []string `yaml:"volumes,omitempty"`

	// Port mappings.
	Ports []string `yaml:"ports,omitempty"`

	// Additional packages to install in the container.
	Packages []string `yaml:"packages,omitempty"`

	// Feature toggles.
	DIND    *bool `yaml:"dind,omitempty"`
	DINDGpu *bool `yaml:"dind_gpu,omitempty"`
	GPU     *bool `yaml:"gpu,omitempty"`

	// Cross-agent access.
	AllowAgents []string `yaml:"allow_agents,omitempty"`
	Isolated    *bool    `yaml:"isolated,omitempty"`

	// Tool provisioning.
	Tools map[string]ToolDef `yaml:"tools,omitempty"`
}

// ToolDef defines a tool to provision in the container.
type ToolDef struct {
	Type     string `yaml:"type"`               // binary-download, tar-extract, tar-extract-selective, apt, go-install, custom
	URL      string `yaml:"url,omitempty"`       // download URL (supports templating)
	Binary   string `yaml:"binary,omitempty"`    // binary name to extract
	Files    []string `yaml:"files,omitempty"`   // additional files to extract (tar-extract-selective)
	Package  string `yaml:"package,omitempty"`   // package name (apt, go-install)
	Install  string `yaml:"install,omitempty"`   // shell script (custom type)
	Checksum string `yaml:"checksum,omitempty"`  // optional SHA256 checksum
}
```

**Step 2: Write the YAML loader with test**

Create `internal/config/loader_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadFile_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")

	content := []byte(`
image: "ubuntu:24.04"
hostname: "test-host"
env:
  FOO: "bar"
  BAZ: "qux"
variables:
  my_var: "my_value"
volumes:
  - "/host:/container"
args:
  - "--flag"
`)
	require.NoError(t, os.WriteFile(path, content, 0644))

	cfg, err := LoadFile(path)
	require.NoError(t, err)

	assert.Equal(t, "ubuntu:24.04", cfg.Image)
	assert.Equal(t, "test-host", cfg.Hostname)
	assert.Equal(t, "bar", cfg.Env["FOO"])
	assert.Equal(t, "qux", cfg.Env["BAZ"])
	assert.Equal(t, "my_value", cfg.Variables["my_var"])
	assert.Equal(t, []string{"/host:/container"}, cfg.Volumes)
	assert.Equal(t, []string{"--flag"}, cfg.Args)
}

func TestLoadFile_Missing(t *testing.T) {
	cfg, err := LoadFile("/nonexistent/path.yaml")
	assert.NoError(t, err, "missing file should not error, returns empty config")
	assert.Equal(t, "", cfg.Image)
}

func TestLoadFile_Empty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))

	cfg, err := LoadFile(path)
	assert.NoError(t, err)
	assert.Equal(t, "", cfg.Image)
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestLoadFile -v`
Expected: FAIL — `LoadFile` not defined.

**Step 4: Implement LoadFile**

Create `internal/config/loader.go`:
```go
package config

import (
	"errors"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile loads a Config from a YAML file. Returns an empty Config if the file
// does not exist (not an error — missing tiers are normal).
func LoadFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}

	var cfg Config
	if len(data) == 0 {
		return cfg, nil
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}
```

**Step 5: Add yaml dependency and run tests**

Run:
```bash
go get gopkg.in/yaml.v3
go get github.com/stretchr/testify
go mod tidy
go test ./internal/config/ -run TestLoadFile -v
```
Expected: All PASS.

**Step 6: Commit**

```bash
git add internal/config/types.go internal/config/loader.go internal/config/loader_test.go go.mod go.sum
git commit -m "feat(config): add config types and YAML file loader"
```

---

### Task 3: Config Merge Engine

**Files:**
- Create: `internal/config/merge.go`
- Create: `internal/config/merge_test.go`

**Step 1: Write merge tests**

Create `internal/config/merge_test.go`:
```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool { return &b }

func TestMerge_ScalarsLastWins(t *testing.T) {
	base := Config{Image: "base-image", Hostname: "base-host", Version: "1.0"}
	over := Config{Image: "over-image", Version: "2.0"}

	result := Merge(base, over)

	assert.Equal(t, "over-image", result.Image)
	assert.Equal(t, "base-host", result.Hostname, "unset scalar should not overwrite")
	assert.Equal(t, "2.0", result.Version)
}

func TestMerge_MapsPerKeyReplace(t *testing.T) {
	base := Config{
		Env:       map[string]string{"A": "1", "B": "2"},
		Variables: map[string]string{"X": "10"},
	}
	over := Config{
		Env:       map[string]string{"B": "override", "C": "3"},
		Variables: map[string]string{"Y": "20"},
	}

	result := Merge(base, over)

	assert.Equal(t, "1", result.Env["A"], "untouched key preserved")
	assert.Equal(t, "override", result.Env["B"], "overlapping key replaced")
	assert.Equal(t, "3", result.Env["C"], "new key added")
	assert.Equal(t, "10", result.Variables["X"])
	assert.Equal(t, "20", result.Variables["Y"])
}

func TestMerge_ListsAppend(t *testing.T) {
	base := Config{
		Volumes: []string{"/a:/a"},
		Args:    []string{"--flag1"},
		Ports:   []string{"8080:8080"},
	}
	over := Config{
		Volumes: []string{"/b:/b"},
		Args:    []string{"--flag2"},
		Ports:   []string{"9090:9090"},
	}

	result := Merge(base, over)

	assert.Equal(t, []string{"/a:/a", "/b:/b"}, result.Volumes)
	assert.Equal(t, []string{"--flag1", "--flag2"}, result.Args)
	assert.Equal(t, []string{"8080:8080", "9090:9090"}, result.Ports)
}

func TestMerge_BoolPtrsLastWins(t *testing.T) {
	base := Config{DIND: boolPtr(true), GPU: boolPtr(false)}
	over := Config{DIND: boolPtr(false)}

	result := Merge(base, over)

	assert.Equal(t, false, *result.DIND, "overridden bool")
	assert.Equal(t, false, *result.GPU, "preserved bool")
}

func TestMerge_ToolsPerKeyReplace(t *testing.T) {
	base := Config{
		Tools: map[string]ToolDef{
			"act":  {Type: "tar-extract", URL: "old-url"},
			"helm": {Type: "binary-download", URL: "helm-url"},
		},
	}
	over := Config{
		Tools: map[string]ToolDef{
			"act": {Type: "tar-extract", URL: "new-url"},
		},
	}

	result := Merge(base, over)

	assert.Equal(t, "new-url", result.Tools["act"].URL, "tool replaced entirely")
	assert.Equal(t, "helm-url", result.Tools["helm"].URL, "untouched tool preserved")
}

func TestMergeAll_FiveTiers(t *testing.T) {
	tiers := []Config{
		{Image: "default-image", Env: map[string]string{"A": "1"}, Volumes: []string{"/default"}},
		{Env: map[string]string{"A": "agent-override"}},
		{Volumes: []string{"/profile"}},
		{Image: "agent-profile-image"},
		{Env: map[string]string{"A": "env-override"}},
	}

	result := MergeAll(tiers...)

	assert.Equal(t, "agent-profile-image", result.Image)
	assert.Equal(t, "env-override", result.Env["A"])
	assert.Equal(t, []string{"/default", "/profile"}, result.Volumes)
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestMerge -v`
Expected: FAIL — `Merge` and `MergeAll` not defined.

**Step 3: Implement merge engine**

Create `internal/config/merge.go`:
```go
package config

// Merge combines two Configs. The `over` config takes precedence.
// Scalars: over wins if non-zero. Maps: per-key replace. Lists: append.
func Merge(base, over Config) Config {
	result := base

	// Scalars — over wins if set
	if over.Image != "" {
		result.Image = over.Image
	}
	if over.Hostname != "" {
		result.Hostname = over.Hostname
	}
	if over.Version != "" {
		result.Version = over.Version
	}

	// Bool pointers — over wins if non-nil
	if over.DIND != nil {
		result.DIND = over.DIND
	}
	if over.DINDGpu != nil {
		result.DINDGpu = over.DINDGpu
	}
	if over.GPU != nil {
		result.GPU = over.GPU
	}
	if over.Isolated != nil {
		result.Isolated = over.Isolated
	}

	// Maps — per-key replace
	result.Env = mergeMaps(result.Env, over.Env)
	result.Variables = mergeMaps(result.Variables, over.Variables)
	result.Tools = mergeToolMaps(result.Tools, over.Tools)

	// Lists — append
	result.Volumes = appendUnique(result.Volumes, over.Volumes)
	result.Args = append(result.Args, over.Args...)
	result.Ports = appendUnique(result.Ports, over.Ports)
	result.Packages = appendUnique(result.Packages, over.Packages)
	result.AllowAgents = appendUnique(result.AllowAgents, over.AllowAgents)

	return result
}

// MergeAll merges multiple configs in order (first = lowest priority).
func MergeAll(configs ...Config) Config {
	if len(configs) == 0 {
		return Config{}
	}
	result := configs[0]
	for _, c := range configs[1:] {
		result = Merge(result, c)
	}
	return result
}

func mergeMaps(base, over map[string]string) map[string]string {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		result := make(map[string]string, len(over))
		for k, v := range over {
			result[k] = v
		}
		return result
	}
	result := make(map[string]string, len(base)+len(over))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range over {
		result[k] = v
	}
	return result
}

func mergeToolMaps(base, over map[string]ToolDef) map[string]ToolDef {
	if len(over) == 0 {
		return base
	}
	if len(base) == 0 {
		result := make(map[string]ToolDef, len(over))
		for k, v := range over {
			result[k] = v
		}
		return result
	}
	result := make(map[string]ToolDef, len(base)+len(over))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range over {
		result[k] = v
	}
	return result
}

func appendUnique(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base))
	for _, v := range base {
		seen[v] = true
	}
	result := make([]string, len(base))
	copy(result, base)
	for _, v := range extra {
		if !seen[v] {
			result = append(result, v)
			seen[v] = true
		}
	}
	return result
}
```

**Step 4: Run tests**

Run: `go test ./internal/config/ -run TestMerge -v`
Expected: All PASS.

**Step 5: Commit**

```bash
git add internal/config/merge.go internal/config/merge_test.go
git commit -m "feat(config): add 5-tier config merge engine"
```

---

### Task 4: Template Resolution

**Files:**
- Create: `internal/config/template.go`
- Create: `internal/config/template_test.go`

**Step 1: Write template tests**

Create `internal/config/template_test.go`:
```go
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveTemplates_EnvVars(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{
			"llm_host": "my-host:8080",
		},
		Env: map[string]string{
			"LLM_ENDPOINT": "https://{{ .llm_host }}/v1",
			"STATIC":       "no-template",
		},
	}

	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)

	assert.Equal(t, "https://my-host:8080/v1", resolved.Env["LLM_ENDPOINT"])
	assert.Equal(t, "no-template", resolved.Env["STATIC"])
}

func TestResolveTemplates_Volumes(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{
			"storage_shared": "/home/user/.ai-shim/shared",
		},
		Volumes: []string{
			"{{ .storage_shared }}/bin:/usr/local/bin",
			"/static:/static",
		},
	}

	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)

	assert.Equal(t, "/home/user/.ai-shim/shared/bin:/usr/local/bin", resolved.Volumes[0])
	assert.Equal(t, "/static:/static", resolved.Volumes[1])
}

func TestResolveTemplates_Image(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{"img_tag": "24.04"},
		Image:    "ubuntu:{{ .img_tag }}",
	}

	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)

	assert.Equal(t, "ubuntu:24.04", resolved.Image)
}

func TestResolveTemplates_NoVariables(t *testing.T) {
	cfg := Config{
		Env: map[string]string{"KEY": "value"},
	}

	resolved, err := ResolveTemplates(cfg)
	require.NoError(t, err)
	assert.Equal(t, "value", resolved.Env["KEY"])
}

func TestResolveTemplates_UndefinedVariable(t *testing.T) {
	cfg := Config{
		Variables: map[string]string{},
		Env:       map[string]string{"X": "{{ .undefined }}"},
	}

	_, err := ResolveTemplates(cfg)
	assert.Error(t, err, "undefined template variable should error")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestResolveTemplates -v`
Expected: FAIL — `ResolveTemplates` not defined.

**Step 3: Implement template resolution**

Create `internal/config/template.go`:
```go
package config

import (
	"bytes"
	"fmt"
	"text/template"
)

// ResolveTemplates resolves {{ .var }} templates in all string fields using
// the Variables map as the data source. Variables themselves are not templated.
func ResolveTemplates(cfg Config) (Config, error) {
	vars := cfg.Variables
	if vars == nil {
		vars = make(map[string]string)
	}

	resolve := func(s string) (string, error) {
		if s == "" {
			return s, nil
		}
		tmpl, err := template.New("").Option("missingkey=error").Parse(s)
		if err != nil {
			return "", fmt.Errorf("parsing template %q: %w", s, err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, vars); err != nil {
			return "", fmt.Errorf("executing template %q: %w", s, err)
		}
		return buf.String(), nil
	}

	result := cfg

	// Resolve scalar fields
	var err error
	if result.Image, err = resolve(result.Image); err != nil {
		return Config{}, err
	}
	if result.Hostname, err = resolve(result.Hostname); err != nil {
		return Config{}, err
	}

	// Resolve map values
	if result.Env, err = resolveMap(result.Env, resolve); err != nil {
		return Config{}, err
	}

	// Resolve list values
	if result.Volumes, err = resolveSlice(result.Volumes, resolve); err != nil {
		return Config{}, err
	}
	if result.Ports, err = resolveSlice(result.Ports, resolve); err != nil {
		return Config{}, err
	}

	// Resolve tool URLs
	if len(result.Tools) > 0 {
		resolved := make(map[string]ToolDef, len(result.Tools))
		for k, td := range result.Tools {
			if td.URL, err = resolve(td.URL); err != nil {
				return Config{}, err
			}
			resolved[k] = td
		}
		result.Tools = resolved
	}

	return result, nil
}

func resolveMap(m map[string]string, resolve func(string) (string, error)) (map[string]string, error) {
	if len(m) == 0 {
		return m, nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		resolved, err := resolve(v)
		if err != nil {
			return nil, err
		}
		result[k] = resolved
	}
	return result, nil
}

func resolveSlice(s []string, resolve func(string) (string, error)) ([]string, error) {
	if len(s) == 0 {
		return s, nil
	}
	result := make([]string, len(s))
	for i, v := range s {
		resolved, err := resolve(v)
		if err != nil {
			return nil, err
		}
		result[i] = resolved
	}
	return result, nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/config/ -run TestResolveTemplates -v`
Expected: All PASS.

**Step 5: Commit**

```bash
git add internal/config/template.go internal/config/template_test.go
git commit -m "feat(config): add template resolution for variables"
```

---

### Task 5: Tier-Aware Config Resolver

**Files:**
- Create: `internal/config/resolver.go`
- Create: `internal/config/resolver_test.go`

**Step 1: Write resolver tests**

Create `internal/config/resolver_test.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Create tier directories
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	// default.yaml
	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "ghcr.io/catthehacker/ubuntu:act-24.04"
hostname: "ai-shim"
variables:
  llm_host: "default-host:8080"
env:
  LLM_ENDPOINT: "{{ .llm_host }}"
`)

	// agents/claude.yaml
	writeYAML(t, filepath.Join(dir, "agents", "claude.yaml"), `
env:
  LLM_ENDPOINT: "https://{{ .llm_host }}/v1"
  CLAUDE_SPECIFIC: "yes"
`)

	// profiles/work.yaml
	writeYAML(t, filepath.Join(dir, "profiles", "work.yaml"), `
volumes:
  - "/work/shared:/shared"
`)

	// agent-profiles/claude_work.yaml
	writeYAML(t, filepath.Join(dir, "agent-profiles", "claude_work.yaml"), `
hostname: "claude-work"
args:
  - "--profile=work"
`)

	return dir
}

func writeYAML(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func TestResolve_FullTierMerge(t *testing.T) {
	configDir := setupConfigDir(t)

	cfg, err := Resolve(configDir, "claude", "work")
	require.NoError(t, err)

	assert.Equal(t, "claude-work", cfg.Hostname, "agent-profile tier wins for hostname")
	assert.Equal(t, "ghcr.io/catthehacker/ubuntu:act-24.04", cfg.Image, "default image preserved")
	assert.Equal(t, "https://default-host:8080/v1", cfg.Env["LLM_ENDPOINT"], "agent tier overrides and templates resolve")
	assert.Equal(t, "yes", cfg.Env["CLAUDE_SPECIFIC"], "agent-specific env carried through")
	assert.Equal(t, []string{"/work/shared:/shared"}, cfg.Volumes, "profile volumes present")
	assert.Equal(t, []string{"--profile=work"}, cfg.Args, "agent-profile args present")
}

func TestResolve_MissingTiers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "ubuntu:24.04"
`)

	cfg, err := Resolve(dir, "nonexistent", "noprofile")
	require.NoError(t, err)

	assert.Equal(t, "ubuntu:24.04", cfg.Image, "default still applies")
}

func TestResolve_EnvVarOverride(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "profiles"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agent-profiles"), 0755))

	writeYAML(t, filepath.Join(dir, "default.yaml"), `
image: "default-image"
hostname: "default-host"
`)

	t.Setenv("AI_SHIM_IMAGE", "env-image")
	t.Setenv("AI_SHIM_VERBOSE", "1")

	cfg, err := Resolve(dir, "test", "test")
	require.NoError(t, err)

	assert.Equal(t, "env-image", cfg.Image, "env var overrides image")
	assert.Equal(t, "default-host", cfg.Hostname, "non-overridden field preserved")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run TestResolve -v`
Expected: FAIL — `Resolve` not defined.

**Step 3: Implement resolver**

Create `internal/config/resolver.go`:
```go
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolve loads, merges, and resolves the full 5-tier config for the given
// agent and profile. configDir is the path to ~/.ai-shim/config/.
func Resolve(configDir, agent, profile string) (Config, error) {
	// Load 4 file tiers
	defaultCfg, err := LoadFile(filepath.Join(configDir, "default.yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading default config: %w", err)
	}

	agentCfg, err := LoadFile(filepath.Join(configDir, "agents", agent+".yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading agent config: %w", err)
	}

	profileCfg, err := LoadFile(filepath.Join(configDir, "profiles", profile+".yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading profile config: %w", err)
	}

	agentProfileCfg, err := LoadFile(filepath.Join(configDir, "agent-profiles", agent+"_"+profile+".yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading agent-profile config: %w", err)
	}

	// Tier 5: environment variable overrides
	envCfg := loadEnvOverrides()

	// Merge all tiers
	merged := MergeAll(defaultCfg, agentCfg, profileCfg, agentProfileCfg, envCfg)

	// Resolve templates
	resolved, err := ResolveTemplates(merged)
	if err != nil {
		return Config{}, fmt.Errorf("resolving templates: %w", err)
	}

	return resolved, nil
}

// loadEnvOverrides reads AI_SHIM_* environment variables and returns a Config
// representing tier 5 overrides.
func loadEnvOverrides() Config {
	var cfg Config

	if v := os.Getenv("AI_SHIM_IMAGE"); v != "" {
		cfg.Image = v
	}
	if v := os.Getenv("AI_SHIM_VERSION"); v != "" {
		cfg.Version = v
	}
	if v := os.Getenv("AI_SHIM_DIND"); v != "" {
		b := v == "1" || v == "true"
		cfg.DIND = &b
	}
	if v := os.Getenv("AI_SHIM_DIND_GPU"); v != "" {
		b := v == "1" || v == "true"
		cfg.DINDGpu = &b
	}
	if v := os.Getenv("AI_SHIM_GPU"); v != "" {
		b := v == "1" || v == "true"
		cfg.GPU = &b
	}

	return cfg
}
```

**Step 4: Run tests**

Run: `go test ./internal/config/ -run TestResolve -v`
Expected: All PASS.

**Step 5: Run all config tests together**

Run: `go test ./internal/config/ -v`
Expected: All PASS.

**Step 6: Commit**

```bash
git add internal/config/resolver.go internal/config/resolver_test.go
git commit -m "feat(config): add 5-tier resolver with env var overrides"
```

---

### Task 6: Wire Up Main Entrypoint (Placeholder)

**Files:**
- Modify: `cmd/ai-shim/main.go`

**Step 1: Update main.go with basic structure**

Replace `cmd/ai-shim/main.go`:
```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

const version = "dev"

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: cannot determine executable path: %v\n", err)
		os.Exit(1)
	}

	name := filepath.Base(exe)

	if name == "ai-shim" || name == "ai-shim.exe" {
		// Direct invocation — management mode
		if err := runManage(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Symlink invocation — agent launch mode
	if err := runAgent(name, os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
		os.Exit(1)
	}
}

func runManage(args []string) error {
	if len(args) == 0 || args[0] == "version" {
		fmt.Printf("ai-shim %s\n", version)
		return nil
	}
	return fmt.Errorf("unknown command: %s", args[0])
}

func runAgent(name string, args []string) error {
	return fmt.Errorf("agent launch not yet implemented (invoked as %q with args %v)", name, args)
}
```

**Step 2: Build and verify**

Run:
```bash
make build
./ai-shim version
```
Expected: Prints `ai-shim dev`.

**Step 3: Commit**

```bash
git add cmd/ai-shim/main.go
git commit -m "feat: wire up main entrypoint with direct vs symlink detection"
```

---

## Phase 1 Complete

After this phase you have:
- Go module with clean project structure
- Conventional commit hook enforcing message format
- Makefile for build/test/lint
- Full config system: YAML loading, 5-tier merge, template resolution
- Env var overrides (`AI_SHIM_*`) as tier 5
- Main entrypoint that detects symlink vs direct invocation
- Comprehensive tests for all config behavior

**Next:** Phase 2 — Storage, Workspace & Symlink Parsing
