package container

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// ContainerSpec describes a container to create and run.
type ContainerSpec struct {
	Image        string
	Hostname     string
	Env          []string
	Mounts       []mount.Mount
	WorkingDir   string
	Entrypoint   []string
	Cmd          []string
	User         string
	Labels       map[string]string
	Ports        nat.PortMap
	ExposedPorts nat.PortSet
	TTY          bool
	Stdin        bool
	GPU          bool
}

// Runner manages container lifecycle via the Docker API.
type Runner struct {
	client *client.Client
}

// NewRunner creates a Runner connected to the Docker daemon.
func NewRunner(ctx context.Context) (*Runner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("connecting to docker: %w", err)
	}
	return &Runner{client: cli}, nil
}

// Run creates, starts, attaches to, and waits for a container. Returns exit code.
func (r *Runner) Run(ctx context.Context, spec ContainerSpec) (int, error) {
	containerCfg := &container.Config{
		Image:        spec.Image,
		Hostname:     spec.Hostname,
		Env:          spec.Env,
		WorkingDir:   spec.WorkingDir,
		Entrypoint:   spec.Entrypoint,
		Cmd:          spec.Cmd,
		User:         spec.User,
		Labels:       spec.Labels,
		Tty:          spec.TTY,
		OpenStdin:    spec.Stdin,
		AttachStdin:  spec.Stdin,
		AttachStdout: true,
		AttachStderr: true,
		ExposedPorts: spec.ExposedPorts,
	}

	hostCfg := &container.HostConfig{
		Mounts:       spec.Mounts,
		AutoRemove:   true,
		PortBindings: spec.Ports,
	}

	if spec.GPU {
		hostCfg.DeviceRequests = []container.DeviceRequest{
			{Count: -1, Capabilities: [][]string{{"gpu"}}},
		}
	}

	resp, err := r.client.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return -1, fmt.Errorf("creating container: %w", err)
	}
	containerID := resp.ID

	attachResp, err := r.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  spec.Stdin,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return -1, fmt.Errorf("attaching to container: %w", err)
	}
	defer attachResp.Close()

	if err := r.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return -1, fmt.Errorf("starting container: %w", err)
	}

	if spec.Stdin {
		go func() {
			io.Copy(attachResp.Conn, os.Stdin)
			attachResp.CloseWrite()
		}()
	}

	if spec.TTY {
		io.Copy(os.Stdout, attachResp.Reader)
	} else {
		stdcopy.StdCopy(os.Stdout, os.Stderr, attachResp.Reader)
	}

	statusCh, errCh := r.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case err := <-errCh:
		if err != nil {
			return -1, fmt.Errorf("waiting for container: %w", err)
		}
	case status := <-statusCh:
		return int(status.StatusCode), nil
	}

	return 0, nil
}

// Client returns the underlying Docker client for DIND integration.
func (r *Runner) Client() *client.Client {
	return r.client
}

// Close closes the Docker client connection.
func (r *Runner) Close() error {
	return r.client.Close()
}
