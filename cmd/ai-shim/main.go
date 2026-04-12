package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

// cleanupStaleContainers removes any non-running ai-shim containers for the
// given agent+profile. This handles the SIGKILL/OOM/reboot orphan case
// where AutoRemove never fired and a stale container blocks the next launch.
// For persistent containers that exited while detached, reports their exit code.
// All errors are silently ignored — this is best-effort housekeeping.
func cleanupStaleContainers(ctx context.Context, runner *container.Runner, agentName, profileName string) {
	cli := runner.Client()
	if cli == nil {
		return
	}
	for _, status := range []string{"exited", "dead", "created"} {
		list, err := cli.ContainerList(ctx, container_types.ListOptions{
			All: true,
			Filters: filters.NewArgs(
				filters.Arg("label", container.LabelBase+"=true"),
				filters.Arg("label", container.LabelAgent+"="+agentName),
				filters.Arg("label", container.LabelProfile+"="+profileName),
				filters.Arg("status", status),
			),
		})
		if err != nil {
			logging.Debug("stale container list (%s) failed: %v", status, err)
			continue
		}
		for _, c := range list {
			// Report exit code for persistent containers that exited while detached.
			if c.Labels[container.LabelPersistent] == "true" && status == "exited" {
				inspect, inspErr := cli.ContainerInspect(ctx, c.ID)
				if inspErr == nil && inspect.State != nil {
					fmt.Fprintf(os.Stderr, "ai-shim: previous session exited (code %d)\n", inspect.State.ExitCode)
				}
			}
			if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
				logging.Debug("stale container remove %s failed: %v", c.ID[:12], err)
				continue
			}
			logging.Debug("cleaned up stale container %s (status=%s)", c.ID[:12], status)
		}
	}
}

// dirOnPath reports whether the given directory appears in the current
// $PATH. Used to nudge the user when `manage symlinks create` installs
// into a location that would not be found by their shell.
func dirOnPath(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == "" {
			continue
		}
		entryAbs, err := filepath.Abs(entry)
		if err != nil {
			entryAbs = entry
		}
		if entryAbs == abs {
			return true
		}
	}
	return false
}

var version = "dev"

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
  manage config <agent> [profile] Show resolved config (default: "default")
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
  manage exec <name> <cmd...>   Execute command in running container
  manage attach <agent> [profile] Reattach to a detached session
  manage stop <agent> [profile]  Stop a running session
  manage logs [agent] [profile]  Show launch/exit logs or container logs
  manage watch <agent> [profile] Restart agent on crash with retries
  manage switch-profile <profile> Set the default profile
  completion <bash|zsh>          Generate shell completion script
  help                           Show this help

