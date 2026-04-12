package dind

import (
	"context"
	"fmt"
	"os"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/logging"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

const (
	CacheContainerName = "ai-shim-registry-cache"
	// CacheImage is the fully-qualified reference for the pull-through
	// registry cache image. The full form (registry host + library/)
	// avoids any ambiguity in the Docker SDK's reference handling.
	CacheImage = "docker.io/library/registry:2"
	CachePort  = "5000"
)

// EnsureCache starts the pull-through registry cache if it's not already running.
// Returns the cache container's address (for use as a registry mirror).
//
// Takes a *container.Runner (rather than a raw *client.Client) so it can reuse
// the runner's EnsureImage logic — the Docker SDK's ContainerCreate does NOT
// auto-pull missing images (unlike the `docker run` CLI), so the cache image
// must be pulled explicitly before the cache container is created.
func EnsureCache(ctx context.Context, runner *ai_container.Runner, cacheDir string) (string, error) {
	cli := runner.Client()

	// Check if cache is already running
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	if err != nil {
		return "", fmt.Errorf("checking for cache container: %w", err)
	}

	if len(containers) > 0 {
		// Already running on host network
		return fmt.Sprintf("http://host.docker.internal:%s", CachePort), nil
	}

	// Ensure the cache image is pulled. Without this, ContainerCreate below
	// fails with "No such image" on any host that hasn't pulled registry:2
	// through some other code path first.
	if err := runner.EnsureImage(ctx, CacheImage); err != nil {
		return "", fmt.Errorf("pulling cache image: %w", err)
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
			ai_container.LabelBase:  "true",
			ai_container.LabelCache: "true",
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
		// Bind to host so it's reachable via host.docker.internal from any container
		NetworkMode:   "host",
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyUnlessStopped},
	}

	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, CacheContainerName)
	if err != nil {
		return "", fmt.Errorf("creating cache container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", fmt.Errorf("starting cache container: %w", err)
	}

	return fmt.Sprintf("http://host.docker.internal:%s", CachePort), nil
}

// MaybeStopCache attempts to stop the cache container if no other ai-shim
// containers with uses-cache=true label are running. This is best-effort;
// in race conditions the cache may persist until the next cleanup.
func MaybeStopCache(ctx context.Context, cli *client.Client) {
	// Count containers that use the cache (excluding the cache container itself)
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", ai_container.LabelUsesCache+"=true"),
			filters.Arg("status", "running"),
		),
	})
	if err != nil {
		logging.Debug("cache: failed to list cache consumers: %v", err)
		return
	}

	if len(containers) > 0 {
		logging.Debug("cache: %d consumer(s) still running, keeping cache", len(containers))
		return // other consumers still running
	}

	// No consumers — stop the cache
	cacheContainers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", "^/"+CacheContainerName+"$")),
	})
	if err != nil {
		logging.Debug("cache: failed to find cache container: %v", err)
		return
	}
	if len(cacheContainers) == 0 {
		logging.Debug("cache: no cache container found to stop")
		return
	}

	if err := cli.ContainerRemove(ctx, cacheContainers[0].ID, container.RemoveOptions{Force: true}); err != nil {
		logging.Debug("cache: failed to remove cache container: %v", err)
	} else {
		logging.Debug("cache: removed cache container %s", cacheContainers[0].ID[:12])
	}
}
