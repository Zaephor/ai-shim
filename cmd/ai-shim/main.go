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
  run <agent> [profile] [-- args]  Launch agent without creating a symlink
  version                        Print version
  update                         Check for and install updates
  init                           Initialize ai-shim configuration
  manage agents                  List available agents
  manage profiles                List profiles
  manage config <agent> <profile> Show resolved config
  manage doctor                  Run diagnostics
  manage symlinks <sub> [args]   Manage symlinks (create/list/remove)
  manage dry-run <agent> <profile> Preview container config
  manage backup <profile> [path]  Backup profile to tar.gz
  manage restore <profile> <archive> Restore profile from tar.gz
  manage disk-usage              Show storage usage breakdown
  manage cleanup                 Remove orphaned containers
  manage status                  Show running containers
  manage agent-versions          Show installed agent versions
  manage reinstall <agent>       Force reinstall an agent
  completion <bash|zsh>          Generate shell completion script
  help                           Show this help

Environment Variables:
  AI_SHIM_IMAGE         Override container image
  AI_SHIM_VERSION       Pin agent version
  AI_SHIM_DIND          Enable/disable DIND (0/1)
  AI_SHIM_DIND_GPU      Enable/disable GPU for DIND (0/1)
  AI_SHIM_GPU           Enable/disable GPU (0/1)
  AI_SHIM_NETWORK_SCOPE Network isolation scope
  AI_SHIM_DIND_HOSTNAME DIND container hostname
  AI_SHIM_DIND_CACHE    Enable registry cache (0/1)
  AI_SHIM_DIND_TLS      Enable TLS for DIND socket (0/1)
  AI_SHIM_SECURITY_PROFILE Security profile (default/strict/none)
  AI_SHIM_GIT_NAME      Git user.name for container commits
  AI_SHIM_GIT_EMAIL     Git user.email for container commits
  AI_SHIM_VERBOSE       Enable debug output (0/1)
  AI_SHIM_JSON          Enable JSON output for management commands (0/1)
  AI_SHIM_NO_COLOR      Disable colored output (0/1)
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

	case "completion":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim completion <bash|zsh>")
		}
		switch args[1] {
		case "bash":
			fmt.Print(cli.BashCompletion())
		case "zsh":
			fmt.Print(cli.ZshCompletion())
		default:
			return fmt.Errorf("unsupported shell: %s (supported: bash, zsh)", args[1])
		}
		return nil

	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim run <agent> [profile] [-- args...]")
		}
		agentName := args[1]
		profile := "default"
		var passthroughArgs []string

		// Parse: ai-shim run <agent> [profile] [-- args...]
		remaining := args[2:]
		for i, arg := range remaining {
			if arg == "--" {
				passthroughArgs = remaining[i+1:]
				break
			}
			if i == 0 {
				profile = arg
			}
		}

		// Construct the invocation name and delegate to runAgent
		invocationName := agentName
		if profile != "default" {
			invocationName = agentName + "_" + profile
		}
		exitCode, err := runAgent(invocationName, passthroughArgs)
		if err != nil {
			return err
		}
		os.Exit(exitCode)
		return nil

	case "manage":
		if len(args) < 2 || args[1] == "--help" || args[1] == "-h" {
			fmt.Print(`Usage: ai-shim manage <subcommand>

Subcommands:
  agents       List available agents
  profiles     List profiles
  config       Show resolved config for agent+profile
  doctor       Run diagnostic checks
  symlinks     Manage symlinks (create/list/remove)
  dry-run      Preview container config
  status       Show running containers
  backup       Backup a profile
  restore      Restore a profile from backup
  disk-usage      Show storage usage breakdown
  cleanup         Remove orphaned containers, networks, volumes
  agent-versions  Show installed agent versions
  reinstall       Force reinstall an agent
`)
			return nil
		}
		return runManageSubcommand(args[1:])

	default:
		return fmt.Errorf("unknown command: %s\nRun 'ai-shim help' for usage", args[0])
	}
}

