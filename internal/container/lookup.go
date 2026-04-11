package container

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

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
	name := ""
	if len(c.Names) > 0 {
		name = c.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}
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

// FindAllRunningSessions returns all running persistent containers for the
// given agent and profile across any workspace. Useful for `manage attach`
// when no workspace is specified.
func FindAllRunningSessions(ctx context.Context, cli *client.Client, agentName, profile string) ([]RunningSession, error) {
	f := filters.NewArgs(
		filters.Arg("label", LabelBase+"=true"),
		filters.Arg("label", LabelAgent+"="+agentName),
		filters.Arg("label", LabelProfile+"="+profile),
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
		name := ""
		if len(c.Names) > 0 {
			name = c.Names[0]
			if len(name) > 0 && name[0] == '/' {
				name = name[1:]
			}
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