Environment Variables:
  AI_SHIM_IMAGE         Override container image
  AI_SHIM_VERSION       Pin agent version
  AI_SHIM_UPDATE_INTERVAL Agent update interval (always/never/1d/7d/24h or any Go duration)
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
  AI_SHIM_SELFUPDATE_REPOSITORY GitHub owner/repo for self-update
  AI_SHIM_SELFUPDATE_API_URL GitHub API base URL (for Enterprise)
  AI_SHIM_SELFUPDATE_ENABLED Enable/disable self-update (0/1)
  AI_SHIM_SELFUPDATE_PRERELEASE Include pre-releases in update (0/1)
  AI_SHIM_VERBOSE       Enable debug output (0/1)
  AI_SHIM_JSON          Enable JSON output for management commands (0/1)
  AI_SHIM_NO_COLOR      Disable colored output (0/1)
  AI_SHIM_WATCH_RETRIES Max restart count for watch mode (default 3)
  AI_SHIM_DETACH_KEYS   Detach key sequence (default ctrl-],d)
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

		// Load self-update config from default.yaml (global preference).
		layout := storage.NewLayout(storage.DefaultRoot())
		suOpts := selfupdate.Options{}
		if cfg, err := config.LoadFile(filepath.Join(layout.ConfigDir, "default.yaml")); err == nil && cfg.SelfUpdate != nil {
			su := cfg.SelfUpdate
			if su.Repository != "" {
				suOpts.Repository = su.Repository
			}
			if su.APIURL != "" {
				suOpts.APIURL = su.APIURL
			}
			if su.Prerelease != nil && *su.Prerelease {
				suOpts.Prerelease = true
			}
			if !cfg.IsSelfUpdateEnabled() {
				fmt.Println("Self-update is disabled in ~/.ai-shim/config/default.yaml.")
				fmt.Println("Set `selfupdate.enabled: true` or remove the override to re-enable.")
				return nil
			}
		}
		// Also check env var override (highest priority).
		if envCfg := config.LoadEnvSelfUpdate(); envCfg != nil {
			if envCfg.Repository != "" {
				suOpts.Repository = envCfg.Repository
			}
			if envCfg.APIURL != "" {
				suOpts.APIURL = envCfg.APIURL
			}
			if envCfg.Prerelease != nil && *envCfg.Prerelease {
				suOpts.Prerelease = true
			}
			if envCfg.Enabled != nil && !*envCfg.Enabled {
				fmt.Println("Self-update is disabled via AI_SHIM_SELFUPDATE_ENABLED=0.")
				return nil
			}
		}

		repo := suOpts.Repository
		if repo == "" {
			repo = selfupdate.DefaultRepository
		}

		latest, err := selfupdate.CheckLatest(suOpts)
		if err != nil {
			return fmt.Errorf("checking for updates: %w", err)
		}
		if !selfupdate.NeedsUpdate(version, latest) {
			fmt.Printf("ai-shim %s is up to date (latest: %s)\n", version, latest)
			return nil
		}
		fmt.Printf("Update available: %s -> %s\n", version, latest)

		release, err := selfupdate.FetchRelease(suOpts)
		if err != nil {
			fmt.Printf("Download manually: https://github.com/%s/releases/latest\n", repo)
			return fmt.Errorf("fetching release info: %w", err)
		}

		downloadURL, err := selfupdate.FindAssetURL(release)
		if err != nil {
			fmt.Printf("Download manually: https://github.com/%s/releases/latest\n", repo)
			return fmt.Errorf("finding download for your platform: %w", err)
		}

		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("cannot determine binary path: %w", err)
		}
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
  agents          List available agents
  profiles        List profiles
  config          Show resolved config for agent+profile
  doctor          Run diagnostic checks
  symlinks        Manage symlinks (create/list/remove)
  dry-run         Preview container config
  status          Show running containers
  exec            Execute command in a running container
  attach          Reattach to a detached session
  stop            Stop a running session
  watch           Restart agent on crash with retries
  switch-profile  Set the default profile
  backup          Backup a profile
  restore         Restore a profile from backup
  disk-usage      Show storage usage breakdown
  cleanup         Remove orphaned containers, networks, volumes
  logs            Show launch/exit logs or container logs
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
		"agents":         "Usage: ai-shim manage agents\n\n  List all built-in and configured agents.",
		"profiles":       "Usage: ai-shim manage profiles\n\n  List all configured and launched profiles.",
		"config":         "Usage: ai-shim manage config <agent> [profile]\n\n  Show the fully resolved config for an agent+profile combination.\n  Profile defaults to \"default\" if omitted.",
		"doctor":         "Usage: ai-shim manage doctor\n\n  Run diagnostic checks (Docker, storage, image availability).",
		"symlinks":       "Usage: ai-shim manage symlinks <create|list|remove> [args...]\n\n  create <agent> [profile] [dir]  Create a symlink\n  list [dir]                      List ai-shim symlinks\n  remove <path>                   Remove a symlink\n\nName rules:\n  Agent and profile names must start with a letter or digit and may only\n  contain ASCII letters, digits, '.', '_', and '-' (max 63 characters).\n  These restrictions match Docker container naming so the resulting\n  containers and on-disk paths are always well-formed.",
		"dry-run":        "Usage: ai-shim manage dry-run <agent> <profile> [args...]\n\n  Preview the full container configuration without launching.",
		"cleanup":        "Usage: ai-shim manage cleanup\n\n  Remove orphaned ai-shim containers, networks, and volumes.",
		"status":         "Usage: ai-shim manage status\n\n  Show running ai-shim containers.",
		"backup":         "Usage: ai-shim manage backup <profile> [output-path]\n\n  Create a tar.gz backup of a profile's home directory.",
		"restore":        "Usage: ai-shim manage restore <profile> <archive-path>\n\n  Restore a profile from a tar.gz backup.",
		"disk-usage":     "Usage: ai-shim manage disk-usage\n\n  Show storage usage breakdown by category and profile.",
		"agent-versions": "Usage: ai-shim manage agent-versions\n\n  Show installed agent versions by checking bin directories.",
		"reinstall":      "Usage: ai-shim manage reinstall <agent>\n\n  Force reinstall an agent by clearing its bin cache.",
		"exec":           "Usage: ai-shim manage exec <name> <command...>\n\n  Execute a command in a running ai-shim container.",
		"attach":         "Usage: ai-shim manage attach <agent> [profile]\n\n  Reattach to a detached ai-shim session.\n  Detach from a running session with Ctrl+], d.",
		"stop":           "Usage: ai-shim manage stop <agent> [profile]\n\n  Stop a running ai-shim session and remove its container.",
		"watch":          "Usage: ai-shim manage watch <agent> [profile]\n\n  Launch an agent and restart it on crash.\n  Set AI_SHIM_WATCH_RETRIES to control max restarts (default 3).",
		"logs":           "Usage: ai-shim manage logs [agent] [profile]\n\n  Without arguments: show the launch/exit log.\n  With agent [profile]: show Docker container logs for the most recent matching container.",
		"switch-profile": "Usage: ai-shim manage switch-profile <profile>\n\n  Set the default profile used when no profile is specified.",
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
				return fmt.Errorf("listing agents (JSON): %w", err)
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
				return fmt.Errorf("listing profiles (JSON): %w", err)
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.ListProfiles(layout)
		if err != nil {
			return fmt.Errorf("listing profiles: %w", err)
		}
		fmt.Print(output)
		return nil

	case "config":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: ai-shim manage config <agent> [profile]")
			fmt.Fprintln(os.Stderr, "")
			fmt.Fprintln(os.Stderr, "Run 'ai-shim manage agents' to see available agents.")
			fmt.Fprintln(os.Stderr, "Run 'ai-shim manage profiles' to see available profiles.")
			return fmt.Errorf("missing required argument: agent")
		}
		agentName := args[1]
		profile := "default"
		if len(args) >= 3 {
			profile = args[2]
		}
		if jsonMode {
			output, err := cli.ShowConfigJSON(layout, agentName, profile)
			if err != nil {
				return fmt.Errorf("showing config (JSON) for %s/%s: %w", agentName, profile, err)
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.ShowConfig(layout, agentName, profile)
		if err != nil {
			return fmt.Errorf("showing config for %s/%s: %w", agentName, profile, err)
		}
		fmt.Print(output)
		return nil

	case "doctor":
		if jsonMode {
			output, err := cli.DoctorJSON()
			if err != nil {
				return fmt.Errorf("running doctor (JSON): %w", err)
			}
			fmt.Print(output)
			return nil
		}
		fmt.Print(cli.Doctor())
		return nil

	case "logs":
		// manage logs — show persistent log
		// manage logs <agent> [profile] — show Docker container logs
		if len(args) >= 2 {
			agent := args[1]
			profile := ""
			if len(args) >= 3 {
				profile = args[2]
			}
			output, err := cli.ContainerLogs(agent, profile, 100)
			if err != nil {
				// Fall back to persistent log filtered by agent
				output, err = cli.ShowLogs(layout, agent, profile, 50)
				if err != nil {
					return fmt.Errorf("showing logs for %s/%s: %w", agent, profile, err)
				}
			}
			fmt.Print(output)
		} else {
			output, err := cli.ShowLogs(layout, "", "", 50)
			if err != nil {
				return fmt.Errorf("showing persistent logs: %w", err)
			}
			fmt.Print(output)
		}
		return nil

	case "symlinks":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage symlinks <list|create|remove> [args...]")
		}
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("determining executable path: %w", err)
		}
		switch args[1] {
		case "list":
			explicit := ""
			if len(args) > 2 {
				explicit = args[2]
			}
			dir, err := cli.ResolveSymlinkDir(layout.ConfigDir, explicit)
			if err != nil {
				return fmt.Errorf("resolving symlink directory: %w", err)
			}
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				fmt.Printf("Symlink directory %s does not exist yet. "+
					"Run `ai-shim manage symlinks create <agent>` to create it.\n", dir)
				return nil
			}
			links, err := cli.ListSymlinks(dir, exe)
			if err != nil {
				return fmt.Errorf("listing symlinks in %s: %w", dir, err)
			}
			if len(links) == 0 {
				fmt.Printf("No ai-shim symlinks found in %s.\n", dir)
			} else {
				fmt.Printf("Symlinks in %s:\n", dir)
				for _, l := range links {
					fmt.Println("  " + l)
				}
			}
			return nil
		case "create":
			if len(args) < 3 {
				return fmt.Errorf("usage: ai-shim manage symlinks create <agent> [profile] [dir]\n  agent/profile names: ASCII letters/digits and '._-' only, must start with letter or digit, max 63 chars\n  dir defaults to symlink_dir in default.yaml or ~/.local/bin")
			}
			agentName := args[2]
			profile := "default"
			explicit := ""
			if len(args) > 3 {
				profile = args[3]
			}
			if len(args) > 4 {
				explicit = args[4]
			}
			dir, err := cli.ResolveSymlinkDir(layout.ConfigDir, explicit)
			if err != nil {
				return fmt.Errorf("resolving symlink directory: %w", err)
			}
			// Create the target directory if it doesn't exist yet —
			// friendly on fresh machines where ~/.local/bin has never
			// been touched.
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating symlink directory %s: %w", dir, err)
			}
			path, err := cli.CreateSymlink(agentName, profile, dir, exe)
			if err != nil {
				return fmt.Errorf("creating symlink for %s/%s in %s: %w", agentName, profile, dir, err)
			}
			fmt.Printf("Created symlink: %s\n", path)
			// Friendly nudge when the target dir isn't on $PATH.
			if !dirOnPath(dir) {
				fmt.Fprintf(os.Stderr,
					"ai-shim: note: %s is not on your $PATH. "+
						"Add it (e.g. to ~/.bashrc or ~/.zshrc) so `%s` is directly invocable.\n",
					dir, filepath.Base(path))
			}
			return nil
		case "remove":
			if len(args) < 3 {
				return fmt.Errorf("usage: ai-shim manage symlinks remove <path>")
			}
			if err := cli.RemoveSymlink(args[2]); err != nil {
				return fmt.Errorf("removing symlink %s: %w", args[2], err)
			}
			return nil
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
			return fmt.Errorf("dry-run for %s/%s: %w", args[1], args[2], err)
		}
		fmt.Print(output)
		return nil

	case "status":
		if jsonMode {
			output, err := cli.StatusJSON()
			if err != nil {
				return fmt.Errorf("getting status (JSON): %w", err)
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.Status()
		if err != nil {
			return fmt.Errorf("getting status: %w", err)
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
			return fmt.Errorf("backing up profile %s: %w", args[1], err)
		}
		fmt.Printf("Profile %s backed up successfully\n", args[1])
		return nil

	case "restore":
		if len(args) < 3 {
			return fmt.Errorf("usage: ai-shim manage restore <profile> <archive-path>")
		}
		if err := cli.RestoreProfile(layout, args[1], args[2]); err != nil {
			return fmt.Errorf("restoring profile %s from %s: %w", args[1], args[2], err)
		}
		fmt.Printf("Profile %s restored successfully\n", args[1])
		return nil

	case "disk-usage":
		if jsonMode {
			output, err := cli.DiskUsageJSON(layout)
			if err != nil {
				return fmt.Errorf("computing disk usage (JSON): %w", err)
			}
			fmt.Print(output)
			return nil
		}
		output, err := cli.DiskUsage(layout)
		if err != nil {
			return fmt.Errorf("computing disk usage: %w", err)
		}
		fmt.Print(output)
		return nil

	case "cleanup":
		result, err := cli.Cleanup()
		if err != nil {
			return fmt.Errorf("cleaning up orphaned resources: %w", err)
		}
		totalRemoved := len(result.RemovedContainers) + len(result.RemovedNetworks) + len(result.RemovedVolumes)
		if totalRemoved == 0 && len(result.Failed) == 0 && len(result.Errors) == 0 {
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
			if len(result.Errors) > 0 {
				fmt.Printf("Encountered %d error(s) during cleanup:\n", len(result.Errors))
				for _, e := range result.Errors {
					fmt.Println("  " + e)
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
			return fmt.Errorf("usage: ai-shim manage reinstall <agent>")
		}
		agentName := args[1]
		if err := cli.Reinstall(layout, agentName); err != nil {
			return fmt.Errorf("reinstalling agent %s: %w", agentName, err)
		}
		fmt.Printf("Agent %s bin cache cleared. It will be reinstalled on next launch.\n", agentName)
		return nil

	case "exec":
		if len(args) < 3 {
			return fmt.Errorf("usage: ai-shim manage exec <name> <command...>")
		}
		exitCode, err := cli.Exec(args[1], args[2:])
		if err != nil {
			return fmt.Errorf("exec into %s: %w", args[1], err)
		}
		os.Exit(exitCode)
		return nil

	case "attach":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage attach <agent> [profile]")
		}
		agentName := args[1]
		profile := "default"
		if len(args) > 2 {
			profile = args[2]
		}
		exitCode, err := manageAttach(agentName, profile)
		if err != nil {
			return fmt.Errorf("attach to %s/%s: %w", agentName, profile, err)
		}
		os.Exit(exitCode)
		return nil

	case "stop":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage stop <agent> [profile]")
		}
		agentName := args[1]
		profile := "default"
		if len(args) > 2 {
			profile = args[2]
		}
		return manageStop(agentName, profile)

	case "watch":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage watch <agent> [profile]")
		}
		agentName := args[1]
		profile := "default"
		if len(args) > 2 {
			profile = args[2]
		}

		// Use current profile as fallback if profile not specified
		if profile == "default" {
			if current := cli.CurrentProfile(layout); current != "default" {
				profile = current
			}
		}

		invocationName := agentName
		if profile != "default" {
			invocationName = agentName + "_" + profile
		}

		maxRetries := cli.WatchRetries()
		exitCode, err := cli.WatchLoop(maxRetries, func() (int, error) {
			return runAgent(invocationName, nil)
		}, func(d time.Duration) {
			time.Sleep(d)
		})
		if err != nil {
			return fmt.Errorf("watch loop for %s: %w", invocationName, err)
		}
		os.Exit(exitCode)
		return nil

	case "switch-profile":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage switch-profile <profile>")
		}
		if err := cli.SwitchProfile(layout, args[1]); err != nil {
			return fmt.Errorf("switching to profile %s: %w", args[1], err)
		}
		fmt.Printf("Default profile set to: %s\n", args[1])
		return nil

	default:
		return fmt.Errorf("unknown manage subcommand: %s\nAvailable: agents, profiles, config, doctor, symlinks, dry-run, status, backup, restore, disk-usage, cleanup, agent-versions, reinstall, exec, watch, switch-profile", args[0])
	}
}

