package container

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/testutil"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNetnsOwner(t *testing.T) {
	tests := []struct {
		name        string
		networkMode string
		wantID      string
		wantOK      bool
	}{
		{"joins a container", "container:abc123def456", "abc123def456", true},
		{"bridge", "bridge", "", false},
		{"host", "host", "", false},
		{"none", "none", "", false},
		{"named network", "ai-shim-work-abc123", "", false},
		{"empty", "", "", false},
		{"prefix with no id", "container:", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := parseNetnsOwner(tt.networkMode)
			if gotID != tt.wantID || gotOK != tt.wantOK {
				t.Errorf("parseNetnsOwner(%q) = (%q, %v), want (%q, %v)",
					tt.networkMode, gotID, gotOK, tt.wantID, tt.wantOK)
			}
		})
	}
}

// TestNetnsOwnerAlive covers the three states that matter on reattach: a
// container not sharing a namespace at all, one sharing a live owner, and
// one whose owner has been destroyed — the unrecoverable case, where the
// dependent keeps only lo and never regains eth0.
func TestNetnsOwnerAlive(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping Docker-backed netns test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
	cli := runner.Client()
	require.NoError(t, runner.EnsureImage(ctx, "alpine:latest"))

	stamp := time.Now().UnixNano()

	start := func(name string, hostCfg *dockercontainer.HostConfig) string {
		resp, err := cli.ContainerCreate(ctx,
			&dockercontainer.Config{
				Image:      "alpine:latest",
				Entrypoint: []string{"sleep", "infinity"},
			},
			hostCfg, nil, nil, name,
		)
		require.NoError(t, err, "creating %q", name)
		t.Cleanup(func() {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = cli.ContainerRemove(cleanupCtx, resp.ID, dockercontainer.RemoveOptions{Force: true})
		})
		require.NoError(t, cli.ContainerStart(ctx, resp.ID, dockercontainer.StartOptions{}), "starting %q", name)
		return resp.ID
	}

	ownerID := start(fmt.Sprintf("ai-shim-netns-owner-%d", stamp), &dockercontainer.HostConfig{})
	joinerID := start(fmt.Sprintf("ai-shim-netns-joiner-%d", stamp), &dockercontainer.HostConfig{
		NetworkMode: dockercontainer.NetworkMode("container:" + ownerID),
	})

	t.Run("not sharing a namespace", func(t *testing.T) {
		_, joined, _ := NetnsOwnerAlive(ctx, cli, ownerID)
		assert.False(t, joined, "a container on its own network must report joined=false")
	})

	t.Run("owner alive", func(t *testing.T) {
		owner, joined, alive := NetnsOwnerAlive(ctx, cli, joinerID)
		assert.True(t, joined)
		assert.True(t, alive, "a live owner must report alive")
		assert.Equal(t, ownerID, owner)
	})

	t.Run("owner destroyed", func(t *testing.T) {
		require.NoError(t, cli.ContainerRemove(ctx, ownerID, dockercontainer.RemoveOptions{Force: true}))

		_, joined, alive := NetnsOwnerAlive(ctx, cli, joinerID)
		assert.True(t, joined, "the joiner still records its owner in HostConfig after the owner is gone")
		assert.False(t, alive, "a destroyed owner must report not alive: this session cannot recover networking")
	})
}
