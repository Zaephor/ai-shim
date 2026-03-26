package network

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/client"
	dnetwork "github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveName_Global(t *testing.T) {
	name := ResolveName("global", "claude-code", "work", "a1b2c3d4")
	assert.Equal(t, "ai-shim", name)
}

func TestResolveName_Profile(t *testing.T) {
	name := ResolveName("profile", "claude-code", "work", "a1b2c3d4")
	assert.Equal(t, "ai-shim-work", name)
}

func TestResolveName_Workspace(t *testing.T) {
	name := ResolveName("workspace", "claude-code", "work", "a1b2c3d4")
	assert.Equal(t, "ai-shim-a1b2c3d4", name)
}

func TestResolveName_ProfileWorkspace(t *testing.T) {
	name := ResolveName("profile-workspace", "claude-code", "work", "a1b2c3d4")
	assert.Equal(t, "ai-shim-work-a1b2c3d4", name)
}

func TestResolveName_Isolated(t *testing.T) {
	name := ResolveName("isolated", "claude-code", "work", "a1b2c3d4")
	assert.Contains(t, name, "ai-shim-claude-code-work-a1b2c3d4")
}

func TestResolveName_DefaultIsIsolated(t *testing.T) {
	name := ResolveName("", "claude-code", "work", "a1b2c3d4")
	assert.Contains(t, name, "ai-shim-claude-code-work-a1b2c3d4")
}

func TestResolveName_UnknownScopeDefaultsToIsolated(t *testing.T) {
	name := ResolveName("banana", "claude-code", "work", "a1b2c3d4")
	assert.Contains(t, name, "ai-shim-claude-code-work-a1b2c3d4")
}

func TestResolveName_IsolatedHasRandomSuffix(t *testing.T) {
	n1 := ResolveName("isolated", "claude", "work", "abc123")
	n2 := ResolveName("isolated", "claude", "work", "abc123")
	assert.NotEqual(t, n1, n2, "isolated names should differ due to random suffix")
}

func TestEnsureNetwork_CreatesAndReturns(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	handle, err := EnsureNetwork(ctx, cli, "ai-shim-test-ensure", map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	assert.NotEmpty(t, handle.ID)
	assert.True(t, handle.Created, "should have created a new network")

	// Cleanup
	err = handle.Remove(ctx)
	assert.NoError(t, err)
}

func TestEnsureNetwork_HandlesAlreadyExists(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := "ai-shim-test-race-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Create network directly
	resp, err := cli.NetworkCreate(ctx, name, dnetwork.CreateOptions{})
	require.NoError(t, err)
	defer cli.NetworkRemove(ctx, resp.ID)

	// EnsureNetwork should handle the existing network gracefully
	handle, err := EnsureNetwork(ctx, cli, name, nil)
	require.NoError(t, err)
	assert.Equal(t, resp.ID, handle.ID)
	assert.False(t, handle.Created, "should detect pre-existing network")
}

func TestEnsureNetwork_ReusesExisting(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := "ai-shim-test-reuse"

	// Create first
	h1, err := EnsureNetwork(ctx, cli, name, map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	defer h1.Remove(ctx)

	// Second call should reuse
	h2, err := EnsureNetwork(ctx, cli, name, nil)
	require.NoError(t, err)
	assert.Equal(t, h1.ID, h2.ID)
	assert.False(t, h2.Created, "should have reused existing network")
}
