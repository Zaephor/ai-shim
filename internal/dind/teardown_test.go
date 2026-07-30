package dind

import (
	"context"
	"fmt"
	"testing"
	"time"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/container"
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
func teardownFixture(t *testing.T, ctx context.Context, cli *client.Client, name, sessionName, role string) string {
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

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:      "alpine:latest",
			Entrypoint: []string{"sleep", "infinity"},
			Labels:     labels,
		},
		&container.HostConfig{AutoRemove: false},
		nil, nil, name,
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

	dindA := teardownFixture(t, ctx, cli, sessionA+"-dind", sessionA, "dind")
	holderA := teardownFixture(t, ctx, cli, sessionA+"-netns", sessionA, "netns-holder")
	dindB := teardownFixture(t, ctx, cli, sessionB+"-dind", sessionB, "dind")
	holderB := teardownFixture(t, ctx, cli, sessionB+"-netns", sessionB, "netns-holder")

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
