package container

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

// containerName returns the Docker container name with the leading slash stripped.
func containerName(c types.Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return ""
}

// RunningSession describes a running container eligible for reattach.
type RunningSession struct {
	ContainerID   string
	ContainerName string
	AgentName     string
	Profile       string
	WorkspaceHash string
	WorkspaceDir  string
	CreatedAt     time.Time
}

// FindRunningSession looks for a running persistent container matching the
// given agent, profile, and workspace hash. Returns nil if none found.
func FindRunningSession(ctx context.Context, cli *client.Client, agentName, profile, wsHash string) (*RunningSession, error) {
	f := filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelAgent+"="+agentName),
		filters.Arg("label", LabelProfile+"="+profile),
		filters.Arg("label", LabelPersistent+"=true"),
		filters.Arg("label", LabelWorkspace+"="+wsHash),
		filters.Arg("status", "running"),
	)

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	if len(containers) == 0 {
		return nil, nil
	}

	c := containers[0]
	name := containerName(c)
	if c.Labels[LabelAgent] == "" {
		return nil, fmt.Errorf("container %s missing required label %s", name, LabelAgent)
	}

	return &RunningSession{
		ContainerID:   c.ID,
		ContainerName: name,
		AgentName:     c.Labels[LabelAgent],
		Profile:       c.Labels[LabelProfile],
		WorkspaceHash: c.Labels[LabelWorkspace],
		WorkspaceDir:  c.Labels[LabelWorkspaceDir],
		CreatedAt:     time.Unix(c.Created, 0),
	}, nil
}

// FindRunningSessionsInWorkspace returns every running persistent container
// matching agent+profile+workspaceHash. Sessions are sorted most-recently-
// created first so pickers can offer the most likely target as index [1].
// Returns an empty slice (not nil-err) when no sessions match.
func FindRunningSessionsInWorkspace(ctx context.Context, cli *client.Client, agentName, profile, wsHash string) ([]RunningSession, error) {
	f := filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelAgent+"="+agentName),
		filters.Arg("label", LabelProfile+"="+profile),
		filters.Arg("label", LabelPersistent+"=true"),
		filters.Arg("label", LabelWorkspace+"="+wsHash),
		filters.Arg("status", "running"),
	)

	containers, err := cli.ContainerList(ctx, container.ListOptions{Filters: f})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	sessions := make([]RunningSession, 0, len(containers))
	for _, c := range containers {
		name := containerName(c)
		if c.Labels[LabelAgent] == "" {
			fmt.Fprintf(os.Stderr, "ai-shim: skipping container %s: missing label %s\n", name, LabelAgent)
			continue
		}
		sessions = append(sessions, RunningSession{
			ContainerID:   c.ID,
			ContainerName: name,
			AgentName:     c.Labels[LabelAgent],
			Profile:       c.Labels[LabelProfile],
			WorkspaceHash: c.Labels[LabelWorkspace],
			WorkspaceDir:  c.Labels[LabelWorkspaceDir],
			CreatedAt:     time.Unix(c.Created, 0),
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].CreatedAt.After(sessions[j].CreatedAt)
	})
	return sessions, nil
}

// FindSessionByContainerName looks up a running ai-shim container by its
// exact Docker name. Returns nil if no matching container is found. An error
// is returned when the container exists but is not running or does not carry
// the ai-shim label.
func FindSessionByContainerName(ctx context.Context, cli *client.Client, name string) (*RunningSession, error) {
	// Docker name filter matches substrings, so we list and do exact match.
	f := filters.NewArgs(
		filters.Arg("name", name),
	)
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	// Find exact name match (Docker names have leading /).
	for _, c := range containers {
		cName := containerName(c)
		if cName != name {
			continue
		}

		// Verify ai-shim label.
		if c.Labels[LabelBase] != "true" {
			return nil, fmt.Errorf("container %q is not an ai-shim container", name)
		}

		// Verify running.
		if c.State != "running" {
			return nil, fmt.Errorf("container %q is not running (state: %s)", name, c.State)
		}

		if c.Labels[LabelAgent] == "" {
			return nil, fmt.Errorf("container %s missing required label %s", cName, LabelAgent)
		}

		return &RunningSession{
			ContainerID:   c.ID,
			ContainerName: cName,
			AgentName:     c.Labels[LabelAgent],
			Profile:       c.Labels[LabelProfile],
			WorkspaceHash: c.Labels[LabelWorkspace],
			WorkspaceDir:  c.Labels[LabelWorkspaceDir],
			CreatedAt:     time.Unix(c.Created, 0),
		}, nil
	}
	return nil, nil
}

// FindAllRunningSessions returns all running persistent containers for the
// given agent and profile across any workspace. Useful for `manage attach`
// when no workspace is specified.
func FindAllRunningSessions(ctx context.Context, cli *client.Client, agentName, profile string) ([]RunningSession, error) {
	f := filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelAgent+"="+agentName),
		filters.Arg("label", LabelProfile+"="+profile),
		filters.Arg("label", LabelRole+"=agent"),
		filters.Arg("label", LabelPersistent+"=true"),
		filters.Arg("status", "running"),
	)

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: f,
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	sessions := make([]RunningSession, 0, len(containers))
	for _, c := range containers {
		name := containerName(c)
		if c.Labels[LabelAgent] == "" {
			fmt.Fprintf(os.Stderr, "ai-shim: skipping container %s: missing label %s\n", name, LabelAgent)
			continue
		}
		sessions = append(sessions, RunningSession{
			ContainerID:   c.ID,
			ContainerName: name,
			AgentName:     c.Labels[LabelAgent],
			Profile:       c.Labels[LabelProfile],
			WorkspaceHash: c.Labels[LabelWorkspace],
			WorkspaceDir:  c.Labels[LabelWorkspaceDir],
			CreatedAt:     time.Unix(c.Created, 0),
		})
	}
	return sessions, nil
}