func runAgent(name string, args []string) (int, error) {
	// 1. Parse symlink name → agent + profile
	agentName, profileName, err := invocation.ParseName(name)
	if err != nil {
		return 1, fmt.Errorf("parsing invocation name: %w", err)
	}

	// 1.5. If profile is "default" (no profile in symlink name), check for
	// a current-profile marker file as a fallback.
	if profileName == "default" {
		fallbackLayout := storage.NewLayout(storage.DefaultRoot())
		if current := cli.CurrentProfile(fallbackLayout); current != "default" {
			profileName = current
		}
	}

	// Re-validate profile after the marker-file fallback so a hand-edited
	// .current-profile cannot smuggle invalid characters into container or
	// path construction downstream.
	if err := invocation.ValidateProfileName(profileName); err != nil {
		return 1, fmt.Errorf("invalid current profile: %w", err)
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
	if errs := cfg.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "ai-shim: config error: %s\n", e)
		}
		return 1, fmt.Errorf("invalid config: %d error(s)", len(errs))
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
			fmt.Fprintf(os.Stderr, "ai-shim: invalid volume: %v\n", e)
		}
		return 1, fmt.Errorf("invalid volume config: %d error(s)", len(errs))
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
	defer func() { _ = runner.Close() }()

	// 6.1 Best-effort cleanup of stopped containers from previous aborted runs
	// for this same agent+profile. Aborted runs (SIGKILL, OOM, host reboot)
	// can leave containers in "exited" or "dead" state which will collide
	// with the new container's name. Ignore errors — this is housekeeping.
	cleanupStaleContainers(ctx, runner, agentName, profileName)

	// 6.2 Check for existing running session (reattach support)
	if container.IsTTY() {
		wsHash := workspace.HashPath(platInfo.Hostname, pwd)
		session, lookupErr := container.FindRunningSession(ctx, runner.Client(), agentName, profileName, wsHash)
		if lookupErr != nil {
			logging.Debug("session lookup failed: %v", lookupErr)
		}
		if session != nil {
			action := promptReattach(session)
			switch action {
			case "reattach":
				return handleReattach(ctx, runner, session, cfg, filepath.Join(layout.Root, "logs"))
			case "new":
				stopSession(ctx, runner.Client(), session)
				// fall through to create new container
			case "exit":
				return 0, nil
			}
		}
	}

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

	// detached tracks whether the user triggered a detach so DIND cleanup
	// can be skipped (preserving the sidecar and network for reattach).
	var detached bool

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
		defer func() {
			if detached {
				return // preserve network for reattach
			}
			if err := netHandle.Remove(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove network: %v\n", err)
			}
		}()

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
			addr, err := dind.EnsureCache(ctx, runner, cacheDir)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: registry cache unavailable, image pulls will be slower: %v\n", err)
				fmt.Fprintf(os.Stderr, "ai-shim: hint: run 'ai-shim manage cleanup' if stale cache container exists\n")
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
			// Chgrp the DIND docker.sock to the agent's GID so the
			// non-root agent container can access the socket. Without
			// this the socket ends up root:2375 mode 660 and any docker
			// CLI call from inside the agent fails with "permission
			// denied" (docker:dind's "docker" group has GID 2375).
			SocketGID: platInfo.GID,
		})
		if err != nil {
			return 1, fmt.Errorf("starting DIND sidecar: %w", err)
		}
		defer func() {
			if detached {
				return // preserve DIND sidecar for reattach
			}
			if err := sidecar.Stop(ctx); err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to stop DIND sidecar: %v\n", err)
			}
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
	logging.LogLaunch(logDir, agentName, profileName, spec.Name, image)
	result, err := runner.Run(ctx, spec)
	detached = result.Detached
	logging.LogExit(logDir, spec.Name, result.ExitCode)
	if err != nil {
		return 1, fmt.Errorf("running container: %w", err)
	}

	if result.Detached {
		fmt.Fprintf(os.Stderr, "\nai-shim: detached from %s. Reattach by running the same command.\n", spec.Name)
		return 0, nil
	}

	return result.ExitCode, nil
}

