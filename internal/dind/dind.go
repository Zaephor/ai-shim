package dind

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	ai_container "github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/parse"
	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
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
	certsVolume   string // Docker volume name for TLS certs (empty if TLS disabled)
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
	ContainerName string          // display name for the DIND container
	Hostname      string          // hostname inside the DIND container
	NetworkID     string          // pre-created network ID to join
	Mirrors       []string        // registry mirror URLs
	CacheAddr     string          // pull-through cache address (added as mirror)
	Resources     *ResourceLimits // optional resource constraints
	TLS           bool            // enable TLS for DIND socket communication
	// SocketGID, when non-zero, tells Start to chgrp /var/run/docker.sock
	// to this GID inside the DIND container after the daemon is ready.
	// Required so that agent containers running as a non-root UID/GID can
	// use the DIND socket — the docker:dind image creates the socket with
	// group "docker" (GID 2375), which the agent's GID is not a member of.
	SocketGID int
}

// Start creates and starts the DIND sidecar, returning a Sidecar handle.
// The caller must provide a pre-created network via cfg.NetworkID.
// Start calls runner.EnsureImage internally so callers do not need to
// pre-pull the DIND image.
func Start(ctx context.Context, runner *ai_container.Runner, cfg Config) (*Sidecar, error) {
	image := cfg.Image
	if image == "" {
		image = DefaultImage
	}

	cli := runner.Client()

	// Ensure the DIND image is present before ContainerCreate; the Docker SDK
	// does not auto-pull (unlike `docker run`), so a missing image would cause
	// ContainerCreate to fail with "No such image".
	if err := runner.EnsureImage(ctx, image); err != nil {
		return nil, fmt.Errorf("preparing DIND image %s: %w", image, err)
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

	// Copy labels to avoid mutating the caller's map.
	labels := make(map[string]string, len(cfg.Labels)+2)
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	labels[ai_container.LabelDIND] = "true"
	if cfg.CacheAddr != "" {
		labels[ai_container.LabelUsesCache] = "true"
	}

	// TLS configuration
	var tlsEnv string
	var certsVolName string
	if cfg.TLS {
		tlsEnv = "DOCKER_TLS_CERTDIR=/certs"
		certsVolName = baseName + "-certs"
		_, err := cli.VolumeCreate(ctx, volume.CreateOptions{
			Name:   certsVolName,
			Labels: cfg.Labels,
		})
		if err != nil {
			_ = cli.VolumeRemove(ctx, socketVolName, true)
			return nil, fmt.Errorf("creating DIND certs volume: %w", err)
		}
	} else {
		tlsEnv = "DOCKER_TLS_CERTDIR="
	}

	containerCfg := &container.Config{
		Image:      image,
		Hostname:   cfg.Hostname,
		Labels:     labels,
		Env:        []string{tlsEnv},
		Entrypoint: entrypoint,
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeVolume,
			Source: socketVolName,
			Target: "/var/run",
		},
	}
	if cfg.TLS {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: certsVolName,
			Target: "/certs",
		})
	}

	hostCfg := &container.HostConfig{
		Privileged:  true,
		NetworkMode: container.NetworkMode(cfg.NetworkID),
		Mounts:      mounts,
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
				_ = cli.VolumeRemove(ctx, socketVolName, true)
				if certsVolName != "" {
					_ = cli.VolumeRemove(ctx, certsVolName, true)
				}
				return nil, fmt.Errorf("invalid DIND memory limit %q: %w", cfg.Resources.Memory, err)
			}
			hostCfg.Memory = memBytes
		}
		if cfg.Resources.CPUs != "" {
			cpus, err := strconv.ParseFloat(cfg.Resources.CPUs, 64)
			if err != nil {
				_ = cli.VolumeRemove(ctx, socketVolName, true)
				if certsVolName != "" {
					_ = cli.VolumeRemove(ctx, certsVolName, true)
				}
				return nil, fmt.Errorf("invalid DIND CPU limit %q: %w", cfg.Resources.CPUs, err)
			}
			hostCfg.NanoCPUs = int64(cpus * 1e9)
		}
	}

	resp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, cfg.ContainerName)
	if err != nil {
		// Clean up volumes on failure
		_ = cli.VolumeRemove(ctx, socketVolName, true)
		if certsVolName != "" {
			_ = cli.VolumeRemove(ctx, certsVolName, true)
		}
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
		certsVolume:   certsVolName,
	}

	// Wait for the Docker daemon inside DIND to be ready
	healthCtx, cancel := context.WithTimeout(ctx, HealthTimeout)
	defer cancel()
	if err := sidecar.WaitForReady(healthCtx); err != nil {
		_ = sidecar.Stop(ctx)
		return nil, fmt.Errorf("waiting for DIND daemon: %w", err)
	}

	// Rebind /var/run/docker.sock's group to the agent's GID so the
	// non-root agent container can use the socket. The docker:dind image
	// ships with group "docker" at GID 2375 (matching the default TCP
	// port by convention); dockerd creates the socket as root:docker mode
	// 660. Without this chgrp, agents running as the host user's UID/GID
	// (typically 1000:1000) hit "permission denied" on every DIND call.
	//
	// The daemon is ready by this point (WaitForReady's `docker info`
	// exec succeeds only after dockerd has bound the unix socket), so
	// the socket must exist.
	if cfg.SocketGID != 0 {
		chgrpCmd := []string{"chgrp", strconv.Itoa(cfg.SocketGID), "/var/run/docker.sock"}
		exitCode, _, stderr, err := sidecar.exec(ctx, chgrpCmd)
		if err != nil {
			_ = sidecar.Stop(ctx)
			return nil, fmt.Errorf("chgrp DIND socket: %w", err)
		}
		if exitCode != 0 {
			_ = sidecar.Stop(ctx)
			return nil, fmt.Errorf("chgrp DIND socket: exit %d: %s", exitCode, bytes.TrimSpace(stderr))
		}

		// Fix TLS client cert permissions so the non-root agent can read them.
		// The docker:dind entrypoint creates /certs/client/ as root:root 0700.
		// We chgrp and chmod the directory tree to grant the agent's GID access.
		// This must run after WaitForReady because dockerd creates the certs
		// during startup.
		if cfg.TLS {
			gidStr := strconv.Itoa(cfg.SocketGID)
			certChgrpCmd := []string{"sh", "-c", "chgrp -R " + gidStr + " /certs/client && chmod -R g+rX /certs/client"}
			exitCode, _, stderr, err := sidecar.exec(ctx, certChgrpCmd)
			if err != nil {
				_ = sidecar.Stop(ctx)
				return nil, fmt.Errorf("chgrp DIND certs: %w", err)
			}
			if exitCode != 0 {
				_ = sidecar.Stop(ctx)
				return nil, fmt.Errorf("chgrp DIND certs: exit %d: %s", exitCode, bytes.TrimSpace(stderr))
			}
		}
	}

	return sidecar, nil
}

