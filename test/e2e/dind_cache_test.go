package e2e

import (
	"context"
	"fmt"
	"io/fs"
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
// to use it as a mirror, an image is pulled through DIND, and the host-side
// cache directory is inspected for registry layer files (_manifests, _layers).
//
// This guards the dind.EnsureCache + dind.Start(cacheAddr=...) plumbing and
// ensures the host-side bind mount at /var/lib/registry captures pulled layers.
func TestDINDCachePullThrough(t *testing.T) {
	testutil.SkipIfNoDocker(t)
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
	require.Contains(t, cacheAddr, "host.docker.internal:5000",
		"cache address must use host.docker.internal:5000")
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

	// Pull alpine:latest inside DIND — this goes through the cache mirror.
	exitCode, stdout, stderr := execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"docker", "pull", "alpine:latest"})
	require.Equal(t, 0, exitCode,
		"docker pull alpine:latest inside DIND must succeed; stdout=%q stderr=%q", stdout, stderr)
	t.Logf("first pull output: %s", stdout)

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
			base := filepath.Base(path)
			if base == "_manifests" {
				manifestCount++
			} else if base == "_layers" {
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