// promptReattach asks the user what to do with an existing running session.
// Returns "reattach", "new", or "exit".
func promptReattach(session *container.RunningSession) string {
	age := time.Since(session.CreatedAt).Truncate(time.Second)
	dir := session.WorkspaceDir
	if dir == "" {
		dir = "(unknown)"
	}
	fmt.Fprintf(os.Stderr, "ai-shim: running session found for %s/%s\n", session.AgentName, session.Profile)
	fmt.Fprintf(os.Stderr, "  container: %s (running %s)\n", session.ContainerName, age)
	fmt.Fprintf(os.Stderr, "  workspace: %s\n", dir)
	fmt.Fprintf(os.Stderr, "\n  [Y] Reattach  [n] Exit  [new] Stop old and start fresh\n")
	fmt.Fprintf(os.Stderr, "  Choice [Y/n/new]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "exit"
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	switch input {
	case "", "y", "yes":
		return "reattach"
	case "n", "no":
		return "exit"
	case "new":
		return "new"
	default:
		fmt.Fprintf(os.Stderr, "ai-shim: unrecognized choice %q, exiting\n", input)
		return "exit"
	}
}

// handleReattach reconnects to an existing container session.
func handleReattach(ctx context.Context, runner *container.Runner, session *container.RunningSession, cfg config.Config, logDir string) (int, error) {
	fmt.Fprintf(os.Stderr, "ai-shim: reattaching to %s...\n", session.ContainerName)

	// Show recent container logs for context.
	showRecentLogs(ctx, runner.Client(), session.ContainerID)

	result, err := runner.Reattach(ctx, session.ContainerID, true)
	if err != nil {
		return 1, fmt.Errorf("reattaching to container: %w", err)
	}

	if result.Detached {
		fmt.Fprintf(os.Stderr, "\nai-shim: detached from %s. Reattach by running the same command.\n", session.ContainerName)
		return 0, nil
	}

	// Container exited while we were attached — clean up.
	_ = runner.Client().ContainerRemove(ctx, session.ContainerID, container_types.RemoveOptions{Force: true})
	logging.LogExit(logDir, session.ContainerName, result.ExitCode)

	// Clean up DIND sidecar if present.
	if cfg.IsDINDEnabled() {
		stopDINDForSession(ctx, runner.Client(), session)
	}

	return result.ExitCode, nil
}

// showRecentLogs prints the last few lines of container output for context
// when reattaching. Errors are silently ignored.
func showRecentLogs(ctx context.Context, cli *client.Client, containerID string) {
	tail := "10"
	reader, err := cli.ContainerLogs(ctx, containerID, container_types.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		return
	}
	defer func() { _ = reader.Close() }()

	fmt.Fprintf(os.Stderr, "--- last %s lines ---\n", tail)
	_, _ = io.Copy(os.Stderr, reader)
	fmt.Fprintf(os.Stderr, "--- reattached ---\n\n")
}

// stopSession stops a running container and its associated DIND sidecar.
func stopSession(ctx context.Context, cli *client.Client, session *container.RunningSession) {
	stopTimeout := 5
	_ = cli.ContainerStop(ctx, session.ContainerID, container_types.StopOptions{
		Timeout: &stopTimeout,
	})
	_ = cli.ContainerRemove(ctx, session.ContainerID, container_types.RemoveOptions{Force: true})
	stopDINDForSession(ctx, cli, session)
}

// stopDINDForSession finds and stops the DIND sidecar associated with a session,
// including removing its socket and certs volumes to avoid leaking Docker volumes.
func stopDINDForSession(ctx context.Context, cli *client.Client, session *container.RunningSession) {
	// Find DIND sidecar by labels
	f := filters.NewArgs(
		filters.Arg("label", container.LabelBase+"=true"),
		filters.Arg("label", container.LabelDIND+"=true"),
		filters.Arg("label", container.LabelAgent+"="+session.AgentName),
		filters.Arg("label", container.LabelProfile+"="+session.Profile),
		filters.Arg("status", "running"),
	)
	list, err := cli.ContainerList(ctx, container_types.ListOptions{Filters: f})
	if err != nil {
		return
	}
	for _, c := range list {
		// Derive volume names from the container name before removing the container.
		// Container names in the Docker API have a leading "/" that must be stripped.
		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}

		stopTimeout := 5
		_ = cli.ContainerStop(ctx, c.ID, container_types.StopOptions{Timeout: &stopTimeout})
		_ = cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true})

		// Remove associated volumes. Errors are silently ignored — if the volumes
		// are already gone (e.g. removed by a previous cleanup pass) that is fine.
		if containerName != "" {
			_ = cli.VolumeRemove(ctx, containerName+"-socket", true)
			_ = cli.VolumeRemove(ctx, containerName+"-certs", true)
		}
	}
}

