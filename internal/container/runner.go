package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/docker/docker/api/types/container"
	image_types "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// ContainerSpec describes a container to create and run.
type ContainerSpec struct {
	Name         string // container display name
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
	NetworkID    string // Docker network ID to attach container to
}

// Runner manages container lifecycle via the Docker API.
type Runner struct {
	client *client.Client
}

// NewRunner creates a Runner connected to the Docker daemon.
func NewRunner(ctx context.Context) (*Runner, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w\n\nIs Docker installed? Check: https://docs.docker.com/get-docker/", err)
	}
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		return nil, fmt.Errorf("cannot connect to docker daemon: %w\n\nIs Docker running? Try:\n  Linux: sudo systemctl start docker\n  macOS: open -a Docker\n  Check: docker info", err)
	}
	return &Runner{client: cli}, nil
}

// EnsureImage pulls a Docker image if it's not available locally.
// Provides progress output to stderr.
func (r *Runner) EnsureImage(ctx context.Context, image string) error {
	// Check if image exists locally
	_, _, err := r.client.ImageInspectWithRaw(ctx, image)
	if err == nil {
		return nil // already available
	}

	fmt.Fprintf(os.Stderr, "ai-shim: pulling image %s...\n", image)
	reader, err := r.client.ImagePull(ctx, image, image_types.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", image, err)
	}
	defer reader.Close()
	// Consume the reader to complete the pull
	io.Copy(io.Discard, reader)
	fmt.Fprintf(os.Stderr, "ai-shim: image %s ready\n", image)
	return nil
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

	networkCfg := &network.NetworkingConfig{}
	if spec.NetworkID != "" {
		networkCfg.EndpointsConfig = map[string]*network.EndpointSettings{
			spec.NetworkID: {},
		}
	}

	resp, err := r.client.ContainerCreate(ctx, containerCfg, hostCfg, networkCfg, nil, spec.Name)
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

	// Forward signals to container (critical for non-TTY mode;
	// in TTY mode signals pass through the PTY naturally)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			_ = r.client.ContainerKill(ctx, containerID, sig.String())
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

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
		return 0, nil
	case status := <-statusCh:
		return int(status.StatusCode), nil
	}
}

// ImageUser represents user information extracted from a Docker image.
type ImageUser struct {
	Username string // e.g. "runner", "ubuntu", "root"
	HomeDir  string // e.g. "/home/runner", "/root"
	UID      string // e.g. "1000"
}

// InspectImageUser extracts the default user and home directory from an image.
// Falls back to the platform user info if image doesn't specify.
func (r *Runner) InspectImageUser(ctx context.Context, image string) (ImageUser, error) {
	inspect, _, err := r.client.ImageInspectWithRaw(ctx, image)
	if err != nil {
		return ImageUser{}, fmt.Errorf("inspecting image %s: %w", image, err)
	}

	result := ImageUser{
		Username: "user",
		HomeDir:  "/home/user",
	}

	// Check image config for User
	if inspect.Config != nil && inspect.Config.User != "" {
		result.UID = inspect.Config.User
	}

	// Check image config for HOME in Env
	if inspect.Config != nil {
		for _, env := range inspect.Config.Env {
			if strings.HasPrefix(env, "HOME=") {
				result.HomeDir = strings.TrimPrefix(env, "HOME=")
			}
			if strings.HasPrefix(env, "USER=") {
				result.Username = strings.TrimPrefix(env, "USER=")
			}
		}
	}

	return result, nil
}

// Client returns the underlying Docker client for DIND integration.
func (r *Runner) Client() *client.Client {
	return r.client
}

// Close closes the Docker client connection.
func (r *Runner) Close() error {
	return r.client.Close()
}
