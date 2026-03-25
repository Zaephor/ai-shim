package selfupdate

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNeedsUpdate_DifferentVersions(t *testing.T) {
	assert.True(t, NeedsUpdate("1.0.0", "1.1.0"))
	assert.True(t, NeedsUpdate("v1.0.0", "v1.1.0"))
}

func TestNeedsUpdate_SameVersion(t *testing.T) {
	assert.False(t, NeedsUpdate("1.0.0", "1.0.0"))
	assert.False(t, NeedsUpdate("v1.0.0", "v1.0.0"))
}

func TestNeedsUpdate_DevVersion(t *testing.T) {
	assert.False(t, NeedsUpdate("dev", "1.0.0"), "dev builds should never auto-update")
}

func TestNeedsUpdate_MixedPrefix(t *testing.T) {
	assert.False(t, NeedsUpdate("v1.0.0", "1.0.0"), "v prefix should be normalized")
}

func TestAssetName_Format(t *testing.T) {
	name := AssetName()
	assert.Contains(t, name, runtime.GOOS)
	assert.Contains(t, name, runtime.GOARCH)
	assert.Contains(t, name, "ai-shim")
}

func TestFindAssetURL_Found(t *testing.T) {
	release := Release{
		Assets: []Asset{
			{Name: "ai-shim_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux_amd64"},
			{Name: "ai-shim_darwin_arm64.tar.gz", BrowserDownloadURL: "https://example.com/darwin_arm64"},
		},
	}
	url, err := FindAssetURL(release)
	assert.NoError(t, err)
	assert.NotEmpty(t, url)
}

func TestDownloadAndReplace_InvalidURL(t *testing.T) {
	err := DownloadAndReplace("http://invalid.example.com/nonexistent", "/tmp/fake-binary")
	assert.Error(t, err, "should error on invalid download URL")
}

func TestBackupPath(t *testing.T) {
	path := BackupPath("/usr/local/bin/ai-shim")
	assert.Equal(t, "/usr/local/bin/ai-shim.bak", path)
}

func TestFindAssetURL_NotFound(t *testing.T) {
	release := Release{
		Assets: []Asset{
			{Name: "ai-shim_windows_amd64.exe", BrowserDownloadURL: "https://example.com/win"},
		},
	}
	// This might pass or fail depending on runtime.GOOS
	if runtime.GOOS == "windows" {
		url, err := FindAssetURL(release)
		assert.NoError(t, err)
		assert.NotEmpty(t, url)
	} else {
		_, err := FindAssetURL(release)
		assert.Error(t, err)
	}
}
