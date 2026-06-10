package cli

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"
)

const (
	defaultWatchRetries = 3
	watchRestartDelay   = 2 * time.Second
)

// WatchRetries reads the max retry count from AI_SHIM_WATCH_RETRIES env var.
// Returns defaultWatchRetries if the env var is unset or invalid.
func WatchRetries() int {
	v := os.Getenv("AI_SHIM_WATCH_RETRIES")
	if v == "" {
		return defaultWatchRetries
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		fmt.Fprintf(os.Stderr, "ai-shim: invalid AI_SHIM_WATCH_RETRIES=%q, using default %d\n", v, defaultWatchRetries)
		return defaultWatchRetries
	}
	return n
}

// WatchLoop repeatedly invokes runFn until it returns exit code 0, the retry
// limit is reached, or ctx is cancelled. Callers should also use ctx within
// runFn to enforce timeouts on individual runs.
//
// It calls runFn repeatedly on non-zero exit codes, up to maxRetries times.
// On zero exit (clean shutdown), it stops immediately. When ctx is cancelled
// (e.g. the user presses Ctrl+C), it stops without restarting rather than
// trapping the user until retries are exhausted. It returns the last exit
// code and error.
//
// runFn should return (exitCode, error). If error is non-nil, the loop stops.
// sleepFn is called between retries (allows testing without real delays); a
// production sleepFn should itself honor ctx so the inter-retry wait is
// interruptible.
func WatchLoop(ctx context.Context, maxRetries int, runFn func() (int, error), sleepFn func(time.Duration)) (int, error) {
	retries := 0
	for {
		exitCode, err := runFn()
		if err != nil {
			return exitCode, err
		}

		// Clean exit — stop watching
		if exitCode == 0 {
			return 0, nil
		}

		// Interrupted (e.g. Ctrl+C): the non-zero exit is the user asking to
		// stop, not a crash to recover from. Don't restart.
		if ctx.Err() != nil {
			return exitCode, nil
		}

		retries++
		if retries > maxRetries {
			fmt.Fprintf(os.Stderr, "ai-shim: watch: max retries (%d) exceeded, giving up\n", maxRetries)
			return exitCode, nil
		}

		fmt.Fprintf(os.Stderr, "ai-shim: watch: process exited with code %d, restarting (%d/%d)...\n", exitCode, retries, maxRetries)
		sleepFn(watchRestartDelay)

		// The wait may have spanned an interrupt; re-check before looping.
		if ctx.Err() != nil {
			return exitCode, nil
		}
	}
}
