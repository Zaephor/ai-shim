package container

import "github.com/docker/docker/api/types/filters"

// DINDSessionFilters locates the DIND sidecar belonging to one session.
//
// The scope is the session label alone. Agent, profile and workspace are
// deliberately absent: parallel sessions share all three, so a filter built
// from them matches every sibling's sidecar, and teardown for one session
// then destroys the DIND of sessions that are still running. Their agent
// containers survive — those are removed by ID — leaving them attached to a
// destroyed network namespace with no way to recover it.
func DINDSessionFilters(session *RunningSession) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelDIND+"=true"),
		filters.Arg("label", LabelSession+"="+session.ContainerName),
		filters.Arg("status", "running"),
	)
}

// HolderSessionFilters locates the netns holder belonging to one session
// (holder mode). Scoped by session label for the same reason as
// DINDSessionFilters — and more urgently: destroying a sibling's holder is
// unrecoverable, because a container:<id> dependent keeps only lo and never
// regains eth0, even if the owner is restarted with the same ID.
func HolderSessionFilters(session *RunningSession) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelRole+"=netns-holder"),
		filters.Arg("label", LabelSession+"="+session.ContainerName),
		filters.Arg("status", "running"),
	)
}
