package cli

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/docker"
	"github.com/ai-shim/ai-shim/internal/testutil"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExec_NoContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	// Exec should fail when no matching container is running
	exitCode, err := Exec("nonexistent-container-xyz", []string{"echo", "hello"})
	assert.Error(t, err)
	assert.Equal(t, -1, exitCode)
	assert.Contains(t, err.Error(), "nonexistent-container-xyz")
}

func TestExec_EmptyCommand(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	// Even with an empty command, it should fail because the container doesn't exist
	exitCode, err := Exec("nonexistent-container-xyz", []string{})
	assert.Error(t, err)
	assert.Equal(t, -1, exitCode)
}

func TestFindContainerByName_NotFound(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	require.NoError(t, err)
	defer cli.Close()

	_, err = findContainerByName(ctx, cli, "nonexistent-container-xyz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running ai-shim container")
}

func TestIsTTY(t *testing.T) {
	// Verify IsTTY returns a valid bool without panicking.
	result := container.IsTTY()
	assert.IsType(t, true, result, "IsTTY should return a bool")
}

func TestFindContainerByName_FindsRunning(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	require.NoError(t, err)
	defer cli.Close()

	// Start a labelled container
	name := fmt.Sprintf("test-find-%d", time.Now().UnixNano()%100000)
	resp, err := cli.ContainerCreate(ctx, &container_types.Config{
		Image:  "alpine:latest",
		Cmd:    []string{"sleep", "30"},
		Labels: map[string]string{container.LabelBase: "true"},
	}, &container_types.HostConfig{}, nil, nil, name)
	require.NoError(t, err)
	require.NoError(t, cli.ContainerStart(ctx, resp.ID, container_types.StartOptions{}))
	defer func() {
		_ = cli.ContainerRemove(ctx, resp.ID, container_types.RemoveOptions{Force: true})
	}()

	// findContainerByName should locate it
	id, err := findContainerByName(ctx, cli, name)
	require.NoError(t, err)
	assert.Equal(t, resp.ID, id)
}
