package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultImage_HasRequiredTools(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping image compatibility test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), installRunTimeout)
	defer cancel()
	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	// Pull the default image once.
	require.NoError(t, runner.EnsureImage(ctx, container.DefaultImage),
		"must be able to pull default image")

	tools := []struct {
		name    string
		command string
	}{
		{"node", "node --version"},
		{"npm", "npm --version"},
		{"python3", "python3 --version"},
		{"git", "git --version"},
		{"curl", "curl --version"},
		{"bash", "bash --version"},
		{"sh", "sh -c 'echo ok'"},
	}

	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			result, runErr := runner.Run(ctx, container.ContainerSpec{
				Image:      container.DefaultImage,
				Entrypoint: []string{"sh", "-c", tc.command},
				Labels:     map[string]string{container.LabelBase: "test"},
				Name:       fmt.Sprintf("e2e-imagecompat-%s-%d", tc.name, time.Now().UnixNano()%100000),
				User:       fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
			})
			require.NoError(t, runErr, "%s: container execution error", tc.name)
			assert.Equal(t, 0, result.ExitCode, "%s: expected exit code 0", tc.name)
		})
	}
}
