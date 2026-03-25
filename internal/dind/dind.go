package dind

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

const (
	DefaultImage  = "docker:dind"
	NetworkName   = "ai-shim-dind"
	HealthTimeout = 30 * time.Second
)

// Sidecar manages the DIND sidecar container lifecycle.
type Sidecar struct {
	client      *client.Client
	containerID string
	networkID   string
}

// Config holds DIND sidecar configuration.
type Config struct {
	Image     string // defaults to docker:dind
	GPU       bool   // GPU passthrough for DIND
	UseSysbox bool   // use sysbox runtime if available
	Labels    map[string]string
}

// Start creates and starts the DIND sidecar, returning a Sidecar handle.
func Start(ctx context.Context, cli *client.Client, cfg Config) (*Sidecar, error) {
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}

	// Create a dedicated network
	networkResp, err := cli.NetworkCreate(ctx, NetworkName+"-"+fmt.Sprintf("%d", time.Now().UnixNano()), network.CreateOptions{
		Labels: cfg.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("creating DIND network: %w", err)
	}

	containerCfg := &container.Config{
		Image:  image,
		Labels: cfg.Labels,
		Env:    []string{"DOCKER_TLS_CERTDIR="}, // disable TLS for simplicity
	}

	hostCfg := &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(networkResp.ID),
	}

	// Use Sysbox if requested
	if cfg.UseSysbox {
		hostCfg.Runtime = "sysbox-runc"
		hostCfg.Privileged = false
	}

	// GPU for DIND
	if cfg.GPU {
		hostCfg.DeviceRequests = []container.DeviceRequest{
			{Count: -1, Capabilities: [][]string{{"gpu"}}},
		}
	}

	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		// Clean up network on failure
		if cleanupErr := cli.NetworkRemove(ctx, networkResp.ID); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to clean up network: %v\n", cleanupErr)
		}
		return nil, fmt.Errorf("creating DIND container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		if cleanupErr := cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to clean up container: %v\n", cleanupErr)
		}
		if cleanupErr := cli.NetworkRemove(ctx, networkResp.ID); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to clean up network: %v\n", cleanupErr)
		}
		return nil, fmt.Errorf("starting DIND container: %w", err)
	}

	sidecar := &Sidecar{
		client:      cli,
		containerID: resp.ID,
		networkID:   networkResp.ID,
	}

	return sidecar, nil
}

// ContainerID returns the DIND container ID for network attachment.
func (s *Sidecar) ContainerID() string {
	return s.containerID
}

// NetworkID returns the shared network ID.
func (s *Sidecar) NetworkID() string {
	return s.networkID
}

// Stop removes the DIND sidecar container and its network.
func (s *Sidecar) Stop(ctx context.Context) error {
	var firstErr error

	if err := s.client.ContainerRemove(ctx, s.containerID, container.RemoveOptions{Force: true}); err != nil {
		firstErr = fmt.Errorf("removing DIND container: %w", err)
	}

	if err := s.client.NetworkRemove(ctx, s.networkID); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("removing DIND network: %w", err)
	}

	return firstErr
}

// DetectSysbox checks if the sysbox-runc runtime is available.
func DetectSysbox(ctx context.Context, cli *client.Client) bool {
	info, err := cli.Info(ctx)
	if err != nil {
		return false
	}
	for name := range info.Runtimes {
		if name == "sysbox-runc" {
			return true
		}
	}
	return false
}
