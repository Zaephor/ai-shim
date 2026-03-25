package network

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

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
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			suffix = []byte(fmt.Sprintf("%x", time.Now().UnixNano())[:8])
		}
		return fmt.Sprintf("%s-%s-%s-%s-%x", prefix, agentName, profile, workspaceHash, suffix[:4])
	default:
		// Unknown scope defaults to isolated
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			suffix = []byte(fmt.Sprintf("%x", time.Now().UnixNano())[:8])
		}
		return fmt.Sprintf("%s-%s-%s-%s-%x", prefix, agentName, profile, workspaceHash, suffix[:4])
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
		return nil, fmt.Errorf("creating network %s: %w", name, err)
	}

	return &Handle{
		ID:      resp.ID,
		Name:    name,
		Created: true,
		client:  cli,
	}, nil
}

// Remove removes the network only if we created it.
func (h *Handle) Remove(ctx context.Context) error {
	if !h.Created {
		return nil // don't remove pre-existing networks
	}
	return h.client.NetworkRemove(ctx, h.ID)
}
