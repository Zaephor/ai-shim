package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/testutil"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDINDSharedTmp_NestedRunSeesAgentTmp proves dind_shared_tmp end to end:
// a non-root agent-like client writes a file under /tmp, then
// `docker run -v /tmp/...` against the sidecar's daemon must see that file,
// and a nested write must come back to the agent's /tmp. Without the shared
// volume, DIND resolves /tmp/... in its own filesystem and the nested
// container sees an empty directory.
//
// The agent mount mirrors cmd/ai-shim/main.go: the sidecar's TmpVolume is
// mounted at /tmp in the agent alongside the socket volume.
func TestDINDSharedTmp_NestedRunSeesAgentTmp(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow DIND shared-tmp test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, "docker:latest"))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-dind-sharedtmp-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(context.Background()) })

	clientGID := os.Getgid()
	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-dind-sharedtmp-sc-%d", time.Now().UnixNano()),
		Hostname:      "dind-sharedtmp",
		NetworkID:     netHandle.ID,
		Labels:        labels,
		SocketGID:     clientGID,
		SharedTmp:     true,
	})
	require.NoError(t, err)
	tmpVolume := sidecar.TmpVolume()
	require.NotEmpty(t, tmpVolume)
	stopped := false
	t.Cleanup(func() {
		if stopped {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sidecar.Stop(cleanupCtx)
	})

	// Distinct exit codes: 10=agent cannot write /tmp, 11=pull failed,
	// 12=nested run did not see the agent's file, 13=nested write did not
	// reach the agent's /tmp.
	result, err := runner.Run(ctx, container.ContainerSpec{
		Name:  fmt.Sprintf("ai-shim-test-dind-sharedtmp-client-%d", time.Now().UnixNano()),
		Image: "docker:latest",
		User:  fmt.Sprintf("%d:%d", os.Getuid(), clientGID),
		Env:   []string{"DOCKER_HOST=unix:///var/run/dind/docker.sock"},
		Entrypoint: []string{"sh", "-c", `
mkdir -p /tmp/work && echo agent-tmp-ok > /tmp/work/in || exit 10
docker pull -q alpine:latest >/dev/null || exit 11
test "$(docker run --rm -v /tmp/work:/w alpine cat /w/in)" = agent-tmp-ok || exit 12
docker run --rm -v /tmp/work:/w alpine sh -c 'echo nested-ok > /w/out'
test "$(cat /tmp/work/out)" = nested-ok || exit 13
`},
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Source: sidecar.SocketVolume(), Target: "/var/run/dind"},
			{Type: mount.TypeVolume, Source: tmpVolume, Target: "/tmp"},
		},
		NetworkID: netHandle.ID,
		Labels:    labels,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode,
		"10=agent /tmp not writable, 11=alpine pull in DIND failed, 12=nested run missed agent file, 13=nested write missed agent /tmp")

	require.NoError(t, sidecar.Stop(ctx))
	stopped = true
	_, err = runner.Client().VolumeInspect(ctx, tmpVolume)
	assert.True(t, cerrdefs.IsNotFound(err), "shared tmp volume must be reaped with the sidecar, got err=%v", err)
}
