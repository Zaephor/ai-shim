package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These E2E tests guard the DIND workspace-sharing fix introduced in commit
// 78c975b and extended in aef102e to tool-cache directories. The invariant:
//
//   When the agent invokes `docker run -v <host>:<target>` against the DIND
//   sidecar's daemon, Docker resolves <host> in DIND's own filesystem — not
//   the agent's. Before the fix the sidecar ran with only its Docker socket
//   volume, so the agent's workspace path did not exist inside DIND. Nested
//   runs silently saw an empty overlay directory instead of the host content.
//
//   Config.SharedMounts propagates bind mounts into the sidecar at identical
//   source/target paths so the nested-run source resolves to real host bytes.
//
// The load-bearing assertion is: a sentinel file written into a host directory
// is visible inside the DIND sidecar at the same path when (and only when)
// that directory is listed in SharedMounts. We exec directly against the
// sidecar container — if the path exists inside DIND, any nested `docker run`
// with the same source will also resolve it, because that's exactly how the
// DIND daemon's host-path resolution works.

// execInSidecar runs a command inside the DIND sidecar container and returns
// the exit code plus captured stdout/stderr. The sidecar's own `exec` helper
// is unexported, so this test uses the outer Docker SDK directly — which is
// the same code path production inspection code follows.
func execInSidecar(t *testing.T, ctx context.Context, runner *container.Runner, containerID string, cmd []string) (int, string, string) {
	t.Helper()
	cli := runner.Client()

	resp, err := cli.ContainerExecCreate(ctx, containerID, dockercontainer.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	require.NoError(t, err, "creating exec in sidecar")

	attach, err := cli.ContainerExecAttach(ctx, resp.ID, dockercontainer.ExecStartOptions{})
	require.NoError(t, err, "attaching to exec")
	defer attach.Close()

	var stdoutBuf, stderrBuf strings.Builder
	_, err = stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attach.Reader)
	require.NoError(t, err, "draining exec output")

	inspect, err := cli.ContainerExecInspect(ctx, resp.ID)
	require.NoError(t, err, "inspecting exec")

	return inspect.ExitCode, stdoutBuf.String(), stderrBuf.String()
}

