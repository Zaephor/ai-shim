package testutil

import (
	"context"
	"os"
	"testing"

	"github.com/ai-shim/ai-shim/internal/docker"
)

// SkipIfNoDocker skips the test if Docker is not available or if running
// in short mode (-short flag). Docker-dependent tests are integration tests
// that should only run in the E2E CI job, not in the unit test job.
// When AI_SHIM_CI=1, it fails the test instead of skipping (used by the E2E job).
func SkipIfNoDocker(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping Docker test in short mode")
	}
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

// SkipIfNestedDocker skips the test if the current process is running
// inside a Docker container (detected by the presence of /.dockerenv).
// Some Docker features like TLS cert generation in DIND are too slow
// in nested virtualization to complete within test timeouts.
func SkipIfNestedDocker(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("skipping: nested Docker detected (/.dockerenv exists); TLS DIND is too slow in nested virtualization")
	}
}
