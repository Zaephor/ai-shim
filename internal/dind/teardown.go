package dind

import (
	"context"
	"errors"
	"fmt"
	"strings"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/logging"
	"github.com/Zaephor/ai-shim/internal/network"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// StopForSession tears down the sidecars belonging to exactly one session:
// its DIND container and that container's socket and certs volumes, its
// netns holder, and finally the session network if nothing remains attached.
//
// Scoping is by session label. Parallel sessions in one workspace share
// agent, profile and workspace labels, so a coarser lookup destroys sidecars
// out from under sessions that are still running — and a destroyed netns
// holder cannot be recovered, because a container:<id> dependent never
// regains eth0.
//
// Best-effort: every individual container or volume failure is collected and
// the teardown continues, so one wedged container cannot strand the rest.
// The joined error is for reporting, not control flow. This does not extend
// to failing to enumerate what to tear down in the first place: if the
// initial DIND lookup errors, StopForSession returns immediately rather than
// force-removing the netns holder out from under a DIND it never found —
// that DIND rejoins no namespace and restarts forever, which is worse than
// the leak a failed lookup leaves behind.
//
// Rollout note: a session already running when this binary starts carries no
// ai-shim.session label (it was launched by an earlier binary), so this
// lookup will not match its sidecars — its DIND, netns holder and volumes
// leak silently. Drain all active sessions before deploying a binary with
// this change; `ai-shim manage cleanup --force` removes anything left
// behind. Plain `ai-shim manage cleanup` will not: the leaked sidecar still
// carries its restart policy, so it is still running and is not an orphan
// by state.
func StopForSession(ctx context.Context, cli *client.Client, session *ai_container.RunningSession) error {
	var errs []error

	list, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: ai_container.DINDSessionFilters(session),
	})
	if err != nil {
		return fmt.Errorf("listing DIND containers for session %s: %w", session.ContainerName, err)
	} else if len(list) == 0 {
		logging.Debug("no DIND sidecar found for session %s", session.ContainerName)
	}

	for _, c := range list {
		// Derive volume names from the container name before removing the
		// container. Names in the Docker API carry a leading "/".
		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}

		stopTimeout := 5
		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &stopTimeout}); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("stopping DIND container %s: %w", c.ID, err))
		}
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("removing DIND container %s: %w", c.ID, err))
		}

		if containerName != "" {
			for _, vol := range []string{containerName + "-socket", containerName + "-certs"} {
				if err := cli.VolumeRemove(ctx, vol, true); err != nil && !cerrdefs.IsNotFound(err) {
					errs = append(errs, fmt.Errorf("removing volume %s: %w", vol, err))
				}
			}
		}
	}

	// Remove the netns holder (holder mode only). This must happen before
	// RemoveOrphanedForSession below: the holder stays attached to the
	// session's bridge network for as long as it runs, so leaving it would
	// make the network look non-orphaned and leak it too.
	holderList, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: ai_container.HolderSessionFilters(session),
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("listing netns holder for session %s: %w", session.ContainerName, err))
	}
	for _, c := range holderList {
		// Force-remove rather than stop-then-remove. The holder is `sleep
		// infinity` as PID 1, which installs no SIGTERM handler, so a
		// graceful stop can only wait out its full timeout before the
		// daemon force-kills it anyway. Holder.Stop does the same.
		if err := cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}); err != nil {
			errs = append(errs, fmt.Errorf("removing netns holder %s: %w", c.ID, err))
		}
	}

	if err := network.RemoveOrphanedForSession(ctx, cli, session.AgentName, session.Profile, session.WorkspaceHash); err != nil {
		errs = append(errs, fmt.Errorf("removing session network: %w", err))
	}

	return errors.Join(errs...)
}
