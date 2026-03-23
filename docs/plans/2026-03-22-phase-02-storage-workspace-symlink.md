# Phase 2: Storage, Workspace & Symlink Parsing — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement symlink name parsing, storage layout management, and workspace path hashing — all pure functions with no Docker dependency.

**Architecture:** Three independent packages: `workspace` for symlink parsing, `storage` for `~/.ai-shim/` layout management, and `workspace` also handles path hashing. All are consumed by the container lifecycle in Phase 4.

**Tech Stack:** Go 1.24+, `crypto/sha256`, `github.com/stretchr/testify`

---

### Task 1: Symlink Name Parsing

**Files:**
- Create: `internal/invocation/parse.go`
- Create: `internal/invocation/parse_test.go`

**Step 1: Write tests** in `internal/invocation/parse_test.go`:
```go
package invocation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseName_AgentAndProfile(t *testing.T) {
	agent, profile, err := ParseName("claude_work")
	require.NoError(t, err)
	assert.Equal(t, "claude", agent)
	assert.Equal(t, "work", profile)
}

func TestParseName_AgentWithDashes(t *testing.T) {
	agent, profile, err := ParseName("claude-code_work")
	require.NoError(t, err)
	assert.Equal(t, "claude-code", agent)
	assert.Equal(t, "work", profile)
}

func TestParseName_ProfileWithDashes(t *testing.T) {
	agent, profile, err := ParseName("gemini_my-profile")
	require.NoError(t, err)
	assert.Equal(t, "gemini", agent)
	assert.Equal(t, "my-profile", profile)
}

func TestParseName_AgentOnly(t *testing.T) {
	agent, profile, err := ParseName("claude")
	require.NoError(t, err)
	assert.Equal(t, "claude", agent)
	assert.Equal(t, "default", profile, "no underscore means default profile")
}

func TestParseName_MultipleUnderscores(t *testing.T) {
	agent, profile, err := ParseName("claude_work_extra")
	require.NoError(t, err)
	assert.Equal(t, "claude", agent)
	assert.Equal(t, "work_extra", profile, "only first underscore splits")
}

func TestParseName_Empty(t *testing.T) {
	_, _, err := ParseName("")
	assert.Error(t, err)
}
```

**Step 2: Run tests to verify they fail**

**Step 3: Implement** in `internal/invocation/parse.go`:
```go
package invocation

import (
	"fmt"
	"strings"
)

// ParseName splits a symlink name into agent and profile using underscore as
// the delimiter. Only the first underscore is used for splitting — dashes are
// allowed in both agent and profile names.
//
// Examples:
//   - "claude_work"       -> agent="claude",      profile="work"
//   - "claude-code_work"  -> agent="claude-code",  profile="work"
//   - "claude"            -> agent="claude",      profile="default"
//   - "claude_work_extra" -> agent="claude",      profile="work_extra"
func ParseName(name string) (agent, profile string, err error) {
	if name == "" {
		return "", "", fmt.Errorf("empty invocation name")
	}

	idx := strings.IndexByte(name, '_')
	if idx < 0 {
		return name, "default", nil
	}

	agent = name[:idx]
	profile = name[idx+1:]

	if agent == "" {
		return "", "", fmt.Errorf("empty agent name in %q", name)
	}
	if profile == "" {
		return "", "", fmt.Errorf("empty profile name in %q", name)
	}

	return agent, profile, nil
}
```

**Step 4: Run tests, verify all pass**

**Step 5: Commit:**
```bash
git add internal/invocation/
git commit -m "feat(invocation): add symlink name parsing with underscore delimiter"
```

---

### Task 2: Storage Layout Manager

**Files:**
- Create: `internal/storage/paths.go`
- Create: `internal/storage/paths_test.go`

**Step 1: Write tests** in `internal/storage/paths_test.go`:
```go
package storage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)

	assert.Equal(t, root, layout.Root)
	assert.Equal(t, filepath.Join(root, "config"), layout.ConfigDir)
	assert.Equal(t, filepath.Join(root, "shared", "bin"), layout.SharedBin)
	assert.Equal(t, filepath.Join(root, "shared", "cache"), layout.SharedCache)
}

func TestLayout_AgentPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)

	assert.Equal(t, filepath.Join(root, "agents", "claude", "bin"), layout.AgentBin("claude"))
	assert.Equal(t, filepath.Join(root, "agents", "claude", "cache"), layout.AgentCache("claude"))
}

func TestLayout_ProfilePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)

	assert.Equal(t, filepath.Join(root, "profiles", "work", "home"), layout.ProfileHome("work"))
}

func TestLayout_EnsureAll(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".ai-shim")
	layout := NewLayout(root)

	err := layout.EnsureDirectories("claude", "work")
	require.NoError(t, err)

	// Verify directories were created
	for _, dir := range []string{
		layout.ConfigDir,
		filepath.Join(layout.ConfigDir, "agents"),
		filepath.Join(layout.ConfigDir, "profiles"),
		filepath.Join(layout.ConfigDir, "agent-profiles"),
		layout.SharedBin,
		layout.SharedCache,
		layout.AgentBin("claude"),
		layout.AgentCache("claude"),
		layout.ProfileHome("work"),
	} {
		_, err := os.Stat(dir)
		assert.NoError(t, err, "directory should exist: %s", dir)
	}
}

func TestDefaultRoot(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".ai-shim"), DefaultRoot())
}
```

