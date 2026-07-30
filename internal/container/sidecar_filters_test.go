package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func filterTestSession() *RunningSession {
	return &RunningSession{
		ContainerName: "claude-code-work-abc123-aaaaaaaa",
		AgentName:     "claude-code",
		Profile:       "work",
		WorkspaceHash: "abc123",
	}
}

func TestDINDSessionFilters_ScopedToSession(t *testing.T) {
	f := DINDSessionFilters(filterTestSession())

	assert.Contains(t, f.Get("label"), LabelBase+"=true")
	assert.Contains(t, f.Get("label"), LabelDIND+"=true")
	assert.Contains(t, f.Get("label"), LabelSession+"=claude-code-work-abc123-aaaaaaaa",
		"DIND teardown must match the one session that owns the sidecar")
	assert.Contains(t, f.Get("status"), "running")
}

func TestHolderSessionFilters_ScopedToSession(t *testing.T) {
	f := HolderSessionFilters(filterTestSession())

	assert.Contains(t, f.Get("label"), LabelBase+"=true")
	assert.Contains(t, f.Get("label"), LabelRole+"=netns-holder")
	assert.Contains(t, f.Get("label"), LabelSession+"=claude-code-work-abc123-aaaaaaaa",
		"holder teardown must match the one session that owns the holder")
	assert.Contains(t, f.Get("status"), "running")
}

// Parallel sessions share agent, profile and workspace labels. If either
// filter still carries those, teardown for one session matches every
// sibling's sidecars — the defect this change exists to fix.
func TestSidecarFilters_DoNotUseWorkspaceScoping(t *testing.T) {
	for name, labels := range map[string][]string{
		"dind":   DINDSessionFilters(filterTestSession()).Get("label"),
		"holder": HolderSessionFilters(filterTestSession()).Get("label"),
	} {
		for _, shared := range []string{
			LabelAgent + "=claude-code",
			LabelProfile + "=work",
			LabelWorkspace + "=abc123",
		} {
			assert.NotContains(t, labels, shared,
				"%s filter must not match on %s: parallel siblings share it, so teardown would sweep them too", name, shared)
		}
	}
}
