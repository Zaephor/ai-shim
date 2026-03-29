package e2e

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
)

// TestConcurrent_ParallelLaunch verifies that multiple containers can be
// launched simultaneously without name collisions or race conditions.
func TestConcurrent_ParallelLaunch(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow concurrent test")
	}

	const numContainers = 3

	var wg sync.WaitGroup
	results := make([]struct {
		output   string
		exitCode int
		err      error
	}, numContainers)

	for i := 0; i < numContainers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			profileName := fmt.Sprintf("concurrent-%d", idx)
			layout := setupJourneyLayout(t)
			cfg := config.Config{
				Image: container.DefaultImage,
			}

			// Use a unique container name via spec override
			containerName := fmt.Sprintf("ai-shim-concurrent-test-%d", idx)
			override := func(s *container.ContainerSpec) {
				s.Name = containerName
			}

			verifyCmd := fmt.Sprintf("echo CONCURRENT-%d-OK", idx)
			output, exitCode := buildAndRun(t, layout, "opencode", profileName, cfg, verifyCmd, override)
			results[idx].output = output
			results[idx].exitCode = exitCode
		}(i)
	}

	wg.Wait()

	for i := 0; i < numContainers; i++ {
		assert.Equal(t, 0, results[i].exitCode, "container %d should exit 0", i)
		assert.Contains(t, results[i].output, fmt.Sprintf("CONCURRENT-%d-OK", i),
			"container %d should produce expected output", i)
	}
}
