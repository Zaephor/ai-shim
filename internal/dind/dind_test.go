package dind

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-shim/ai-shim/internal/network"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getClient(t *testing.T) *client.Client {
	t.Helper()
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal("failed to create docker client:", err)
	}
	return cli
}

func TestStart_AndStop(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	// Create network first
	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sidecar.ContainerID())

	err = sidecar.Stop(ctx)
	assert.NoError(t, err)
}

func TestStart_CustomImage(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind-img", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Image:     "docker:dind",
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.NotEmpty(t, sidecar.ContainerID())
}

func TestStart_ContainerName(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind-name", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		ContainerName: "test-dind-container",
		Hostname:      "test-dind-host",
		Labels:        map[string]string{"ai-shim": "test"},
		NetworkID:     netHandle.ID,
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.Equal(t, "test-dind-host", sidecar.Hostname())
	assert.Equal(t, "test-dind-container", sidecar.ContainerName())
}

func TestStart_ReturnsSocketVolume(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind-socket", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind",
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.NotEmpty(t, sidecar.SocketVolume(), "should return the Docker socket volume name")
}

func TestStart_WithMirrors(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-mirrors", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind",
		Mirrors:   []string{"https://mirror.gcr.io"},
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.NotEmpty(t, sidecar.ContainerID())
}

func TestEnsureCache_StartsAndStops(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()
	ctx := context.Background()

	// Pull registry image if needed (skip if pull fails)
	pullReader, err := cli.ImagePull(ctx, "docker.io/library/registry:2", image.PullOptions{})
	if err != nil {
		t.Skip("Cannot pull registry:2 image:", err)
	}
	io.Copy(io.Discard, pullReader)
	pullReader.Close()

	// Use a path accessible to the Docker daemon
	cacheDir := filepath.Join(os.Getenv("HOME"), ".ai-shim", "test-registry-cache")
	os.MkdirAll(cacheDir, 0755)
	t.Cleanup(func() { os.RemoveAll(cacheDir) })
	addr, err := EnsureCache(ctx, cli, cacheDir)
	require.NoError(t, err)
	assert.Contains(t, addr, "host.docker.internal")
	assert.Contains(t, addr, CachePort)

	// Cleanup
	containers, _ := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	for _, c := range containers {
		cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
	}
}

func TestDetectSysbox(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	// Just verify it doesn't panic/error -- sysbox likely not available in CI
	_ = DetectSysbox(ctx, cli)
}
