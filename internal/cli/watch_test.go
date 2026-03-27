package cli

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWatchRetries_Default(t *testing.T) {
	t.Setenv("AI_SHIM_WATCH_RETRIES", "")
	assert.Equal(t, 3, WatchRetries())
}

func TestWatchRetries_CustomValue(t *testing.T) {
	t.Setenv("AI_SHIM_WATCH_RETRIES", "5")
	assert.Equal(t, 5, WatchRetries())
}

func TestWatchRetries_Zero(t *testing.T) {
	t.Setenv("AI_SHIM_WATCH_RETRIES", "0")
	assert.Equal(t, 0, WatchRetries())
}

func TestWatchRetries_Invalid(t *testing.T) {
	t.Setenv("AI_SHIM_WATCH_RETRIES", "abc")
	assert.Equal(t, 3, WatchRetries())
}

func TestWatchRetries_Negative(t *testing.T) {
	t.Setenv("AI_SHIM_WATCH_RETRIES", "-1")
	assert.Equal(t, 3, WatchRetries())
}

func TestWatchRetries_Unset(t *testing.T) {
	os.Unsetenv("AI_SHIM_WATCH_RETRIES")
	assert.Equal(t, 3, WatchRetries())
}

func TestWatchLoop_CleanExit(t *testing.T) {
	callCount := 0
	runFn := func() (int, error) {
		callCount++
		return 0, nil
	}
	noSleep := func(d time.Duration) {}

	exitCode, err := WatchLoop(3, runFn, noSleep)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 1, callCount, "should run exactly once on clean exit")
}

func TestWatchLoop_RetriesOnFailure(t *testing.T) {
	callCount := 0
	runFn := func() (int, error) {
		callCount++
		if callCount <= 2 {
			return 1, nil // fail first 2 times
		}
		return 0, nil // succeed on 3rd
	}
	noSleep := func(d time.Duration) {}

	exitCode, err := WatchLoop(3, runFn, noSleep)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 3, callCount, "should retry twice then succeed")
}

func TestWatchLoop_ExhaustsRetries(t *testing.T) {
	callCount := 0
	runFn := func() (int, error) {
		callCount++
		return 1, nil // always fail
	}
	noSleep := func(d time.Duration) {}

	exitCode, err := WatchLoop(3, runFn, noSleep)
	require.NoError(t, err)
	assert.Equal(t, 1, exitCode)
	assert.Equal(t, 4, callCount, "should run initial + 3 retries")
}

func TestWatchLoop_StopsOnError(t *testing.T) {
	callCount := 0
	runFn := func() (int, error) {
		callCount++
		return -1, assert.AnError
	}
	noSleep := func(d time.Duration) {}

	exitCode, err := WatchLoop(3, runFn, noSleep)
	assert.Error(t, err)
	assert.Equal(t, -1, exitCode)
	assert.Equal(t, 1, callCount, "should stop immediately on error")
}

func TestWatchLoop_ZeroRetries(t *testing.T) {
	callCount := 0
	runFn := func() (int, error) {
		callCount++
		return 1, nil
	}
	noSleep := func(d time.Duration) {}

	exitCode, err := WatchLoop(0, runFn, noSleep)
	require.NoError(t, err)
	assert.Equal(t, 1, exitCode)
	assert.Equal(t, 1, callCount, "with 0 retries, should run once and stop")
}

func TestWatchLoop_SleepCalledBetweenRetries(t *testing.T) {
	sleepCalls := 0
	var sleepDuration time.Duration
	runFn := func() (int, error) {
		if sleepCalls < 2 {
			return 1, nil
		}
		return 0, nil
	}
	sleepFn := func(d time.Duration) {
		sleepCalls++
		sleepDuration = d
	}

	exitCode, err := WatchLoop(3, runFn, sleepFn)
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
	assert.Equal(t, 2, sleepCalls, "should sleep between each retry")
	assert.Equal(t, watchRestartDelay, sleepDuration, "should use configured delay")
}

func TestWatchLoop_PreservesNonZeroExitCode(t *testing.T) {
	runFn := func() (int, error) {
		return 42, nil
	}
	noSleep := func(d time.Duration) {}

	exitCode, err := WatchLoop(1, runFn, noSleep)
	require.NoError(t, err)
	assert.Equal(t, 42, exitCode, "should preserve the actual exit code")
}
