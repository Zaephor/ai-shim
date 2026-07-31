package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/cli"
	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	dvolume "github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cleanupFixture creates a container carrying the ai-shim base label, which
// is the only thing `manage cleanup` matches on. Registers its own removal.
//
// The Docker client parameter is named dcli, not cli: this package already
// imports internal/cli as cli, and shadowing it here would hide the package
// from the rest of the function.
//
// If volName is non-empty, a volume of that name is created with the same
// base label and mounted, so the volume pass of Cleanup encounters it while
// it is in use.
func cleanupFixture(t *testing.T, ctx context.Context, dcli *client.Client, name, session, role, volName string) string {
	t.Helper()

	hostConfig := &container.HostConfig{AutoRemove: false}
	if volName != "" {
		_, err := dcli.VolumeCreate(ctx, dvolume.CreateOptions{
			Name:   volName,
			Labels: map[string]string{ai_container.LabelBase: "true"},
		})
		require.NoError(t, err, "creating fixture volume %q", volName)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = dcli.VolumeRemove(cleanupCtx, volName, true)
		})
		hostConfig.Binds = []string{volName + ":/data"}
	}

	resp, err := dcli.ContainerCreate(ctx,
		&container.Config{
			Image:      "alpine:latest",
			Entrypoint: []string{"sleep", "infinity"},
			Labels: map[string]string{
				ai_container.LabelBase:      "true",
				ai_container.LabelAgent:     "test-cleanup",
				ai_container.LabelProfile:   "default",
				ai_container.LabelWorkspace: "wshash",
				ai_container.LabelSession:   session,
				ai_container.LabelRole:      role,
			},
		},
		hostConfig,
		nil, nil, name,
	)
	require.NoError(t, err, "creating fixture %q", name)

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = dcli.ContainerRemove(cleanupCtx, resp.ID, container.RemoveOptions{Force: true})
	})

	require.NoError(t, dcli.ContainerStart(ctx, resp.ID, container.StartOptions{}), "starting fixture %q", name)
	return resp.ID
}

// TestCleanup_KeepsRunningRemovesExited is the regression test for cleanup's
// unscoped sweep. It force-removed every ai-shim container on the host
// regardless of state or workspace, so running an unrelated `manage cleanup`
// destroyed live sessions — including other users' sessions on a shared
// daemon — while its help text advertised removing orphans.
func TestCleanup_KeepsRunningRemovesExited(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed cleanup test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	dcli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()
	liveSession := fmt.Sprintf("ai-shim-cleanup-live-%d", stamp)
	deadSession := fmt.Sprintf("ai-shim-cleanup-dead-%d", stamp)
	liveVol := liveSession + "-data"

	liveID := cleanupFixture(t, ctx, dcli, liveSession, liveSession, "agent", liveVol)
	deadID := cleanupFixture(t, ctx, dcli, deadSession, deadSession, "agent", "")

	// Drive the second fixture to "exited". `sleep infinity` as PID 1
	// ignores SIGTERM, so kill rather than stop.
	require.NoError(t, dcli.ContainerKill(ctx, deadID, "SIGKILL"))
	require.Eventually(t, func() bool {
		insp, err := dcli.ContainerInspect(ctx, deadID)
		return err == nil && insp.State != nil && !insp.State.Running
	}, 30*time.Second, 200*time.Millisecond, "the exited fixture must reach a non-running state")

	result, err := cli.Cleanup(false)
	require.NoError(t, err)

	insp, err := dcli.ContainerInspect(ctx, liveID)
	require.NoError(t, err, "the running container must survive cleanup")
	require.NotNil(t, insp.State)
	assert.True(t, insp.State.Running,
		"cleanup must not stop a running session; it removed live sessions in every workspace before this fix")

	_, err = dcli.ContainerInspect(ctx, deadID)
	assert.True(t, cerrdefs.IsNotFound(err),
		"cleanup must still remove an exited container, got err=%v", err)

	// The live session's volume is still mounted, so the daemon refuses to
	// remove it. That is the intended outcome, not a failure to report.
	_, err = dcli.VolumeInspect(ctx, liveVol)
	assert.NoError(t, err, "a live session's volume must survive cleanup")
	for _, f := range result.Failed {
		assert.NotContains(t, f, liveVol,
			"an in-use volume must be skipped silently, not reported as a removal failure")
	}
}

// TestCleanup_ForceRemovesRunning covers the documented rollout path. A
// sidecar leaked by a session launched before the ai-shim.session label
// existed carries RestartPolicyUnlessStopped, so it is still running and is
// not an orphan by state. --force is the only way to reclaim it.
func TestCleanup_ForceRemovesRunning(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed cleanup test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := ai_container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	dcli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	session := fmt.Sprintf("ai-shim-cleanup-force-%d", time.Now().UnixNano())
	id := cleanupFixture(t, ctx, dcli, session, session, "dind", "")

	_, err = cli.Cleanup(true)
	require.NoError(t, err)

	_, err = dcli.ContainerInspect(ctx, id)
	assert.True(t, cerrdefs.IsNotFound(err),
		"--force must remove a running container, got err=%v", err)
}
