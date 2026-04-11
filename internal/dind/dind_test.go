package dind

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ai_container "github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/network"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
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
	// Ensure DIND image is available
	ctx := context.Background()
	_, err = cli.ImageInspect(ctx, DefaultImage)
	if err != nil {
		reader, pullErr := cli.ImagePull(ctx, DefaultImage, image.PullOptions{})
		if pullErr != nil {
			t.Fatal("failed to pull DIND image:", pullErr)
		}
		_, _ = io.Copy(io.Discard, reader)
		_ = reader.Close()
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
	ctx := context.Background()
	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	// Use a path accessible to the Docker daemon
	cacheDir := filepath.Join(os.Getenv("HOME"), ".ai-shim", "test-registry-cache")
	os.MkdirAll(cacheDir, 0755)
	t.Cleanup(func() { os.RemoveAll(cacheDir) })
	addr, err := EnsureCache(ctx, runner, cacheDir)
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

// TestEnsureCache_PullsImageWhenMissing is a regression test for a bug where
// EnsureCache called ContainerCreate without first pulling the cache image.
// The Docker SDK's ContainerCreate does NOT auto-pull (unlike the `docker run`
// CLI), so on any host where registry:2 was not already cached, enabling the
// DIND registry mirror failed with "No such image: registry:2".
func TestEnsureCache_PullsImageWhenMissing(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	// Clean up any pre-existing cache container from a prior run so we hit
	// the cold-start code path inside EnsureCache.
	existing, _ := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	for _, c := range existing {
		_ = cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
	}

	// Force-remove the cache image (both canonical and short refs) so
	// EnsureCache must pull it itself. This mirrors a fresh developer
	// machine that has never used the registry mirror before.
	_, _ = cli.ImageRemove(ctx, CacheImage, image.RemoveOptions{Force: true})
	_, _ = cli.ImageRemove(ctx, "registry:2", image.RemoveOptions{Force: true})

	cacheDir := filepath.Join(os.Getenv("HOME"), ".ai-shim", "test-registry-cache-pull")
	require.NoError(t, os.MkdirAll(cacheDir, 0755))
	t.Cleanup(func() { _ = os.RemoveAll(cacheDir) })

	addr, err := EnsureCache(ctx, runner, cacheDir)
	require.NoError(t, err, "EnsureCache must pull the cache image when it is not present locally")
	assert.Contains(t, addr, "host.docker.internal")
	assert.Contains(t, addr, CachePort)

	// Cleanup cache container but leave the image so other tests benefit.
	containers, _ := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	for _, c := range containers {
		_ = cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true})
	}
}

func TestMaybeStopCache_DoesNothingWhenNoCache(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()
	ctx := context.Background()

	// Should not panic or error when no cache exists
	MaybeStopCache(ctx, cli)
}

func TestMaybeStopCache_RemovesActualCacheContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()
	ctx := context.Background()

	// Create a container named like the cache container with ai-shim cache labels
	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"sleep", "3600"},
		Labels: map[string]string{
			ai_container.LabelBase:  "true",
			ai_container.LabelCache: "true",
		},
	}, nil, nil, nil, CacheContainerName)
	require.NoError(t, err)
	t.Cleanup(func() {
		// Belt-and-suspenders cleanup in case MaybeStopCache fails
		cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
	})

	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	// Verify the container exists
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	require.NoError(t, err)
	require.Len(t, containers, 1, "cache container should exist before MaybeStopCache")

	// Call MaybeStopCache - should remove it since no consumers exist
	MaybeStopCache(ctx, cli)

	// Verify the container is gone
	containers, err = cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	require.NoError(t, err)
	assert.Empty(t, containers, "cache container should be removed after MaybeStopCache")

	// Call MaybeStopCache again - should be a no-op
	MaybeStopCache(ctx, cli)
}

func TestStart_WithMirrors_VerifyEntrypoint(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-mirrors-verify", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind",
		Mirrors:   []string{"https://mirror.gcr.io", "https://custom.mirror.io"},
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	// Inspect container to verify mirrors in entrypoint
	inspect, err := cli.ContainerInspect(ctx, sidecar.ContainerID())
	require.NoError(t, err)

	// Entrypoint should contain --registry-mirror flags
	entrypoint := strings.Join(inspect.Config.Entrypoint, " ")
	assert.Contains(t, entrypoint, "--registry-mirror=https://mirror.gcr.io")
	assert.Contains(t, entrypoint, "--registry-mirror=https://custom.mirror.io")
}

func TestWaitForReady_Timeout(t *testing.T) {
	// Unit test: verify that WaitForReady returns an error when context is cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	sidecar := &Sidecar{
		containerID: "nonexistent-container-id",
	}

	err := sidecar.WaitForReady(ctx)
	assert.Error(t, err, "should error on cancelled context")
	assert.Contains(t, err.Error(), "context")
}

func TestStart_WaitsForReady(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind-ready", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind-ready",
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	// After Start returns, the daemon should be ready.
	// Verify by exec-ing docker info inside the container.
	execCfg := container.ExecOptions{
		Cmd: []string{"docker", "info"},
	}
	execResp, err := cli.ContainerExecCreate(ctx, sidecar.ContainerID(), execCfg)
	require.NoError(t, err)
	err = cli.ContainerExecStart(ctx, execResp.ID, container.ExecStartOptions{})
	assert.NoError(t, err, "docker info should succeed after Start returns")
}

