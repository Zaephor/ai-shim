# Phase 4: Container Lifecycle — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Wire everything together to actually launch agents in Docker containers with proper mounts, env vars, TTY, signal passthrough, and cleanup.

**Architecture:** The `platform` package abstracts OS-specific details (socket, UID). The `container` package wraps the Docker SDK to create, attach, wait, and remove containers. The main entrypoint orchestrates the full flow: parse symlink → resolve config → ensure storage → build container spec → launch → cleanup.

**Tech Stack:** Go 1.24+, `github.com/docker/docker`, `github.com/docker/go-connections`, `github.com/stretchr/testify`

---

### Task 1: Platform Abstraction

**Files:**
- Create: `internal/platform/platform.go`
- Create: `internal/platform/platform_test.go`

**platform/platform.go:**
```go
package platform

import (
	"os"
	"os/user"
	"runtime"
	"strconv"
)

// Info holds platform-specific details needed for container configuration.
type Info struct {
	DockerSocket string
	UID          int
	GID          int
	Username     string
	Hostname     string
}

// Detect returns platform information for the current system.
func Detect() Info {
	info := Info{
		DockerSocket: detectSocket(),
		Hostname:     detectHostname(),
	}

	if u, err := user.Current(); err == nil {
		info.Username = u.Username
		info.UID, _ = strconv.Atoi(u.Uid)
		info.GID, _ = strconv.Atoi(u.Gid)
	}

	return info
}

func detectSocket() string {
	// Check common socket paths in order of preference
	candidates := []string{
		"/var/run/docker.sock",
	}

	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		if home != "" {
			candidates = append(candidates,
				home+"/.docker/run/docker.sock",
				home+"/.colima/default/docker.sock",
				home+"/.colima/docker.sock",
			)
		}
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Fallback
	return "/var/run/docker.sock"
}

func detectHostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "localhost"
	}
	return h
}
```

**platform/platform_test.go:**
```go
package platform

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetect_HasHostname(t *testing.T) {
	info := Detect()
	assert.NotEmpty(t, info.Hostname)
}

func TestDetect_HasUsername(t *testing.T) {
	info := Detect()
	assert.NotEmpty(t, info.Username)
}

func TestDetect_HasUID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UID not applicable on Windows")
	}
	info := Detect()
	assert.True(t, info.UID >= 0)
}

func TestDetect_SocketPath(t *testing.T) {
	info := Detect()
	assert.NotEmpty(t, info.DockerSocket)
}
```

Follow TDD. Commit as: `feat(platform): add platform detection for socket, UID, hostname`

---

### Task 2: Container Runner

**Files:**
- Create: `internal/container/runner.go`
- Create: `internal/container/runner_test.go`

This is the Docker SDK wrapper. Integration tests require Docker.

**container/runner.go:**
```go
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
	Image      string
	Hostname   string
	Env        []string            // KEY=VALUE pairs
	Mounts     []mount.Mount
	WorkingDir string
	Entrypoint []string
	Cmd        []string
	User       string              // "uid:gid"
	Labels     map[string]string
	Ports      nat.PortMap
	ExposedPorts nat.PortSet
	TTY        bool
	Stdin      bool
	GPU        bool
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
	// Verify connectivity
	if _, err := cli.Ping(ctx); err != nil {
		return nil, fmt.Errorf("connecting to docker: %w", err)
	}
	return &Runner{client: cli}, nil
}

// Run creates, starts, attaches to, and waits for a container.
// Returns the container's exit code.
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
			{
				Count:        -1, // all GPUs
				Capabilities: [][]string{{"gpu"}},
			},
		}
	}

	resp, err := r.client.ContainerCreate(ctx, containerCfg, hostCfg, &network.NetworkingConfig{}, nil, "")
	if err != nil {
		return -1, fmt.Errorf("creating container: %w", err)
	}
	containerID := resp.ID

	// Attach before starting to not miss any output
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

	// Stream I/O
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

	// Wait for container to exit
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

// Close closes the Docker client connection.
func (r *Runner) Close() error {
	return r.client.Close()
}
```

