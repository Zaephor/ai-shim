package testutil

import (
	"context"
	"testing"

	"github.com/ai-shim/ai-shim/internal/docker"
)

// SkipIfNoDocker skips the test if Docker is not available.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	cli, err := docker.NewClient(context.Background())
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	cli.Close()
}