**Step 2: Run tests to verify they fail**

**Step 3: Implement** in `internal/storage/paths.go`:
```go
package storage

import (
	"os"
	"path/filepath"
)

// Layout holds all resolved paths for the ai-shim storage hierarchy.
type Layout struct {
	Root        string
	ConfigDir   string
	SharedBin   string
	SharedCache string
}

// NewLayout creates a Layout rooted at the given path.
func NewLayout(root string) Layout {
	return Layout{
		Root:        root,
		ConfigDir:   filepath.Join(root, "config"),
		SharedBin:   filepath.Join(root, "shared", "bin"),
		SharedCache: filepath.Join(root, "shared", "cache"),
	}
}

// DefaultRoot returns the default storage root (~/.ai-shim).
func DefaultRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-shim")
}

// AgentBin returns the bin directory for an agent's runtime.
func (l Layout) AgentBin(agent string) string {
	return filepath.Join(l.Root, "agents", agent, "bin")
}

// AgentCache returns the cache directory for an agent's install cache.
func (l Layout) AgentCache(agent string) string {
	return filepath.Join(l.Root, "agents", agent, "cache")
}

// ProfileHome returns the home directory for a profile (mounted as /home/<user>).
func (l Layout) ProfileHome(profile string) string {
	return filepath.Join(l.Root, "profiles", profile, "home")
}

// EnsureDirectories creates all required directories for a given agent+profile.
func (l Layout) EnsureDirectories(agent, profile string) error {
	dirs := []string{
		l.ConfigDir,
		filepath.Join(l.ConfigDir, "agents"),
		filepath.Join(l.ConfigDir, "profiles"),
		filepath.Join(l.ConfigDir, "agent-profiles"),
		l.SharedBin,
		l.SharedCache,
		l.AgentBin(agent),
		l.AgentCache(agent),
		l.ProfileHome(profile),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
```

**Step 4: Run tests, verify all pass**

**Step 5: Commit:**
```bash
git add internal/storage/
git commit -m "feat(storage): add storage layout manager with path resolution"
```

---

### Task 3: Workspace Path Hashing

**Files:**
- Create: `internal/workspace/hash.go`
- Create: `internal/workspace/hash_test.go`

**Step 1: Write tests** in `internal/workspace/hash_test.go`:
```go
package workspace

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHashPath_Deterministic(t *testing.T) {
	h1 := HashPath("myhost", "/home/user/projects/myapp")
	h2 := HashPath("myhost", "/home/user/projects/myapp")
	assert.Equal(t, h1, h2, "same inputs should produce same hash")
}

func TestHashPath_Length(t *testing.T) {
	h := HashPath("myhost", "/home/user/projects/myapp")
	assert.Len(t, h, 12, "hash should be 12 characters")
}

func TestHashPath_DifferentPaths(t *testing.T) {
	h1 := HashPath("myhost", "/home/user/project-a")
	h2 := HashPath("myhost", "/home/user/project-b")
	assert.NotEqual(t, h1, h2, "different paths should produce different hashes")
}

func TestHashPath_DifferentHosts(t *testing.T) {
	h1 := HashPath("host-a", "/home/user/project")
	h2 := HashPath("host-b", "/home/user/project")
	assert.NotEqual(t, h1, h2, "different hostnames should produce different hashes")
}

func TestHashPath_HexCharacters(t *testing.T) {
	h := HashPath("myhost", "/some/path")
	for _, c := range h {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"hash should contain only hex characters, got %c", c)
	}
}

func TestContainerWorkdir(t *testing.T) {
	w := ContainerWorkdir("myhost", "/home/user/projects/myapp")
	assert.Contains(t, w, "/workspace/")
	assert.Len(t, w, len("/workspace/")+12)
}
```

**Step 2: Run tests to verify they fail**

**Step 3: Implement** in `internal/workspace/hash.go`:
```go
package workspace

import (
	"crypto/sha256"
	"fmt"
)

// HashPath returns a deterministic 12-character hex hash of the hostname + path.
// The hostname acts as a salt for privacy — prevents reversing the hash to
// recover host filesystem paths.
func HashPath(hostname, absPath string) string {
	h := sha256.Sum256([]byte(hostname + absPath))
	return fmt.Sprintf("%x", h)[:12]
}

// ContainerWorkdir returns the container-side working directory for a host path.
func ContainerWorkdir(hostname, absPath string) string {
	return "/workspace/" + HashPath(hostname, absPath)
}
```

**Step 4: Run tests, verify all pass**

**Step 5: Commit:**
```bash
git add internal/workspace/
git commit -m "feat(workspace): add deterministic path hashing for container workdir"
```

---

## Phase 2 Complete

After this phase you have:
- Symlink name parser (`_` delimiter, dashes allowed, default profile fallback)
- Storage layout manager (`~/.ai-shim/` hierarchy, directory creation)
- Workspace path hasher (SHA256 with hostname salt, 12-char hex)
- All consumed by Phase 4 (Container Lifecycle)

**Next:** Phase 3 — Agent Registry & Install System