func TestStart_TLS(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind-tls", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind-tls",
		TLS:       true,
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.NotEmpty(t, sidecar.ContainerID())
	assert.NotEmpty(t, sidecar.CertsVolume(), "TLS sidecar should have a certs volume")

	// Verify TLS env var was set on the container
	inspect, err := cli.ContainerInspect(ctx, sidecar.ContainerID())
	require.NoError(t, err)

	var foundTLSEnv bool
	for _, env := range inspect.Config.Env {
		if env == "DOCKER_TLS_CERTDIR=/certs" {
			foundTLSEnv = true
		}
	}
	assert.True(t, foundTLSEnv, "DOCKER_TLS_CERTDIR should be set to /certs")

	// Verify certs volume mount exists
	var foundCertsMount bool
	for _, m := range inspect.Mounts {
		if m.Destination == "/certs" {
			foundCertsMount = true
		}
	}
	assert.True(t, foundCertsMount, "should have /certs volume mount")
}

func TestStart_NoTLS(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-dind-notls", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	sidecar, err := Start(ctx, cli, Config{
		Labels:    map[string]string{"ai-shim": "test"},
		NetworkID: netHandle.ID,
		Hostname:  "test-dind-notls",
		TLS:       false,
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.Empty(t, sidecar.CertsVolume(), "non-TLS sidecar should not have a certs volume")

	// Verify TLS is disabled
	inspect, err := cli.ContainerInspect(ctx, sidecar.ContainerID())
	require.NoError(t, err)

	var foundEmptyTLS bool
	for _, env := range inspect.Config.Env {
		if env == "DOCKER_TLS_CERTDIR=" {
			foundEmptyTLS = true
		}
	}
	assert.True(t, foundEmptyTLS, "DOCKER_TLS_CERTDIR should be empty when TLS disabled")
}

func TestDetectSysbox(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	// Just verify it doesn't panic/error -- sysbox likely not available in CI
	_ = DetectSysbox(ctx, cli)
}

// TestSidecar_StartStopLifecycle tests the DIND sidecar container lifecycle
// without waiting for the Docker daemon inside to become ready.
func TestSidecar_StartStopLifecycle(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()
	ctx := context.Background()

	// Create a network for the sidecar
	netHandle, err := network.EnsureNetwork(ctx, cli, "ai-shim-test-lifecycle", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer netHandle.Remove(ctx)

	// Ensure the DIND image is available
	_, inspectErr := cli.ImageInspect(ctx, DefaultImage)
	if inspectErr != nil {
		reader, pullErr := cli.ImagePull(ctx, DefaultImage, image.PullOptions{})
		if pullErr != nil {
			t.Fatal("failed to pull DIND image:", pullErr)
		}
		_, _ = io.Copy(io.Discard, reader)
		_ = reader.Close()
	}

	// Manually create volume + container (bypassing Start to avoid WaitForReady)
	containerName := "ai-shim-test-lifecycle-dind"
	socketVolName := containerName + "-socket"

	_, err = cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   socketVolName,
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:  DefaultImage,
		Labels: map[string]string{"ai-shim": "test"},
		Env:    []string{"DOCKER_TLS_CERTDIR="},
	}, &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(netHandle.ID),
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: socketVolName, Target: "/var/run"},
		},
	}, nil, nil, containerName)
	require.NoError(t, err)

	err = cli.ContainerStart(ctx, resp.ID, container.StartOptions{})
	require.NoError(t, err)

	sidecar := &Sidecar{
		client:        cli,
		containerID:   resp.ID,
		containerName: containerName,
		hostname:      "test-lifecycle",
		socketVolume:  socketVolName,
	}

	// Verify accessors return expected values
	assert.NotEmpty(t, sidecar.ContainerID(), "ContainerID should be non-empty")
	assert.NotEmpty(t, sidecar.SocketVolume(), "SocketVolume should be non-empty")
	assert.Equal(t, containerName, sidecar.ContainerName())
	assert.Equal(t, "test-lifecycle", sidecar.Hostname())
	assert.Empty(t, sidecar.CertsVolume(), "no TLS so CertsVolume should be empty")

	// Stop the sidecar
	err = sidecar.Stop(ctx)
	require.NoError(t, err)

	// Verify the container is gone
	_, inspectErr = cli.ContainerInspect(ctx, resp.ID)
	assert.Error(t, inspectErr, "container should not exist after Stop")
}

func TestDetectSysbox_ReturnsFalseWithoutSysbox(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()
	ctx := context.Background()

	// Sysbox is not installed in this environment, so it should return false
	result := DetectSysbox(ctx, cli)
	assert.False(t, result, "DetectSysbox should return false when sysbox-runc is not installed")

	// Verify the underlying Docker info call works (no error path)
	info, err := cli.Info(ctx)
	require.NoError(t, err, "Docker info should succeed")
	// Double-check: sysbox-runc should not be in the runtimes list
	for name := range info.Runtimes {
		assert.NotEqual(t, "sysbox-runc", name, "sysbox-runc should not be available")
	}
}
