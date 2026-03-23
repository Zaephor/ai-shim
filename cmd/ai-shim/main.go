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
	// Use os.Args[0] instead of os.Executable() because the latter resolves
	// symlinks, which would defeat symlink-based invocation detection.
	name := filepath.Base(os.Args[0])

	if name == "ai-shim" || name == "ai-shim.exe" {
		// Direct invocation — management mode
		if err := runManage(os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Symlink invocation — agent launch mode
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

func runAgent(name string, args []string) (int, error) {
	// 1. Parse symlink name → agent + profile
	agentName, profileName, err := invocation.ParseName(name)
	if err != nil {
		return 1, fmt.Errorf("parsing invocation name: %w", err)
	}

	// 2. Lookup agent definition
	agentDef, ok := agent.Lookup(agentName)
	if !ok {
		return 1, fmt.Errorf("unknown agent: %s", agentName)
	}

	// 3. Detect platform
	platInfo := platform.Detect()

	// 4. Setup storage layout, ensure directories
	layout := storage.NewLayout(storage.DefaultRoot())
	if err := layout.EnsureDirectories(agentName, profileName); err != nil {
		return 1, fmt.Errorf("setting up directories: %w", err)
	}

	// 5. Resolve config
	cfg, err := config.Resolve(layout.ConfigDir, agentName, profileName)
	if err != nil {
		return 1, fmt.Errorf("resolving config: %w", err)
	}

	// 6. Build container spec
	spec := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  profileName,
		Layout:   layout,
		Platform: platInfo,
		Args:     args,
	})

	// 7. Create Docker runner
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return 1, fmt.Errorf("creating container runner: %w", err)
	}
	defer runner.Close()

	// 8. Run container, return its exit code
	exitCode, err := runner.Run(ctx, spec)
	if err != nil {
		return 1, fmt.Errorf("running container: %w", err)
	}

	return exitCode, nil
}