**container/runner_test.go** — integration test (requires Docker):
```go
package container

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func skipIfNoDocker(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	if err != nil {
		t.Skip("Docker not available:", err)
	}
	runner.Close()
}

func TestNewRunner(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()
}

func TestRun_SimpleCommand(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image: "alpine:latest",
		Cmd:   []string{"echo", "hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_ExitCode(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image: "alpine:latest",
		Cmd:   []string{"sh", "-c", "exit 42"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 42, exitCode)
}

func TestRun_WithEnv(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image: "alpine:latest",
		Env:   []string{"TEST_VAR=hello"},
		Cmd:   []string{"sh", "-c", "test \"$TEST_VAR\" = hello"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithWorkdir(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:      "alpine:latest",
		WorkingDir: "/tmp",
		Cmd:        []string{"sh", "-c", "test \"$(pwd)\" = /tmp"},
		Labels:     map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithHostname(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image:    "alpine:latest",
		Hostname: "test-shim",
		Cmd:      []string{"sh", "-c", "test \"$(hostname)\" = test-shim"},
		Labels:   map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}

func TestRun_WithMount(t *testing.T) {
	skipIfNoDocker(t)
	ctx := context.Background()
	runner, err := NewRunner(ctx)
	require.NoError(t, err)
	defer runner.Close()

	tmpDir := t.TempDir()
	exitCode, err := runner.Run(ctx, ContainerSpec{
		Image: "alpine:latest",
		Mounts: []mount.Mount{
			{Type: mount.TypeBind, Source: tmpDir, Target: "/testmount"},
		},
		Cmd:    []string{"test", "-d", "/testmount"},
		Labels: map[string]string{"ai-shim": "test"},
	})
	require.NoError(t, err)
	assert.Equal(t, 0, exitCode)
}
```

Follow TDD. Commit as: `feat(container): add Docker container runner with lifecycle management`

---

### Task 3: Container Spec Builder

**Files:**
- Create: `internal/container/builder.go`
- Create: `internal/container/builder_test.go`

Builds a ContainerSpec from resolved config, storage layout, agent def, and platform info.

**container/builder.go:**
```go
package container

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/go-connections/nat"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/install"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/workspace"
)

const defaultImage = "ghcr.io/catthehacker/ubuntu:act-24.04"
const defaultHostname = "ai-shim"

// BuildSpec creates a ContainerSpec from all the resolved components.
func BuildSpec(
	cfg config.Config,
	agentDef agent.Definition,
	layout storage.Layout,
	plat platform.Info,
	passthroughArgs []string,
	agentName, profileName string,
) ContainerSpec {
	image := cfg.Image
	if image == "" {
		image = defaultImage
	}

	hostname := cfg.Hostname
	if hostname == "" {
		hostname = defaultHostname
	}

	// Build env KEY=VALUE list
	env := make([]string, 0, len(cfg.Env))
	for k, v := range cfg.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Build mounts
	mounts := buildMounts(cfg, layout, agentName, profileName)

	// Build entrypoint
	allArgs := append(cfg.Args, passthroughArgs...)
	entrypoint := install.GenerateEntrypoint(install.EntrypointParams{
		InstallType: agentDef.InstallType,
		Package:     agentDef.Package,
		Binary:      agentDef.Binary,
		Version:     cfg.Version,
		AgentArgs:   allArgs,
	})

	// User
	user := fmt.Sprintf("%d:%d", plat.UID, plat.GID)

	// Workspace
	pwd, _ := os.Getwd()
	workdir := workspace.ContainerWorkdir(plat.Hostname, pwd)

	// Labels
	labels := map[string]string{
		"ai-shim":         "true",
		"ai-shim.agent":   agentName,
		"ai-shim.profile": profileName,
	}

	// Ports
	portBindings, exposedPorts := buildPorts(cfg.Ports)

	// GPU
	gpu := false
	if cfg.GPU != nil {
		gpu = *cfg.GPU
	}

	// TTY
	isTTY := isTerminal()

	spec := ContainerSpec{
		Image:        image,
		Hostname:     hostname,
		Env:          env,
		Mounts:       mounts,
		WorkingDir:   workdir,
		Entrypoint:   []string{"/bin/sh", "-c", entrypoint},
		User:         user,
		Labels:       labels,
		Ports:        portBindings,
		ExposedPorts: exposedPorts,
		TTY:          isTTY,
		Stdin:        isTTY,
		GPU:          gpu,
	}

	return spec
}

func buildMounts(cfg config.Config, layout storage.Layout, agentName, profileName string) []mount.Mount {
	pwd, _ := os.Getwd()

	mounts := []mount.Mount{
		// Shared bin
		{Type: mount.TypeBind, Source: layout.SharedBin, Target: "/opt/ai-shim/shared/bin"},
		// Agent bin
		{Type: mount.TypeBind, Source: layout.AgentBin(agentName), Target: "/opt/ai-shim/agents/" + agentName + "/bin"},
		// Agent cache
		{Type: mount.TypeBind, Source: layout.AgentCache(agentName), Target: "/opt/ai-shim/agents/" + agentName + "/cache"},
		// Profile home
		{Type: mount.TypeBind, Source: layout.ProfileHome(profileName), Target: "/home/user"},
		// Workspace
		{Type: mount.TypeBind, Source: pwd, Target: workspace.ContainerWorkdir("", pwd)},
	}

	// Extra volumes from config
	for _, vol := range cfg.Volumes {
		parts := filepath.SplitList(vol)
		// Simple host:container parsing
		if src, dst, ok := parseVolume(vol); ok {
			mounts = append(mounts, mount.Mount{
				Type:   mount.TypeBind,
				Source: src,
				Target: dst,
			})
		}
		_ = parts
	}

	return mounts
}

func parseVolume(vol string) (source, target string, ok bool) {
	// Format: /host/path:/container/path or /host/path:/container/path:ro
	for i := 1; i < len(vol); i++ {
		if vol[i] == ':' {
			return vol[:i], vol[i+1:], true
		}
	}
	return "", "", false
}

func buildPorts(ports []string) (nat.PortMap, nat.PortSet) {
	if len(ports) == 0 {
		return nil, nil
	}
	portMap := nat.PortMap{}
	portSet := nat.PortSet{}
	for _, p := range ports {
		// Format: hostPort:containerPort
		for i := 0; i < len(p); i++ {
			if p[i] == ':' {
				hostPort := p[:i]
				containerPort := p[i+1:]
				port := nat.Port(containerPort + "/tcp")
				portSet[port] = struct{}{}
				portMap[port] = []nat.PortBinding{{HostPort: hostPort}}
				break
			}
		}
	}
	return portMap, portSet
}

func isTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
```

