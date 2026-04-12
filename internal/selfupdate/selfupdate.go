package selfupdate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// httpClient is the shared HTTP client with a timeout for all GitHub API and
// download requests. Prevents indefinite hangs on slow/unresponsive servers.
var httpClient = &http.Client{Timeout: 30 * time.Second}

const (
	// DefaultRepository is the GitHub owner/repo checked for releases when
	// no override is configured.
	DefaultRepository = "Zaephor/ai-shim"
	// DefaultAPIURL is the base URL for the GitHub REST API.
	DefaultAPIURL = "https://api.github.com"
)

// GitHubAPI is overridable in tests so httptest servers can intercept
// requests without touching Options. Production code should use
// Options.apiURL() which falls through to this value.
var GitHubAPI = DefaultAPIURL

// Options configures how the self-update functions query GitHub.
// Zero-value fields fall back to package defaults.
type Options struct {
	Repository string // default: DefaultRepository
	APIURL     string // default: DefaultAPIURL (tests may override via GitHubAPI var)
	Prerelease bool   // default: false — only stable releases
}

func (o Options) apiURL() string {
	if o.APIURL != "" {
		return o.APIURL
	}
	return GitHubAPI
}

func (o Options) repository() string {
	if o.Repository != "" {
		return o.Repository
	}
	return DefaultRepository
}

// Release represents a GitHub release.
type Release struct {
	TagName    string  `json:"tag_name"`
	Prerelease bool    `json:"prerelease"`
	Assets     []Asset `json:"assets"`
}

// Asset represents a GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckLatest fetches the latest release tag from GitHub. When
// opts.Prerelease is true it considers pre-release versions; otherwise
// only stable releases are returned.
func CheckLatest(opts Options) (string, error) {
	if opts.Prerelease {
		releases, err := fetchReleases(opts)
		if err != nil {
			return "", err
		}
		if len(releases) == 0 {
			return "", fmt.Errorf("no releases found in %s", opts.repository())
		}
		return releases[0].TagName, nil
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", opts.apiURL(), opts.repository())
	resp, err := httpClient.Get(url)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("parsing release info: %w", err)
	}

	return release.TagName, nil
}

// NeedsUpdate compares current version against latest.
func NeedsUpdate(current, latest string) bool {
	current = strings.TrimPrefix(current, "v")
	latest = strings.TrimPrefix(latest, "v")
	return current != latest && current != "dev"
}

// AssetName returns the expected release asset filename for the current platform.
func AssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("ai-shim_%s_%s", os, arch)
}

// FindAssetURL locates the download URL for the current platform in a release.
func FindAssetURL(release Release) (string, error) {
	name := AssetName()
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, name) {
			return asset.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("no release asset found for %s", name)
}

// BackupPath returns the backup file path for the current binary.
func BackupPath(currentPath string) string {
	return currentPath + ".bak"
}

// DownloadAndReplace downloads a binary from url and replaces the file at currentPath.
// Creates a backup at currentPath.bak before replacing.
func DownloadAndReplace(url, currentPath string) error {
	resp, err := httpClient.Get(url)
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(currentPath), "ai-shim-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }() // cleanup on error

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("writing update: %w", err)
	}
	_ = tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	backupPath := BackupPath(currentPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	if err := os.Rename(tmpPath, currentPath); err != nil {
		_ = os.Rename(backupPath, currentPath)
		return fmt.Errorf("replacing binary: %w", err)
	}

	return nil
}

// FetchRelease fetches the full release info for the latest version.
// Respects opts.Prerelease: when true, the newest release (including
// pre-releases) is returned.
func FetchRelease(opts Options) (Release, error) {
	if opts.Prerelease {
		releases, err := fetchReleases(opts)
		if err != nil {
			return Release{}, err
		}
		if len(releases) == 0 {
			return Release{}, fmt.Errorf("no releases found in %s", opts.repository())
		}
		return releases[0], nil
	}

	url := fmt.Sprintf("%s/repos/%s/releases/latest", opts.apiURL(), opts.repository())
	resp, err := httpClient.Get(url)
	if err != nil {
		return Release{}, fmt.Errorf("fetching release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("parsing release info: %w", err)
	}
	return release, nil
}

// fetchReleases returns the first page of releases (newest first, including
// pre-releases). Used when Options.Prerelease is true.
func fetchReleases(opts Options) ([]Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=10", opts.apiURL(), opts.repository())
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("listing releases: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("parsing releases: %w", err)
	}
	return releases, nil
}
