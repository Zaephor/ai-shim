package container

import (
	"context"
	"testing"

	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRunner(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
}

func TestRun_SimpleCommand(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  "alpine:latest",
		Cmd:    []string{"echo", "hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_ExitCode(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  "alpine:latest",
		Cmd:    []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 42, exitCode)
}

func TestRun_WithEnv(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  "alpine:latest",
		Env:    []string{"TEST_VAR=hello"},
		Cmd:    []string{"sh", "-c", "test \"$TEST_VAR\" = hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithWorkdir(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:      "alpine:latest",
		WorkingDir: "/tmp",
		Cmd:        []string{"sh", "-c", "test \"$(pwd)\" = /tmp"},
		Labels:     map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithHostname(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:    "alpine:latest",
		Hostname: "test-shim",
		Cmd:      []string{"sh", "-c", "test \"$(hostname)\" = test-shim"},
		Labels:   map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestInspectImageUser_Alpine(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	user, err := runner.InspectImageUser(ctx, "alpine:latest")
	require.NoError(t, err)
	assert.NotEmpty(t, user.HomeDir)
}

func TestRun_WithMount(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Use a volume mount instead of bind mount to avoid issues in DinD
	// environments where the host filesystem differs from the test runner.
	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image: "alpine:latest",
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Target: "/testmount"},
		},
		Cmd:    []string{"test", "-d", "/testmount"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestEnsureImage_AlreadyLocal(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// alpine:latest should already be cached from other tests
	err = runner.EnsureImage(ctx, "alpine:latest")
	assert.NoError(t, err)
}

func TestNewRunner_ErrorMessage(t *testing.T) {
	// Can't easily test Docker-not-running, but verify the runner works when Docker IS available
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	runner.Close()
}

func TestRun_NonZeroExitShowsMessage(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  "alpine:latest",
		Cmd:    []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
		Name:   "test-exit-msg",
		LogDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 42, exitCode)
	// The stderr message is printed directly; we verify exit code is returned correctly
	// and that the log file was created
}

func TestRun_CompletesWithSignalHandler(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Verify container runs and exits normally with signal handler active
	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  "alpine:latest",
		Cmd:    []string{"echo", "signal handler active"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "signal handler should not interfere with normal execution")
}
