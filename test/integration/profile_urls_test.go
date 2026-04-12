package integration

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/config"
	"gopkg.in/yaml.v3"
)

func TestProfileExamples_URLsReachable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping URL liveness check in short mode (network-dependent)")
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

	// Collect all unique URLs from all profile files, tracking where each appears.
	type urlEntry struct {
		url      string
		file     string
		toolName string
	}
	seen := make(map[string]bool)
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
			if tool.URL != "" && !seen[tool.URL] {
				seen[tool.URL] = true
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
		Timeout: 15 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Don't follow redirects — we just want the initial status code.
			return http.ErrUseLastResponse
		},
	}

	// Limit concurrency to 5 parallel requests.
	sem := make(chan struct{}, 5)
	var mu sync.Mutex
	var broken []string

	var wg sync.WaitGroup
	for _, entry := range urls {
		wg.Add(1)
		go func(e urlEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			req, reqErr := http.NewRequest(http.MethodHead, e.url, nil)
			if reqErr != nil {
				mu.Lock()
				broken = append(broken, fmt.Sprintf("%s tool %q: invalid URL %q: %v", e.file, e.toolName, e.url, reqErr))
				mu.Unlock()
				return
			}
			req.Header.Set("User-Agent", "ai-shim-ci-url-check/1.0")

			resp, doErr := client.Do(req)
			if doErr != nil {
				mu.Lock()
				broken = append(broken, fmt.Sprintf("%s tool %q: HEAD %q failed: %v", e.file, e.toolName, e.url, doErr))
				mu.Unlock()
				return
			}
			resp.Body.Close()

			switch {
			case resp.StatusCode >= 200 && resp.StatusCode < 400:
				// OK: 2xx success or 3xx redirect.
			default:
				mu.Lock()
				broken = append(broken, fmt.Sprintf("%s tool %q: HEAD %q returned %d", e.file, e.toolName, e.url, resp.StatusCode))
				mu.Unlock()
			}
		}(entry)
	}
	wg.Wait()

	t.Logf("%d/%d URLs reachable", len(urls)-len(broken), len(urls))

	for _, msg := range broken {
		t.Errorf("broken URL: %s", msg)
	}
}