func printSubcommandHelp(cmd string) error {
	helps := map[string]string{
		"agents":     "Usage: ai-shim manage agents\n\n  List all built-in and configured agents.",
		"profiles":   "Usage: ai-shim manage profiles\n\n  List all profiles in ~/.ai-shim/profiles/.",
		"config":     "Usage: ai-shim manage config <agent> <profile>\n\n  Show the fully resolved config for an agent+profile combination.",
		"doctor":     "Usage: ai-shim manage doctor\n\n  Run diagnostic checks (Docker, storage, image availability).",
		"symlinks":   "Usage: ai-shim manage symlinks <create|list|remove> [args...]\n\n  create <agent> [profile] [dir]  Create a symlink\n  list [dir]                      List ai-shim symlinks\n  remove <path>                   Remove a symlink",
		"dry-run":    "Usage: ai-shim manage dry-run <agent> <profile> [args...]\n\n  Preview the full container configuration without launching.",
		"cleanup":    "Usage: ai-shim manage cleanup\n\n  Remove orphaned ai-shim containers, networks, and volumes.",
		"status":     "Usage: ai-shim manage status\n\n  Show running ai-shim containers.",
		"backup":     "Usage: ai-shim manage backup <profile> [output-path]\n\n  Create a tar.gz backup of a profile's home directory.",
		"restore":    "Usage: ai-shim manage restore <profile> <archive-path>\n\n  Restore a profile from a tar.gz backup.",
		"disk-usage":      "Usage: ai-shim manage disk-usage\n\n  Show storage usage breakdown by category and profile.",
		"agent-versions":  "Usage: ai-shim manage agent-versions\n\n  Show installed agent versions by checking bin directories.",
		"reinstall":       "Usage: ai-shim manage reinstall <agent> [profile]\n\n  Force reinstall an agent by clearing its bin cache.",
	}
	if help, ok := helps[cmd]; ok {
		fmt.Println(help)
		return nil
	}
	return fmt.Errorf("unknown subcommand: %s", cmd)
}

