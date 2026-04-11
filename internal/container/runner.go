package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ai-shim/ai-shim/internal/docker"
	"github.com/ai-shim/ai-shim/internal/logging"
	"github.com/ai-shim/ai-shim/internal/parse"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	image_types "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// ResourceLimits defines container resource constraints.
type ResourceLimits struct {
	Memory string
	CPUs   string
}

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
	NetworkID    string          // Docker network ID to attach container to
	Resources    *ResourceLimits // optional resource constraints
	SecurityOpt  []string        // Docker SecurityOpt (e.g. no-new-privileges)
	CapDrop      []string        // Linux capabilities to drop
	LogDir       string          // directory for exit logs (empty = no logging)
	Persistent   bool            // when true, container supports detach/reattach (AutoRemove disabled)
}

// AttachResult describes how an attach session ended.
type AttachResult struct {
	ExitCode int  // container exit code (-1 if detached or error)
	Detached bool // true if user triggered detach
}

// Runner manages container lifecycle via the Docker API.
type Runner struct {
	client *client.Client
}

// NewRunner creates a Runner connected to the Docker daemon.
func NewRunner(ctx context.Context) (*Runner, error) {
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return nil, err
	}
	return &Runner{client: cli}, nil
}

// isPermanentImagePullError returns true if the error is one we should not
// retry on (auth/not-found errors won't recover from a delay).
func isPermanentImagePullError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	permanentSubstrings := []string{
		"not found",
		"unauthorized",
		"denied",
		"manifest unknown",
		"repository does not exist",
		"requested access to the resource is denied",
	}
	for _, s := range permanentSubstrings {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// pullImageOnce performs a single image pull attempt and consumes the stream.
// Docker reports pull errors mid-stream as JSON messages, so we scan the
// returned body for an error field rather than relying on the ImagePull return.
func (r *Runner) pullImageOnce(ctx context.Context, image string) error {
	reader, err := r.client.ImagePull(ctx, image, image_types.PullOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	body, copyErr := io.ReadAll(reader)
	if copyErr != nil {
		logging.Debug("image pull stream: %v", copyErr)
	}
	if idx := strings.Index(string(body), "\"error\""); idx >= 0 {
		snippet := string(body[idx:])
		if len(snippet) > 256 {
			snippet = snippet[:256]
		}
		return fmt.Errorf("pull stream: %s", snippet)
	}
	return nil
}

// EnsureImage pulls a Docker image if it's not available locally.
// Provides progress output to stderr. Retries up to 3 times with exponential
// backoff (1s, 2s) on transient errors. Permanent errors (not found,
// unauthorized) are returned immediately without retry.
func (r *Runner) EnsureImage(ctx context.Context, image string) error {
	// Fast path: already present locally.
	if _, err := r.client.ImageInspect(ctx, image); err == nil {
		return nil
	}

	fmt.Fprintf(os.Stderr, "ai-shim: pulling image %s...\n", image)

	const maxAttempts = 3
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			fmt.Fprintf(os.Stderr, "ai-shim: pull attempt %d/%d failed, retrying in %s...\n", attempt, maxAttempts, backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		err := r.pullImageOnce(ctx, image)
		if err == nil {
			fmt.Fprintf(os.Stderr, "ai-shim: image %s ready\n", image)
			return nil
		}
		lastErr = err
		if isPermanentImagePullError(err) {
			return fmt.Errorf("pulling image %s: %w", image, err)
		}
		logging.Debug("image pull attempt %d failed: %v", attempt+1, err)
	}
	return fmt.Errorf("pulling image %s after %d attempts: %w", image, maxAttempts, lastErr)
}

// Run creates, starts, attaches to, and waits for a container.
func (r *Runner) Run(ctx context.Context, spec ContainerSpec) (AttachResult, error) {
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
		AutoRemove:   !spec.Persistent,
		PortBindings: spec.Ports,
		SecurityOpt:  spec.SecurityOpt,
		CapDrop:      spec.CapDrop,
	}

	if spec.GPU {
		hostCfg.DeviceRequests = []container.DeviceRequest{
			{Count: -1, Capabilities: [][]string{{"gpu"}}},
		}
	}

	if spec.Resources != nil {
		if spec.Resources.Memory != "" {
			memBytes, err := parse.Memory(spec.Resources.Memory)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: invalid memory limit %q: %v\n", spec.Resources.Memory, err)
			} else {
				hostCfg.Memory = memBytes
			}
		}
		if spec.Resources.CPUs != "" {
			cpus, err := strconv.ParseFloat(spec.Resources.CPUs, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: invalid cpu limit %q: %v\n", spec.Resources.CPUs, err)
			} else {
				hostCfg.NanoCPUs = int64(cpus * 1e9)
			}
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
		return AttachResult{ExitCode: -1}, fmt.Errorf("creating container: %w", err)
	}
	containerID := resp.ID

	// Attach BEFORE start. The order matters: for fast-exit commands
	// (e.g. `echo hello`) the container can terminate in ~1ms, before
	// a post-start ContainerAttach would connect. When that happens the
	// attach returns a stream that never delivers data or EOF, and
	// stdcopy.StdCopy in streamAttached blocks forever. Attaching first
	// guarantees the output pipeline is hooked up before the container
	// process runs.
	attachResp, err := r.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  spec.Stdin,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return AttachResult{ExitCode: -1}, fmt.Errorf("attaching to container: %w", err)
	}

	if err := r.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		attachResp.Close()
		return AttachResult{ExitCode: -1}, fmt.Errorf("starting container: %w", err)
	}

	result, err := r.streamAttached(ctx, containerID, attachResp, spec)
	if err != nil {
		return result, err
	}

	// For persistent containers that exited normally, explicitly remove.
	if spec.Persistent && !result.Detached {
		_ = r.client.ContainerRemove(context.Background(), containerID, container.RemoveOptions{Force: true})
	}

	return result, nil
}

