package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/ai-shim/ai-shim/internal/docker"
)

// SkipIfNoDocker skips the test if Docker is not available.
// When AI_SHIM_CI=1, it fails the test instead of skipping.
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		if os.Getenv("AI_SHIM_CI") == "1" {
			t.Fatalf("Docker required in CI but not available: %v", err)
		}
		t.Skip("Docker not available:", err)
	}
	_ = cli.Close()
}
