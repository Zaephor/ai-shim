package container

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Zaephor/ai-shim/internal/docker"
	"github.com/Zaephor/ai-shim/internal/logging"
	"github.com/Zaephor/ai-shim/internal/parse"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	image_types "github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

// reattachResetSequence is the ANSI escape sequence written to stdout on
// reattach: clear the screen (ESC[2J), home the cursor (ESC[H), and make it
// visible (ESC[?25h). Previous sessions may have left the terminal with an
// invisible cursor or stale glyphs; this resets it to a known state before
// the inner TUI repaints.
const reattachResetSequence = "\033[2J\033[H\033[?25h"

// containerResizer issues TTY resize calls. Runner satisfies this against the
// Docker API; tests substitute a fake so reattach-terminal logic can be
// exercised without a live daemon.
type containerResizer interface {
	Resize(ctx context.Context, containerID string, width, height uint) error
}

// Resize implements containerResizer by delegating to the Docker client.
func (r *Runner) Resize(ctx context.Context, containerID string, width, height uint) error {
	return r.client.ContainerResize(ctx, containerID, container.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

// prepareReattachTerminal writes the ANSI reset sequence to out and then
// forces a SIGWINCH inside the container by resizing to height+1 and back to
// height. Docker only signals the container pty on a real dimension change,
// so re-sending the current size is a no-op — the intermediate off-by-one
// resize guarantees the inner TUI receives SIGWINCH and redraws against the
// freshly cleared canvas.
//
// When the host terminal size cannot be determined (width or height is 0)
// the function is a no-op: no bytes are written and no resize is issued.
func prepareReattachTerminal(ctx context.Context, out io.Writer, resizer containerResizer, containerID string, width, height uint) {
	if width == 0 || height == 0 {
		return
	}
	_, _ = io.WriteString(out, reattachResetSequence)
	_ = resizer.Resize(ctx, containerID, width, height+1)
	_ = resizer.Resize(ctx, containerID, width, height)
}

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
	Reattach     bool            // true when attaching to an already-running container (not initial run)
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
				return AttachResult{ExitCode: -1}, fmt.Errorf("invalid memory limit %q: %w", spec.Resources.Memory, err)
			}
			hostCfg.Memory = memBytes
		}
		if spec.Resources.CPUs != "" {
			cpus, err := strconv.ParseFloat(spec.Resources.CPUs, 64)
			if err != nil {
				return AttachResult{ExitCode: -1}, fmt.Errorf("invalid cpu limit %q: %w", spec.Resources.CPUs, err)
			}
			hostCfg.NanoCPUs = int64(cpus * 1e9)
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
		Reattach:   true,
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

	// triggerDetach closes detachCh exactly once, regardless of how many
	// goroutines call it concurrently. This shared Once covers both the signal
	// handler below and the DetachableReader, eliminating the TOCTOU gap that
	// would cause a panic if both sites raced to close the channel.
	var detachOnce sync.Once
	triggerDetach := func() {
		detachOnce.Do(func() { close(detachCh) })
	}

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
				// Use the shared Once so this can never race with DetachableReader.
				triggerDetach()
				return
			}
			if err := r.client.ContainerKill(ctx, containerID, sig.String()); err != nil {
				// The container may have already exited; this is expected on normal
				// shutdown, so log at debug level rather than treating as an error.
				logging.Debug("signal %s forwarding failed: %v", sig, err)
			}
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

		if spec.Reattach {
			// Reset the host terminal on reattach: clear the screen, home
			// the cursor, make sure it is visible, and force a SIGWINCH
			// inside the container so its TUI redraws against the fresh
			// canvas. The previous attach left the terminal in whatever
			// state the inner TUI last drew — without this users see
			// stale output with an invisible cursor on reconnect.
			width, height := getTerminalSize()
			prepareReattachTerminal(ctx, os.Stdout, r, containerID, width, height)
		} else {
			r.resizeContainer(ctx, containerID)
		}

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
		// NOTE: this goroutine may outlive streamAttached when blocked on
		// os.Stdin.Read(). This is a known Go limitation (os.Stdin reads
		// cannot be cancelled). It is benign because the process exits
		// shortly after streamAttached returns. If streamAttached is ever
		// called multiple times in a single process (e.g. watch mode with
		// in-process retry), this would need to be addressed.
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
				stdin = NewDetachableReaderWithTrigger(os.Stdin, keys, triggerDetach)
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
	//
	// For non-persistent (AutoRemove=true) containers we wait on the
	// "removed" condition rather than "not-running" so that Run() does
	// not return until Docker has finished auto-removing the container.
	// Without this, callers (and tests like TestE2E_NonPersistentContainer-
	// AutoRemove) that inspect the container immediately after Run returns
	// would race Docker's asynchronous removal and occasionally still see
	// the stopped-but-not-yet-removed container.
	//
	// Persistent containers disable AutoRemove and are removed explicitly
	// by Run after attachAndStream returns, so they must keep the
	// "not-running" condition — waiting for "removed" would deadlock since
	// nothing else removes them.
	waitCondition := container.WaitConditionNotRunning
	if !spec.Persistent {
		waitCondition = container.WaitConditionRemoved
	}
	statusCh, errCh := r.client.ContainerWait(ctx, containerID, waitCondition)
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

		// Best-effort: persist the container's last N lines of output so
		// users can debug crashes after the container is gone.
		r.captureContainerOutput(containerID, spec)

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

