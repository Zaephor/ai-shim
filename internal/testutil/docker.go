package testutil

import (
	"context"
	"testing"

	"github.com/docker/docker/client"
)

// SkipIfNoDocker skips the test if Docker is not available.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	defer cli.Close()
	ctx := context.Background()
	if _, err := cli.Ping(ctx); err != nil {
		t.Skip("Docker not available:", err)
	}
}
