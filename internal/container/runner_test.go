package container

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	result, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"echo", "hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
}

// TestRun_FastExitDoesNotHang is a regression test for a race condition
// introduced in the detach/reattach refactor (commit de59ef8) where the
// Run function called ContainerStart before ContainerAttach. For fast-exit
// commands (`echo hello` finishes in ~1ms), the container would terminate
// before the attach connection was established, leaving stdcopy.StdCopy
// blocked forever on a stream that never produced data or EOF.
//
// The loop increases the chance of hitting the race on machines where the
// timing is tighter; a single iteration is sufficient to hang on most
// hosts. A per-Run context deadline ensures the test fails fast instead of
// timing out the whole test binary if the bug regresses.
func TestRun_FastExitDoesNotHang(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	for i := 0; i < 5; i++ {
		runCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		result, err := runner.Run(runCtx, ContainerSpec{
			Image:  testImage,
			Cmd:    []string{"echo", "hello"},
			Labels: map[string]string{"ai-shim": "test"},
		})
		cancel()
		require.NoError(t, err, "iteration %d: Run must not hang when the container exits before attach", i)
		assert.Equal(t, 0, result.ExitCode, "iteration %d", i)
	}
}

func TestRun_ExitCode(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	result, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestRun_WithEnv(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	result, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Env:    []string{"TEST_VAR=hello"},
		Cmd:    []string{"sh", "-c", "test \"$TEST_VAR\" = hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
}

func TestRun_WithWorkdir(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	result, err := runner.Run(ctx, ContainerSpec{
		Image:      testImage,
		WorkingDir: "/tmp",
		Cmd:        []string{"sh", "-c", "test \"$(pwd)\" = /tmp"},
		Labels:     map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
}

func TestRun_WithHostname(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	result, err := runner.Run(ctx, ContainerSpec{
		Image:    testImage,
		Hostname: "test-shim",
		Cmd:      []string{"sh", "-c", "test \"$(hostname)\" = test-shim"},
		Labels:   map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
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
	result, err := runner.Run(ctx, ContainerSpec{
		Image: testImage,
		Mounts: []mount.Mount{
			{Type: mount.TypeVolume, Target: "/testmount"},
		},
		Cmd:    []string{"test", "-d", "/testmount"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode)
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

	result, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
		Name:   "test-exit-msg",
		LogDir: t.TempDir(),
	})
	require.NoError(t, err)
	assert.Equal(t, 42, result.ExitCode)
}

func TestRun_CompletesWithSignalHandler(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	result, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"echo", "signal handler active"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, result.ExitCode, "signal handler should not interfere with normal execution")
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

func TestSaveExitLog_LogDirIsFile(t *testing.T) {
	// If logDir points to a regular file (not a directory), saveExitLog should
	// not panic — it should silently fail because MkdirAll will error.
	tmpDir := t.TempDir()
	fakeDir := filepath.Join(tmpDir, "not-a-dir")
	require.NoError(t, os.WriteFile(fakeDir, []byte("I am a file"), 0644))

	r := &Runner{}
	// Must not panic
	r.saveExitLog(fakeDir, "test-container", 42)

	// The file should still be a regular file, not replaced
	info, err := os.Stat(fakeDir)
	require.NoError(t, err)
	assert.False(t, info.IsDir(), "file should not have been replaced with a directory")
}

func TestSaveExitLog_AppendsMultipleEntries(t *testing.T) {
	logDir := t.TempDir()
	r := &Runner{}

	r.saveExitLog(logDir, "multi", 1)
	r.saveExitLog(logDir, "multi", 2)
	r.saveExitLog(logDir, "multi", 3)

	data, err := os.ReadFile(filepath.Join(logDir, "multi.log"))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, lines, 3, "should have 3 log entries from 3 calls")
	assert.Contains(t, lines[0], "exit_code=1")
	assert.Contains(t, lines[1], "exit_code=2")
	assert.Contains(t, lines[2], "exit_code=3")
}

// TestRun_NoGoroutineLeakWithBackgroundContext is a regression test for a bug
// where Run() spawned a watcher goroutine doing `<-ctx.Done()`. When callers
// passed context.Background() (whose Done() returns nil), the watcher blocked
// on a nil channel forever and leaked one goroutine per Run() call. CI stack
// dumps showed 30+ leaked goroutines accumulating during the e2e suite.
//
// The fix wires a `runDone` channel that defer-closes on every Run() return
// path; the watcher selects on both, so normal exits free it.
func TestRun_NoGoroutineLeakWithBackgroundContext(t *testing.T) {
	ctx := context.Background()
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	// Settle baseline: do one warm-up Run so any one-shot init goroutines
	// (Docker client lazy state, etc.) are already counted.
	_, err := runner.Run(ctx, ContainerSpec{
		Image:  testImage,
		Cmd:    []string{"true"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	baseline := runtime.NumGoroutine()

	// Run several more containers with the non-cancellable context. Without
	// the fix, each call leaks the watcher goroutine, so the count grows
	// roughly linearly with N. With the fix, it stays flat.
	const n = 5
	for i := 0; i < n; i++ {
		_, err := runner.Run(ctx, ContainerSpec{
			Image:  testImage,
			Cmd:    []string{"true"},
			Labels: map[string]string{"ai-shim": "test"},
		})
		require.NoError(t, err)
	}

	// Allow any in-flight cleanup (deferred close, scheduler) to settle.
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
	after := runtime.NumGoroutine()

	// Allow a small slack for scheduler jitter and unrelated runtime
	// goroutines, but reject anything close to N leaks.
	const slack = 2
	assert.LessOrEqual(t, after, baseline+slack,
		"goroutine leak: baseline=%d after %d Run() calls=%d (slack=%d)",
		baseline, n, after, slack)
}

func TestRun_ContextCancellationStopsContainer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := newTestRunner(t, ctx)
	defer runner.Close()

	done := make(chan struct{})
	var result AttachResult
	var runErr error

	go func() {
		defer close(done)
		result, runErr = runner.Run(ctx, ContainerSpec{
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

	_ = result
	_ = runErr
}

func TestEnsureImage_PullsNewImage(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()

	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Pull a specific small image
	err = runner.EnsureImage(ctx, "alpine:3.20")
	require.NoError(t, err, "should pull alpine:3.20 without error")

	// Second call should be a no-op (image cached)
	err = runner.EnsureImage(ctx, "alpine:3.20")
	require.NoError(t, err, "cached image should return without error")

	// Bogus image should error
	err = runner.EnsureImage(ctx, "nonexistent/image:fake")
	assert.Error(t, err, "non-existent image should produce an error")
}

func TestIsPermanentImagePullError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", fmt.Errorf("manifest for foo:bar not found"), true},
		{"unauthorized", fmt.Errorf("Error response: unauthorized"), true},
		{"denied", fmt.Errorf("pull access denied for foo"), true},
		{"manifest unknown", fmt.Errorf("manifest unknown"), true},
		{"repo missing", fmt.Errorf("repository does not exist"), true},
		{"network", fmt.Errorf("dial tcp: i/o timeout"), false},
		{"connection reset", fmt.Errorf("connection reset by peer"), false},
		{"eof", fmt.Errorf("unexpected EOF"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isPermanentImagePullError(tc.err))
		})
	}
}

// TestEnsureImage_BogusImageFailsFast verifies that pulling a non-existent
// image returns quickly (no 3x retries with backoff for "not found" errors).
func TestEnsureImage_BogusImageFailsFast(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()

	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Bogus image — should fail with a clear "not found"-class error and
	// not retry through 1s + 2s backoff (>3s total).
	start := time.Now()
	err = runner.EnsureImage(ctx, "ai-shim-test-nonexistent/definitely-not-real:fake-tag")
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pulling image")
	// If we retried 3x with backoff (1s+2s) we'd be over 3s. Allow 2.5s of
	// network slop but ensure we didn't burn through full backoff.
	assert.Less(t, elapsed, 3*time.Second, "permanent error should not retry: took %s", elapsed)
}

func TestInspectImageUser_UbuntuImage(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	ctx := context.Background()

	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Ensure the image is available
	require.NoError(t, runner.EnsureImage(ctx, "ubuntu:24.04"))

	user, err := runner.InspectImageUser(ctx, "ubuntu:24.04")
	require.NoError(t, err)
	assert.NotEmpty(t, user.HomeDir, "HomeDir should be non-empty")
	assert.NotEmpty(t, user.Username, "Username should be non-empty")
}
