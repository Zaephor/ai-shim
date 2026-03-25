package dind

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const (
	DefaultImage  = "docker:dind"
	HealthTimeout = 30 * time.Second
)

// Sidecar manages the DIND sidecar container lifecycle.
type Sidecar struct {
	client        *client.Client
	containerID   string
	containerName string
	hostname      string
}

// Config holds DIND sidecar configuration.
type Config struct {
	Image         string // defaults to docker:dind
	GPU           bool   // GPU passthrough for DIND
	UseSysbox     bool   // use sysbox runtime if available
	Labels        map[string]string
	ContainerName string // display name for the DIND container
	Hostname      string // hostname inside the DIND container
	NetworkID     string // pre-created network ID to join
}

// Start creates and starts the DIND sidecar, returning a Sidecar handle.
// The caller must provide a pre-created network via cfg.NetworkID.
func Start(ctx context.Context, cli *client.Client, cfg Config) (*Sidecar, error) {
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}

	containerCfg := &container.Config{
		Image:    image,
		Hostname: cfg.Hostname,
		Labels:   cfg.Labels,
		Env:      []string{"DOCKER_TLS_CERTDIR="}, // disable TLS for simplicity
	}

	hostCfg := &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(cfg.NetworkID),
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

	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, cfg.ContainerName)
	if err != nil {
		return nil, fmt.Errorf("creating DIND container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		if cleanupErr := cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true}); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to clean up container: %v\n", cleanupErr)
		}
		return nil, fmt.Errorf("starting DIND container: %w", err)
	}

	sidecar := &Sidecar{
		client:        cli,
		containerID:   resp.ID,
		containerName: cfg.ContainerName,
		hostname:      cfg.Hostname,
	}

	return sidecar, nil
}

// ContainerID returns the DIND container ID.
func (s *Sidecar) ContainerID() string {
	return s.containerID
}

// ContainerName returns the DIND container name for DNS resolution.
func (s *Sidecar) ContainerName() string {
	return s.containerName
}

// Hostname returns the hostname configured for the DIND container.
func (s *Sidecar) Hostname() string {
	return s.hostname
}

// Stop removes the DIND sidecar container.
func (s *Sidecar) Stop(ctx context.Context) error {
	if err := s.client.ContainerRemove(ctx, s.containerID, container.RemoveOptions{Force: true}); err != nil {
		return fmt.Errorf("removing DIND container: %w", err)
	}
	return nil
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
