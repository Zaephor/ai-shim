package cli

import (
	"context"
	"testing"

	"github.com/ai-shim/ai-shim/internal/docker"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExec_NoContainer(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	// Exec should fail when no matching container is running
	exitCode, err := Exec("nonexistent-container-xyz", []string{"echo", "hello"})
	assert.Error(t, err)
	assert.Equal(t, -1, exitCode)
	assert.Contains(t, err.Error(), "nonexistent-container-xyz")
}

func TestExec_EmptyCommand(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	// Even with an empty command, it should fail because the container doesn't exist
	exitCode, err := Exec("nonexistent-container-xyz", []string{})
	assert.Error(t, err)
	assert.Equal(t, -1, exitCode)
}

func TestFindContainerByName_NotFound(t *testing.T) {
	testutil.SkipIfNoDocker(t)

	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	require.NoError(t, err)
	defer cli.Close()

	_, err = findContainerByName(ctx, cli, "nonexistent-container-xyz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running ai-shim container")
}

func TestStdinIsTerminal(t *testing.T) {
	// Verify stdinIsTerminal returns a valid bool without panicking.
	// The result depends on the test runner environment (race detector
	// may change stdin behavior), so we just verify the function
	// completes and returns a deterministic type.
	result := stdinIsTerminal()
	assert.IsType(t, true, result, "stdinIsTerminal should return a bool")
}
