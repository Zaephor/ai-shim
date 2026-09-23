package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReadonlyVolumes_AgentContainer builds a real agent spec from config
// volumes in all three forms (":ro", ":rw", no mode) and runs it. The ro
// mount must be readable but reject writes; the rw and default mounts must
// accept writes that land in the host source directory.
func TestReadonlyVolumes_AgentContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx, cancel := context.WithTimeout(context.Background(), quickRunTimeout)
	defer cancel()

	root := dockerTempDir(t)
	layout := storage.NewLayout(root)
	agentDef, ok := agent.Lookup("opencode")
	require.True(t, ok)
	require.NoError(t, layout.EnsureDirectories(agentDef.Name, "default"))
	require.NoError(t, layout.EnsureAgentData("default", agentDef.DataDirs, agentDef.DataFiles))

	roDir, rwDir, defDir := dockerTempDir(t), dockerTempDir(t), dockerTempDir(t)
	require.NoError(t, os.WriteFile(filepath.Join(roDir, "marker"), []byte("ro-content"), 0644))

	cfg := config.Config{
		Image: "alpine:latest",
		Volumes: []string{
			roDir + ":/mnt/ro:ro",
			rwDir + ":/mnt/rw:rw",
			defDir + ":/mnt/def",
		},
	}

	spec, err := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  "default",
		Layout:   layout,
		Platform: platform.Detect(),
		HomeDir:  "/home/user",
	})
	require.NoError(t, err)

	// Distinct exit codes identify which assertion failed inside the container.
	spec.Entrypoint = []string{"sh", "-c", `
test "$(cat /mnt/ro/marker)" = ro-content || exit 10
if touch /mnt/ro/new 2>/dev/null; then exit 11; fi
echo rw > /mnt/rw/written || exit 12
echo def > /mnt/def/written || exit 13
`}
	spec.Cmd = nil
	spec.TTY = false
	spec.Stdin = false

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	require.NoError(t, runner.EnsureImage(ctx, cfg.Image))

	result, err := runner.Run(ctx, spec)
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode,
		"10=ro unreadable, 11=ro write succeeded, 12=rw write failed, 13=default write failed")

	_, err = os.Stat(filepath.Join(roDir, "new"))
	assert.True(t, os.IsNotExist(err), "write to :ro volume must not reach the host dir")
	data, err := os.ReadFile(filepath.Join(rwDir, "written"))
	require.NoError(t, err, ":rw write must land on the host dir")
	assert.Equal(t, "rw\n", string(data))
	data, err = os.ReadFile(filepath.Join(defDir, "written"))
	require.NoError(t, err, "no-mode write must land on the host dir")
	assert.Equal(t, "def\n", string(data))
}

// TestReadonlyVolumes_DINDNestedRun proves the ro/rw mode carries into the
// DIND sidecar: from an agent-like client, `docker run -v <target>:/x`
// against the sidecar's daemon must fail to write for a :ro volume and
// succeed for a :rw one.
//
// buildDINDSharedMounts lives in package main and is not importable, so
// SharedMounts mirrors its user-volume loop: each resolved volume is bound
// at its agent-side target with the same ReadOnly flag.
func TestReadonlyVolumes_DINDNestedRun(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow DIND readonly-volume test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, "docker:latest"))

	roDir, rwDir := dockerTempDir(t), dockerTempDir(t)
	vols, errs, _ := container.ResolveVolumes([]string{
		roDir + ":/data/ro:ro",
		rwDir + ":/data/rw:rw",
	})
	require.Empty(t, errs)
	require.Len(t, vols, 2)

	var mounts []mount.Mount
	for _, v := range vols {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   v.Source,
			Target:   v.Target,
			ReadOnly: v.ReadOnly,
		})
	}

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-dind-rovol-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(context.Background()) })

	clientGID := os.Getgid()
	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-dind-rovol-sc-%d", time.Now().UnixNano()),
		Hostname:      "dind-rovol",
		NetworkID:     netHandle.ID,
		Labels:        labels,
		SocketGID:     clientGID,
		SharedMounts:  mounts,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = sidecar.Stop(cleanupCtx)
	})

	// Agent-like client: non-root, DIND socket plus the same user volumes
	// at the same targets, as the agent container gets them.
	agentMounts := append([]mount.Mount{{
		Type:   mount.TypeVolume,
		Source: sidecar.SocketVolume(),
		Target: "/var/run/dind",
	}}, mounts...)

	// Distinct exit codes: 10=pull failed, 11=ro nested write succeeded,
	// 12=rw nested write failed.
	result, err := runner.Run(ctx, container.ContainerSpec{
		Name:  fmt.Sprintf("ai-shim-test-dind-rovol-client-%d", time.Now().UnixNano()),
		Image: "docker:latest",
		User:  fmt.Sprintf("%d:%d", os.Getuid(), clientGID),
		Env:   []string{"DOCKER_HOST=unix:///var/run/dind/docker.sock"},
		Entrypoint: []string{"sh", "-c", `
docker pull -q alpine:latest >/dev/null || exit 10
if docker run --rm -v /data/ro:/x alpine sh -c 'touch /x/f'; then exit 11; fi
docker run --rm -v /data/rw:/x alpine sh -c 'touch /x/f' || exit 12
`},
		Mounts:    agentMounts,
		NetworkID: netHandle.ID,
		Labels:    labels,
	})
	require.NoError(t, err)
	require.Equal(t, 0, result.ExitCode,
		"10=alpine pull in DIND failed, 11=nested write to :ro succeeded, 12=nested write to :rw failed")

	_, err = os.Stat(filepath.Join(roDir, "f"))
	assert.True(t, os.IsNotExist(err), "nested write to :ro volume must not reach the host dir")
	_, err = os.Stat(filepath.Join(rwDir, "f"))
	assert.NoError(t, err, "nested write to :rw volume must land on the host dir")
}
