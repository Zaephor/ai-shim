package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

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

	// Use a timeout for the container lookup — the exec itself runs
	// as long as the user's command takes.
	lookupCtx, lookupCancel := context.WithTimeout(ctx, 30*time.Second)
	defer lookupCancel()
	containerID, err := findContainerByName(lookupCtx, cli, containerName)
	if err != nil {
		return -1, err
	}

	isTTY := container.IsTTY()

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