// OutputLogTailLines is the maximum number of trailing lines captured from
// the container's combined stdout/stderr when persisting output on exit.
const OutputLogTailLines = 100

// OutputLogSuffix is the file suffix used for persisted container output logs.
const OutputLogSuffix = ".output.log"

// saveContainerOutput reads up to tailLines trailing lines from r and writes
// them to <logDir>/<name>.output.log. It is best-effort: any error is
// warned on stderr but never propagated. The reader r is consumed until EOF.
func saveContainerOutput(logDir, name string, r io.Reader, tailLines int) {
	if logDir == "" || r == nil {
		return
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: output log: cannot create log dir %s: %v\n", logDir, err)
		return
	}

	// Read all lines, keeping only the last tailLines.
	scanner := bufio.NewScanner(r)
	// Allow up to 1 MB per line to handle wide terminal output.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var ring []string
	for scanner.Scan() {
		ring = append(ring, scanner.Text())
		if len(ring) > tailLines {
			ring = ring[1:]
		}
	}
	// scanner.Err() is intentionally ignored — partial reads are fine.

	if len(ring) == 0 {
		return
	}

	logFile := filepath.Join(logDir, name+OutputLogSuffix)
	var b strings.Builder
	for _, line := range ring {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(logFile, []byte(b.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: output log: cannot write %s: %v\n", logFile, err)
	}
}

// captureContainerOutput fetches the container's logs via the Docker API and
// persists the last OutputLogTailLines lines to disk. For AutoRemove=true
// containers the container may already be gone by the time we call
// ContainerLogs — that is handled gracefully (skip, no error).
func (r *Runner) captureContainerOutput(containerID string, spec ContainerSpec) {
	if spec.LogDir == "" || spec.Name == "" {
		return
	}
	tail := fmt.Sprintf("%d", OutputLogTailLines)
	logReader, err := r.client.ContainerLogs(context.Background(), containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		// Expected for AutoRemove containers — the container is already gone.
		logging.Debug("output log: ContainerLogs failed for %s: %v", spec.Name, err)
		return
	}
	defer func() { _ = logReader.Close() }()

	// Docker multiplexes stdout/stderr with an 8-byte header per frame when
	// TTY is false. For TTY containers the stream is raw bytes. Demux
	// non-TTY output into a single combined stream via stdcopy.
	var src io.Reader
	if spec.TTY {
		src = logReader
	} else {
		pr, pw := io.Pipe()
		go func() {
			// StdCopy demuxes into pw (combined). Ignore the returned
			// byte counts — we only care about the content.
			_, _ = stdcopy.StdCopy(pw, pw, logReader)
			pw.Close()
		}()
		src = pr
	}

	saveContainerOutput(spec.LogDir, spec.Name, src, OutputLogTailLines)
}

// saveExitLog appends an exit log entry to a log file for the container.
// Exit logging is best-effort: failures are warned on stderr but never fatal.
func (r *Runner) saveExitLog(logDir, name string, exitCode int) {
	if logDir == "" {
		return
	}
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: exit log: cannot create log dir %s: %v\n", logDir, err)
		return
	}
	logFile := filepath.Join(logDir, name+".log")
	entry := fmt.Sprintf("%s container=%s exit_code=%d\n", time.Now().Format(time.RFC3339), name, exitCode)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: exit log: cannot open %s: %v\n", logFile, err)
		return
	}
	defer func() { _ = f.Close() }()

	// Advisory exclusive lock prevents concurrent processes from interleaving
	// exit-log entries. flock(2) is available on Linux and macOS.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: exit log: cannot lock %s: %v\n", logFile, err)
		return
	}
	defer func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }()

	if _, err := f.WriteString(entry); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: exit log: cannot write to %s: %v\n", logFile, err)
	}
}

// Client returns the underlying Docker client for DIND integration.
func (r *Runner) Client() *client.Client {
	return r.client
}

// Close closes the Docker client connection.
func (r *Runner) Close() error {
	return r.client.Close()
}
