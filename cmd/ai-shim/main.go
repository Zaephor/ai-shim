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
	"github.com/ai-shim/ai-shim/internal/logging"
	"github.com/ai-shim/ai-shim/internal/network"
	"github.com/ai-shim/ai-shim/internal/platform"
	"github.com/ai-shim/ai-shim/internal/security"
	"github.com/ai-shim/ai-shim/internal/selfupdate"
	"github.com/ai-shim/ai-shim/internal/storage"
	"github.com/ai-shim/ai-shim/internal/workspace"
	"github.com/docker/docker/api/types/mount"
)

const version = "dev"

func main() {
	logging.Init()

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

func printHelp() {
	fmt.Print(`ai-shim - AI coding agent container launcher

Usage:
  <agent>_<profile> [args...]    Launch agent via symlink
  ai-shim <command> [args...]    Management commands

Commands:
  version                        Print version
  update                         Check for and install updates
  init                           Initialize ai-shim configuration
  manage agents                  List available agents
  manage profiles                List profiles
  manage config <agent> <profile> Show resolved config
  manage doctor                  Run diagnostics
  manage symlinks <sub> [args]   Manage symlinks (create/list/remove)
  manage dry-run <agent> <profile> Preview container config
  manage cleanup                 Remove orphaned containers
  manage status                  Show running containers
  help                           Show this help

Environment Variables:
  AI_SHIM_IMAGE         Override container image
  AI_SHIM_VERSION       Pin agent version
  AI_SHIM_DIND          Enable/disable DIND (0/1)
  AI_SHIM_GPU           Enable/disable GPU (0/1)
  AI_SHIM_NETWORK_SCOPE Network isolation scope
  AI_SHIM_DIND_HOSTNAME DIND container hostname
  AI_SHIM_DIND_CACHE    Enable registry cache (0/1)
  AI_SHIM_VERBOSE       Enable debug output (0/1)
`)
}

func formatAgentList() string {
	var s string
	for _, name := range agent.Names() {
		s += "  " + name + "\n"
	}
	return s
}

func runManage(args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	switch args[0] {
	case "help", "--help", "-h":
		printHelp()
		return nil

	case "init":
		layout := storage.NewLayout(storage.DefaultRoot())
		if err := cli.Init(layout); err != nil {
			return err
		}
		fmt.Printf("Initialized ai-shim at %s\n", layout.Root)
		fmt.Println("Next: ai-shim manage symlinks create <agent> <profile>")
		return nil

	case "version":
		fmt.Printf("ai-shim %s\n", version)
		return nil

	case "update":
		if version == "dev" {
			fmt.Printf("ai-shim %s is a development build, skipping update check\n", version)
			return nil
		}
		latest, err := selfupdate.CheckLatest()
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}
		if !selfupdate.NeedsUpdate(version, latest) {
			fmt.Printf("ai-shim %s is up to date (latest: %s)\n", version, latest)
			return nil
		}
		fmt.Printf("Update available: %s -> %s\n", version, latest)

		// Fetch full release to find download URL
		release, err := selfupdate.FetchRelease()
		if err != nil {
			fmt.Printf("Download manually: https://github.com/%s/releases/latest\n", selfupdate.GitHubRepo)
			return fmt.Errorf("fetching release info: %w", err)
		}

		downloadURL, err := selfupdate.FindAssetURL(release)
		if err != nil {
			fmt.Printf("Download manually: https://github.com/%s/releases/latest\n", selfupdate.GitHubRepo)
			return fmt.Errorf("finding download for your platform: %w", err)
		}

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine binary path: %w", err)
		}
		// Resolve symlinks to get the actual binary
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return fmt.Errorf("resolving binary path: %w", err)
		}

		fmt.Printf("Downloading %s...\n", downloadURL)
		if err := selfupdate.DownloadAndReplace(downloadURL, exe); err != nil {
			return fmt.Errorf("updating binary: %w", err)
		}
		fmt.Printf("Updated to %s successfully. Backup at %s\n", latest, selfupdate.BackupPath(exe))
		return nil

	case "manage":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage <subcommand>")
		}
		return runManageSubcommand(args[1:])

	default:
		return fmt.Errorf("unknown command: %s\nRun 'ai-shim help' for usage", args[0])
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
		exe, _ := os.Executable()
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

	case "status":
		output, err := cli.Status()
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil

	case "cleanup":
		result, err := cli.Cleanup()
		if err != nil {
			return err
		}
		if len(result.Removed) == 0 && len(result.Failed) == 0 {
			fmt.Println("No orphaned containers found.")
		} else {
			if len(result.Removed) > 0 {
				fmt.Printf("Removed %d orphaned container(s):\n", len(result.Removed))
				for _, r := range result.Removed {
					fmt.Println("  " + r)
				}
			}
			if len(result.Failed) > 0 {
				fmt.Printf("Failed to remove %d container(s):\n", len(result.Failed))
				for _, f := range result.Failed {
					fmt.Println("  " + f)
				}
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown manage subcommand: %s\nAvailable: agents, profiles, config, doctor, symlinks, dry-run, status, cleanup", args[0])
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
		return 1, fmt.Errorf("unknown agent: %s\n\nAvailable agents:\n%s\nUse 'ai-shim manage agents' for details", agentName, formatAgentList())
	}

	// 3. Detect platform
	platInfo := platform.Detect()

	// 4. Setup storage layout, ensure directories
	layout := storage.NewLayout(storage.DefaultRoot())

	// Check for first run
	if cli.IsFirstRun(layout) {
		cli.PrintFirstRunHelp(layout)
		return 1, fmt.Errorf("run 'ai-shim init' to set up")
	}
	if err := layout.EnsureDirectories(agentName, profileName); err != nil {
		return 1, fmt.Errorf("setting up directories: %w", err)
	}

	// 5. Resolve config
	cfg, err := config.Resolve(layout.ConfigDir, agentName, profileName)
	if err != nil {
		return 1, fmt.Errorf("resolving config: %w", err)
	}

	// 5.1 Validate config
	if warnings := cfg.Validate(); len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "ai-shim: config warning: %s\n", w)
		}
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
		return 1, fmt.Errorf("invalid volume config: %v", errs[0])
	}

	// 6. Create Docker runner
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return 1, fmt.Errorf("creating container runner: %w", err)
	}
	defer runner.Close()

	// 6.5 Ensure container image is available
	image := cfg.Image
	if image == "" {
		image = container.DefaultImage
	}
	if err := runner.EnsureImage(ctx, image); err != nil {
		return 1, fmt.Errorf("preparing image: %w", err)
	}

	// 6.6 Detect home directory from container image
	imageUser, err := runner.InspectImageUser(ctx, image)
	if err != nil {
		// Non-fatal: use defaults
		imageUser = container.ImageUser{HomeDir: "/home/user", Username: "user"}
	}

	// 7. Build container spec
	logDir := filepath.Join(layout.Root, "logs")

	logging.Debug("agent=%s profile=%s", agentName, profileName)
	logging.Debug("platform: uid=%d gid=%d hostname=%s", platInfo.UID, platInfo.GID, platInfo.Hostname)
	logging.Debug("image=%s hostname=%s", image, cfg.Hostname)

	spec := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  profileName,
		Layout:   layout,
		Platform: platInfo,
		Args:     args,
		HomeDir:  imageUser.HomeDir,
		LogDir:   logDir,
	})

	logging.Debug("workdir=%s", spec.WorkingDir)
	if logging.IsVerbose() {
		logging.Debug("environment:")
		logging.DebugEnv(cfg.Env)
	}
	logging.Debug("container name=%s", spec.Name)

	// 7.5 Create shared network and start DIND sidecar if enabled
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

		dindHostname := "ai-shim-dind"
		if cfg.DINDHostname != "" {
			dindHostname = cfg.DINDHostname
		}

		wsHash := workspace.HashPath(platInfo.Hostname, pwd)
		networkName := network.ResolveName(cfg.NetworkScope, agentName, profileName, wsHash)

		netHandle, err := network.EnsureNetwork(ctx, runner.Client(), networkName, spec.Labels)
		if err != nil {
			return 1, fmt.Errorf("creating network: %w", err)
		}
		defer netHandle.Remove(ctx)

		// Attach agent container to network
		spec.NetworkID = netHandle.ID

		// Registry mirrors (default includes Google's mirror)
		mirrors := cfg.DINDMirrors
		if len(mirrors) == 0 {
			mirrors = []string{"https://mirror.gcr.io"}
		}

		// Pull-through cache
		var cacheAddr string
		cacheEnabled := false
		if cfg.DINDCache != nil && *cfg.DINDCache {
			cacheEnabled = true
		}

		if cacheEnabled {
			cacheDir := filepath.Join(layout.Root, "shared", "registry-cache")
			addr, err := dind.EnsureCache(ctx, runner.Client(), cacheDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to start registry cache: %v\n", err)
			} else {
				cacheAddr = addr
			}
		}

		// Pull DIND image if needed
		dindImage := dind.DefaultImage
		if err := runner.EnsureImage(ctx, dindImage); err != nil {
			return 1, fmt.Errorf("preparing DIND image: %w", err)
		}

		// Start DIND sidecar on same network
		dindName := spec.Name + "-dind"
		var dindResources *dind.ResourceLimits
		if cfg.DINDResources != nil {
			dindResources = &dind.ResourceLimits{
				Memory: cfg.DINDResources.Memory,
				CPUs:   cfg.DINDResources.CPUs,
			}
		}
		sidecar, err := dind.Start(ctx, runner.Client(), dind.Config{
			GPU:           dindGPU,
			UseSysbox:     useSysbox,
			Labels:        spec.Labels,
			ContainerName: dindName,
			Hostname:      dindHostname,
			NetworkID:     netHandle.ID,
			Mirrors:       mirrors,
			CacheAddr:     cacheAddr,
			Resources:     dindResources,
		})
		if err != nil {
			return 1, fmt.Errorf("starting DIND sidecar: %w", err)
		}
		defer func() {
			sidecar.Stop(ctx)
			if cacheEnabled {
				dind.MaybeStopCache(ctx, runner.Client())
			}
		}()

		// Mount DIND socket into agent container
		spec.Mounts = append(spec.Mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: sidecar.SocketVolume(),
			Target: "/var/run/dind",
		})
		spec.Env = append(spec.Env, "DOCKER_HOST=unix:///var/run/dind/docker.sock")
	}

	// 8. Run container, return its exit code
	exitCode, err := runner.Run(ctx, spec)
	if err != nil {
		return 1, fmt.Errorf("running container: %w", err)
	}

	return exitCode, nil
}
