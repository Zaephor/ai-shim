package dind

import (
	"context"
	"testing"

	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getClient(t *testing.T) *client.Client {
	t.Helper()
	testutil.SkipIfNoDocker(t)
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatal("failed to create docker client:", err)
	}
	return cli
}

func TestStart_AndStop(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	sidecar, err := Start(ctx, cli, Config{
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, sidecar.ContainerID())
	assert.NotEmpty(t, sidecar.NetworkID())

	err = sidecar.Stop(ctx)
	assert.NoError(t, err)
}

func TestStart_CustomImage(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	sidecar, err := Start(ctx, cli, Config{
		Image:  "docker:dind",
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	defer sidecar.Stop(ctx)

	assert.NotEmpty(t, sidecar.ContainerID())
}

func TestDetectSysbox(t *testing.T) {
	cli := getClient(t)
	defer cli.Close()
	ctx := context.Background()

	// Just verify it doesn't panic/error — sysbox likely not available in CI
	_ = DetectSysbox(ctx, cli)
}