// manageAttach implements `ai-shim manage attach <agent> [profile]`.
func manageAttach(agentName, profile string) (int, error) {
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return 1, fmt.Errorf("creating container runner: %w", err)
	}
	defer func() { _ = runner.Close() }()

	sessions, err := container.FindAllRunningSessions(ctx, runner.Client(), agentName, profile)
	if err != nil {
		return 1, fmt.Errorf("finding sessions: %w", err)
	}
	if len(sessions) == 0 {
		return 1, fmt.Errorf("no running session found for %s/%s", agentName, profile)
	}

	// If multiple sessions (different workspaces), let user choose.
	session := &sessions[0]
	if len(sessions) > 1 {
		fmt.Fprintf(os.Stderr, "ai-shim: multiple running sessions for %s/%s:\n", agentName, profile)
		for i, s := range sessions {
			dir := s.WorkspaceDir
			if dir == "" {
				dir = "(unknown)"
			}
			age := time.Since(s.CreatedAt).Truncate(time.Second)
			fmt.Fprintf(os.Stderr, "  [%d] %s (running %s, workspace: %s)\n", i+1, s.ContainerName, age, dir)
		}
		fmt.Fprintf(os.Stderr, "  Choice [1]: ")

		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if input != "" {
				idx := 0
				if _, err := fmt.Sscanf(input, "%d", &idx); err == nil && idx >= 1 && idx <= len(sessions) {
					session = &sessions[idx-1]
				}
			}
		}
	}

	fmt.Fprintf(os.Stderr, "ai-shim: reattaching to %s...\n", session.ContainerName)
	showRecentLogs(ctx, runner.Client(), session.ContainerID)

	result, err := runner.Reattach(ctx, session.ContainerID, true)
	if err != nil {
		return 1, fmt.Errorf("reattaching: %w", err)
	}

	if result.Detached {
		fmt.Fprintf(os.Stderr, "\nai-shim: detached from %s.\n", session.ContainerName)
		return 0, nil
	}

	// Container exited — clean up.
	_ = runner.Client().ContainerRemove(ctx, session.ContainerID, container_types.RemoveOptions{Force: true})
	return result.ExitCode, nil
}

// manageStop implements `ai-shim manage stop <agent> [profile]`.
func manageStop(agentName, profile string) error {
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return fmt.Errorf("creating container runner: %w", err)
	}
	defer func() { _ = runner.Close() }()

	sessions, err := container.FindAllRunningSessions(ctx, runner.Client(), agentName, profile)
	if err != nil {
		return fmt.Errorf("finding sessions: %w", err)
	}
	if len(sessions) == 0 {
		return fmt.Errorf("no running session found for %s/%s", agentName, profile)
	}

	for _, s := range sessions {
		stopSession(ctx, runner.Client(), &s)
		fmt.Fprintf(os.Stderr, "ai-shim: stopped %s\n", s.ContainerName)
	}
	return nil
}