// WaitForReady polls the DIND container until the Docker daemon is responsive.
// It execs "docker info" inside the container repeatedly until it succeeds
// or the context is cancelled/times out.
func (s *Sidecar) WaitForReady(ctx context.Context) error {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("DIND health check timed out: %w", ctx.Err())
		case <-ticker.C:
			if s.isDaemonReady(ctx) {
				return nil
			}
		}
	}
}

// exec runs a command inside the DIND container and waits for it to
// finish. Returns the exit code plus captured stdout/stderr.
//
// This uses ContainerExecAttach (not bare ContainerExecStart) and drains
// the hijacked stream to EOF before inspecting. Without draining, the
// daemon may still be running the exec when we call ExecInspect, and
// ExitCode reads back as the Go zero value (0) — yielding a false-positive
// "success" for commands that haven't actually completed yet.
func (s *Sidecar) exec(ctx context.Context, cmd []string) (exitCode int, stdout, stderr []byte, err error) {
	resp, err := s.client.ContainerExecCreate(ctx, s.containerID, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return -1, nil, nil, fmt.Errorf("creating exec: %w", err)
	}

	attach, err := s.client.ContainerExecAttach(ctx, resp.ID, container.ExecStartOptions{})
	if err != nil {
		return -1, nil, nil, fmt.Errorf("attaching to exec: %w", err)
	}
	defer attach.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdoutBuf, &stderrBuf, attach.Reader); err != nil && err != io.EOF {
		return -1, stdoutBuf.Bytes(), stderrBuf.Bytes(), fmt.Errorf("reading exec output: %w", err)
	}

	inspect, err := s.client.ContainerExecInspect(ctx, resp.ID)
	if err != nil {
		return -1, stdoutBuf.Bytes(), stderrBuf.Bytes(), fmt.Errorf("inspecting exec: %w", err)
	}
	return inspect.ExitCode, stdoutBuf.Bytes(), stderrBuf.Bytes(), nil
}

// isDaemonReady checks if the Docker daemon inside the DIND container is responding.
func (s *Sidecar) isDaemonReady(ctx context.Context) bool {
	exitCode, _, _, err := s.exec(ctx, []string{"docker", "info"})
	return err == nil && exitCode == 0
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

// CertsVolume returns the Docker volume name containing TLS certs,
// or empty string if TLS is not enabled.
func (s *Sidecar) CertsVolume() string {
	return s.certsVolume
}

// Stop removes the DIND sidecar container and its socket volume.
func (s *Sidecar) Stop(ctx context.Context) error {
	var firstErr error
	if err := s.client.ContainerRemove(ctx, s.containerID, container.RemoveOptions{Force: true}); err != nil {
		firstErr = fmt.Errorf("removing DIND container: %w", err)
	}
	// Remove the socket volume
	if s.socketVolume != "" {
		if err := s.client.VolumeRemove(ctx, s.socketVolume, true); err != nil && !cerrdefs.IsNotFound(err) && firstErr == nil {
			firstErr = fmt.Errorf("removing DIND socket volume: %w", err)
		}
	}
	// Remove the certs volume if TLS was enabled
	if s.certsVolume != "" {
		if err := s.client.VolumeRemove(ctx, s.certsVolume, true); err != nil && !cerrdefs.IsNotFound(err) && firstErr == nil {
			firstErr = fmt.Errorf("removing DIND certs volume: %w", err)
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
