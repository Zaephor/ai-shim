package testutil

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Zaephor/ai-shim/internal/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// SkipIfNoDocker skips the test if Docker is not available or if running
// in short mode (-short flag). Docker-dependent tests are integration tests
// that should only run in the E2E CI job, not in the unit test job.
// When AI_SHIM_CI=1, it fails the test instead of skipping (used by the E2E job).
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Docker test in short mode")
	}
	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		if os.Getenv("AI_SHIM_CI") == "1" {
			t.Fatalf("Docker required in CI but not available: %v", err)
		}
		t.Skip("Docker not available:", err)
	}
	_ = cli.Close()
}

// PullImage pulls ref and drains the progress stream to completion. The Docker
// SDK's ContainerCreate does NOT auto-pull (unlike `docker run`), so tests that
// create containers from a fixed image must pull it first or they fail on any
// host where the image is not already cached. The pull is async; the reader
// must be fully drained before the image is usable.
func PullImage(t *testing.T, ctx context.Context, cli *client.Client, ref string) {
	t.Helper()
	reader, err := cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		t.Fatalf("pulling image %s: %v", ref, err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatalf("draining image pull for %s: %v", ref, err)
	}
}

// RequireDaemonLocalFS skips the test unless the Docker daemon shares a
// filesystem with the test process — i.e. it can bind-mount a directory the
// test just created. Integration tests that bind-mount host paths (e.g. the
// DIND registry cache) only work when the daemon is local; against a remote or
// DIND daemon (DOCKER_HOST pointing at another host's socket) the bind source
// "does not exist" from the daemon's view and the test would fail spuriously.
// This probe lets those tests skip cleanly instead.
func RequireDaemonLocalFS(t *testing.T, ctx context.Context, cli *client.Client) {
	t.Helper()
	PullImage(t, ctx, cli, "alpine:latest")

	dir := t.TempDir()
	const marker = "ai-shim-fs-probe"
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte(marker), 0o644); err != nil {
		t.Fatalf("writing probe marker: %v", err)
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: "alpine:latest",
		Cmd:   []string{"cat", "/probe/marker"},
	}, &container.HostConfig{
		Mounts: []mount.Mount{{Type: mount.TypeBind, Source: dir, Target: "/probe"}},
	}, nil, nil, "")
	if err != nil {
		// Daemon rejected the bind source outright (can't see the path).
		t.Skipf("daemon filesystem not shared with test host (%v); skipping bind-mount integration test", err)
	}
	defer func() { _ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}) }()

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		t.Fatalf("starting probe container: %v", err)
	}
	waitCh, errCh := cli.ContainerWait(ctx, resp.ID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("waiting for probe container: %v", err)
		}
	case <-waitCh:
	}

	logs, err := cli.ContainerLogs(ctx, resp.ID, container.LogsOptions{ShowStdout: true})
	if err != nil {
		t.Fatalf("reading probe logs: %v", err)
	}
	defer func() { _ = logs.Close() }()
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, io.Discard, logs); err != nil {
		t.Fatalf("demuxing probe logs: %v", err)
	}
	if !strings.Contains(buf.String(), marker) {
		t.Skip("daemon filesystem not shared with test host (probe marker not visible in container); skipping bind-mount integration test")
	}
}

// SkipIfNestedDocker skips the test if the current process is running
// inside a Docker container (detected by the presence of /.dockerenv).
// Some Docker features like TLS cert generation in DIND are too slow
// in nested virtualization to complete within test timeouts.
func SkipIfNestedDocker(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("skipping: nested Docker detected (/.dockerenv exists); TLS DIND is too slow in nested virtualization")
	}
}
