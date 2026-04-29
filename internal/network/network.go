package network

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/filters"
	dnetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const prefix = "ai-shim"

// Handle represents a Docker network managed by ai-shim.
type Handle struct {
	ID      string
	Name    string
	Created bool // true if we created it, false if it pre-existed
	client  *client.Client
}

// isolatedName generates a unique network name with a random suffix for
// isolated and unknown scopes.
func isolatedName(agentName, profile, workspaceHash string) string {
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		s := fmt.Sprintf("%x", time.Now().UnixNano())
		if len(s) >= 8 {
			s = s[:8]
		}
		return fmt.Sprintf("%s-%s-%s-%s-%s", prefix, agentName, profile, workspaceHash, s)
	}
	return fmt.Sprintf("%s-%s-%s-%s-%x", prefix, agentName, profile, workspaceHash, suffix[:4])
}

// ResolveName returns the deterministic network name for a given scope.
func ResolveName(scope, agentName, profile, workspaceHash string) string {
	switch scope {
	case "global":
		return prefix
	case "profile":
		return prefix + "-" + profile
	case "workspace":
		return prefix + "-" + workspaceHash
	case "profile-workspace":
		return prefix + "-" + profile + "-" + workspaceHash
	case "isolated", "":
		return isolatedName(agentName, profile, workspaceHash)
	default:
		// Unknown scope defaults to isolated
		return isolatedName(agentName, profile, workspaceHash)
	}
}

// EnsureNetwork creates a network if it doesn't exist, or returns the existing one.
func EnsureNetwork(ctx context.Context, cli *client.Client, name string, labels map[string]string) (*Handle, error) {
	// Check if network already exists
	networks, err := cli.NetworkList(ctx, dnetwork.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^"+name+"$")),
	})
	if err != nil {
		return nil, fmt.Errorf("listing networks: %w", err)
	}

	// Use exact name match (Docker's filter is prefix-based)
	for _, n := range networks {
		if n.Name == name {
			return &Handle{
				ID:      n.ID,
				Name:    name,
				Created: false,
				client:  cli,
			}, nil
		}
	}

	// Create new network
	resp, err := cli.NetworkCreate(ctx, name, dnetwork.CreateOptions{
		Labels: labels,
	})
	if err != nil {
		// Network might have been created by another process (race condition).
		// Retry the lookup before giving up.
		networks, listErr := cli.NetworkList(ctx, dnetwork.ListOptions{
			Filters: filters.NewArgs(filters.Arg("name", "^"+name+"$")),
		})
		if listErr != nil {
			return nil, fmt.Errorf("creating network %s: %w", name, err)
		}
		for _, n := range networks {
			if n.Name == name {
				return &Handle{ID: n.ID, Name: name, Created: false, client: cli}, nil
			}
		}
		return nil, fmt.Errorf("creating network %s: %w", name, err)
	}

	return &Handle{
		ID:      resp.ID,
		Name:    name,
		Created: true,
		client:  cli,
	}, nil
}

// RemoveOrphanedForSession removes ai-shim networks tagged with the given
// agent/profile/workspace labels that have no containers attached. Safe for
// both isolated (per-session unique) and shared networks — the container-count
// check prevents removing a shared network still used by a parallel session.
func RemoveOrphanedForSession(ctx context.Context, cli *client.Client, agentName, profile, workspaceHash string) error {
	f := filters.NewArgs(
		filters.Arg("label", "ai-shim=true"),
		filters.Arg("label", "ai-shim.agent="+agentName),
		filters.Arg("label", "ai-shim.profile="+profile),
		filters.Arg("label", "ai-shim.workspace="+workspaceHash),
	)
	networks, err := cli.NetworkList(ctx, dnetwork.ListOptions{Filters: f})
	if err != nil {
		return fmt.Errorf("listing networks for session cleanup: %w", err)
	}

	var errs []error
	for _, n := range networks {
		inspect, err := cli.NetworkInspect(ctx, n.ID, dnetwork.InspectOptions{})
		if err != nil {
			if cerrdefs.IsNotFound(err) {
				continue
			}
			errs = append(errs, fmt.Errorf("inspecting network %s: %w", n.Name, err))
			continue
		}
		if len(inspect.Containers) > 0 {
			continue // still in use by another session
		}
		if err := cli.NetworkRemove(ctx, n.ID); err != nil && !cerrdefs.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("removing network %s: %w", n.Name, err))
		}
	}
	return errors.Join(errs...)
}

// Remove removes the network only if we created it.
//
// The call is idempotent: if another process (or a previous run) has
// already removed the network, the resulting "not found" error from the
// Docker API is treated as success. Any other error is wrapped and
// returned so callers can surface it instead of silently swallowing
// genuine failures.
func (h *Handle) Remove(ctx context.Context) error {
	if !h.Created {
		return nil // don't remove pre-existing networks
	}
	err := h.client.NetworkRemove(ctx, h.ID)
	if err == nil {
		return nil
	}
	if cerrdefs.IsNotFound(err) {
		// Already gone — concurrent removal raced us. Treat as success.
		return nil
	}
	return fmt.Errorf("removing network %s (%s): %w", h.Name, h.ID, err)
}
