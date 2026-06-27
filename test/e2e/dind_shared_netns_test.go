package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDINDSharedNetns_LoopbackSharingMatchesToggle is the load-bearing guard for
// the dind_shared_netns feature: when the agent joins the DIND sidecar's network
// namespace (HostConfig.NetworkMode = "container:<dindID>"), the two containers
// share one network stack — same loopback — which is what lets tools like kind
// (kubeconfig at https://127.0.0.1:<port>, published on the DIND netns) be
// reached from the agent. When the toggle is off, the agent keeps its own netns
// on the bridge and must NOT share the sidecar's loopback.
//
// Rather than race on a loopback listener, this compares the network-namespace
// identity directly: readlink /proc/<pid>/ns/net yields "net:[<inode>]"; equal
// inode == shared netns, different inode == isolated. Both directions are
// checked against a single sidecar to prove the positive case isn't tautological
// (mirrors the positive/negative pairing in dind_workspace_sharing_test.go).
func TestDINDSharedNetns_LoopbackSharingMatchesToggle(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow DIND shared-netns test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"), "alpine image for agent stand-in")

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-netns-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(ctx) })

	dindName := fmt.Sprintf("ai-shim-test-netns-sc-%d", time.Now().UnixNano())
	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: dindName,
		Hostname:      "dind-netns",
		NetworkID:     netHandle.ID,
		Labels:        labels,
	})
	require.NoError(t, err, "starting DIND sidecar")
	t.Cleanup(func() {
		cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sidecar.Stop(cctx)
	})

	// Network-namespace identity of the sidecar (PID 1 = dockerd in its netns).
	code, dindNS, stderr := execInSidecar(t, ctx, runner, sidecar.ContainerID(),
		[]string{"readlink", "/proc/1/ns/net"})
	require.Equal(t, 0, code, "readlink in sidecar failed: %s", stderr)
	dindNS = strings.TrimSpace(dindNS)
	require.NotEmpty(t, dindNS, "sidecar netns identity")

	// ON: agent joins the sidecar's netns via NetworkMode=container:<id> (no
	// bridge attach). Its own /proc/self/ns/net must equal the sidecar's.
	onResult, err := runner.Run(ctx, container.ContainerSpec{
		Image:       "alpine:latest",
		NetworkMode: "container:" + sidecar.ContainerID(),
		Entrypoint:  []string{"sh", "-c", fmt.Sprintf(`[ "$(readlink /proc/self/ns/net)" = %q ]`, dindNS)},
		Labels:      labels,
	})
	require.NoError(t, err, "running agent stand-in with shared netns")
	assert.Equal(t, 0, onResult.ExitCode,
		"agent must share the DIND network namespace (%s) when NetworkMode=container:<dind>", dindNS)

	// OFF: agent attaches to the bridge normally (no NetworkMode). It must have
	// a DIFFERENT netns — proving the ON assertion measures real netns sharing.
	offResult, err := runner.Run(ctx, container.ContainerSpec{
		Image:      "alpine:latest",
		NetworkID:  netHandle.ID,
		Entrypoint: []string{"sh", "-c", fmt.Sprintf(`[ "$(readlink /proc/self/ns/net)" != %q ]`, dindNS)},
		Labels:     labels,
	})
	require.NoError(t, err, "running agent stand-in on its own bridge netns")
	assert.Equal(t, 0, offResult.ExitCode,
		"agent on the bridge must NOT share the DIND netns (%s) when shared-netns is off", dindNS)
}
