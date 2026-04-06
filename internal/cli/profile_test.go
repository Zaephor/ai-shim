package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestSwitchProfile_InvalidName_Unicode(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := SwitchProfile(layout, "日本語")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile name")
}

func TestSwitchProfile_InvalidName_LeadingDash(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := SwitchProfile(layout, "-leading")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile name")
}

func TestSwitchProfile_InvalidName_LeadingDot(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	err := SwitchProfile(layout, ".leading")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile name")
}

func TestSwitchProfile_InvalidName_TooLong(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	long := ""
	for i := 0; i < 64; i++ {
		long += "a"
	}
	err := SwitchProfile(layout, long)
	assert.Error(t, err)
}

func TestSwitchProfile_AllowsDottedName(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	require.NoError(t, SwitchProfile(layout, "my.profile"))
	assert.Equal(t, "my.profile", CurrentProfile(layout))
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

func TestSwitchProfile_RejectsSpecialChars(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)

	invalid := []string{"a b", "a;b", "$(evil)", "a/b", "a\\b", "a@b"}
	for _, name := range invalid {
		err := SwitchProfile(layout, name)
		assert.Error(t, err, "should reject profile name %q", name)
		assert.Contains(t, err.Error(), "invalid profile name")
	}
}

// TestSwitchProfile_ConcurrentAtomicity verifies that many concurrent
// SwitchProfile calls never leave the marker file in a corrupted state.
// The contents must always equal one of the values written, not a partial
// or interleaved mixture. This is a regression test for the atomic
// rename-based write.
func TestSwitchProfile_ConcurrentAtomicity(t *testing.T) {
	root := t.TempDir()
	layout := storage.NewLayout(root)
	require.NoError(t, os.MkdirAll(layout.ConfigDir, 0755))

	const goroutines = 20
	const iterations = 50

	valid := make(map[string]bool, goroutines)
	for g := 0; g < goroutines; g++ {
		valid[fmt.Sprintf("profile%d", g)] = true
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(gid int) {
			defer wg.Done()
			name := fmt.Sprintf("profile%d", gid)
			for i := 0; i < iterations; i++ {
				require.NoError(t, SwitchProfile(layout, name))
			}
		}(g)
	}

	// Concurrently keep reading the marker file. Every read must succeed
	// and observe one of the valid profile names — never an empty string,
	// partial write, or junk.
	stop := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			select {
			case <-stop:
				return
			default:
			}
			data, err := os.ReadFile(filepath.Join(layout.ConfigDir, currentProfileFile))
			if err != nil {
				if os.IsNotExist(err) {
					continue // not yet written by any goroutine
				}
				t.Errorf("read failed: %v", err)
				return
			}
			profile := string(data)
			// Strip trailing newline written by SwitchProfile.
			if len(profile) > 0 && profile[len(profile)-1] == '\n' {
				profile = profile[:len(profile)-1]
			}
			if !valid[profile] {
				t.Errorf("read corrupted/unexpected marker contents: %q", profile)
				return
			}
		}
	}()

	wg.Wait()
	close(stop)
	<-readerDone

	// Final state must be one of the valid values.
	final := CurrentProfile(layout)
	assert.True(t, valid[final], "final profile %q should be one of the written values", final)

	// No leftover temp files should remain in the config dir.
	entries, err := os.ReadDir(layout.ConfigDir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotContains(t, e.Name(), ".current-profile.tmp.",
			"temporary marker file should have been renamed: %s", e.Name())
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
