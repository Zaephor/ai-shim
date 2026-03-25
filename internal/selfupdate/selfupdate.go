package selfupdate

import (
	"encoding/json"
	"fmt"
	"net/http"
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

// AssetName returns the expected asset filename for the current platform.
func AssetName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("ai-shim_%s_%s", os, arch)
}

// FindAssetURL finds the download URL for the current platform in a release.
func FindAssetURL(release Release) (string, error) {
	name := AssetName()
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, name) {
			return asset.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("no release asset found for %s", name)
}
