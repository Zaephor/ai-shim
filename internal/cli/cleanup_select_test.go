package cli

import (
	"errors"
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

// TestIsInUseError distinguishes the daemon's in-use refusals from real
// failures. After cleanup stopped removing live containers, their volumes
// and networks stay attached, and the daemon refuses to remove them. That is
// the correct outcome, not a failure to report to the user.
//
// Matching is on the message rather than errdefs because the daemon maps
// both of these to a generic conflict, which would also swallow unrelated
// conflicts such as a name collision.
func TestIsInUseError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "volume in use",
			err:  errors.New("Error response from daemon: remove ai-shim-vol: volume is in use - [69ddd49dc028]"),
			want: true,
		},
		{
			name: "network has active endpoints",
			err:  errors.New(`Error response from daemon: error while removing network: network ai-shim-net has active endpoints (name:"c" id:"ed84de40e81a")`),
			want: true,
		},
		{
			name: "unrelated conflict is a real failure",
			err:  errors.New("Error response from daemon: a volume with the name ai-shim-vol already exists"),
			want: false,
		},
		{
			name: "permission denied is a real failure",
			err:  errors.New("permission denied while trying to connect to the Docker daemon socket"),
			want: false,
		},
		{
			name: "nil is not an in-use error",
			err:  nil,
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isInUseError(tc.err))
		})
	}
}
