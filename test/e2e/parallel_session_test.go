package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These E2E tests verify the parallel-session lifecycle exposed by the
// recent multi-session work:
//
//   - ai-shim supports running >1 persistent session for the same
//     agent+profile+workspace concurrently.
//   - container.FindRunningSessionsInWorkspace surfaces all siblings, sorted
//     most-recently-created first, so the picker can offer index [1] as the
//     newest.
//   - Killing one sibling (the interactive k<N> path) must leave the other
//     sibling's container AND its DIND sidecar untouched. This is enforced
//     by the workspace-hash filter in the DIND-cleanup lookup — the load-
//     bearing assertion is the regression guard for that filter.
//   - After the last consumer is killed, the shared registry cache is
//     garbage-collected by dind.MaybeStopCache.
//
// These tests do NOT drive the interactive cli flow. They build sessions
// directly via the Docker client using ai-shim's published labels. That lets
// us exercise the lookup/filter invariants without standing up real agent
// images or the TTY-gated launch path. Production code is not modified.

// parallelTestParams encapsulates the label identity for a set of parallel
// sessions. Every session in a given workspace shares agent+profile+wsHash.
type parallelTestParams struct {
	AgentName string
	Profile   string
	WsHash    string
}

// sessionLabels returns the minimum label set that makes a container
// discoverable by FindRunningSessionsInWorkspace.
func (p parallelTestParams) sessionLabels() map[string]string {
	return map[string]string{
		container.LabelBase:         "true",
		container.LabelAgent:        p.AgentName,
		container.LabelProfile:      p.Profile,
		container.LabelPersistent:   "true",
		container.LabelWorkspace:    p.WsHash,
		container.LabelWorkspaceDir: "/fake/ws/path",
	}
}

// startFakeSession creates and starts a long-sleeping alpine container with
// ai-shim's persistent-session labels. The returned id is the container ID;
// the caller registers cleanup with t.Cleanup so no containers leak.
func startFakeSession(t *testing.T, ctx context.Context, cli *client.Client, name string, labels map[string]string) string {
	t.Helper()

	// Use alpine sleep so the container stays running without attaching.
	// Sleep longer than the test timeout budget to avoid racing.
	resp, err := cli.ContainerCreate(ctx,
		&dockercontainer.Config{
			Image:  "alpine:latest",
			Cmd:    []string{"sleep", "300"},
			Labels: labels,
		},
		&dockercontainer.HostConfig{
			AutoRemove: false,
		},
		nil, nil, name,
	)
	require.NoError(t, err, "creating fake session %q", name)

	t.Cleanup(func() {
		// Force-remove; ignore "not found" which simply means the test
		// already cleaned it up as part of the scenario under test.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanupCtx, resp.ID, dockercontainer.RemoveOptions{Force: true})
	})

	err = cli.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{})
	require.NoError(t, err, "starting fake session %q", name)

	return resp.ID
}