// Reattach connects to an already-running container for detach/reattach
// sessions. Sets up the same I/O streaming as Run but skips creation.
func (r *Runner) Reattach(ctx context.Context, containerID string, tty bool) (AttachResult, error) {
	spec := ContainerSpec{
		TTY:        tty,
		Stdin:      tty,
		Persistent: true,
	}
	attachResp, err := r.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  spec.Stdin,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return AttachResult{ExitCode: -1}, fmt.Errorf("attaching to container: %w", err)
	}
	return r.streamAttached(ctx, containerID, attachResp, spec)
}

// streamAttached handles I/O streaming, signal forwarding, TTY setup, and
// detach detection for a container the caller has already attached to.
// Takes ownership of attachResp and closes it on return.
//
// The caller must open attachResp BEFORE starting the container (for Run)
// or after confirming the container is already running (for Reattach). This
// ordering is load-bearing: attaching after a fast-exit container has
// terminated yields a stream that never produces data or EOF, and the
// stdcopy.StdCopy call below would block forever.
func (r *Runner) streamAttached(ctx context.Context, containerID string, attachResp types.HijackedResponse, spec ContainerSpec) (AttachResult, error) {
	defer attachResp.Close()

	// runDone signals normal exit so the cancellation watcher below can
	// terminate. Without this, callers passing a non-cancellable context
	// (e.g. context.Background(), whose Done() returns nil) would leak the
	// watcher goroutine forever — it would block on <-nil indefinitely.
	runDone := make(chan struct{})
	defer close(runDone)

	// detachCh is closed when the user triggers the detach key sequence.
	detachCh := make(chan struct{})

	// Stop container when context is cancelled (e.g. programmatic shutdown).
	// For persistent containers, context cancellation also just detaches.
	go func() {
		select {
		case <-ctx.Done():
			if !spec.Persistent {
				stopTimeout := 10 // seconds
				_ = r.client.ContainerStop(context.Background(), containerID, container.StopOptions{
					Timeout: &stopTimeout,
				})
			}
		case <-runDone:
		}
	}()

	// Forward signals to container. For persistent sessions, SIGHUP triggers
	// detach instead of kill (matches screen/tmux behavior).
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		for sig := range sigCh {
			if spec.Persistent && sig == syscall.SIGHUP {
				// Terminal hangup → detach instead of killing.
				select {
				case <-detachCh:
				default:
					close(detachCh)
				}
				return
			}
			_ = r.client.ContainerKill(ctx, containerID, sig.String())
		}
	}()
	defer func() {
		signal.Stop(sigCh)
		close(sigCh)
	}()

	if spec.TTY {
		if restore := makeRaw(); restore != nil {
			defer restore()
		}

		r.resizeContainer(ctx, containerID)

		winchCh := make(chan os.Signal, 1)
		signal.Notify(winchCh, syscall.SIGWINCH)
		go func() {
			for range winchCh {
				r.resizeContainer(ctx, containerID)
			}
		}()
		defer func() {
			signal.Stop(winchCh)
			close(winchCh)
		}()
	}

	if spec.Stdin {
		go func() {
			var stdin io.Reader = os.Stdin
			if spec.Persistent && spec.TTY {
				// Parse custom detach keys from environment.
				keys := DefaultDetachKeys
				if envKeys := os.Getenv("AI_SHIM_DETACH_KEYS"); envKeys != "" {
					if parsed, err := ParseDetachKeys(envKeys); err == nil {
						keys = parsed
					} else {
						fmt.Fprintf(os.Stderr, "ai-shim: warning: invalid AI_SHIM_DETACH_KEYS: %v (using default)\n", err)
					}
				}
				stdin = NewDetachableReaderWithKeys(os.Stdin, detachCh, keys)
			}
			if _, err := io.Copy(attachResp.Conn, stdin); err != nil {
				// Suppress errors during detach — they're expected.
				select {
				case <-detachCh:
				default:
					fmt.Fprintf(os.Stderr, "ai-shim: warning: stdin copy error: %v\n", err)
				}
			}
			// Only signal EOF to container if not detaching.
			select {
			case <-detachCh:
			default:
				_ = attachResp.CloseWrite()
			}
		}()
	}

	// Stream container output. This blocks until the attach connection closes
	// (container exits or we close it on detach).
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		if spec.TTY {
			if _, err := io.Copy(os.Stdout, attachResp.Reader); err != nil {
				select {
				case <-detachCh:
				default:
					fmt.Fprintf(os.Stderr, "ai-shim: warning: stdout copy error: %v\n", err)
				}
			}
		} else {
			_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, attachResp.Reader)
		}
	}()

	// Wait for container exit or detach.
	statusCh, errCh := r.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
	select {
	case <-detachCh:
		// User triggered detach — close the attach connection to unblock
		// the output goroutine, then return.
		attachResp.Close()
		<-outputDone
		return AttachResult{ExitCode: -1, Detached: true}, nil

	case err := <-errCh:
		<-outputDone
		if err != nil {
			return AttachResult{ExitCode: -1}, fmt.Errorf("waiting for container: %w", err)
		}
		return AttachResult{ExitCode: 0}, nil

	case status := <-statusCh:
		<-outputDone
		exitCode := int(status.StatusCode)
		if exitCode != 0 {
			r.saveExitLog(spec.LogDir, spec.Name, exitCode)
			fmt.Fprintf(os.Stderr, "\nai-shim: container %s exited with code %d\n", spec.Name, exitCode)
			if spec.LogDir != "" {
				fmt.Fprintf(os.Stderr, "ai-shim: exit log: %s/%s.log\n", spec.LogDir, spec.Name)
			}
		}
		return AttachResult{ExitCode: exitCode}, nil
	}
}

// resizeContainer sets the container's TTY size to match the host terminal.
func (r *Runner) resizeContainer(ctx context.Context, containerID string) {
	width, height := getTerminalSize()
	if width == 0 || height == 0 {
		return
	}
	_ = r.client.ContainerResize(ctx, containerID, container.ResizeOptions{
		Height: height,
		Width:  width,
	})
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
	inspect, err := r.client.ImageInspect(ctx, image)
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

// saveExitLog appends an exit log entry to a log file for the container.
func (r *Runner) saveExitLog(logDir, name string, exitCode int) {
	if logDir == "" {
		return
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return
	}
	logFile := filepath.Join(logDir, name+".log")
	entry := fmt.Sprintf("%s container=%s exit_code=%d\n", time.Now().Format(time.RFC3339), name, exitCode)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	// Advisory exclusive lock prevents concurrent processes from interleaving
	// exit-log entries. flock(2) is available on Linux and macOS.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	_, _ = f.WriteString(entry)
}

// Client returns the underlying Docker client for DIND integration.
func (r *Runner) Client() *client.Client {
	return r.client
}

// Close closes the Docker client connection.
func (r *Runner) Close() error {
	return r.client.Close()
}