// TestDINDWorkspaceSharing_AgentWorkspaceVisible is the positive assertion
// for commit 78c975b: a sentinel file written into a host directory that is
// passed through SharedMounts (agent workspace, same-source-same-target) must
// be readable inside the DIND sidecar at the same path.
//
// If the SharedMounts plumbing ever regresses (e.g. Start drops the field,
// mounts land at a different target, or the sidecar is created without the
// extra bind mounts), this test fails with an empty stdout or a non-zero
// exit from `cat` inside the sidecar.
func TestDINDWorkspaceSharing_AgentWorkspaceVisible(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow DIND workspace-sharing test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })

	// Host-side workspace. dockerTempDir roots under the project's tmp/ so the
	// path is reachable by the Docker daemon in nested-Docker CI setups.
	hostWorkspace := dockerTempDir(t)
	const sentinelName = "host-sentinel.txt"
	const sentinelBody = "host-content-workspace-sharing-ok\n"
	sentinelPath := filepath.Join(hostWorkspace, sentinelName)
	require.NoError(t, os.WriteFile(sentinelPath, []byte(sentinelBody), 0644))

	// Host-side tool cache dir, also propagated via SharedMounts to cover the
	// aef102e fix path (tool caches listed in ToolsOrder -> buildDINDSharedMounts).
	hostToolCache := dockerTempDir(t)
	const toolMarkerName = "tool-marker.txt"
	const toolMarkerBody = "tool-cache-propagated-ok\n"
	require.NoError(t, os.WriteFile(filepath.Join(hostToolCache, toolMarkerName), []byte(toolMarkerBody), 0644))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-dind-ws-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(ctx) })

	dindName := fmt.Sprintf("ai-shim-test-dind-ws-sc-%d", time.Now().UnixNano())
	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: dindName,
		Hostname:      "dind-ws-share",
		NetworkID:     netHandle.ID,
		Labels:        labels,
		// Mirror the production wiring from cmd/ai-shim/main.go
		// buildDINDSharedMounts: each host path is bound into the sidecar at
		// the identical target path so `-v host:target` from the agent
		// resolves to the host bytes inside DIND.
		SharedMounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: hostWorkspace,
				Target: hostWorkspace,
			},
			{
				Type:   mount.TypeBind,
				Source: hostToolCache,
				Target: hostToolCache,
			},
		},
	})
	require.NoError(t, err, "starting DIND sidecar with SharedMounts")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sidecar.Stop(cleanupCtx)
	})

	// Positive: the workspace sentinel must be readable inside DIND at the
	// identical host path. This is the direct assertion that SharedMounts
	// placed the bind mount where `docker run -v <hostWorkspace>:...` would
	// resolve it inside the DIND daemon.
	exitCode, stdout, stderr := execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"cat", filepath.Join(hostWorkspace, sentinelName)})
	require.Equal(t, 0, exitCode,
		"cat inside DIND must succeed; stderr=%q — SharedMounts did not propagate workspace", stderr)
	assert.Equal(t, sentinelBody, stdout,
		"DIND sidecar must see host sentinel bytes verbatim at the shared path")

	// Positive: the tool-cache marker must also be readable inside DIND at
	// its identical host path. Guards aef102e's extension of SharedMounts to
	// persistent tool cache directories so nested `docker run -v toolCache:X`
	// from the agent resolves to host bytes instead of an empty overlay.
	exitCode, stdout, stderr = execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"cat", filepath.Join(hostToolCache, toolMarkerName)})
	require.Equal(t, 0, exitCode,
		"cat of tool-cache marker inside DIND must succeed; stderr=%q", stderr)
	assert.Equal(t, toolMarkerBody, stdout,
		"DIND sidecar must see tool-cache marker bytes at the shared path")
}

// TestDINDWorkspaceSharing_NoMountsMeansInvisible is the companion negative
// assertion: without SharedMounts the sentinel file is NOT visible inside the
// DIND sidecar. This proves the positive test above is actually measuring the
// SharedMounts plumbing and not some unrelated path-propagation side effect
// (e.g. a daemon-shared tmpfs or /tmp leakage). If this test ever passes the
// `cat` call, the positive test is tautological and must be reconsidered.
func TestDINDWorkspaceSharing_NoMountsMeansInvisible(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow DIND workspace-sharing negative test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })

	hostWorkspace := dockerTempDir(t)
	const sentinelName = "host-sentinel.txt"
	const sentinelBody = "should-be-invisible-without-sharedmounts\n"
	require.NoError(t, os.WriteFile(filepath.Join(hostWorkspace, sentinelName),
		[]byte(sentinelBody), 0644))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-dind-ws-neg-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(ctx) })

	dindName := fmt.Sprintf("ai-shim-test-dind-ws-neg-sc-%d", time.Now().UnixNano())
	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: dindName,
		Hostname:      "dind-ws-share-neg",
		NetworkID:     netHandle.ID,
		Labels:        labels,
		// Intentionally no SharedMounts — this is the pre-fix state.
	})
	require.NoError(t, err, "starting DIND sidecar without SharedMounts")
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sidecar.Stop(cleanupCtx)
	})

	// Without SharedMounts the sidecar's filesystem is an isolated overlay.
	// The host workspace path either does not exist inside DIND, or exists
	// but is empty. Either way `cat <path>/host-sentinel.txt` must NOT yield
	// the host bytes — that's the precise behaviour commit 78c975b fixed.
	exitCode, stdout, _ := execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"cat", filepath.Join(hostWorkspace, sentinelName)})
	assert.NotEqual(t, 0, exitCode,
		"cat of host sentinel inside DIND must fail when SharedMounts is absent")
	assert.NotEqual(t, sentinelBody, stdout,
		"DIND sidecar must not see host sentinel bytes without SharedMounts")
}
