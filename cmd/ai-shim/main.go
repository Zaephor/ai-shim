package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ai-shim/ai-shim/internal/agent"
	"github.com/ai-shim/ai-shim/internal/cli"
	"github.com/ai-shim/ai-shim/internal/config"
	"github.com/ai-shim/ai-shim/internal/container"
	"github.com/ai-shim/ai-shim/internal/dind"
	"github.com/ai-shim/ai-shim/internal/invocation"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/selfupdate"
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
	if len(args) == 0 {
		fmt.Printf("ai-shim %s\n", version)
		return nil
	}

	switch args[0] {
	case "version":
		fmt.Printf("ai-shim %s\n", version)
		return nil

	case "update":
		latest, err := selfupdate.CheckLatest()
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}
		if !selfupdate.NeedsUpdate(version, latest) {
			fmt.Printf("ai-shim %s is up to date (latest: %s)\n", version, latest)
			return nil
		}
		fmt.Printf("Update available: %s -> %s\n", version, latest)
		fmt.Println("Download from: https://github.com/ai-shim/ai-shim/releases/latest")
		return nil

	case "manage":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage <subcommand>")
		}
		return runManageSubcommand(args[1:])

	default:
		return fmt.Errorf("unknown command: %s\nAvailable: version, update, manage", args[0])
	}
}

func runManageSubcommand(args []string) error {
	layout := storage.NewLayout(storage.DefaultRoot())

	switch args[0] {
	case "agents":
		fmt.Print(cli.ListAgents())
		return nil

	case "profiles":
		output, err := cli.ListProfiles(layout)
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil

	case "config":
		if len(args) < 3 {
			return fmt.Errorf("usage: ai-shim manage config <agent> <profile>")
		}
		output, err := cli.ShowConfig(layout, args[1], args[2])
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil

	case "doctor":
		fmt.Print(cli.Doctor())
		return nil

	case "symlinks":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage symlinks <list|create|remove> [args...]")
		}
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine executable path: %w", err)
		}
		switch args[1] {
		case "list":
			dir := "."
			if len(args) > 2 {
				dir = args[2]
			}
			links, err := cli.ListSymlinks(dir, exe)
			if err != nil {
				return err
			}
			if len(links) == 0 {
				fmt.Println("No ai-shim symlinks found.")
			} else {
				for _, l := range links {
					fmt.Println("  " + l)
				}
			}
			return nil
		case "create":
			if len(args) < 3 {
				return fmt.Errorf("usage: ai-shim manage symlinks create <agent> [profile] [dir]")
			}
			agentName := args[2]
			profile := "default"
			dir := "."
			if len(args) > 3 {
				profile = args[3]
			}
			if len(args) > 4 {
				dir = args[4]
			}
			path, err := cli.CreateSymlink(agentName, profile, dir, exe)
			if err != nil {
				return err
			}
			fmt.Printf("Created symlink: %s\n", path)
			return nil
		case "remove":
			if len(args) < 3 {
				return fmt.Errorf("usage: ai-shim manage symlinks remove <path>")
			}
			return cli.RemoveSymlink(args[2])
		default:
			return fmt.Errorf("unknown symlinks subcommand: %s", args[1])
		}

	case "dry-run":
		if len(args) < 3 {
			return fmt.Errorf("usage: ai-shim manage dry-run <agent> <profile> [args...]")
		}
		var extraArgs []string
		if len(args) > 3 {
			extraArgs = args[3:]
		}
		output, err := cli.DryRun(layout, args[1], args[2], extraArgs)
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil

	case "cleanup":
		removed, err := cli.Cleanup()
		if err != nil {
			return err
		}
		if len(removed) == 0 {
			fmt.Println("No orphaned containers found.")
		} else {
			fmt.Printf("Removed %d orphaned container(s):\n", len(removed))
			for _, r := range removed {
				fmt.Println("  " + r)
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown manage subcommand: %s\nAvailable: agents, profiles, config, doctor, symlinks, dry-run, cleanup", args[0])
	}
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

	// 5.5 Validate working directory
	pwd, err := os.Getwd()
	if err != nil {
		return 1, fmt.Errorf("getting working directory: %w", err)
	}
	if err := security.ValidateWorkingDirectory(pwd); err != nil {
		return 1, err
	}

	// 5.6 Validate config volumes
	if errs := container.ValidateConfigVolumes(cfg.Volumes); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "ai-shim: %v\n", e)
		}
		return 1, fmt.Errorf("invalid volume configuration (%d error(s))", len(errs))
	}

	// 6. Create Docker runner
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return 1, fmt.Errorf("creating container runner: %w", err)
	}
	defer runner.Close()

	// 6.5 Detect home directory from container image
	image := cfg.Image
	if image == "" {
		image = container.DefaultImage
	}
	imageUser, err := runner.InspectImageUser(ctx, image)
	if err != nil {
		// Non-fatal: use defaults
		imageUser = container.ImageUser{HomeDir: "/home/user", Username: "user"}
	}

	// 7. Build container spec
	spec := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  profileName,
		Layout:   layout,
		Platform: platInfo,
		Args:     args,
		HomeDir:  imageUser.HomeDir,
	})

	// 7.5 Start DIND sidecar if enabled
	dindEnabled := false
	if cfg.DIND != nil && *cfg.DIND {
		dindEnabled = true
	}

	if dindEnabled {
		dindGPU := false
		if cfg.DINDGpu != nil {
			dindGPU = *cfg.DINDGpu
		}

		useSysbox := dind.DetectSysbox(ctx, runner.Client())

		sidecar, err := dind.Start(ctx, runner.Client(), dind.Config{
			GPU:       dindGPU,
			UseSysbox: useSysbox,
			Labels:    spec.Labels,
		})
		if err != nil {
			return 1, fmt.Errorf("starting DIND sidecar: %w", err)
		}
		defer sidecar.Stop(ctx)

		// Add DOCKER_HOST env var pointing to DIND
		spec.Env = append(spec.Env, "DOCKER_HOST=tcp://"+sidecar.ContainerID()+":2375")
	}

	// 8. Run container, return its exit code
	exitCode, err := runner.Run(ctx, spec)
	if err != nil {
		return 1, fmt.Errorf("running container: %w", err)
	}

	return exitCode, nil
}
