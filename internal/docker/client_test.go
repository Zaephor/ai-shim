package docker

import (
	"context"
	"os"
	"testing"
	"time"

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

func TestNewClient_WithInvalidHost(t *testing.T) {
	orig := os.Getenv("DOCKER_HOST")
	t.Cleanup(func() {
		if orig == "" {
			os.Unsetenv("DOCKER_HOST")
		} else {
			os.Setenv("DOCKER_HOST", orig)
		}
	})

	require.NoError(t, os.Setenv("DOCKER_HOST", "tcp://localhost:99999"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := NewClient(ctx)
	assert.Error(t, err, "NewClient should fail with invalid DOCKER_HOST")
	assert.Contains(t, err.Error(), "cannot connect to docker daemon")
}

func TestNewClientNoPing_ReturnsClient(t *testing.T) {
	cli, err := NewClientNoPing()
	require.NoError(t, err)
	defer cli.Close()
	// Client created but not verified connected — that's the point of NoPing
}
