package container

import (
	"context"
	"strings"

	"github.com/docker/docker/client"
)

// netnsOwnerPrefix is Docker's network-mode form for joining another
// container's network namespace.
const netnsOwnerPrefix = "container:"

// ParseNetnsOwner returns the container ID whose network namespace the given
// network mode joins, and whether the mode is that kind at all. Any other
// mode — bridge, host, none, a named network — returns ok=false.
func ParseNetnsOwner(networkMode string) (string, bool) {
	if !strings.HasPrefix(networkMode, netnsOwnerPrefix) {
		return "", false
	}
	id := strings.TrimPrefix(networkMode, netnsOwnerPrefix)
	if id == "" {
		return "", false
	}
	return id, true
}

// NetnsOwnerAlive reports whether the given container shares another
// container's network namespace and, if so, whether that owner still exists
// and is running.
//
// A dead owner is terminal for the dependent: it keeps only lo, and eth0
// never returns — not even if a container with the same ID is started again.
// The dependent has to be recreated.
//
// Two inspect calls, no exec. Deliberately returns no error: this is a
// diagnostic, and a Docker hiccup here must never block a reattach. An
// inspect failure reports joined=false, the "nothing to say" answer.
func NetnsOwnerAlive(ctx context.Context, cli *client.Client, containerID string) (owner string, joined bool, alive bool) {
	insp, err := cli.ContainerInspect(ctx, containerID)
	if err != nil || insp.HostConfig == nil {
		return "", false, false
	}
	ownerID, ok := ParseNetnsOwner(string(insp.HostConfig.NetworkMode))
	if !ok {
		return "", false, false
	}
	ownerInsp, err := cli.ContainerInspect(ctx, ownerID)
	if err != nil || ownerInsp.State == nil {
		return ownerID, true, false
	}
	return ownerID, true, ownerInsp.State.Running
}
