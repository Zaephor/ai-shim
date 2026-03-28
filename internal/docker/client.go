package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/client"
)

// NewClient creates a Docker client with API version negotiation and verifies connectivity.
func NewClient(ctx context.Context) (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w\n\nIs Docker installed? Check: https://docs.docker.com/get-docker/", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("cannot connect to docker daemon: %w\n\nIs Docker running? Try:\n  Linux: sudo systemctl start docker\n  macOS: open -a Docker\n  Check: docker info", err)
	}
	return cli, nil
}

// NewClientNoPing creates a Docker client without verifying connectivity.
// Use when connectivity failure is handled separately.
func NewClientNoPing() (*client.Client, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	return cli, nil
}
