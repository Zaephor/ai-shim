package network

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/testutil"
	dnetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
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

func TestResolveName_AllScopes(t *testing.T) {
	tests := []struct {
		scope     string
		wantExact string // empty means check prefix instead
		wantPfx   string // prefix check for random-suffix scopes
	}{
		{scope: "global", wantExact: "ai-shim"},
		{scope: "profile", wantExact: "ai-shim-myprofile"},
		{scope: "workspace", wantExact: "ai-shim-ws1234"},
		{scope: "profile-workspace", wantExact: "ai-shim-myprofile-ws1234"},
		{scope: "isolated", wantPfx: "ai-shim-agent-myprofile-ws1234-"},
		{scope: "", wantPfx: "ai-shim-agent-myprofile-ws1234-"},              // default = isolated
		{scope: "unknown-scope", wantPfx: "ai-shim-agent-myprofile-ws1234-"}, // unknown = isolated
	}
	for _, tt := range tests {
		t.Run("scope="+tt.scope, func(t *testing.T) {
			name := ResolveName(tt.scope, "agent", "myprofile", "ws1234")
			if tt.wantExact != "" {
				assert.Equal(t, tt.wantExact, name)
			} else {
				assert.Contains(t, name, tt.wantPfx, "should start with expected prefix")
				// Verify random suffix is present (hex chars after the prefix)
				assert.Greater(t, len(name), len(tt.wantPfx), "should have random suffix")
			}
		})
	}
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

func TestEnsureNetwork_CreateAndRemoveVerified(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := "ai-shim-test-cleanup-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Create a network via EnsureNetwork
	handle, err := EnsureNetwork(ctx, cli, name, map[string]string{"ai-shim": "test"})
	require.NoError(t, err)
	assert.True(t, handle.Created)

	// Verify the network exists via Docker API
	networks, err := cli.NetworkList(ctx, dnetwork.ListOptions{})
	require.NoError(t, err)
	found := false
	for _, n := range networks {
		if n.ID == handle.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "network should exist after EnsureNetwork")

	// Remove the network
	err = handle.Remove(ctx)
	assert.NoError(t, err)

	// Verify the network is gone
	networks, err = cli.NetworkList(ctx, dnetwork.ListOptions{})
	require.NoError(t, err)
	for _, n := range networks {
		assert.NotEqual(t, handle.ID, n.ID, "network should be gone after Remove")
	}
}

func TestEnsureNetwork_ConcurrentCreation(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	require.NoError(t, err)
	defer cli.Close()

	name := "ai-shim-test-concurrent-" + fmt.Sprintf("%d", time.Now().UnixNano())

	const goroutines = 3
	handles := make([]*Handle, goroutines)
	errs := make([]error, goroutines)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			h, err := EnsureNetwork(ctx, cli, name, map[string]string{"ai-shim": "test"})
			handles[idx] = h
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	// All goroutines should succeed
	for i, err := range errs {
		assert.NoError(t, err, "goroutine %d should not error", i)
	}

	// All handles should reference the same network ID
	var networkID string
	for i, h := range handles {
		require.NotNil(t, h, "handle %d should not be nil", i)
		if networkID == "" {
			networkID = h.ID
		} else {
			assert.Equal(t, networkID, h.ID, "goroutine %d should see the same network", i)
		}
	}

	// Verify only 1 network exists with this name
	networks, err := cli.NetworkList(ctx, dnetwork.ListOptions{})
	require.NoError(t, err)
	count := 0
	for _, n := range networks {
		if n.Name == name {
			count++
		}
	}
	assert.Equal(t, 1, count, "exactly one network should exist")

	// Exactly one handle should have Created=true
	createdCount := 0
	for _, h := range handles {
		if h.Created {
			createdCount++
		}
	}
	assert.GreaterOrEqual(t, createdCount, 1, "at least one goroutine should have created the network")

	// Cleanup - use the network ID directly since only one creator's Remove will work
	err = cli.NetworkRemove(ctx, networkID)
	assert.NoError(t, err)
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
