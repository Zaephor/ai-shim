# Phase 3: Agent Registry & Install System — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Define built-in agent definitions and implement the install system (npm, uv, binary, custom install types) with version pinning and cache-aware logic.

**Architecture:** The `agent` package defines the built-in registry as Go structs. The `install` package provides installer implementations per type. Install logic generates entrypoint scripts that run inside the container — ai-shim doesn't install agents on the host.

**Tech Stack:** Go 1.24+, `github.com/stretchr/testify`

---

### Task 1: Agent Registry

**Files:**
- Create: `internal/agent/registry.go`
- Create: `internal/agent/registry_test.go`

**agent/registry.go:**
```go
package agent

// Definition describes a built-in agent.
type Definition struct {
	Name        string
	InstallType string   // "npm", "uv", "binary", "custom"
	Package     string   // package name or install command
	Binary      string   // resulting binary name
	HomePaths   []string // config paths in home dir (e.g. ".claude", ".claude.json")
}

// builtins is the built-in agent registry.
var builtins = map[string]Definition{
	"claude-code": {
		Name:        "claude-code",
		InstallType: "custom",
		Package:     "curl -fsSL https://claude.ai/install.sh | bash",
		Binary:      "claude",
		HomePaths:   []string{".claude", ".claude.json"},
	},
	"gemini-cli": {
		Name:        "gemini-cli",
		InstallType: "npm",
		Package:     "@google/gemini-cli",
		Binary:      "gemini",
		HomePaths:   []string{".gemini"},
	},
	"qwen-code": {
		Name:        "qwen-code",
		InstallType: "npm",
		Package:     "@qwen-code/qwen-code",
		Binary:      "qwen",
		HomePaths:   []string{".qwen"},
	},
	"codex": {
		Name:        "codex",
		InstallType: "npm",
		Package:     "@openai/codex",
		Binary:      "codex",
		HomePaths:   []string{".codex"},
	},
	"pi": {
		Name:        "pi",
		InstallType: "npm",
		Package:     "@mariozechner/pi-coding-agent",
		Binary:      "pi",
		HomePaths:   []string{".pi"},
	},
	"gsd": {
		Name:        "gsd",
		InstallType: "npm",
		Package:     "gsd-pi",
		Binary:      "gsd",
		HomePaths:   []string{".gsd"},
	},
	"aider": {
		Name:        "aider",
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
		HomePaths:   []string{".aider"},
	},
	"goose": {
		Name:        "goose",
		InstallType: "custom",
		Package:     "curl -fsSL https://github.com/block/goose/releases/download/stable/download_cli.sh | bash",
		Binary:      "goose",
		HomePaths:   []string{".config/goose"},
	},
}

// Lookup returns the definition for a named agent, or false if not found.
func Lookup(name string) (Definition, bool) {
	def, ok := builtins[name]
	return def, ok
}

// All returns all built-in agent definitions.
func All() map[string]Definition {
	result := make(map[string]Definition, len(builtins))
	for k, v := range builtins {
		result[k] = v
	}
	return result
}

// Names returns all built-in agent names sorted.
func Names() []string {
	names := make([]string, 0, len(builtins))
	for k := range builtins {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
```

**agent/registry_test.go:**
```go
package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLookup_BuiltinAgent(t *testing.T) {
	def, ok := Lookup("claude-code")
	require.True(t, ok)
	assert.Equal(t, "claude", def.Binary)
	assert.Equal(t, "custom", def.InstallType)
	assert.Contains(t, def.HomePaths, ".claude")
}

func TestLookup_NotFound(t *testing.T) {
	_, ok := Lookup("nonexistent")
	assert.False(t, ok)
}

func TestAll_ContainsAllAgents(t *testing.T) {
	all := All()
	expectedAgents := []string{"claude-code", "gemini-cli", "qwen-code", "codex", "pi", "gsd", "aider", "goose"}
	for _, name := range expectedAgents {
		_, ok := all[name]
		assert.True(t, ok, "missing agent: %s", name)
	}
}

func TestAll_ReturnsCopy(t *testing.T) {
	all := All()
	all["test"] = Definition{Name: "test"}
	_, ok := Lookup("test")
	assert.False(t, ok, "modifying All() result should not affect registry")
}

func TestNames_Sorted(t *testing.T) {
	names := Names()
	assert.Equal(t, len(All()), len(names))
	for i := 1; i < len(names); i++ {
		assert.True(t, names[i-1] < names[i], "names should be sorted")
	}
}

func TestInstallTypes(t *testing.T) {
	tests := []struct {
		agent       string
		installType string
	}{
		{"claude-code", "custom"},
		{"gemini-cli", "npm"},
		{"aider", "uv"},
		{"goose", "custom"},
	}
	for _, tt := range tests {
		def, ok := Lookup(tt.agent)
		require.True(t, ok, "agent %s should exist", tt.agent)
		assert.Equal(t, tt.installType, def.InstallType, "agent %s", tt.agent)
	}
}
```

