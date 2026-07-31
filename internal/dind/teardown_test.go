package dind

import (
	"context"
	"fmt"
	"testing"
	"time"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dnetwork "github.com/docker/docker/api/types/network"
	dvolume "github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// teardownFixture creates a fake sidecar for one session: a long-sleeping
// alpine container carrying the labels production stamps on a DIND sidecar
// or a netns holder. Registers its own cleanup.
//
// `sleep infinity` as PID 1 matters: PID 1 installs no default SIGTERM
// handler, so these containers ignore SIGTERM exactly as the real holder
// does. A teardown that stops them gracefully waits out its full timeout.
//
// If networkName is non-empty, the fixture is attached to that network
// instead of the default bridge — used to pin the holder-before-network-
// removal ordering, where the fixture must actually be attached to the
// network RemoveOrphanedForSession inspects for RemoveOrphanedForSession's
// container-count check to have teeth.
func teardownFixture(t *testing.T, ctx context.Context, cli *client.Client, name, sessionName, role, networkName string) string {
	t.Helper()

	labels := map[string]string{
		ai_container.LabelBase:      "true",
		ai_container.LabelAgent:     "test-teardown",
		ai_container.LabelProfile:   "default",
		ai_container.LabelWorkspace: "wshash",
		ai_container.LabelSession:   sessionName,
		ai_container.LabelRole:      role,
	}
	if role == "dind" {
		labels[ai_container.LabelDIND] = "true"
	}

	var netConfig *dnetwork.NetworkingConfig
	if networkName != "" {
		netConfig = &dnetwork.NetworkingConfig{
			EndpointsConfig: map[string]*dnetwork.EndpointSettings{
				networkName: {},
			},
		}
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:      "alpine:latest",
			Entrypoint: []string{"sleep", "infinity"},
			Labels:     labels,
		},
		&container.HostConfig{AutoRemove: false},
		netConfig, nil, name,
	)
	require.NoError(t, err, "creating fixture %q", name)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanupCtx, resp.ID, container.RemoveOptions{Force: true})
	})

	require.NoError(t, cli.ContainerStart(ctx, resp.ID, container.StartOptions{}), "starting fixture %q", name)
	return resp.ID
}

// running reports whether a container exists and is running. A removed
// container reports false, not an error.
func running(t *testing.T, ctx context.Context, cli *client.Client, id string) bool {
	t.Helper()
	insp, err := cli.ContainerInspect(ctx, id)
	if err != nil {
		return false
	}
	return insp.State != nil && insp.State.Running
}

// TestStopForSession_LeavesSiblingSidecarsAlone is the regression test for
// the defect this change fixes. Two sessions run in the same workspace with
// identical agent, profile and workspace labels — the parallel-session case.
// Tearing one down must not touch the other's DIND sidecar or netns holder.
func TestStopForSession_LeavesSiblingSidecarsAlone(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()
	sessionA := fmt.Sprintf("ai-shim-teardown-a-%d", stamp)
	sessionB := fmt.Sprintf("ai-shim-teardown-b-%d", stamp)

	dindA := teardownFixture(t, ctx, cli, sessionA+"-dind", sessionA, "dind", "")
	holderA := teardownFixture(t, ctx, cli, sessionA+"-netns", sessionA, "netns-holder", "")
	dindB := teardownFixture(t, ctx, cli, sessionB+"-dind", sessionB, "dind", "")
	holderB := teardownFixture(t, ctx, cli, sessionB+"-netns", sessionB, "netns-holder", "")

	// Volumes are named after the DIND container StopForSession finds, not
	// the fixture helper's own bookkeeping, so create them here to exercise
	// the removal loop instead of always hitting its not-found branch.
	socketVol := sessionA + "-dind-socket"
	certsVol := sessionA + "-dind-certs"
	for _, name := range []string{socketVol, certsVol} {
		_, err := cli.VolumeCreate(ctx, dvolume.CreateOptions{Name: name})
		require.NoError(t, err, "creating fixture volume %q", name)
		t.Cleanup(func(name string) func() {
			return func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = cli.VolumeRemove(cleanupCtx, name, true)
			}
		}(name))
	}

	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: sessionA,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	require.NoError(t, err)

	assert.False(t, running(t, ctx, cli, dindA), "session A's DIND must be gone")
	assert.False(t, running(t, ctx, cli, holderA), "session A's netns holder must be gone")
	assert.True(t, running(t, ctx, cli, dindB),
		"sibling session B's DIND must survive: destroying it strands a running agent")
	assert.True(t, running(t, ctx, cli, holderB),
		"sibling session B's netns holder must survive: destroying it is unrecoverable")

	for _, name := range []string{socketVol, certsVol} {
		_, err := cli.VolumeInspect(ctx, name)
		assert.True(t, cerrdefs.IsNotFound(err),
			"session A's volume %q must be removed by teardown, got err=%v", name, err)
	}
}

