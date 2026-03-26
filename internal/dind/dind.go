package dind

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	ai_container "github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/parse"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
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
	socketVolume  string // Docker volume name for the DIND socket
}

// ResourceLimits defines container resource constraints for DIND.
type ResourceLimits struct {
	Memory string
	CPUs   string
}

// Config holds DIND sidecar configuration.
type Config struct {
	Image         string // defaults to docker:dind
	GPU           bool   // GPU passthrough for DIND
	UseSysbox     bool   // use sysbox runtime if available
	Labels        map[string]string
	ContainerName string // display name for the DIND container
	Hostname      string // hostname inside the DIND container
	NetworkID     string   // pre-created network ID to join
	Mirrors       []string         // registry mirror URLs
	CacheAddr     string           // pull-through cache address (added as mirror)
	Resources     *ResourceLimits  // optional resource constraints
}

// Start creates and starts the DIND sidecar, returning a Sidecar handle.
// The caller must provide a pre-created network via cfg.NetworkID.
func Start(ctx context.Context, cli *client.Client, cfg Config) (*Sidecar, error) {
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}

	// Create a volume for the DIND docker socket
	baseName := cfg.ContainerName
	if baseName == "" {
		baseName = "ai-shim-dind"
	}
	socketVolName := baseName + "-socket"
	_, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name:   socketVolName,
		Labels: cfg.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("creating DIND socket volume: %w", err)
	}

	// Build dockerd command with mirrors (cache first = highest priority)
	var entrypoint []string
	dockerdArgs := []string{"dockerd"}
	if cfg.CacheAddr != "" {
		dockerdArgs = append(dockerdArgs, "--registry-mirror="+cfg.CacheAddr)
	}
	for _, mirror := range cfg.Mirrors {
		dockerdArgs = append(dockerdArgs, "--registry-mirror="+mirror)
	}
	if len(dockerdArgs) > 1 {
		entrypoint = dockerdArgs
	}

	labels := cfg.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if cfg.CacheAddr != "" {
		labels[ai_container.LabelUsesCache] = "true"
	}

	containerCfg := &container.Config{
		Image:      image,
		Hostname:   cfg.Hostname,
		Labels:     labels,
		Env:        []string{"DOCKER_TLS_CERTDIR="}, // disable TLS for simplicity
		Entrypoint: entrypoint,
	}

	hostCfg := &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(cfg.NetworkID),
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeVolume,
				Source: socketVolName,
				Target: "/var/run",
			},
		},
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

	// Resource limits for DIND
	if cfg.Resources != nil {
		if cfg.Resources.Memory != "" {
			memBytes, err := parse.Memory(cfg.Resources.Memory)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: invalid memory limit %q: %v\n", cfg.Resources.Memory, err)
			} else {
				hostCfg.Resources.Memory = memBytes
			}
		}
		if cfg.Resources.CPUs != "" {
			cpus, err := strconv.ParseFloat(cfg.Resources.CPUs, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: invalid cpu limit %q: %v\n", cfg.Resources.CPUs, err)
			} else {
				hostCfg.Resources.NanoCPUs = int64(cpus * 1e9)
			}
		}
	}

	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, cfg.ContainerName)
	if err != nil {
		// Clean up the volume on failure
		_ = cli.VolumeRemove(ctx, socketVolName, true)
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
		socketVolume:  socketVolName,
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

// SocketVolume returns the Docker volume name containing the DIND socket.
func (s *Sidecar) SocketVolume() string {
	return s.socketVolume
}

// Stop removes the DIND sidecar container and its socket volume.
func (s *Sidecar) Stop(ctx context.Context) error {
	var firstErr error
	if err := s.client.ContainerRemove(ctx, s.containerID, container.RemoveOptions{Force: true}); err != nil {
		firstErr = fmt.Errorf("removing DIND container: %w", err)
	}
	// Remove the socket volume
	if s.socketVolume != "" {
		if err := s.client.VolumeRemove(ctx, s.socketVolume, true); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("removing DIND socket volume: %w", err)
		}
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
