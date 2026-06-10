package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// sha256Hex returns the lowercase hex SHA256 of b, matching the digests in a
// GoReleaser checksums.txt manifest.
func sha256Hex(b []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(b))
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
	// A valid-format checksum gets past the initial guard so the failure is the
	// unreachable host, not the checksum check.
	err := DownloadAndReplace(context.Background(), "http://invalid.example.com/nonexistent", "/tmp/fake-binary", strings.Repeat("a", 64))
	assert.Error(t, err, "should error on invalid download URL")
}

func TestDownloadAndReplace_EmptyChecksumRejected(t *testing.T) {
	err := DownloadAndReplace(context.Background(), "http://example.com/dl", "/tmp/fake-binary", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum")
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

	err := DownloadAndReplace(context.Background(), srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath, sha256Hex(archive))
	require.NoError(t, err)

	data, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, data)

	backupPath := BackupPath(currentPath)
	backupData, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), backupData)
}

// TestDownloadAndReplace_ChecksumMismatch verifies a tampered/wrong-checksum
// download is rejected before any filesystem swap: the current binary is left
// intact and no backup is created.
func TestDownloadAndReplace_ChecksumMismatch(t *testing.T) {
	archive := makeTarGz(t, "ai-shim", []byte("malicious replacement"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	dir := t.TempDir()
	currentPath := filepath.Join(dir, "ai-shim")
	require.NoError(t, os.WriteFile(currentPath, []byte("old"), 0755))

	err := DownloadAndReplace(context.Background(), srv.URL+"/dl", currentPath, strings.Repeat("b", 64))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksum mismatch")

	data, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, []byte("old"), data, "binary must be untouched on checksum failure")

	_, statErr := os.Stat(BackupPath(currentPath))
	assert.True(t, os.IsNotExist(statErr), "no backup should be created when verification fails")
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

	err := DownloadAndReplace(context.Background(), srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath, sha256Hex(archive))
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

	err := DownloadAndReplace(context.Background(), srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath, sha256Hex(archive))
	require.NoError(t, err)

	got, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, binaryContent, got)
}

// TestDownloadAndReplace_TarGzBinaryNotFound verifies a clear error when the
// archive does not contain an "ai-shim" binary. The checksum matches so the
// failure is genuinely the missing binary, not verification.
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

	err := DownloadAndReplace(context.Background(), srv.URL+"/ai-shim_linux_amd64.tar.gz", currentPath, sha256Hex(archive))
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

	err := DownloadAndReplace(context.Background(), srv.URL+"/ai-shim", currentPath, strings.Repeat("a", 64))
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

	version, err := CheckLatest(context.Background(), Options{})
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

	release, err := FetchRelease(context.Background(), Options{})
	require.NoError(t, err)
	assert.Equal(t, "v0.2.0", release.TagName)
	assert.Len(t, release.Assets, 1)
}

func TestFindChecksumsURL(t *testing.T) {
	release := Release{
		Assets: []Asset{
			{Name: "ai-shim_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/dl"},
			{Name: "checksums.txt", BrowserDownloadURL: "https://example.com/checksums.txt"},
		},
	}
	url, err := FindChecksumsURL(release)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/checksums.txt", url)
}

func TestFindChecksumsURL_Missing(t *testing.T) {
	release := Release{
		Assets: []Asset{
			{Name: "ai-shim_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.com/dl"},
		},
	}
	_, err := FindChecksumsURL(release)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "checksums.txt")
}

func TestFetchExpectedChecksum(t *testing.T) {
	want := strings.Repeat("a", 64)
	other := strings.Repeat("b", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  ai-shim_linux_amd64.tar.gz\n%s  ai-shim_darwin_arm64.tar.gz\n", want, other)
	}))
	defer srv.Close()

	sum, err := FetchExpectedChecksum(context.Background(), srv.URL+"/checksums.txt", "ai-shim_linux_amd64.tar.gz")
	require.NoError(t, err)
	assert.Equal(t, want, sum)
}

func TestFetchExpectedChecksum_MissingEntry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  some-other-file.tar.gz\n", strings.Repeat("a", 64))
	}))
	defer srv.Close()

	_, err := FetchExpectedChecksum(context.Background(), srv.URL+"/checksums.txt", "ai-shim_linux_amd64.tar.gz")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no checksum entry")
}

func TestDownloadAndReplace_NonExistentCurrentPath(t *testing.T) {
	// The directory /nonexistent/path/ doesn't exist, so the archive temp file
	// cannot be created (it is placed alongside currentPath).
	archive := makeTarGz(t, "ai-shim", []byte("binary"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		w.Write(archive)
	}))
	defer srv.Close()

	err := DownloadAndReplace(context.Background(), srv.URL+"/dl", "/nonexistent/path/ai-shim", sha256Hex(archive))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "creating temp archive")
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
	err := DownloadAndReplace(context.Background(), srv.URL+"/dl", currentPath, sha256Hex(archive))
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

	_, err := CheckLatest(context.Background(), Options{})
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

	_, err := CheckLatest(context.Background(), Options{})
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

	_, err := FetchRelease(context.Background(), Options{})
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
	tag, err := CheckLatest(context.Background(), Options{Prerelease: true})
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
	tag, err := CheckLatest(context.Background(), Options{Prerelease: false})
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

	release, err := FetchRelease(context.Background(), Options{Prerelease: true})
	require.NoError(t, err)
	assert.Equal(t, "v0.3.0-rc1", release.TagName)
	assert.True(t, release.Prerelease)
}