**container/builder_test.go:**
```go
package container

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
)

func TestBuildSpec_Defaults(t *testing.T) {
	cfg := config.Config{}
	agentDef := agent.Definition{
		Name: "claude-code", InstallType: "custom",
		Package: "curl -fsSL https://claude.ai/install.sh | bash",
		Binary: "claude",
	}
	layout := storage.NewLayout("/tmp/test-ai-shim")
	plat := platform.Info{UID: 1000, GID: 1000, Username: "testuser", Hostname: "testhost"}

	spec := BuildSpec(cfg, agentDef, layout, plat, nil, "claude-code", "work")

	assert.Equal(t, defaultImage, spec.Image)
	assert.Equal(t, defaultHostname, spec.Hostname)
	assert.Equal(t, "1000:1000", spec.User)
	assert.Equal(t, "true", spec.Labels["ai-shim"])
	assert.Equal(t, "claude-code", spec.Labels["ai-shim.agent"])
	assert.Equal(t, "work", spec.Labels["ai-shim.profile"])
}

func TestBuildSpec_ConfigOverrides(t *testing.T) {
	gpu := true
	cfg := config.Config{
		Image:    "custom-image:latest",
		Hostname: "my-host",
		Env:      map[string]string{"KEY": "value"},
		GPU:      &gpu,
	}
	agentDef := agent.Definition{
		Name: "gemini-cli", InstallType: "npm",
		Package: "@google/gemini-cli", Binary: "gemini",
	}
	layout := storage.NewLayout("/tmp/test-ai-shim")
	plat := platform.Info{UID: 501, GID: 20, Username: "user", Hostname: "mac"}

	spec := BuildSpec(cfg, agentDef, layout, plat, []string{"--verbose"}, "gemini-cli", "personal")

	assert.Equal(t, "custom-image:latest", spec.Image)
	assert.Equal(t, "my-host", spec.Hostname)
	assert.Contains(t, spec.Env, "KEY=value")
	assert.Equal(t, "501:20", spec.User)
	assert.True(t, spec.GPU)
}

func TestBuildSpec_HasRequiredMounts(t *testing.T) {
	cfg := config.Config{}
	agentDef := agent.Definition{Name: "test", InstallType: "npm", Package: "test", Binary: "test"}
	layout := storage.NewLayout("/home/user/.ai-shim")
	plat := platform.Info{UID: 1000, GID: 1000, Hostname: "host"}

	spec := BuildSpec(cfg, agentDef, layout, plat, nil, "test", "default")

	mountTargets := make([]string, len(spec.Mounts))
	for i, m := range spec.Mounts {
		mountTargets[i] = m.Target
	}

	assert.Contains(t, mountTargets, "/opt/ai-shim/shared/bin")
	assert.Contains(t, mountTargets, "/home/user")
}

func TestBuildSpec_Ports(t *testing.T) {
	cfg := config.Config{Ports: []string{"8080:3000"}}
	agentDef := agent.Definition{Name: "test", InstallType: "npm", Package: "test", Binary: "test"}
	layout := storage.NewLayout("/tmp/test")
	plat := platform.Info{UID: 1000, GID: 1000, Hostname: "host"}

	spec := BuildSpec(cfg, agentDef, layout, plat, nil, "test", "default")

	assert.NotNil(t, spec.Ports)
	assert.NotNil(t, spec.ExposedPorts)
}

func TestParseVolume(t *testing.T) {
	src, dst, ok := parseVolume("/host/path:/container/path")
	assert.True(t, ok)
	assert.Equal(t, "/host/path", src)
	assert.Equal(t, "/container/path", dst)
}

func TestParseVolume_Invalid(t *testing.T) {
	_, _, ok := parseVolume("nocolon")
	assert.False(t, ok)
}
```