Follow TDD. Commit as: `feat(agent): add built-in agent registry with 8 agents`

---

### Task 2: Install Script Generator

**Files:**
- Create: `internal/install/entrypoint.go`
- Create: `internal/install/entrypoint_test.go`

This generates the entrypoint shell script that runs inside the container to install/update and launch the agent.

**install/entrypoint.go:**
```go
package install

import (
	"fmt"
	"strings"
)

// EntrypointParams holds parameters for generating a container entrypoint script.
type EntrypointParams struct {
	InstallType string
	Package     string
	Binary      string
	Version     string   // empty = latest
	AgentArgs   []string // default args + passthrough args
}

// GenerateEntrypoint creates a shell script that installs/updates the agent
// and then executes it with the given args.
func GenerateEntrypoint(p EntrypointParams) string {
	var b strings.Builder
	b.WriteString("#!/bin/sh\nset -e\n\n")

	switch p.InstallType {
	case "npm":
		b.WriteString(generateNPMInstall(p))
	case "uv":
		b.WriteString(generateUVInstall(p))
	case "custom":
		b.WriteString(generateCustomInstall(p))
	}

	b.WriteString(fmt.Sprintf("\nexec %s", p.Binary))
	for _, arg := range p.AgentArgs {
		b.WriteString(fmt.Sprintf(" %s", shellQuote(arg)))
	}
	b.WriteString("\n")

	return b.String()
}

func generateNPMInstall(p EntrypointParams) string {
	pkg := p.Package
	if p.Version != "" {
		pkg = fmt.Sprintf("%s@%s", p.Package, p.Version)
	}
	return fmt.Sprintf("npm install -g %s 2>/dev/null\n", pkg)
}

func generateUVInstall(p EntrypointParams) string {
	pkg := p.Package
	if p.Version != "" {
		pkg = fmt.Sprintf("%s==%s", p.Package, p.Version)
	}
	return fmt.Sprintf("uv tool install %s 2>/dev/null || uv tool upgrade %s 2>/dev/null\n", pkg, p.Package)
}

func generateCustomInstall(p EntrypointParams) string {
	return p.Package + "\n"
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\"'\\$`!#&|;(){}[]<>?*~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
```

**install/entrypoint_test.go:**
```go
package install

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateEntrypoint_NPM(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "@google/gemini-cli",
		Binary:      "gemini",
		AgentArgs:   []string{"--verbose"},
	})
	assert.Contains(t, script, "npm install -g @google/gemini-cli")
	assert.Contains(t, script, "exec gemini --verbose")
}

func TestGenerateEntrypoint_NPMWithVersion(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "@google/gemini-cli",
		Binary:      "gemini",
		Version:     "1.2.3",
	})
	assert.Contains(t, script, "npm install -g @google/gemini-cli@1.2.3")
}

func TestGenerateEntrypoint_UV(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
	})
	assert.Contains(t, script, "uv tool install aider-chat")
	assert.Contains(t, script, "exec aider")
}

func TestGenerateEntrypoint_UVWithVersion(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "uv",
		Package:     "aider-chat",
		Binary:      "aider",
		Version:     "0.50.0",
	})
	assert.Contains(t, script, "uv tool install aider-chat==0.50.0")
}

func TestGenerateEntrypoint_Custom(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "custom",
		Package:     "curl -fsSL https://claude.ai/install.sh | bash",
		Binary:      "claude",
		AgentArgs:   []string{"--dangerously-skip-permissions"},
	})
	assert.Contains(t, script, "curl -fsSL https://claude.ai/install.sh | bash")
	assert.Contains(t, script, "exec claude --dangerously-skip-permissions")
}

func TestGenerateEntrypoint_ShellQuoting(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test-pkg",
		Binary:      "test",
		AgentArgs:   []string{"--msg", "hello world"},
	})
	assert.Contains(t, script, "'hello world'")
}

func TestGenerateEntrypoint_StartsWithShebang(t *testing.T) {
	script := GenerateEntrypoint(EntrypointParams{
		InstallType: "npm",
		Package:     "test",
		Binary:      "test",
	})
	assert.True(t, len(script) > 0 && script[:11] == "#!/bin/sh\n")
}
```

Follow TDD. Commit as: `feat(install): add entrypoint script generator for agent install/launch`

---

## Phase 3 Complete

After this phase you have:
- Built-in registry of 8 agents with install metadata
- Entrypoint script generator for npm, uv, and custom install types
- Version pinning support in install scripts
- Shell quoting for agent args

**Next:** Phase 4 — Container Lifecycle (Core Launch)
