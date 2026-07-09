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
	return strings.TrimSpace(out)
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

// TestNetnsMode_HolderSurvivesDINDDeath is the load-bearing survival
// guarantee: DIND dying must NOT change the holder's netns, and DIND
// (unless-stopped) must come back sharing the same netns.
//
// Death is induced by shrinking the running sidecar's memory cgroup below
// its steady-state usage (via ContainerUpdate) so the kernel OOM-killer
// reaps dockerd directly -- a genuine, kernel-initiated process death, and a
// faithful stand-in for a real OOM.
//
// This deliberately does NOT use runner.Client().ContainerKill: Docker's
// restart-policy machinery treats any API-initiated stop/kill as a manual
// action and disables unless-stopped restarts for it -- documented,
// "working as designed" behavior (moby/moby#26087, moby/moby#39729).
// ContainerKill was verified empirically (outside this test) to leave the
// sidecar exited with RestartCount staying at 0 even under --restart=always,
// while a resource-limit or in-container process death restarts normally.
// Using ContainerKill here would make this test fail for every correct
// implementation, not just broken ones.
func TestNetnsMode_HolderSurvivesDINDDeath(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow netns survival test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
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

	// Let dockerd reach steady state before squeezing its memory, so the OOM
	// below is deterministic rather than racing container boot.
	readyCtx, readyCancel := context.WithTimeout(ctx, 60*time.Second)
	require.NoError(t, sidecar.WaitForReady(readyCtx), "DIND must be ready before inducing OOM")
	readyCancel()

	before := netnsInode(t, ctx, runner, holder.ContainerID())

	// Shrink the memory cgroup well below dockerd's steady-state RSS
	// (observed ~25MiB) to force an immediate, kernel-initiated OOM kill.
	updateMemory := func(bytes int64) {
		_, err := runner.Client().ContainerUpdate(ctx, sidecar.ContainerID(), dockercontainer.UpdateConfig{
			Resources: dockercontainer.Resources{Memory: bytes, MemorySwap: bytes},
		})
		require.NoError(t, err, "updating DIND memory cgroup")
	}
	const tightMemory = 16 * 1024 * 1024  // triggers OOM
	const roomyMemory = 512 * 1024 * 1024 // lets the restarted daemon stabilize
	updateMemory(tightMemory)

	// Confirm the OOM actually happened (unless-stopped restarted the
	// container at least once) before easing the cap back up. This check
	// only touches ContainerInspect, so it's safe to run from the
	// background goroutine testify's Eventually spawns.
	require.Eventually(t, func() bool {
		insp, err := runner.Client().ContainerInspect(ctx, sidecar.ContainerID())
		return err == nil && insp.RestartCount > 0
	}, 30*time.Second, 1*time.Second, "DIND should be OOM-killed and auto-restarted under a 16MiB memory cap")

	// The holder's netns must be unaffected by DIND's death/restart cycle.
	after := netnsInode(t, ctx, runner, holder.ContainerID())
	assert.Equal(t, before, after, "holder netns must survive DIND OOM death")

	// Ease the memory cap so the restarted daemon can stabilize instead of
	// flapping against the same tight cap indefinitely.
	updateMemory(roomyMemory)

	// unless-stopped must bring DIND back sharing the same (surviving)
	// netns. execInSidecar/netnsInode call require internally, which is only
	// safe from the goroutine running the test -- so this polls in a plain
	// loop on the main goroutine (not inside testify's Eventually, which
	// runs its condition on a background goroutine).
	//
	// A single State.Running==true check is not enough to safely attempt the
	// exec: a container can still be mid-restart-cycle (Running flips true
	// briefly between crash-loop attempts) and re-enter "restarting" by the
	// time ContainerExecCreate lands, which fails with a daemon error rather
	// than a clean non-zero exit -- execInSidecar's require.NoError would
	// then fail the test on a merely transient state, not a real problem.
	// So first wait for RestartCount to stop increasing (dockerd no longer
	// crash-looping under the now-roomy memory cap) before ever calling
	// execInSidecar.
	restartDeadline := time.Now().Add(90 * time.Second)
	lastCount := -1
	stableSince := time.Time{}
	stabilized := false
	const stableFor = 3 * time.Second
	for time.Now().Before(restartDeadline) {
		insp, err := runner.Client().ContainerInspect(ctx, sidecar.ContainerID())
		if err == nil && insp.State.Running {
			if insp.RestartCount != lastCount {
				lastCount = insp.RestartCount
				stableSince = time.Now()
			} else if !stableSince.IsZero() && time.Since(stableSince) >= stableFor {
				stabilized = true
				break
			}
		} else {
			stableSince = time.Time{}
		}
		time.Sleep(1 * time.Second)
	}
	require.True(t, stabilized, "DIND never stabilized (kept restarting) after easing the memory cap")

	dindNS := netnsInode(t, ctx, runner, sidecar.ContainerID())
	assert.Equal(t, before, dindNS, "DIND should auto-restart into the surviving netns")
}