Follow TDD. Commit as: `feat(container): add container spec builder from resolved config`

---

### Task 4: Wire Main Entrypoint

**Files:**
- Modify: `cmd/ai-shim/main.go`

Update `runAgent` to orchestrate the full launch flow: parse symlink → resolve config → ensure storage → build container spec → launch → exit with agent's code.

**Updated main.go:**
```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/invocation"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/storage"
)

const version = "dev"

func main() {
	name := filepath.Base(os.Args[0])

	if name == "ai-shim" || name == "ai-shim.exe" {
		if err := runManage(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
			os.Exit(1)
		}
		return
	}

	exitCode, err := runAgent(name, os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
		os.Exit(1)
	}
	os.Exit(exitCode)
}

func runManage(args []string) error {
	if len(args) == 0 || args[0] == "version" {
		fmt.Printf("ai-shim %s\n", version)
		return nil
	}
	return fmt.Errorf("unknown command: %s", args[0])
}

func runAgent(name string, passthroughArgs []string) (int, error) {
	// 1. Parse symlink name
	agentName, profileName, err := invocation.ParseName(name)
	if err != nil {
		return -1, fmt.Errorf("parsing invocation name: %w", err)
	}

	// 2. Lookup agent definition
	agentDef, ok := agent.Lookup(agentName)
	if !ok {
		return -1, fmt.Errorf("unknown agent: %s", agentName)
	}

	// 3. Detect platform
	plat := platform.Detect()

	// 4. Setup storage
	layout := storage.NewLayout(storage.DefaultRoot())
	if err := layout.EnsureDirectories(agentName, profileName); err != nil {
		return -1, fmt.Errorf("creating storage directories: %w", err)
	}

	// 5. Resolve config
	cfg, err := config.Resolve(layout.ConfigDir, agentName, profileName)
	if err != nil {
		return -1, fmt.Errorf("resolving config: %w", err)
	}

	// 6. Build container spec
	spec := container.BuildSpec(cfg, agentDef, layout, plat, passthroughArgs, agentName, profileName)

	// 7. Create Docker runner and launch
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return -1, fmt.Errorf("connecting to Docker: %w", err)
	}
	defer runner.Close()

	// 8. Run container
	exitCode, err := runner.Run(ctx, spec)
	if err != nil {
		return -1, fmt.Errorf("running container: %w", err)
	}

	return exitCode, nil
}
```

Build and verify:
```bash
make build
ln -sf ai-shim gemini_work
./gemini_work 2>&1  # Should attempt Docker launch (may fail if no Docker)
rm gemini_work
```

Commit as: `feat: wire main entrypoint to full container launch flow`

---

## Phase 4 Complete

After this phase you have:
- Platform detection (Docker socket, UID/GID, hostname)
- Docker container runner (create, attach, wait, cleanup)
- Container spec builder (mounts, env, entrypoint, ports, labels)
- Full launch flow from symlink to running agent
- Integration tests with real Docker containers

**Next:** Phase 5 — Security & Validation