// TestStopForSession_NoMatchIsNotAnError covers the upgrade case and the
// double-teardown case: a session with no sidecars left tears down cleanly.
func TestStopForSession_NoMatchIsNotAnError(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	err = StopForSession(ctx, runner.Client(), &ai_container.RunningSession{
		ContainerName: fmt.Sprintf("ai-shim-teardown-absent-%d", time.Now().UnixNano()),
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	assert.NoError(t, err, "a session with no sidecars must tear down cleanly")
}

// TestStopForSession_RemovesNetworkOnlyAfterHolderStops pins the ordering
// documented in StopForSession: the netns holder must be stopped before
// network.RemoveOrphanedForSession runs. A still-running holder stays
// attached to the session's network, so RemoveOrphanedForSession's
// container-count check would see it as non-orphaned and leave it running —
// this network's cleanup fixture only matches the holder-then-network order
// because the DIND and holder fixtures below are actually attached to it.
func TestStopForSession_RemovesNetworkOnlyAfterHolderStops(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()
	sessionName := fmt.Sprintf("ai-shim-teardown-order-%d", stamp)
	netName := fmt.Sprintf("ai-shim-teardown-order-net-%d", stamp)

	_, err = cli.NetworkCreate(ctx, netName, dnetwork.CreateOptions{
		Labels: map[string]string{
			ai_container.LabelBase:      "true",
			ai_container.LabelAgent:     "test-teardown",
			ai_container.LabelProfile:   "default",
			ai_container.LabelWorkspace: "wshash",
		},
	})
	require.NoError(t, err, "creating fixture network %q", netName)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cli.NetworkRemove(cleanupCtx, netName)
	})

	teardownFixture(t, ctx, cli, sessionName+"-dind", sessionName, "dind", netName)
	teardownFixture(t, ctx, cli, sessionName+"-netns", sessionName, "netns-holder", netName)

	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: sessionName,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	require.NoError(t, err)

	networks, err := cli.NetworkList(ctx, dnetwork.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^"+netName+"$")),
	})
	require.NoError(t, err)
	assert.Empty(t, networks,
		"session network must be removed: it only looks orphaned once the holder that kept it attached is stopped first")
}

// TestStopForSession_DoesNotWaitOutHolderSIGTERM pins the holder's removal
// to a force-remove. The holder is `sleep infinity` as PID 1, which ignores
// SIGTERM, so a graceful stop can only wait out its full timeout before the
// daemon force-kills it anyway. The fixture reproduces that exactly, so a
// reintroduced ContainerStop shows up as elapsed time.
func TestStopForSession_DoesNotWaitOutHolderSIGTERM(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	session := fmt.Sprintf("ai-shim-teardown-stall-%d", time.Now().UnixNano())
	holder := teardownFixture(t, ctx, cli, session+"-netns", session, "netns-holder", "")

	start := time.Now()
	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: session,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.False(t, running(t, ctx, cli, holder), "the netns holder must be gone")
	assert.Less(t, elapsed, 4*time.Second,
		"holder removal waited on SIGTERM (%s); sleep as PID 1 ignores it, so the wait can only ever time out", elapsed)
}

// TestStopForSession_RemovesExitedDIND covers the crash-looping sidecar. A
// DIND carries RestartPolicyUnlessStopped, so one that is between restarts —
// or has exited outright — is not "running". While the filters carried
// status=running and StopForSession listed with All defaulted to false, such
// a sidecar was invisible to teardown: it outlived the session that owned it
// and its socket and certs volumes leaked with it.
func TestStopForSession_RemovesExitedDIND(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed teardown test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()
	session := fmt.Sprintf("ai-shim-teardown-exited-%d", stamp)

	dindID := teardownFixture(t, ctx, cli, session+"-dind", session, "dind", "")

	// Drive the fixture to "exited". `sleep infinity` as PID 1 ignores
	// SIGTERM, so a graceful stop would burn its full timeout; kill instead.
	require.NoError(t, cli.ContainerKill(ctx, dindID, "SIGKILL"))
	require.Eventually(t, func() bool {
		insp, err := cli.ContainerInspect(ctx, dindID)
		return err == nil && insp.State != nil && !insp.State.Running
	}, 30*time.Second, 200*time.Millisecond, "fixture DIND must reach a non-running state")

	socketVol := session + "-dind-socket"
	certsVol := session + "-dind-certs"
	for _, name := range []string{socketVol, certsVol} {
		_, err := cli.VolumeCreate(ctx, dvolume.CreateOptions{Name: name})
		require.NoError(t, err, "creating fixture volume %q", name)
		t.Cleanup(func(name string) func() {
			return func() {
				cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				_ = cli.VolumeRemove(cleanupCtx, name, true)
			}
		}(name))
	}

	err = StopForSession(ctx, cli, &ai_container.RunningSession{
		ContainerName: session,
		AgentName:     "test-teardown",
		Profile:       "default",
		WorkspaceHash: "wshash",
	})
	require.NoError(t, err)

	_, err = cli.ContainerInspect(ctx, dindID)
	assert.True(t, cerrdefs.IsNotFound(err),
		"an exited DIND must still be removed by teardown, got err=%v", err)

	for _, name := range []string{socketVol, certsVol} {
		_, err := cli.VolumeInspect(ctx, name)
		assert.True(t, cerrdefs.IsNotFound(err),
			"an exited DIND's volume %q must be removed too, got err=%v", name, err)
	}
}
