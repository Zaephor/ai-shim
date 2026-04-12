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
	binaryContent := []byte("#!/bin/sh\necho updated\n")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(binaryContent)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim", currentPath)
	require.NoError(t, err)

	data, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, data)

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
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"tag_name": "v0.2.0"}`)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	version, err := CheckLatest(Options{})
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

	release, err := FetchRelease(Options{})
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", release.TagName)
	assert.Len(t, release.Assets, 1)
}

func TestDownloadAndReplace_NonExistentCurrentPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("binary"))
	}))
	defer srv.Close()

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

	_, err := CheckLatest(Options{})
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

	_, err := CheckLatest(Options{})
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

	_, err := FetchRelease(Options{})
	assert.Error(t, err)
}

func TestFindAssetURL_NotFound(t *testing.T) {
	release := Release{
		Assets: []Asset{
			{Name: "ai-shim_windows_amd64.exe", BrowserDownloadURL: "https://example.com/win"},
		},
	}
	if runtime.GOOS == "windows" {
		url, err := FindAssetURL(release)
		assert.NoError(t, err)
		assert.NotEmpty(t, url)
	} else {
		_, err := FindAssetURL(release)
		assert.Error(t, err)
	}
}

func TestDefaultRepository(t *testing.T) {
	assert.Equal(t, "Zaephor/ai-shim", DefaultRepository,
		"default repository should point at the real project, not a placeholder")
}

func TestOptions_Defaults(t *testing.T) {
	var opts Options
	assert.Equal(t, DefaultRepository, opts.repository())
	assert.Equal(t, GitHubAPI, opts.apiURL())
}

func TestOptions_Overrides(t *testing.T) {
	opts := Options{
		Repository: "myorg/myfork",
		APIURL:     "https://ghe.example.com/api/v3",
	}
	assert.Equal(t, "myorg/myfork", opts.repository())
	assert.Equal(t, "https://ghe.example.com/api/v3", opts.apiURL())
}

func TestCheckLatest_Prerelease(t *testing.T) {
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /releases endpoint returns an array, newest first.
		fmt.Fprintf(w, `[
			{"tag_name": "v0.3.0-rc1", "prerelease": true},
			{"tag_name": "v0.2.0", "prerelease": false}
		]`)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	// With prerelease=true, the newest (pre-release) tag is returned.
	tag, err := CheckLatest(Options{Prerelease: true})
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0-rc1", tag)
}

func TestCheckLatest_NonPrerelease_UsesLatestEndpoint(t *testing.T) {
	origAPI := GitHubAPI
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The /releases/latest endpoint returns a single object.
		fmt.Fprintf(w, `{"tag_name": "v0.2.0"}`)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	// With prerelease=false (default), uses /releases/latest.
	tag, err := CheckLatest(Options{Prerelease: false})
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", tag)
}

func TestFetchRelease_Prerelease(t *testing.T) {
	origAPI := GitHubAPI
	assetName := AssetName()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `[
			{"tag_name": "v0.3.0-rc1", "prerelease": true, "assets": [
				{"name": "%s.tar.gz", "browser_download_url": "https://example.com/rc1"}
			]},
			{"tag_name": "v0.2.0", "prerelease": false, "assets": []}
		]`, assetName)
	}))
	defer srv.Close()

	GitHubAPI = srv.URL
	defer func() { GitHubAPI = origAPI }()

	release, err := FetchRelease(Options{Prerelease: true})
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0-rc1", release.TagName)
	assert.True(t, release.Prerelease)
}
