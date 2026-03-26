package docker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_ReturnsValidClient(t *testing.T) {
	ctx := context.Background()
	cli, err := NewClient(ctx)
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	defer cli.Close()

	// Verify client can actually talk to Docker
	info, err := cli.Info(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, info.ServerVersion, "should get Docker server version")
}

func TestNewClientNoPing_ReturnsClient(t *testing.T) {
	cli, err := NewClientNoPing()
	require.NoError(t, err)
	defer cli.Close()
	// Client created but not verified connected — that's the point of NoPing
}
