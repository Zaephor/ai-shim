package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSwitchProfile_WritesMarkerFile(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	err := SwitchProfile(layout, "work")
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(layout.ConfigDir, currentProfileFile))
	require.NoError(t, err)
	assert.Equal(t, "work\n", string(data))
}

func TestSwitchProfile_Overwrite(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	require.NoError(t, SwitchProfile(layout, "work"))
	require.NoError(t, SwitchProfile(layout, "personal"))

	profile := CurrentProfile(layout)
	assert.Equal(t, "personal", profile)
}

func TestSwitchProfile_EmptyName(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := SwitchProfile(layout, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

func TestSwitchProfile_InvalidName_Slash(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := SwitchProfile(layout, "../etc")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile name")
}

func TestSwitchProfile_InvalidName_Dot(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := SwitchProfile(layout, "..")
	assert.Error(t, err)
}

func TestSwitchProfile_CreatesConfigDir(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	// Don't pre-create config dir

	err := SwitchProfile(layout, "work")
	require.NoError(t, err)

	// Config dir should have been created
	_, err = os.Stat(layout.ConfigDir)
	require.NoError(t, err)
}

func TestCurrentProfile_Default(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	profile := CurrentProfile(layout)
	assert.Equal(t, "default", profile)
}

func TestCurrentProfile_ReadsMarker(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	require.NoError(t, os.WriteFile(
		filepath.Join(layout.ConfigDir, currentProfileFile),
		[]byte("work\n"),
		0644,
	))

	profile := CurrentProfile(layout)
	assert.Equal(t, "work", profile)
}

func TestCurrentProfile_EmptyFile(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	require.NoError(t, os.WriteFile(
		filepath.Join(layout.ConfigDir, currentProfileFile),
		[]byte(""),
		0644,
	))

	profile := CurrentProfile(layout)
	assert.Equal(t, "default", profile)
}

func TestCurrentProfile_WhitespaceOnly(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	require.NoError(t, os.WriteFile(
		filepath.Join(layout.ConfigDir, currentProfileFile),
		[]byte("  \n  "),
		0644,
	))

	profile := CurrentProfile(layout)
	assert.Equal(t, "default", profile)
}

func TestCurrentProfile_TrimsWhitespace(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	require.NoError(t, os.WriteFile(
		filepath.Join(layout.ConfigDir, currentProfileFile),
		[]byte("  work  \n"),
		0644,
	))

	profile := CurrentProfile(layout)
	assert.Equal(t, "work", profile)
}

func TestIsValidProfileName(t *testing.T) {
	valid := []string{"work", "default", "my-profile", "my_profile", "Work123", "a", "A"}
	for _, name := range valid {
		assert.True(t, isValidProfileName(name), "should accept %q", name)
	}
}

func TestIsValidProfileName_Rejects(t *testing.T) {
	invalid := []string{
		".", "..", "../etc", "a/b", "a\\b",
		"hello world", "a;b", "a$b", "a`b",
		"a(b", "a{b", "a<b", "a|b",
		"name.ext", "a@b", "a+b", "a=b",
	}
	for _, name := range invalid {
		assert.False(t, isValidProfileName(name), "should reject %q", name)
	}
}

func TestSwitchProfile_RejectsSpecialChars(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	invalid := []string{"pro.file", "a b", "a;b", "$(evil)"}
	for _, name := range invalid {
		err := SwitchProfile(layout, name)
		assert.Error(t, err, "should reject profile name %q", name)
		assert.Contains(t, err.Error(), "invalid profile name")
	}
}

func TestSwitchAndCurrentProfile_RoundTrip(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	profiles := []string{"work", "personal", "testing", "default"}
	for _, p := range profiles {
		require.NoError(t, SwitchProfile(layout, p))
		assert.Equal(t, p, CurrentProfile(layout))
	}
}
