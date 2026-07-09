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
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// netnsInode returns the network-namespace identity ("net:[<inode>]") of PID 1
// inside the given container, via execInSidecar (defined in
// dind_workspace_sharing_test.go, same package). Equal values across two
// containers mean they share one network namespace.
func netnsInode(t *testing.T, ctx context.Context, runner *container.Runner, id string) string {
	t.Helper()
	code, out, stderr := execInSidecar(t, ctx, runner, id, []string{"readlink", "/proc/1/ns/net"})
	require.Equal(t, 0, code, "readlink failed: %s", stderr)
	ns := strings.TrimSpace(out)
	require.NotEmpty(t, ns, "readlink returned empty netns identity")
	return ns
}

// TestNetnsMode_HolderSharesWithBoth proves the holder pattern: a DIND sidecar
// started with JoinNetns pointing at a netns holder shares the holder's
// network namespace (same /proc/1/ns/net identity).
func TestNetnsMode_HolderSharesWithBoth(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow netns holder test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, dind.DefaultImage))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-holder-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	// Use context.Background(), not ctx: the test's own `defer cancel()` runs
	// before t.Cleanup callbacks fire, so ctx is already cancelled by the
	// time this executes and Remove would silently no-op.
	t.Cleanup(func() { _ = netHandle.Remove(context.Background()) })

	holder, err := dind.StartHolder(ctx, runner, dind.HolderConfig{
		Image:     dind.DefaultImage,
		Name:      fmt.Sprintf("ai-shim-test-holder-h-%d", time.Now().UnixNano()),
		Hostname:  "holder",
		NetworkID: netHandle.ID,
		Labels:    labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Stop(context.Background()) })

	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-holder-d-%d", time.Now().UnixNano()),
		NetworkID:     netHandle.ID,
		JoinNetns:     holder.ContainerID(),
		AutoRestart:   true,
		Labels:        labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cctx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		_ = sidecar.Stop(cctx)
	})

	holderNs := netnsInode(t, ctx, runner, holder.ContainerID())
	dindNs := netnsInode(t, ctx, runner, sidecar.ContainerID())
	assert.Equal(t, holderNs, dindNs, "DIND must share the holder's netns")
}

// TestNetnsMode_HolderSurvivesDINDDeath is the load-bearing survival guarantee:
// when the DIND sidecar dies, the holder's network namespace must be
// unaffected, and a DIND rejoining the holder must land in that same surviving
// namespace -- which is exactly the outcome an unless-stopped auto-restart
// produces in production.
//
// Death is induced with ContainerKill (SIGKILL): a portable, deterministic
// kill available on every Docker daemon. Docker intentionally suppresses the
// restart policy for an API-initiated kill (moby/moby#26087), so this test does
// NOT rely on the sidecar auto-restarting on its own; instead it (1) asserts the
// unless-stopped policy is configured, documenting the recovery intent, and
// (2) starts a fresh DIND to prove it rejoins the surviving netns.
//
// An earlier revision induced a real OOM by shrinking the sidecar's memory
// cgroup, but whether that actually fires the kernel OOM-killer depends on the
// host's cgroup version and swap accounting -- it passed locally yet never
// fired on the CI runner within the timeout. A portable kill + rejoin proves
// the same guarantee without that environmental dependency.
func TestNetnsMode_HolderSurvivesDINDDeath(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow netns survival test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, dind.DefaultImage))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-surv-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	// Use context.Background(), not ctx: the test's own `defer cancel()` runs
	// before t.Cleanup callbacks fire, so ctx is already cancelled by the
	// time this executes and Remove would silently no-op.
	t.Cleanup(func() { _ = netHandle.Remove(context.Background()) })

	holder, err := dind.StartHolder(ctx, runner, dind.HolderConfig{
		Image:     dind.DefaultImage,
		Name:      fmt.Sprintf("ai-shim-test-surv-h-%d", time.Now().UnixNano()),
		Hostname:  "holder",
		NetworkID: netHandle.ID,
		Labels:    labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Stop(context.Background()) })

	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-surv-d-%d", time.Now().UnixNano()),
		NetworkID:     netHandle.ID,
		JoinNetns:     holder.ContainerID(),
		AutoRestart:   true,
		Labels:        labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cctx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		_ = sidecar.Stop(cctx)
	})

	// The sidecar must be configured to auto-restart (unless-stopped): in
	// production this is what brings DIND back after a real OOM into the
	// surviving netns. Assert the policy rather than trying to trigger it,
	// because no API-portable kill fires the restart policy (moby#26087).
	insp, err := runner.Client().ContainerInspect(ctx, sidecar.ContainerID())
	require.NoError(t, err)
	assert.Equal(t, dockercontainer.RestartPolicyUnlessStopped, insp.HostConfig.RestartPolicy.Name,
		"DIND sidecar must be configured with unless-stopped so a real OOM auto-restarts it")

	before := netnsInode(t, ctx, runner, holder.ContainerID())
	require.Equal(t, before, netnsInode(t, ctx, runner, sidecar.ContainerID()),
		"sidecar must share the holder netns before death")

	// Kill the sidecar, simulating DIND death.
	require.NoError(t, runner.Client().ContainerKill(ctx, sidecar.ContainerID(), "KILL"))
	require.Eventually(t, func() bool {
		i, err := runner.Client().ContainerInspect(ctx, sidecar.ContainerID())
		return err == nil && !i.State.Running
	}, 30*time.Second, 500*time.Millisecond, "sidecar should be dead after kill")

	// The holder must still be alive (it owns the namespace) and its netns must
	// be unchanged by the sidecar's death.
	holderInsp, err := runner.Client().ContainerInspect(ctx, holder.ContainerID())
	require.NoError(t, err)
	require.True(t, holderInsp.State.Running, "holder must stay alive when the sidecar dies")
	assert.Equal(t, before, netnsInode(t, ctx, runner, holder.ContainerID()),
		"holder netns must survive DIND death")

	// A DIND rejoining the holder must land in the same surviving netns -- the
	// exact outcome an unless-stopped auto-restart produces.
	sidecar2, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-surv-d2-%d", time.Now().UnixNano()),
		NetworkID:     netHandle.ID,
		JoinNetns:     holder.ContainerID(),
		AutoRestart:   true,
		Labels:        labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cctx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		_ = sidecar2.Stop(cctx)
	})

	// Wait for the fresh sidecar to be running before exec'ing into it (CI
	// runners are slower to settle a just-started container).
	require.Eventually(t, func() bool {
		i, err := runner.Client().ContainerInspect(ctx, sidecar2.ContainerID())
		return err == nil && i.State.Running
	}, 30*time.Second, 500*time.Millisecond, "rejoining sidecar should be running")

	assert.Equal(t, before, netnsInode(t, ctx, runner, sidecar2.ContainerID()),
		"a DIND rejoining the holder must land in the surviving netns")
}
