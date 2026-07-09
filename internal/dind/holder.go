package dind

import (
	"context"
	"fmt"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/docker/docker/api/types/container"
)

// Holder is a minimal container that owns a network namespace shared by the
// agent and the DIND sidecar (both join it via container: network mode).
// Because the holder is a bare `sleep` process it effectively never dies, so
// the shared netns survives DIND death/restart. It is intentionally NOT given
// a restart policy: a restart would create a fresh netns, defeating the point.
type Holder struct {
	client      *ai_container.Runner
	containerID string
}

// HolderConfig configures the netns holder. Because container: joiners inherit
// the owner's /etc/hosts, /etc/resolv.conf, and hostname, all netns-scoped
// settings for the agent and DIND live here.
type HolderConfig struct {
	Image      string // must provide a `sleep` binary; caller resolves the default
	Name       string
	Hostname   string
	NetworkID  string
	ExtraHosts []string
	Labels     map[string]string
}

// holderSpec builds the container and host configs for the holder. Pure; no
// Docker calls, so it is unit-tested directly.
func holderSpec(cfg HolderConfig) (*container.Config, *container.HostConfig) {
	cc := &container.Config{
		Image:      cfg.Image,
		Hostname:   cfg.Hostname,
		Labels:     cfg.Labels,
		Entrypoint: []string{"sleep", "infinity"},
	}
	hc := &container.HostConfig{
		NetworkMode: container.NetworkMode(cfg.NetworkID),
		ExtraHosts:  cfg.ExtraHosts,
	}
	return cc, hc
}

// StartHolder pulls the image if needed, then creates and starts the holder.
// Readiness is simply "started": the netns exists once the container is
// running, and a sleep process needs no health probe.
func StartHolder(ctx context.Context, runner *ai_container.Runner, cfg HolderConfig) (*Holder, error) {
	if cfg.Image == "" {
		return nil, fmt.Errorf("netns holder image must not be empty")
	}
	cli := runner.Client()
	if err := runner.EnsureImage(ctx, cfg.Image); err != nil {
		return nil, fmt.Errorf("preparing netns holder image %s: %w", cfg.Image, err)
	}
	cc, hc := holderSpec(cfg)
	resp, err := cli.ContainerCreate(ctx, cc, hc, nil, nil, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("creating netns holder: %w", err)
	}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("starting netns holder: %w", err)
	}
	return &Holder{client: runner, containerID: resp.ID}, nil
}

// ContainerID returns the holder's container ID (the netns join target).
func (h *Holder) ContainerID() string { return h.containerID }

// Stop force-removes the holder container.
func (h *Holder) Stop(ctx context.Context) error {
	return h.client.Client().ContainerRemove(ctx, h.containerID, container.RemoveOptions{Force: true})
}
