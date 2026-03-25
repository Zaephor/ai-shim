package dind

import (
	"context"
	"fmt"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	CacheContainerName = "ai-shim-registry-cache"
	CacheImage         = "registry:2"
	CachePort          = "5000"
)

// EnsureCache starts the pull-through registry cache if it's not already running.
// Returns the cache container's address (for use as a registry mirror).
func EnsureCache(ctx context.Context, cli *client.Client, cacheDir string, networkID string) (string, error) {
	// Check if cache is already running
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	if err != nil {
		return "", fmt.Errorf("checking for cache container: %w", err)
	}

	if len(containers) > 0 {
		// Already running — ensure it's on our network
		if networkID != "" {
			_ = cli.NetworkConnect(ctx, networkID, containers[0].ID, nil)
		}
		return fmt.Sprintf("http://%s:%s", CacheContainerName, CachePort), nil
	}

	// Create cache directory if needed
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("creating cache directory: %w", err)
	}

	// Start the registry in pull-through mode
	containerCfg := &container.Config{
		Image: CacheImage,
		Env: []string{
			"REGISTRY_PROXY_REMOTEURL=https://registry-1.docker.io",
			"REGISTRY_STORAGE_DELETE_ENABLED=true",
		},
		Labels: map[string]string{
			"ai-shim":       "true",
			"ai-shim.cache": "true",
		},
	}

	hostCfg := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: cacheDir,
				Target: "/var/lib/registry",
			},
		},
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}

	// Join network if provided
	var networkCfg *network.NetworkingConfig
	if networkID != "" {
		networkCfg = &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				networkID: {},
			},
		}
	}

	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, CacheContainerName)
	if err != nil {
		return "", fmt.Errorf("creating cache container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("starting cache container: %w", err)
	}

	return fmt.Sprintf("http://%s:%s", CacheContainerName, CachePort), nil
}

// MaybeStopCache stops the cache container if no other ai-shim containers with
// uses-cache=true label are running.
func MaybeStopCache(ctx context.Context, cli *client.Client) {
	// Count containers that use the cache (excluding the cache container itself)
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "ai-shim.uses-cache=true"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		return
	}

	if len(containers) > 0 {
		return // other consumers still running
	}

	// No consumers — stop the cache
	cacheContainers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	if err != nil || len(cacheContainers) == 0 {
		return
	}

	_ = cli.ContainerRemove(ctx, cacheContainers[0].ID, container.RemoveOptions{Force: true})
}
