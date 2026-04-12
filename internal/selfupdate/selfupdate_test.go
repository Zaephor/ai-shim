package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

// makeTarGz creates a tar.gz archive in memory containing a single file at
// the given path within the archive with the given content.
func makeTarGz(t *testing.T, filePath string, content []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := tw.WriteHeader(&tar.Header{
		Name: filePath,
		Mode: 0755,
		Size: int64(len(content)),
	})
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

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
	// GoReleaser archives at root level: ai-shim_linux_amd64/ai-shim or just ai-shim.
	// The archive name_template produces archives with the binary at root.
	archive := makeTarGz(t, "ai-shim", binaryContent)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath)
	require.NoError(t, err)

	data, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, data)

	backupPath := BackupPath(currentPath)
	backupData, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), backupData)
}

// TestDownloadAndReplace_TarGzExtraction verifies that the binary is correctly
// extracted from a real tar.gz archive (as GoReleaser produces) rather than
// writing the raw archive bytes as the replacement binary.
func TestDownloadAndReplace_TarGzExtraction(t *testing.T) {
	binaryContent := []byte("\x7fELF fake binary content for testing")
	// GoReleaser puts the binary at the root of the archive (no subdirectory).
	archive := makeTarGz(t, "ai-shim", binaryContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old binary"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath)
	require.NoError(t, err)

	got, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, got, "replaced binary should contain the extracted content, not raw tar.gz bytes")
	assert.NotEqual(t, archive, got, "replaced binary must NOT be the raw tar.gz archive")
}

// TestDownloadAndReplace_TarGzWithSubdir verifies extraction works when the
// binary is nested under a subdirectory inside the archive (e.g. ai-shim_linux_amd64/ai-shim).
func TestDownloadAndReplace_TarGzWithSubdir(t *testing.T) {
	binaryContent := []byte("\x7fELF binary in subdir")
	archive := makeTarGz(t, "ai-shim_linux_amd64/ai-shim", binaryContent)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath)
	require.NoError(t, err)

	got, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, got)
}

// TestDownloadAndReplace_TarGzBinaryNotFound verifies a clear error when the
// archive does not contain an "ai-shim" binary.
func TestDownloadAndReplace_TarGzBinaryNotFound(t *testing.T) {
	archive := makeTarGz(t, "README.md", []byte("docs"))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ai-shim")
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
	// The directory /nonexistent/path/ doesn't exist, so CreateTemp fails
	// before we even try to extract the archive.
	archive := makeTarGz(t, "ai-shim", []byte("binary"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	err := DownloadAndReplace(srv.URL+"/dl", "/nonexistent/path/ai-shim")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating temp file")
}

func TestDownloadAndReplace_BackupFails(t *testing.T) {
	// currentPath doesn't exist on disk so os.Rename(currentPath, backupPath) fails.
	archive := makeTarGz(t, "ai-shim", []byte("binary"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
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
