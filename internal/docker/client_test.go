package docker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewClient(t *testing.T) {
	ctx := context.Background()
	cli, err := NewClient(ctx)
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	cli.Close()
}

func TestNewClientNoPing(t *testing.T) {
	cli, err := NewClientNoPing()
	require.NoError(t, err)
	cli.Close()
}
