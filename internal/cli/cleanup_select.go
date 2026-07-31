package cli

import (
	"strings"

	container_types "github.com/docker/docker/api/types/container"
)

// isOrphanedContainer reports whether an ai-shim container is an orphan and
// may be force-removed by `manage cleanup`.
//
// The default is to keep. An unrecognized or empty state must never
// authorize a force-remove: a container left behind is recoverable by
// running cleanup again, while a destroyed live session is not — its agent
// loses a netns it cannot regain, which is the failure mode session-scoped
// teardown exists to prevent.
//
// Only containers are checked. Docker itself refuses to remove an in-use
// volume or network regardless of the force flag, and the cleanup passes
// run containers-first, so declining to remove a live container transitively
// protects that session's volumes and network too.
func isOrphanedContainer(c container_types.Summary) bool {
	switch container_types.ContainerState(c.State) {
	case container_types.StateExited, container_types.StateCreated, container_types.StateDead:
		return true
	default:
		// running, restarting, paused, removing, and anything the daemon
		// starts reporting in a future API version.
		return false
	}
}

// isInUseError reports whether err is the daemon refusing to remove a volume
// or network because a container still references it. For a volume, Docker
// refuses removal for a reference from any existing container, running or
// not; for a network, it refuses while a container endpoint is still
// attached.
//
// Since cleanup stopped removing live containers, a running session's
// volumes and networks necessarily stay referenced, and the daemon rejects
// removing them. Reporting that as a removal failure is wrong: the resource
// belongs to a running session and retaining it is the intended outcome.
//
// The match is on the message. containerd/errdefs maps both refusals to a
// generic conflict, which would also swallow unrelated conflicts such as a
// name collision — a real failure the user should see.
func isInUseError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "volume is in use") ||
		strings.Contains(msg, "has active endpoints")
}
