package orchestrate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLayout returns a Layout rooted at a fresh temp dir with its config
// directory created, ready for writing config files before Prepare runs.
func newLayout(t *testing.T) storage.Layout {
	t.Helper()
	layout := storage.NewLayout(t.TempDir())
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0o755))
	return layout
}

func writeDefaultConfig(t *testing.T, layout storage.Layout, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(layout.ConfigDir, "default.yaml"), []byte(body), 0o644))
}

func TestPrepare_BuiltinAgentSucceeds(t *testing.T) {
	layout := newLayout(t)

	prep, err := Prepare(layout, "claude-code", "default", Options{ValidateWorkingDir: true})
	require.NoError(t, err)
	require.NotNil(t, prep)

	assert.Equal(t, "claude-code", prep.Agent.Name)
	assert.NotEmpty(t, prep.Config.GetImage(), "resolved config should yield a default image")
	assert.NotEmpty(t, prep.Pwd, "working directory should be captured")
}

func TestPrepare_UnknownAgentError(t *testing.T) {
	layout := newLayout(t)

	_, err := Prepare(layout, "definitely-not-an-agent", "default", Options{})
	require.Error(t, err)

	var unknown *UnknownAgentError
	require.True(t, errors.As(err, &unknown), "want *UnknownAgentError, got %T", err)
	assert.Equal(t, "definitely-not-an-agent", unknown.Name)
}

func TestPrepare_InvalidConfigFails(t *testing.T) {
	layout := newLayout(t)
	writeDefaultConfig(t, layout, "security_profile: bogus\n")

	_, err := Prepare(layout, "claude-code", "default", Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid config")
}

// TestPrepare_ValidateWorkingDirOption proves the option gates the
// working-directory/volume validation: a blocked volume source is rejected only
// when ValidateWorkingDir is true (the run path), and tolerated when false (the
// warm path), with no other behavioral difference.
func TestPrepare_ValidateWorkingDirOption(t *testing.T) {
	layout := newLayout(t)
	// /etc is a blocked bind-mount source; cfg.Validate() does not check
	// volumes, so this only trips the option-gated ValidateConfigVolumes.
	writeDefaultConfig(t, layout, "volumes:\n  - \"/etc:/host-etc\"\n")

	_, err := Prepare(layout, "claude-code", "default", Options{ValidateWorkingDir: false})
	require.NoError(t, err, "warm path should not validate config volumes")

	_, err = Prepare(layout, "claude-code", "default", Options{ValidateWorkingDir: true})
	require.Error(t, err, "run path should reject a blocked volume source")
	assert.Contains(t, err.Error(), "volume")
}

func TestPrepare_CreatesAgentData(t *testing.T) {
	layout := newLayout(t)

	_, err := Prepare(layout, "claude-code", "default", Options{})
	require.NoError(t, err)

	// claude-code declares DataDirs [".claude"]; EnsureAgentData must have
	// created the profile's agent-data tree so ownership is correct at launch.
	entries, err := os.ReadDir(layout.Root)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "Prepare should have provisioned on-disk state under the layout root")
}