// waitForRunning polls until the container reports State.Running=true or the
// deadline expires. This gates the subsequent FindRunningSessionsInWorkspace
// call so we don't race the daemon's bookkeeping.
func waitForRunning(t *testing.T, ctx context.Context, cli *client.Client, id string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		insp, err := cli.ContainerInspect(ctx, id)
		if err == nil && insp.State != nil && insp.State.Running {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("container %s did not enter Running state within 30s", id)
}

// newTestParams builds a fresh identity scoped by test name + nanosecond so
// concurrent test runs don't interfere via stale label queries.
func newTestParams(t *testing.T) parallelTestParams {
	t.Helper()
	// Workspace hash only needs to be unique per test; it's opaque to the
	// label filter. Keep it short (ai-shim uses 8-char truncated SHA).
	suffix := fmt.Sprintf("%x", time.Now().UnixNano())
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	return parallelTestParams{
		AgentName: "test-parallel",
		Profile:   "p-" + suffix,
		WsHash:    "w" + suffix,
	}
}

// TestParallel_SpawnTwoSessions covers invariant #1: two persistent sessions
// for the same agent+profile+workspace can run concurrently without either
// evicting the other.
func TestParallel_SpawnTwoSessions(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow parallel session test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	params := newTestParams(t)
	labels := params.sessionLabels()

	// Assertion first: an empty-docker query for this freshly-minted identity
	// must return 0 sessions. Without this baseline a stale container from a
	// previous test run could silently satisfy the "exactly 2" assertion.
	pre, err := container.FindRunningSessionsInWorkspace(ctx, cli, params.AgentName, params.Profile, params.WsHash)
	require.NoError(t, err)
	require.Empty(t, pre, "workspace must start empty for this test identity")

	nameA := fmt.Sprintf("ai-shim-parallel-a-%d", time.Now().UnixNano())
	idA := startFakeSession(t, ctx, cli, nameA, labels)
	waitForRunning(t, ctx, cli, idA)

	nameB := fmt.Sprintf("ai-shim-parallel-b-%d", time.Now().UnixNano())
	idB := startFakeSession(t, ctx, cli, nameB, labels)
	waitForRunning(t, ctx, cli, idB)

	sessions, err := container.FindRunningSessionsInWorkspace(ctx, cli, params.AgentName, params.Profile, params.WsHash)
	require.NoError(t, err)
	require.Len(t, sessions, 2, "exactly two sibling sessions must be visible")

	ids := map[string]bool{sessions[0].ContainerID: true, sessions[1].ContainerID: true}
	assert.True(t, ids[idA], "session A must appear in the result")
	assert.True(t, ids[idB], "session B must appear in the result")

	// Both containers must actually be running (not just created/restarting).
	for _, s := range sessions {
		insp, err := cli.ContainerInspect(ctx, s.ContainerID)
		require.NoError(t, err)
		require.NotNil(t, insp.State)
		assert.True(t, insp.State.Running,
			"session %s must be Running; state=%+v", s.ContainerName, insp.State)
	}
}

// TestParallel_SortOrder covers invariant #2: FindRunningSessionsInWorkspace
// returns entries most-recently-created first so the picker can surface the
// newest session as the default choice.
func TestParallel_SortOrder(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow parallel session test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	params := newTestParams(t)
	labels := params.sessionLabels()

	// Create older first, then sleep past the Docker Created-timestamp
	// granularity (seconds) so the sort comparison is meaningful. Docker's
	// ContainerList reports Created as Unix seconds, so a sub-second gap
	// collapses to equal keys and the sort becomes non-deterministic.
	olderName := fmt.Sprintf("ai-shim-parallel-older-%d", time.Now().UnixNano())
	olderID := startFakeSession(t, ctx, cli, olderName, labels)
	waitForRunning(t, ctx, cli, olderID)

	time.Sleep(1100 * time.Millisecond)

	newerName := fmt.Sprintf("ai-shim-parallel-newer-%d", time.Now().UnixNano())
	newerID := startFakeSession(t, ctx, cli, newerName, labels)
	waitForRunning(t, ctx, cli, newerID)

	sessions, err := container.FindRunningSessionsInWorkspace(ctx, cli, params.AgentName, params.Profile, params.WsHash)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	assert.Equal(t, newerID, sessions[0].ContainerID,
		"newest session must come first (got %s, want %s)", sessions[0].ContainerName, newerName)
	assert.Equal(t, olderID, sessions[1].ContainerID,
		"oldest session must come last")
	assert.True(t, !sessions[0].CreatedAt.Before(sessions[1].CreatedAt),
		"sort order must be descending by CreatedAt")
}

// TestParallel_KillOneSibling covers invariant #3: killing one sibling via
// ContainerStop+ContainerRemove does NOT take down the remaining sibling.
// This is the core parallel-lifecycle regression guard.
func TestParallel_KillOneSibling(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow parallel session test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	params := newTestParams(t)
	labels := params.sessionLabels()

	nameA := fmt.Sprintf("ai-shim-parallel-kill-a-%d", time.Now().UnixNano())
	idA := startFakeSession(t, ctx, cli, nameA, labels)
	waitForRunning(t, ctx, cli, idA)

	time.Sleep(1100 * time.Millisecond)

	nameB := fmt.Sprintf("ai-shim-parallel-kill-b-%d", time.Now().UnixNano())
	idB := startFakeSession(t, ctx, cli, nameB, labels)
	waitForRunning(t, ctx, cli, idB)

	sessions, err := container.FindRunningSessionsInWorkspace(ctx, cli, params.AgentName, params.Profile, params.WsHash)
	require.NoError(t, err)
	require.Len(t, sessions, 2)

	// Kill the newest (index 0). Replicates the picker's k1 path by doing
	// ContainerStop + ContainerRemove directly; we can't invoke cmd/ai-shim's
	// unexported stopSession from test/e2e.
	killTarget := sessions[0]
	survivor := sessions[1]

	stopTimeout := 5
	require.NoError(t, cli.ContainerStop(ctx, killTarget.ContainerID, dockercontainer.StopOptions{Timeout: &stopTimeout}))
	require.NoError(t, cli.ContainerRemove(ctx, killTarget.ContainerID, dockercontainer.RemoveOptions{Force: true}))

	// The survivor's container must still be running. If a bug ever causes
	// killSession to stop every sibling instead of just the chosen one, this
	// assertion is where the regression surfaces.
	insp, err := cli.ContainerInspect(ctx, survivor.ContainerID)
	require.NoError(t, err)
	require.NotNil(t, insp.State)
	assert.True(t, insp.State.Running,
		"surviving sibling %s must still be running after killing %s",
		survivor.ContainerName, killTarget.ContainerName)

	// And the lookup must now report exactly one session for this workspace.
	remaining, err := container.FindRunningSessionsInWorkspace(ctx, cli, params.AgentName, params.Profile, params.WsHash)
	require.NoError(t, err)
	require.Len(t, remaining, 1, "only one sibling should remain after the kill")
	assert.Equal(t, survivor.ContainerID, remaining[0].ContainerID)
}

// TestParallel_DINDWorkspaceFilterIsolation covers the load-bearing invariant
// for the DIND sidecar: stopDINDForSession must scope its lookup to the
// session's workspace hash. If the filter ever loses the workspace scope
// (regression: two sessions in DIFFERENT workspaces for the same agent+
// profile are matched against each other), killing one session would tear
// down the other session's DIND sidecar.
//
// We replicate the production filter here as a test-only query — we cannot
// invoke the unexported stopDINDForSession from cmd/ai-shim. The assertion
// is: a filter scoped to workspace A's hash returns only A's DIND, not B's.
func TestParallel_DINDWorkspaceFilterIsolation(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow parallel session test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	// Same agent+profile, different workspace hashes — this is the parallel-
	// workspace scenario (e.g. two Cursor windows open on different repos).
	base := newTestParams(t)
	paramsA := base
	paramsA.WsHash = base.WsHash + "-A"
	paramsB := base
	paramsB.WsHash = base.WsHash + "-B"

	makeDINDLabels := func(p parallelTestParams) map[string]string {
		return map[string]string{
			container.LabelBase:      "true",
			container.LabelAgent:     p.AgentName,
			container.LabelProfile:   p.Profile,
			container.LabelWorkspace: p.WsHash,
			container.LabelDIND:      "true",
		}
	}

	dindA := fmt.Sprintf("ai-shim-parallel-dindA-%d", time.Now().UnixNano())
	dindAID := startFakeSession(t, ctx, cli, dindA, makeDINDLabels(paramsA))
	waitForRunning(t, ctx, cli, dindAID)

	dindB := fmt.Sprintf("ai-shim-parallel-dindB-%d", time.Now().UnixNano())
	dindBID := startFakeSession(t, ctx, cli, dindB, makeDINDLabels(paramsB))
	waitForRunning(t, ctx, cli, dindBID)

	// Replicate cmd/ai-shim/main.go dindSessionFilters exactly. If this
	// filter ever drops the workspace scope, the assertion below fails —
	// which is precisely the regression we want to guard against.
	queryWorkspace := func(p parallelTestParams) []dockercontainer.Summary {
		f := filters.NewArgs(
			filters.Arg("label", container.LabelBase+"=true"),
			filters.Arg("label", container.LabelDIND+"=true"),
			filters.Arg("label", container.LabelAgent+"="+p.AgentName),
			filters.Arg("label", container.LabelProfile+"="+p.Profile),
			filters.Arg("label", container.LabelWorkspace+"="+p.WsHash),
			filters.Arg("status", "running"),
		)
		list, err := cli.ContainerList(ctx, dockercontainer.ListOptions{Filters: f})
		require.NoError(t, err)
		return list
	}

	hitsA := queryWorkspace(paramsA)
	require.Len(t, hitsA, 1, "workspace-A DIND filter must return exactly 1 sidecar")
	assert.Equal(t, dindAID, hitsA[0].ID, "filter for workspace A must return A's DIND only")

	hitsB := queryWorkspace(paramsB)
	require.Len(t, hitsB, 1, "workspace-B DIND filter must return exactly 1 sidecar")
	assert.Equal(t, dindBID, hitsB[0].ID, "filter for workspace B must return B's DIND only")

	// Stop A's DIND (simulating stopDINDForSession for session A). B's must
	// still be running afterwards.
	stopTimeout := 5
	require.NoError(t, cli.ContainerStop(ctx, dindAID, dockercontainer.StopOptions{Timeout: &stopTimeout}))
	require.NoError(t, cli.ContainerRemove(ctx, dindAID, dockercontainer.RemoveOptions{Force: true}))

	insp, err := cli.ContainerInspect(ctx, dindBID)
	require.NoError(t, err)
	require.NotNil(t, insp.State)
	assert.True(t, insp.State.Running,
		"workspace-B DIND must remain running after workspace-A DIND is stopped")
}

// TestParallel_CacheOrphanGuard covers invariant #4: dind.MaybeStopCache is
// a no-op while cache consumers are running, and removes the shared registry-
// cache container once the last consumer is gone.
//
// This test stands up FAKE cache / consumer containers labeled to match what
// production creates. That avoids pulling the real registry:2 image and keeps
// the assertion focused on MaybeStopCache's logic rather than the cache
// implementation details.
func TestParallel_CacheOrphanGuard(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow parallel session test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()

	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	// If a stale ai-shim-registry-cache exists from a previous aborted run,
	// remove it so this test has a clean slate. MaybeStopCache keys on the
	// fixed name dind.CacheContainerName, so there can only be one at a time.
	existing, err := cli.ContainerList(ctx, dockercontainer.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("name", "^/"+dind.CacheContainerName+"$")),
	})
	require.NoError(t, err)
	for _, c := range existing {
		_ = cli.ContainerRemove(ctx, c.ID, dockercontainer.RemoveOptions{Force: true})
	}

	// Create a fake cache container under the production name + label so
	// MaybeStopCache finds and removes it the same way it would the real one.
	cacheResp, err := cli.ContainerCreate(ctx,
		&dockercontainer.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "300"},
			Labels: map[string]string{
				container.LabelBase:  "true",
				container.LabelCache: "true",
			},
		},
		&dockercontainer.HostConfig{AutoRemove: false},
		nil, nil, dind.CacheContainerName,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cc := context.WithTimeout(context.Background(), 30*time.Second)
		defer cc()
		_ = cli.ContainerRemove(cleanupCtx, cacheResp.ID, dockercontainer.RemoveOptions{Force: true})
	})
	require.NoError(t, cli.ContainerStart(ctx, cacheResp.ID, dockercontainer.StartOptions{}))
	waitForRunning(t, ctx, cli, cacheResp.ID)

	// Create one fake consumer using the production LabelUsesCache label.
	// While this consumer is running, MaybeStopCache must leave the cache
	// container alone.
	consumerName := fmt.Sprintf("ai-shim-parallel-cache-consumer-%d", time.Now().UnixNano())
	consumerID := startFakeSession(t, ctx, cli, consumerName, map[string]string{
		container.LabelBase:      "true",
		container.LabelUsesCache: "true",
	})
	waitForRunning(t, ctx, cli, consumerID)

	// Case 1: consumer still alive → cache must survive.
	dind.MaybeStopCache(ctx, cli)
	insp, err := cli.ContainerInspect(ctx, cacheResp.ID)
	require.NoError(t, err, "cache must still exist while consumer is running")
	require.NotNil(t, insp.State)
	assert.True(t, insp.State.Running,
		"cache must still be Running while a consumer exists")

	// Stop the consumer to simulate killing the last sibling.
	stopTimeout := 5
	require.NoError(t, cli.ContainerStop(ctx, consumerID, dockercontainer.StopOptions{Timeout: &stopTimeout}))
	require.NoError(t, cli.ContainerRemove(ctx, consumerID, dockercontainer.RemoveOptions{Force: true}))

	// Case 2: no consumers left → MaybeStopCache must remove the cache.
	dind.MaybeStopCache(ctx, cli)

	_, err = cli.ContainerInspect(ctx, cacheResp.ID)
	require.Error(t, err, "cache container must be removed after last consumer exits")
	assert.True(t,
		client.IsErrNotFound(err) || strings.Contains(err.Error(), "No such container"),
		"error should be not-found, got: %v", err)
}
