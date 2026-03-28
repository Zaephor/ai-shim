package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/docker"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

// Exec runs a command inside a running ai-shim container identified by name.
// It looks up the container by name using the ai-shim label filter, allocates
// a TTY if stdin is a terminal, and streams stdin/stdout/stderr. It returns
// the exec process exit code.
func Exec(containerName string, cmd []string) (int, error) {
	ctx := context.Background()
	cli, err := docker.NewClient(ctx)
	if err != nil {
		return -1, err
	}
	defer func() { _ = cli.Close() }()

	containerID, err := findContainerByName(ctx, cli, containerName)
	if err != nil {
		return -1, err
	}

	isTTY := stdinIsTerminal()

	execCfg := container_types.ExecOptions{
		Cmd:          cmd,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          isTTY,
	}

	execResp, err := cli.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return -1, fmt.Errorf("creating exec: %w", err)
	}

	attachResp, err := cli.ContainerExecAttach(ctx, execResp.ID, container_types.ExecAttachOptions{
		Tty: isTTY,
	})
	if err != nil {
		return -1, fmt.Errorf("attaching to exec: %w", err)
	}
	defer attachResp.Close()

	// Stream stdin
	go func() {
		_, _ = io.Copy(attachResp.Conn, os.Stdin)
		_ = attachResp.CloseWrite()
	}()

	// Stream stdout/stderr
	if isTTY {
		_, _ = io.Copy(os.Stdout, attachResp.Reader)
	} else {
		_, _ = stdcopy.StdCopy(os.Stdout, os.Stderr, attachResp.Reader)
	}

	// Get exit code
	inspectResp, err := cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return -1, fmt.Errorf("inspecting exec: %w", err)
	}

	return inspectResp.ExitCode, nil
}

// findContainerByName finds a running ai-shim container by its name.
func findContainerByName(ctx context.Context, cli *client.Client, name string) (string, error) {
	containers, err := cli.ContainerList(ctx, container_types.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", container.LabelBase+"=true"),
			filters.Arg("name", name),
		),
	})
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	if len(containers) == 0 {
		return "", fmt.Errorf("no running ai-shim container named %q", name)
	}

	return containers[0].ID, nil
}

// stdinIsTerminal reports whether stdin is connected to a terminal.
func stdinIsTerminal() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}
