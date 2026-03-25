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
)

const (
	GitHubRepo = "ai-shim/ai-shim"
	GitHubAPI  = "https://api.github.com"
)

// Release represents a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset represents a GitHub release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// CheckLatest fetches the latest release tag from GitHub.
func CheckLatest() (string, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", GitHubAPI, GitHubRepo)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("checking for updates: %w", err)
	}
	defer resp.Body.Close()

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
// Used by the update command to find the correct binary to download.
func AssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("ai-shim_%s_%s", os, arch)
}

// FindAssetURL locates the download URL for the current platform in a release.
// Used by the update command to download the correct binary.
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
	// Download to temp file
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("downloading update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	tmpFile, err := os.CreateTemp(filepath.Dir(currentPath), "ai-shim-update-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // cleanup on error

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("writing update: %w", err)
	}
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}

	// Backup current binary
	backupPath := BackupPath(currentPath)
	if err := os.Rename(currentPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Replace with new binary
	if err := os.Rename(tmpPath, currentPath); err != nil {
		// Restore backup on failure
		os.Rename(backupPath, currentPath)
		return fmt.Errorf("replacing binary: %w", err)
	}

	return nil
}

// FetchRelease fetches the full release info for the latest version.
func FetchRelease() (Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", GitHubAPI, GitHubRepo)
	resp, err := http.Get(url)
	if err != nil {
		return Release{}, fmt.Errorf("fetching release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, fmt.Errorf("parsing release info: %w", err)
	}
	return release, nil
}
