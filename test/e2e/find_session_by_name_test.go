package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindSessionByContainerName_ExactMatch(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := fmt.Sprintf("test-find-by-name-%x", time.Now().UnixNano())
	labels := map[string]string{
		container.LabelBase:    "true",
		container.LabelAgent:   "test-agent",
		container.LabelProfile: "test-profile",
		container.LabelRole:    "agent",
	}

	resp, err := cli.ContainerCreate(ctx,
		&dockercontainer.Config{Image: "alpine:latest", Cmd: []string{"sleep", "300"}, Labels: labels},
		&dockercontainer.HostConfig{AutoRemove: false}, nil, nil, name,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanCtx, resp.ID, dockercontainer.RemoveOptions{Force: true})
	})
	require.NoError(t, cli.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}))
	waitForRunning(t, ctx, cli, resp.ID)

	session, err := container.FindSessionByContainerName(ctx, cli, name)
	require.NoError(t, err)
	require.NotNil(t, session)
	assert.Equal(t, name, session.ContainerName)
	assert.Equal(t, "test-agent", session.AgentName)
}

func TestFindSessionByContainerName_NotFound(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	session, err := container.FindSessionByContainerName(ctx, cli, "nonexistent-container-name")
	require.NoError(t, err)
	assert.Nil(t, session)
}

func TestFindSessionByContainerName_RejectsNonAiShim(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := fmt.Sprintf("test-non-aishim-%x", time.Now().UnixNano())
	resp, err := cli.ContainerCreate(ctx,
		&dockercontainer.Config{Image: "alpine:latest", Cmd: []string{"sleep", "300"}},
		&dockercontainer.HostConfig{AutoRemove: false}, nil, nil, name,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanCtx, resp.ID, dockercontainer.RemoveOptions{Force: true})
	})
	require.NoError(t, cli.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}))
	waitForRunning(t, ctx, cli, resp.ID)

	session, err := container.FindSessionByContainerName(ctx, cli, name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an ai-shim container")
	assert.Nil(t, session)
}

func TestFindSessionByContainerName_RejectsStopped(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := fmt.Sprintf("test-stopped-%x", time.Now().UnixNano())
	labels := map[string]string{container.LabelBase: "true"}
	resp, err := cli.ContainerCreate(ctx,
		&dockercontainer.Config{Image: "alpine:latest", Cmd: []string{"true"}, Labels: labels},
		&dockercontainer.HostConfig{AutoRemove: false}, nil, nil, name,
	)
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = cli.ContainerRemove(cleanCtx, resp.ID, dockercontainer.RemoveOptions{Force: true})
	})
	// Start then wait for it to exit (cmd=true exits immediately)
	require.NoError(t, cli.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}))
	statusCh, errCh := cli.ContainerWait(ctx, resp.ID, dockercontainer.WaitConditionNotRunning)
	select {
	case <-statusCh:
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for container to exit")
	}

	session, err := container.FindSessionByContainerName(ctx, cli, name)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
	assert.Nil(t, session)
}
