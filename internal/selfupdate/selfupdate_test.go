package selfupdate

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestDownloadAndReplace_Success(t *testing.T) {
	// Serve a fake binary via httptest
	binaryContent := []byte("#!/bin/sh\necho updated\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryContent)
	}))
	defer srv.Close()

	// Create a fake "current" binary
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim", currentPath)
	require.NoError(t, err)

	// Current path should have the new content
	data, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, data)

	// Backup should exist
	backupPath := BackupPath(currentPath)
	backupData, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), backupData)
}

func TestDownloadAndReplace_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim", currentPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 404")
}

func TestCheckLatest_WithMockServer(t *testing.T) {
	// Override the API URL for testing
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v0.2.0"}`)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	version, err := CheckLatest()
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", version)
}

func TestFetchRelease_WithMockServer(t *testing.T) {
	origAPI := GitHubAPI
	assetName := AssetName()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v0.2.0", "assets": [{"name": "%s", "browser_download_url": "https://example.com/dl"}]}`, assetName)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	release, err := FetchRelease()
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", release.TagName)
	assert.Len(t, release.Assets, 1)
}

func TestDownloadAndReplace_NonExistentCurrentPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("binary"))
	}))
	defer srv.Close()

	// Current path doesn't exist — backup rename should fail
	err := DownloadAndReplace(srv.URL+"/dl", "/nonexistent/path/ai-shim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating temp file")
}

func TestDownloadAndReplace_BackupFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("binary"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	// Don't create currentPath — download succeeds but backup rename fails
	err := DownloadAndReplace(srv.URL+"/dl", currentPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backing up")
}

func TestCheckLatest_ServerError(t *testing.T) {
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	_, err := CheckLatest()
	assert.Error(t, err)
}

func TestCheckLatest_InvalidJSON(t *testing.T) {
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	_, err := CheckLatest()
	assert.Error(t, err)
}

func TestFetchRelease_ServerError(t *testing.T) {
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	_, err := FetchRelease()
	assert.Error(t, err)
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