func runManageSubcommand(args []string) error {
	if len(args) > 1 && (args[1] == "--help" || args[1] == "-h") {
		return printSubcommandHelp(args[0])
	}

	layout := storage.NewLayout(storage.DefaultRoot())

	jsonMode := cli.IsJSONMode()

	switch args[0] {
	case "agents":
		if jsonMode {
			output, err := cli.ListAgentsJSON()
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		}
		fmt.Print(cli.ListAgents())
		return nil

	case "profiles":
		if jsonMode {
			output, err := cli.ListProfilesJSON(layout)
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		}
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
		if jsonMode {
			output, err := cli.ShowConfigJSON(layout, args[1], args[2])
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.ShowConfig(layout, args[1], args[2])
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil

	case "doctor":
		if jsonMode {
			output, err := cli.DoctorJSON()
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		}
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
		if jsonMode {
			output, err := cli.StatusJSON()
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.Status()
		if err != nil {
			return err
		}
		fmt.Print(output)
		return nil

	case "backup":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage backup <profile> [output-path]")
		}
		outputPath := ""
		if len(args) > 2 {
			outputPath = args[2]
		}
		if err := cli.BackupProfile(layout, args[1], outputPath); err != nil {
			return err
		}
		fmt.Printf("Profile %s backed up successfully\n", args[1])
		return nil

	case "restore":
		if len(args) < 3 {
			return fmt.Errorf("usage: ai-shim manage restore <profile> <archive-path>")
		}
		if err := cli.RestoreProfile(layout, args[1], args[2]); err != nil {
			return err
		}
		fmt.Printf("Profile %s restored successfully\n", args[1])
		return nil

	case "disk-usage":
		if jsonMode {
			output, err := cli.DiskUsageJSON(layout)
			if err != nil {
				return err
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.DiskUsage(layout)
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
		totalRemoved := len(result.RemovedContainers) + len(result.RemovedNetworks) + len(result.RemovedVolumes)
		if totalRemoved == 0 && len(result.Failed) == 0 {
			fmt.Println("No orphaned resources found.")
		} else {
			if len(result.RemovedContainers) > 0 {
				fmt.Printf("Removed %d orphaned container(s):\n", len(result.RemovedContainers))
				for _, r := range result.RemovedContainers {
					fmt.Println("  " + r)
				}
			}
			if len(result.RemovedNetworks) > 0 {
				fmt.Printf("Removed %d orphaned network(s):\n", len(result.RemovedNetworks))
				for _, r := range result.RemovedNetworks {
					fmt.Println("  " + r)
				}
			}
			if len(result.RemovedVolumes) > 0 {
				fmt.Printf("Removed %d orphaned volume(s):\n", len(result.RemovedVolumes))
				for _, r := range result.RemovedVolumes {
					fmt.Println("  " + r)
				}
			}
			if len(result.Failed) > 0 {
				fmt.Printf("Failed to remove %d resource(s):\n", len(result.Failed))
				for _, f := range result.Failed {
					fmt.Println("  " + f)
				}
			}
		}
		return nil

	case "agent-versions":
		output := cli.AgentVersions(layout)
		fmt.Print(output)
		return nil

	case "reinstall":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage reinstall <agent> [profile]")
		}
		agentName := args[1]
		if err := cli.Reinstall(layout, agentName); err != nil {
			return err
		}
		fmt.Printf("Agent %s bin cache cleared. It will be reinstalled on next launch.\n", agentName)
		return nil

	default:
		return fmt.Errorf("unknown manage subcommand: %s\nAvailable: agents, profiles, config, doctor, symlinks, dry-run, status, backup, restore, disk-usage, cleanup, agent-versions, reinstall", args[0])
	}
}

func runAgent(name string, args []string) (int, error) {
	// 1. Parse symlink name → agent + profile
	agentName, profileName, err := invocation.ParseName(name)
	if err != nil {
		return 1, fmt.Errorf("parsing invocation name: %w", err)
	}

	// 2. Setup storage layout
	layout := storage.NewLayout(storage.DefaultRoot())

	// 2.5 Load custom agent definitions from config
	if customDefs := agent.LoadCustomAgents(layout.ConfigDir); customDefs != nil {
		agent.SetCustomAgents(customDefs)
	}

	// 3. Lookup agent definition
	agentDef, ok := agent.Lookup(agentName)
	if !ok {
		return 1, fmt.Errorf("unknown agent: %s\n\nAvailable agents:\n%s\nUse 'ai-shim manage agents' for details", agentName, formatAgentList())
	}

	// 4. Detect platform
	platInfo := platform.Detect()

	if platInfo.UID == 0 {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: running as root (UID 0). Container will run as root.\n")
	}

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

	// 5.7 Pre-create agent data dirs/files for correct ownership
	if err := layout.EnsureAgentData(profileName, agentDef.DataDirs, agentDef.DataFiles); err != nil {
		return 1, fmt.Errorf("setting up agent data: %w", err)
	}
	// Also pre-create data for allowed agents
	for _, name := range cfg.AllowAgents {
		if allowed, ok := agent.Lookup(name); ok {
			if err := layout.EnsureAgentData(profileName, allowed.DataDirs, allowed.DataFiles); err != nil {
				return 1, fmt.Errorf("setting up agent data for %s: %w", name, err)
			}
		}
	}

	// 6. Create Docker runner
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return 1, fmt.Errorf("creating container runner: %w", err)
	}
	defer runner.Close()

	// 6.5 Ensure container image is available
	image := cfg.GetImage()
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
	if cfg.Resources != nil {
		logging.Debug("resources: memory=%s cpus=%s", cfg.Resources.Memory, cfg.Resources.CPUs)
	}
	if cfg.IsDINDEnabled() {
		logging.Debug("dind: enabled, hostname=%s, network_scope=%s", cfg.DINDHostname, cfg.NetworkScope)
		if cfg.DINDResources != nil {
			logging.Debug("dind resources: memory=%s cpus=%s", cfg.DINDResources.Memory, cfg.DINDResources.CPUs)
		}
	}

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
	if cfg.IsDINDEnabled() {
		dindGPU := cfg.IsDINDGPUEnabled()

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
		if cfg.IsCacheEnabled() {
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
			TLS:           cfg.IsDINDTLSEnabled(),
		})
		if err != nil {
			return 1, fmt.Errorf("starting DIND sidecar: %w", err)
		}
		defer func() {
			sidecar.Stop(ctx)
			if cfg.IsCacheEnabled() {
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

		// Mount TLS certs volume if TLS is enabled
		if sidecar.CertsVolume() != "" {
			spec.Mounts = append(spec.Mounts, mount.Mount{
				Type:   mount.TypeVolume,
				Source: sidecar.CertsVolume(),
				Target: "/certs",
			})
			spec.Env = append(spec.Env, "DOCKER_TLS_VERIFY=1")
			spec.Env = append(spec.Env, "DOCKER_CERT_PATH=/certs/client")
		}
	}

	// 8. Run container, return its exit code
	exitCode, err := runner.Run(ctx, spec)
	if err != nil {
		return 1, fmt.Errorf("running container: %w", err)
	}

	return exitCode, nil
}
