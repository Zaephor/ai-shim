package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDINDCachePullThrough verifies that DIND's pull-through cache works
// end-to-end: the registry:2 cache container is started, DIND is configured
// to use it as a mirror, an image is pulled through DIND, and we verify:
//
//   - The host-side cache directory contains registry layer files (_manifests, _layers)
//   - The registry's /v2/_catalog API lists the pulled repository (proving
//     the pull actually went through the cache proxy, not directly to Docker Hub)
//
// This guards the dind.EnsureCache + dind.Start(cacheAddr=...) plumbing and
// ensures the host-side bind mount at /var/lib/registry captures pulled layers.
func TestDINDCachePullThrough(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	// The pull-through registry mirror reaches the cache via host-gateway
	// routing inside the DIND sidecar. That routing is unreliable when the
	// test host is itself a container (nested DIND), so skip there — same
	// rationale the TLS DIND tests use.
	testutil.SkipIfNestedDocker(t)
	if testing.Short() {
		t.Skip("skipping slow DIND pull-through cache test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err, "creating container runner")
	t.Cleanup(func() { runner.Close() })

	// Host-side cache directory bind-mounted into registry:2 at /var/lib/registry.
	cacheDir := dockerTempDir(t)

	// Remove any pre-existing cache container to avoid conflicts from prior runs.
	_ = runner.Client().ContainerRemove(ctx, dind.CacheContainerName, dockercontainer.RemoveOptions{Force: true})

	// Start the pull-through registry cache.
	cacheAddr, err := dind.EnsureCache(ctx, runner, cacheDir, "test")
	require.NoError(t, err, "starting pull-through cache")
	// EnsureCache creates the cache under a fixed name with an UnlessStopped
	// restart policy, so a later failure in this test (e.g. the DIND sidecar
	// not coming up) would otherwise leak it and name-conflict the next test
	// that uses the cache. Always force-remove it on teardown.
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = runner.Client().ContainerRemove(cleanupCtx, dind.CacheContainerName, dockercontainer.RemoveOptions{Force: true})
	})
	require.Contains(t, cacheAddr, dind.CacheHostAlias,
		"cache address must use the %s hostname", dind.CacheHostAlias)
	t.Logf("cache address: %s", cacheAddr)

	labels := map[string]string{container.LabelBase: "true"}
	uniqueName := fmt.Sprintf("ai-shim-test-dind-cache-%d", time.Now().UnixNano())

	// Create an isolated network for the DIND sidecar.
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), uniqueName, labels)
	require.NoError(t, err, "creating test network")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = netHandle.Remove(cleanupCtx)
	})

	// Start the DIND sidecar configured to use the cache as a registry mirror.
	sidecar, err := dind.Start(ctx, runner, dind.Config{
		CacheAddr:     cacheAddr,
		NetworkID:     netHandle.ID,
		ContainerName: uniqueName,
		Hostname:      "dind-cache-test",
		Labels:        labels,
	})
	require.NoError(t, err, "starting DIND sidecar with cache")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sidecar.Stop(cleanupCtx)
	})

	// Verify the cache mirror appears in dockerd configuration.
	exitCode, stdout, stderr := execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"docker", "info"})
	require.Equal(t, 0, exitCode, "docker info must succeed; stderr=%q", stderr)
	assert.Contains(t, string(stdout), "Registry Mirrors",
		"docker info must list Registry Mirrors section")
	t.Logf("docker info output (mirrors section):\n%s", stdout)

	// Pull alpine:latest inside DIND — this goes through the cache mirror.
	exitCode, stdout, stderr = execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"docker", "pull", "alpine:latest"})
	require.Equal(t, 0, exitCode,
		"docker pull alpine:latest inside DIND must succeed; stdout=%q stderr=%q", stdout, stderr)
	t.Logf("first pull output: %s", stdout)

	// --- Assertion 1: Registry API proves the cache proxied the image ---
	//
	// The registry:2 container exposes a /v2/_catalog endpoint listing all
	// repositories it has cached. If alpine was pulled *through* the cache
	// (not directly from Docker Hub), it will appear in this catalog.
	// The cache runs on host network, so it's reachable at localhost:5000
	// from the test process (which also runs on the host).
	assertRegistryHasRepo(t, "library/alpine", 10*time.Second,
		"cache registry must list library/alpine after pull — if missing, the pull went directly to Docker Hub, bypassing the cache mirror")

	// --- Assertion 2: Host-side cache directory contains layer files ---
	//
	// Poll the host-side cacheDir for registry layer files with a 10s timeout.
	// The registry writes asynchronously, so we loop until the expected
	// directory structure appears or the timeout expires.
	var manifestCount, layerCount int
	pollDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(pollDeadline) {
		manifestCount, layerCount = 0, 0
		repoRoot := filepath.Join(cacheDir, "docker", "registry", "v2", "repositories")
		filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			switch filepath.Base(path) {
			case "_manifests":
				manifestCount++
			case "_layers":
				layerCount++
			}
			return nil
		})
		if manifestCount >= 1 && layerCount >= 1 {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	assert.GreaterOrEqual(t, manifestCount, 1,
		"host-side cache must contain at least one _manifests directory; cacheDir contents may be incomplete")
	assert.GreaterOrEqual(t, layerCount, 1,
		"host-side cache must contain at least one _layers directory; cacheDir contents may be incomplete")

	if manifestCount == 0 || layerCount == 0 {
		// Log cache directory contents for diagnosis on failure.
		t.Logf("cache directory walk (expected _manifests and _layers):")
		filepath.WalkDir(cacheDir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(cacheDir, path)
			t.Logf("  %s", rel)
			return nil
		})
	}

	// Pull alpine:latest a second time — proves the cached path works,
	// not just the first direct pull.
	exitCode, stdout, stderr = execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"docker", "pull", "alpine:latest"})
	require.Equal(t, 0, exitCode,
		"second docker pull alpine:latest inside DIND must succeed; stdout=%q stderr=%q", stdout, stderr)
	t.Logf("second pull output: %s", stdout)
}

// assertRegistryHasRepo polls the registry cache's /v2/_catalog endpoint
// until the expected repository appears or the timeout expires.
// The registry cache runs on the host network at port 5000, so we query
// localhost:5000 from the test process.
func assertRegistryHasRepo(t *testing.T, expectedRepo string, timeout time.Duration, msgAndArgs ...interface{}) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get("http://localhost:5000/v2/_catalog")
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		var catalog struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}
		resp.Body.Close()

		for _, repo := range catalog.Repositories {
			if repo == expectedRepo {
				t.Logf("registry cache catalog contains %q (full catalog: %v)", expectedRepo, catalog.Repositories)
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}

	assert.Fail(t, fmt.Sprintf("registry cache catalog does not contain %q", expectedRepo), msgAndArgs...)
}
