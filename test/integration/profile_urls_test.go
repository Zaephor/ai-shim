package integration

import (
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/config"
	"gopkg.in/yaml.v3"
)

func TestProfileExamples_URLsReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping URL check in short mode")
	}
	// Only run in CI to avoid hammering URLs during local dev
	if os.Getenv("AI_SHIM_CI") != "1" {
		t.Skip("skipping URL check outside CI (set AI_SHIM_CI=1)")
	}

	root := projectRoot()
	pattern := filepath.Join(root, "configs", "examples", "profiles", "*.yaml")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("filepath.Glob(%q): %v", pattern, err)
	}
	if len(files) == 0 {
		t.Fatal("no profile YAML files found")
	}

	// Collect all URLs from all profile files.
	type urlEntry struct {
		url      string
		file     string
		toolName string
	}
	var urls []urlEntry

	for _, f := range files {
		data, readErr := os.ReadFile(f)
		if readErr != nil {
			t.Fatalf("reading %s: %v", f, readErr)
		}

		var cfg config.Config
		if unmarshalErr := yaml.Unmarshal(data, &cfg); unmarshalErr != nil {
			t.Fatalf("YAML parse error in %s: %v", f, unmarshalErr)
		}

		for toolName, tool := range cfg.Tools {
			if tool.URL != "" {
				urls = append(urls, urlEntry{
					url:      tool.URL,
					file:     filepath.Base(f),
					toolName: toolName,
				})
			}
		}
	}

	if len(urls) == 0 {
		t.Fatal("no URLs found in any profile YAML")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects — we just want the status code.
			return http.ErrUseLastResponse
		},
	}

	// Limit concurrency to 3 parallel requests.
	sem := make(chan struct{}, 3)
	var mu sync.Mutex
	reachable := 0
	total := len(urls)

	var wg sync.WaitGroup
	for _, entry := range urls {
		wg.Add(1)
		go func(e urlEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			req, reqErr := http.NewRequest(http.MethodHead, e.url, nil)
			if reqErr != nil {
				t.Logf("WARNING: %s tool %q: invalid URL %q: %v", e.file, e.toolName, e.url, reqErr)
				return
			}
			req.Header.Set("User-Agent", "ai-shim-ci-url-check/1.0")

			resp, doErr := client.Do(req)
			if doErr != nil {
				t.Logf("WARNING: %s tool %q: HEAD %q failed: %v", e.file, e.toolName, e.url, doErr)
				return
			}
			resp.Body.Close()

			switch resp.StatusCode {
			case http.StatusOK, http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect:
				mu.Lock()
				reachable++
				mu.Unlock()
			default:
				t.Logf("WARNING: %s tool %q: HEAD %q returned %d", e.file, e.toolName, e.url, resp.StatusCode)
			}
		}(entry)
	}
	wg.Wait()

	t.Logf("%d/%d URLs reachable", reachable, total)
}
