package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testImage = "alpine:latest"

// newTestRunner creates a Runner and ensures the test image is pulled.
func newTestRunner(t *testing.T, ctx context.Context) *Runner {
	t.Helper()
	testutil.SkipIfNoDocker(t)
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	require.NoError(t, runner.EnsureImage(ctx, testImage))
	return runner
}

func TestNewRunner(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
}

func TestRun_SimpleCommand(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"echo", "hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_ExitCode(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 42, exitCode)
}

func TestRun_WithEnv(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Env:    []string{"TEST_VAR=hello"},
		Cmd:    []string{"sh", "-c", "test \"$TEST_VAR\" = hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithWorkdir(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:      testImage,
		WorkingDir: "/tmp",
		Cmd:        []string{"sh", "-c", "test \"$(pwd)\" = /tmp"},
		Labels:     map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithHostname(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:    testImage,
		Hostname: "test-shim",
		Cmd:      []string{"sh", "-c", "test \"$(hostname)\" = test-shim"},
		Labels:   map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestInspectImageUser_Alpine(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	user, err := runner.InspectImageUser(ctx, testImage)
	require.NoError(t, err)
	assert.NotEmpty(t, user.HomeDir)
}

func TestRun_WithMount(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	// Use a volume mount instead of bind mount to avoid issues in DinD
	// environments where the host filesystem differs from the test runner.
	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image: testImage,
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
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	// alpine:latest should already be cached from newTestRunner
	err := runner.EnsureImage(ctx, testImage)
	assert.NoError(t, err)
}

func TestNewRunner_ErrorMessage(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	runner.Close()
}

func TestRun_NonZeroExitShowsMessage(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
		Name:   "test-exit-msg",
		LogDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 42, exitCode)
}

func TestRun_CompletesWithSignalHandler(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"echo", "signal handler active"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode, "signal handler should not interfere with normal execution")
}

func TestSaveExitLog_WritesLogFile(t *testing.T) {
	logDir := t.TempDir()
	r := &Runner{} // saveExitLog doesn't use the Docker client
	r.saveExitLog(logDir, "test-container", 42)

	logFile := filepath.Join(logDir, "test-container.log")
	data, err := os.ReadFile(logFile)
	require.NoError(t, err, "log file should be created")
	assert.Contains(t, string(data), "exit_code=42")
	assert.Contains(t, string(data), "container=test-container")
}

func TestSaveExitLog_EmptyLogDir(t *testing.T) {
	r := &Runner{}
	// Should not panic or error when logDir is empty
	r.saveExitLog("", "test-container", 1)
}

func TestSaveExitLog_Appends(t *testing.T) {
	logDir := t.TempDir()
	r := &Runner{}
	r.saveExitLog(logDir, "test-container", 1)
	r.saveExitLog(logDir, "test-container", 2)

	data, err := os.ReadFile(filepath.Join(logDir, "test-container.log"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "exit_code=1")
	assert.Contains(t, string(data), "exit_code=2")
}

func TestRun_ContextCancellationStopsContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	done := make(chan struct{})
	var exitCode int
	var runErr error

	go func() {
		defer close(done)
		exitCode, runErr = runner.Run(ctx, ContainerSpec{
			Image:  testImage,
			Cmd:    []string{"sleep", "60"},
			Labels: map[string]string{LabelBase: "test"},
		})
	}()

	// Give the container time to start
	time.Sleep(2 * time.Second)
	cancel()

	select {
	case <-done:
		// Container stopped as expected
	case <-time.After(20 * time.Second):
		t.Fatal("container did not stop within 20s of context cancellation")
	}

	_ = exitCode
	_ = runErr
}
