package cli

import (
	"testing"

	container_types "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
)

// TestIsOrphanedContainer covers every state the Docker daemon can report
// plus the unrecognized case. `manage cleanup` force-removes whatever this
// returns true for, so a wrong answer here destroys a live session — the
// same defect class as the unscoped sidecar teardown.
func TestIsOrphanedContainer(t *testing.T) {
	for _, tc := range []struct {
		state string
		want  bool
		why   string
	}{
		{"running", false, "a running container is a live session"},
		{"restarting", false, "a restarting container is a live session mid-restart"},
		{"paused", false, "a paused container is a live session and is resumable"},
		{"removing", false, "the daemon already owns a removing container"},
		{"exited", true, "an exited container is an orphan"},
		{"created", true, "a created container never started"},
		{"dead", true, "a dead container is unrecoverable; removal is the only useful action"},
		{"", false, "an empty state must not authorize a force-remove"},
		{"some-future-state", false, "an unrecognized state must not authorize a force-remove"},
	} {
		t.Run(tc.state, func(t *testing.T) {
			got := isOrphanedContainer(container_types.Summary{State: tc.state})
			assert.Equal(t, tc.want, got, tc.why)
		})
	}
}
