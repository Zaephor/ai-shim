package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/Zaephor/ai-shim/internal/agent"
	"github.com/Zaephor/ai-shim/internal/cli"
	"github.com/Zaephor/ai-shim/internal/config"
	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/invocation"
	"github.com/Zaephor/ai-shim/internal/logging"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/platform"
	"github.com/Zaephor/ai-shim/internal/security"
	"github.com/Zaephor/ai-shim/internal/selfupdate"
	"github.com/Zaephor/ai-shim/internal/storage"
	"github.com/Zaephor/ai-shim/internal/workspace"
	container_types "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
)

// staleContainerFilters builds the filter for cleanupStaleContainers. It is
// scoped by agent+profile+workspace so that parallel sessions running out
// of other workspaces (same agent+profile, different wsHash) are not reaped
// when this workspace starts up.
func staleContainerFilters(agentName, profileName, wsHash, status string) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", container.LabelBase+"=true"),
		filters.Arg("label", container.LabelAgent+"="+agentName),
		filters.Arg("label", container.LabelProfile+"="+profileName),
		filters.Arg("label", container.LabelWorkspace+"="+wsHash),
		filters.Arg("status", status),
	)
}

// cleanupStaleContainers removes any non-running ai-shim containers for the
// given agent+profile+workspace. This handles the SIGKILL/OOM/reboot orphan
// case where AutoRemove never fired and a stale container blocks the next
// launch. For persistent containers that exited while detached, reports
// their exit code. All errors are silently ignored — this is best-effort
// housekeeping. The workspace filter is load-bearing: without it, a launch
// in one workspace can reap a sibling workspace's stopped-but-not-yet-
// reattached container.
func cleanupStaleContainers(ctx context.Context, runner *container.Runner, agentName, profileName, wsHash string) {
	cli := runner.Client()
	if cli == nil {
		return
	}
	for _, status := range []string{"exited", "dead", "created"} {
		list, err := cli.ContainerList(ctx, container_types.ListOptions{
			All:     true,
			Filters: staleContainerFilters(agentName, profileName, wsHash, status),
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

// buildDINDSharedMounts assembles the list of bind mounts that must be
// propagated from the host into the DIND sidecar at identical paths. The
// agent container invokes `docker run -v <host>:<target>` against the
// DIND daemon, and Docker resolves <host> in DIND's own filesystem —
// so any host path the agent will hand to DIND must also exist at that
// same path inside DIND.
//
// Two payloads are propagated:
//   - Workspace (pwd → containerWorkdir): so docker-against-DIND against
//     files in the repo resolves correctly.
//   - Tool caches (same-path): every tool with data_dir=true gets its
//     host ToolCachePath bound at an identical path inside DIND. Without
//     this, an install script that calls `docker -v $TOOL_CACHE_DIR:/x`
//     from the agent hits an empty overlay in DIND and silently drops
//     the tool's persisted state (same bug class as commit 78c975b).
//
// ToolsOrder is consulted when non-empty so mount order is deterministic;
// otherwise the tools map is iterated directly.
func buildDINDSharedMounts(pwd, workdir string, tools map[string]config.ToolDef, toolsOrder []string, layout storage.Layout, agentName, profileName string) ([]mount.Mount, error) {
	mounts := []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: pwd,
			Target: workdir,
		},
	}
	// Walk tools in a deterministic order when possible so the mount
	// slice has a stable shape (matters for tests and for diffability).
	order := toolsOrder
	if len(order) == 0 {
		for name := range tools {
			order = append(order, name)
		}
	}
	seen := make(map[string]bool, len(tools))
	for _, name := range order {
		if seen[name] {
			continue
		}
		seen[name] = true
		td, ok := tools[name]
		if !ok || !td.DataDir {
			continue
		}
		hostPath, err := storage.ToolCachePath(layout, name, td.CacheScope, agentName, profileName)
		if err != nil {
			return nil, fmt.Errorf("resolving DIND tool cache path for %q: %w", name, err)
		}
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: hostPath,
			Target: hostPath,
		})
	}
	return mounts, nil
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

const shortRevisionLen = 12

func init() {
	// When built without -ldflags (plain `go build`), version stays "dev".
	// Enrich it with the VCS revision that Go embeds automatically in
	// binaries built from a VCS-tracked module (Go 1.18+). This makes
	// `ai-shim version` useful for identifying dev builds without
	// requiring `make build`.
	if version != "dev" {
		return
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return
	}
	short := revision
	if len(short) > shortRevisionLen {
		short = short[:shortRevisionLen]
	}
	version = "dev-" + short
	if modified == "true" {
		version += "-dirty"
	}
}

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
  manage warm <agent> [profile]  Pre-warm image and caches for an agent
  manage exec <name> <cmd...>   Execute command in running container
  manage attach <container-name>  Reattach to a running container by name
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
  AI_SHIM_DIND_KVM      Enable/disable KVM passthrough for DIND (0/1)
  AI_SHIM_KVM           Enable/disable KVM passthrough for agent (0/1)
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
  attach          Reattach to a running container by name
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
  delete-profile  Remove a profile and its associated data
  warm            Pre-warm image and caches for an agent
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
		"agents":         "Usage: ai-shim manage agents\n\n  List all built-in and configured agents.\n  Custom agents defined via agent_def: in ~/.ai-shim/config/agents/*.yaml\n  are included alongside built-ins.",
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
		"attach":         "Usage: ai-shim manage attach <container-name>\n\n  Reattach to a running ai-shim container by its exact name.\n  Use 'ai-shim manage status' to see container names.\n  Detach from a running session with Ctrl+], d.",
		"stop":           "Usage: ai-shim manage stop <agent> [profile]\n\n  Stop a running ai-shim session and remove its container.",
		"watch":          "Usage: ai-shim manage watch <agent> [profile]\n\n  Launch an agent and restart it on crash.\n  Set AI_SHIM_WATCH_RETRIES to control max restarts (default 3).",
		"logs":           "Usage: ai-shim manage logs [agent] [profile]\n\n  Without arguments: show the launch/exit log.\n  With agent [profile]: show Docker container logs for the most recent matching container.",
		"switch-profile": "Usage: ai-shim manage switch-profile <profile>\n\n  Set the default profile used when no profile is specified.",
		"delete-profile": "Usage: ai-shim manage delete-profile <profile>\n\n  Remove a profile's runtime data (home directory, caches) and associated\n  agent-profile configs. The profile's own config YAML is retained.\n  Refuses to proceed if any sessions are running for the profile.",
		"warm":           "Usage: ai-shim manage warm <agent> [profile]\n\n  Pre-warm the container image and agent install caches.\n  Pulls the image, provisions tools, and installs the agent without\n  launching it. Subsequent launches skip all first-run setup.",
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
			// Try Docker container logs first (if container still running),
			// then persisted output log (captured on exit), then
			// launch/exit log as last resort.
			output, err := cli.ContainerLogs(agent, profile, 100)
			if err != nil {
				output, err = cli.ShowOutputLog(layout, agent, profile)
			}
			if err != nil {
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
			return fmt.Errorf("usage: ai-shim manage attach <container-name>")
		}
		exitCode, err := manageAttachByName(args[1])
		if err != nil {
			return fmt.Errorf("attach to %s: %w", args[1], err)
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

	case "warm":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage warm <agent> [profile]")
		}
		agentName := args[1]
		profile := "default"
		if len(args) > 2 {
			profile = args[2]
		}
		return manageWarm(layout, agentName, profile)

	case "delete-profile":
		if len(args) < 2 {
			return fmt.Errorf("usage: ai-shim manage delete-profile <profile>")
		}
		profileName := args[1]

		// Safety: refuse if any sessions are running for this profile.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		runner, err := container.NewRunner(ctx)
		if err != nil {
			return fmt.Errorf("connecting to Docker: %w", err)
		}
		defer func() { _ = runner.Close() }()

		// Check all agents for running sessions using this profile.
		var runningSessions []container.RunningSession
		for _, agentName := range agent.Names() {
			sessions, findErr := container.FindAllRunningSessions(ctx, runner.Client(), agentName, profileName)
			if findErr != nil {
				continue // best-effort; Docker may not have any matching containers
			}
			runningSessions = append(runningSessions, sessions...)
		}
		// Also check custom agents that may not be in the built-in list.
		if customDefs := agent.LoadCustomAgents(layout.ConfigDir); customDefs != nil {
			agent.SetCustomAgents(customDefs)
			for _, agentName := range agent.Names() {
				sessions, findErr := container.FindAllRunningSessions(ctx, runner.Client(), agentName, profileName)
				if findErr != nil {
					continue
				}
				runningSessions = append(runningSessions, sessions...)
			}
		}

		if len(runningSessions) > 0 {
			fmt.Fprintf(os.Stderr, "Cannot delete profile %q: %d running session(s):\n", profileName, len(runningSessions))
			for _, s := range runningSessions {
				fmt.Fprintf(os.Stderr, "  %s (agent=%s, workspace=%s)\n", s.ContainerName, s.AgentName, s.WorkspaceDir)
			}
			return fmt.Errorf("stop running sessions first")
		}

		// Print what will be removed.
		profileDir := filepath.Join(layout.Root, "profiles", profileName)
		fmt.Fprintf(os.Stderr, "Removing profile data: %s\n", profileDir)

		result, err := cli.DeleteProfile(layout, profileName)
		if err != nil {
			return fmt.Errorf("deleting profile %s: %w", profileName, err)
		}

		// Print summary.
		fmt.Fprintf(os.Stderr, "Profile home removed (%s freed)\n", cli.FormatBytes(result.BytesFreed))
		if len(result.AgentProfilesRemoved) > 0 {
			fmt.Fprintf(os.Stderr, "Agent-profile configs removed:\n")
			for _, name := range result.AgentProfilesRemoved {
				fmt.Fprintf(os.Stderr, "  %s\n", name)
			}
		}
		if result.ConfigRetainedPath != "" {
			fmt.Fprintf(os.Stderr, "Profile config retained at: %s\n", result.ConfigRetainedPath)
		}
		return nil

	default:
		return fmt.Errorf("unknown manage subcommand: %s\nAvailable: agents, profiles, config, doctor, symlinks, dry-run, status, backup, restore, disk-usage, cleanup, agent-versions, reinstall, exec, attach, watch, switch-profile, delete-profile, warm", args[0])
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
	// for this same agent+profile+workspace. Aborted runs (SIGKILL, OOM,
	// host reboot) can leave containers in "exited" or "dead" state which
	// will collide with the new container's name. Scope by workspace hash
	// so cleanups from one workspace do not reap sibling containers
	// running in another workspace for the same agent+profile. Ignore
	// errors — this is housekeeping.
	wsHash := workspace.HashPath(platInfo.Hostname, pwd)
	cleanupStaleContainers(ctx, runner, agentName, profileName, wsHash)

	// 6.2 Check for existing running session(s) (reattach support)
	if container.IsTTY() {
		sessions, lookupErr := container.FindRunningSessionsInWorkspace(ctx, runner.Client(), agentName, profileName, wsHash)
		if lookupErr != nil {
			logging.Debug("session lookup failed: %v", lookupErr)
		}
		// Loop so that "kill <N>" can prune one sibling and re-prompt
		// against the survivors without bouncing the user back out to
		// the shell. Any other action exits the loop.
		for len(sessions) > 0 {
			action, idx := promptReattach(sessions)
			switch action {
			case "reattach":
				return handleReattach(ctx, runner, &sessions[idx], cfg, filepath.Join(layout.Root, "logs"))
			case "new":
				// Stop every existing session for this scope before
				// starting fresh; "new" means "clean slate", not "stop
				// only one arbitrary sibling".
				for i := range sessions {
					stopSession(ctx, runner.Client(), &sessions[i])
				}
				sessions = nil // fall through to create new container
			case "parallel":
				// Leave existing sessions running; fall through to create
				// an additional container. generateContainerName appends
				// a random suffix so the name doesn't collide; labels
				// stay identical so all siblings remain discoverable.
				fmt.Fprintf(os.Stderr, "ai-shim: starting parallel session alongside %d existing\n", len(sessions))
				sessions = nil
			case "kill":
				stopSession(ctx, runner.Client(), &sessions[idx])
				fmt.Fprintf(os.Stderr, "ai-shim: stopped session %s\n", sessions[idx].ContainerName)
				sessions = append(sessions[:idx], sessions[idx+1:]...)
				// loop: re-prompt with one less entry. When the last
				// session is killed the loop exits naturally and falls
				// through to container creation.
				continue
			case "exit":
				return 0, nil
			}
			break
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
		fmt.Fprintf(os.Stderr, "ai-shim: warning: could not inspect image user, defaulting to /home/user: %v\n", err)
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

	spec, err := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  profileName,
		Layout:   layout,
		Platform: platInfo,
		Args:     args,
		HomeDir:  imageUser.HomeDir,
		LogDir:   logDir,
		// Pin pwd at this layer so BuildSpec does not re-read
		// os.Getwd() and risk divergence in nested scenarios.
		Pwd:     pwd,
		Version: version,
	})
	if err != nil {
		return 1, fmt.Errorf("building container spec: %w", err)
	}

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
		dindKVM := cfg.IsDINDKVMEnabled()

		useSysbox := dind.DetectSysbox(ctx, runner.Client())

		dindHostname := "ai-shim-dind"
		if cfg.DINDHostname != "" {
			dindHostname = cfg.DINDHostname
		}

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
			addr, err := dind.EnsureCache(ctx, runner, cacheDir, version)
			if err != nil {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: registry cache unavailable, image pulls will be slower: %v\n", err)
				fmt.Fprintf(os.Stderr, "ai-shim: hint: run 'ai-shim manage cleanup' if stale cache container exists\n")
			} else {
				cacheAddr = addr
			}
		}

		// Start DIND sidecar on same network (Start pulls the image internally)
		dindName := spec.Name + "-dind"
		var dindResources *dind.ResourceLimits
		if cfg.DINDResources != nil {
			dindResources = &dind.ResourceLimits{
				Memory: cfg.DINDResources.Memory,
				CPUs:   cfg.DINDResources.CPUs,
			}
		}
		// Shared mounts between agent and DIND sidecar. When the agent
		// invokes `docker run -v <path>:<target>` against the DIND
		// daemon, Docker resolves <path> in DIND's own filesystem. If
		// those paths are not bind-mounted into DIND at the same path
		// they exist in the agent, the bind source either doesn't exist
		// or resolves to an empty overlay. Propagate the workspace, the
		// pull-through registry cache directory, and every tool cache
		// (for tools with data_dir:true) at identical paths.
		dindSharedMounts, err := buildDINDSharedMounts(
			pwd,
			workspace.ContainerWorkdir(platInfo.Hostname, pwd),
			cfg.Tools,
			cfg.ToolsOrder,
			layout,
			agentName,
			profileName,
		)
		if err != nil {
			return 1, fmt.Errorf("building DIND shared mounts: %w", err)
		}

		sidecar, err := dind.Start(ctx, runner, dind.Config{
			GPU:           dindGPU,
			KVM:           dindKVM,
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
			SocketGID:    platInfo.GID,
			SharedMounts: dindSharedMounts,
			Version:      version,
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

// manageWarm pre-warms the container image and agent install caches for the
// given agent+profile. It resolves config, pulls the image, builds the
// container spec, replaces the agent exec with a no-op, runs the container
// to completion (so all provisioning and install caches populate), and
// removes it.
func manageWarm(layout storage.Layout, agentName, profileName string) error {
	// Load custom agent definitions
	if customDefs := agent.LoadCustomAgents(layout.ConfigDir); customDefs != nil {
		agent.SetCustomAgents(customDefs)
	}

	agentDef, ok := agent.Lookup(agentName)
	if !ok {
		return fmt.Errorf("unknown agent: %s\n\nAvailable agents:\n%s", agentName, formatAgentList())
	}

	platInfo := platform.Detect()

	if err := layout.EnsureDirectories(agentName, profileName); err != nil {
		return fmt.Errorf("setting up directories: %w", err)
	}

	cfg, err := config.Resolve(layout.ConfigDir, agentName, profileName)
	if err != nil {
		return fmt.Errorf("resolving config: %w", err)
	}
	if errs := cfg.Validate(); len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "ai-shim: config error: %s\n", e)
		}
		return fmt.Errorf("invalid config: %d error(s)", len(errs))
	}

	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting working directory: %w", err)
	}

	// Pre-create agent data dirs/files for correct ownership
	if err := layout.EnsureAgentData(profileName, agentDef.DataDirs, agentDef.DataFiles); err != nil {
		return fmt.Errorf("setting up agent data: %w", err)
	}
	for _, name := range cfg.AllowAgents {
		if allowed, ok := agent.Lookup(name); ok {
			if err := layout.EnsureAgentData(profileName, allowed.DataDirs, allowed.DataFiles); err != nil {
				return fmt.Errorf("setting up agent data for %s: %w", name, err)
			}
		}
	}

	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return fmt.Errorf("creating container runner: %w", err)
	}
	defer func() { _ = runner.Close() }()

	image := cfg.GetImage()
	if err := runner.EnsureImage(ctx, image); err != nil {
		return fmt.Errorf("preparing image: %w", err)
	}

	imageUser, err := runner.InspectImageUser(ctx, image)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: could not inspect image user, defaulting to /home/user: %v\n", err)
		imageUser = container.ImageUser{HomeDir: "/home/user", Username: "user"}
	}

	spec, err := container.BuildSpec(container.BuildParams{
		Config:   cfg,
		Agent:    agentDef,
		Profile:  profileName,
		Layout:   layout,
		Platform: platInfo,
		HomeDir:  imageUser.HomeDir,
		Pwd:      pwd,
		Version:  version,
	})
	if err != nil {
		return fmt.Errorf("building container spec: %w", err)
	}

	// Replace the agent exec with a no-op so provisioning runs but the
	// agent binary never launches.
	if err := container.WarmEntrypoint(&spec); err != nil {
		return fmt.Errorf("preparing warm entrypoint: %w", err)
	}

	fmt.Fprintf(os.Stderr, "ai-shim: warming %s/%s...\n", agentName, profileName)

	result, err := runner.Run(ctx, spec)
	if err != nil {
		return fmt.Errorf("running warm container: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("warm container exited with code %d", result.ExitCode)
	}

	fmt.Fprintf(os.Stderr, "ai-shim: warm complete for %s/%s\n", agentName, profileName)
	return nil
}

// promptReattach asks the user what to do with one or more existing running
// sessions for this agent+profile+workspace. Sessions must be provided
// most-recently-created first so index 1 maps to the most likely target.
// Returns (action, index):
//
//   - "reattach": connect to sessions[index]
//   - "new":      stop all listed sessions and start a fresh one
//   - "parallel": leave listed sessions running and start an additional one
//   - "exit":     do nothing and return to the shell
//
// index is only meaningful for "reattach" and is always 0 for the
// single-session case.
func promptReattach(sessions []container.RunningSession) (action string, index int) {
	if len(sessions) == 0 {
		return "exit", 0
	}
	if len(sessions) == 1 {
		return promptReattachSingle(&sessions[0]), 0
	}
	return promptReattachMulti(sessions)
}

func promptReattachSingle(session *container.RunningSession) string {
	age := time.Since(session.CreatedAt).Truncate(time.Second)
	dir := session.WorkspaceDir
	if dir == "" {
		dir = "(unknown)"
	}
	fmt.Fprintf(os.Stderr, "ai-shim: running session found for %s/%s\n", session.AgentName, session.Profile)
	fmt.Fprintf(os.Stderr, "  container: %s (running %s)\n", session.ContainerName, age)
	fmt.Fprintf(os.Stderr, "  workspace: %s\n", dir)
	fmt.Fprintf(os.Stderr, "\n  [Y] Reattach  [n] Exit  [new] Stop old, start fresh  [p] Start in parallel\n")
	fmt.Fprintf(os.Stderr, "  Choice [Y/n/new/p]: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "exit"
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	action := parseSingleChoice(input)
	if action == "exit" && input != "" && input != "n" && input != "no" {
		fmt.Fprintf(os.Stderr, "ai-shim: unrecognized choice %q, exiting\n", input)
	}
	return action
}

// parseSingleChoice is the pure parser behind promptReattachSingle. It maps a
// user-entered string to one of: "reattach", "exit", "new", "parallel". The
// input is expected to already be lowercased and trimmed by the caller; this
// function additionally trims/lowercases defensively so tests can exercise it
// with raw variants. Unknown input maps to "exit" — the stderr warning for
// unrecognized choices lives at the prompt layer so this parser stays
// side-effect-free.
func parseSingleChoice(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "", "y", "yes":
		return "reattach"
	case "n", "no":
		return "exit"
	case "new":
		return "new"
	case "p", "parallel":
		return "parallel"
	default:
		return "exit"
	}
}

func promptReattachMulti(sessions []container.RunningSession) (string, int) {
	first := sessions[0]
	dir := first.WorkspaceDir
	if dir == "" {
		dir = "(unknown)"
	}
	fmt.Fprintf(os.Stderr, "ai-shim: %d running sessions found for %s/%s\n",
		len(sessions), first.AgentName, first.Profile)
	fmt.Fprintf(os.Stderr, "  workspace: %s\n\n", dir)
	for i, s := range sessions {
		age := time.Since(s.CreatedAt).Truncate(time.Second)
		fmt.Fprintf(os.Stderr, "  [%d] %s  (running %s)\n", i+1, s.ContainerName, age)
	}
	fmt.Fprintf(os.Stderr, "\n  1-%d = reattach, [k<N>] kill session N, [n] Exit, [new] Stop ALL and start fresh, [p] Start another in parallel\n",
		len(sessions))
	fmt.Fprintf(os.Stderr, "  Choice: ")

	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return "exit", 0
	}
	input := strings.TrimSpace(strings.ToLower(scanner.Text()))
	action, idx := parseMultiChoice(input, len(sessions))
	if action == "exit" && input != "" && input != "n" && input != "no" {
		fmt.Fprintf(os.Stderr, "ai-shim: unrecognized choice %q, exiting\n", input)
	}
	return action, idx
}

// parseMultiChoice is the pure parser behind promptReattachMulti. Given a
// user-entered string and the number of running sessions, it returns one of:
//
//   - ("reattach", idx) for a numeric 1..n choice (idx is zero-based)
//   - ("kill", idx)     for "k<N>", "k <N>", or "kill <N>" with N in 1..n
//   - ("new", 0)        for "new"
//   - ("parallel", 0)   for "p" / "parallel"
//   - ("exit", 0)       for "", "n", "no", out-of-range numbers, or garbage
//
// The function is case-insensitive and whitespace-tolerant. Unknown input
// maps to "exit"; any stderr warning is the prompt layer's responsibility.
func parseMultiChoice(input string, n int) (string, int) {
	input = strings.TrimSpace(strings.ToLower(input))
	switch input {
	case "", "n", "no":
		return "exit", 0
	case "new":
		return "new", 0
	case "p", "parallel":
		return "parallel", 0
	}
	// Kill a specific session: accept "k<N>", "k <N>", or "kill <N>".
	// Example: "k2" stops session 2 without affecting the rest; the caller
	// loops and re-renders the picker with the survivors.
	if strings.HasPrefix(input, "k") {
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(input, "kill"), "k"))
		if idx, err := strconv.Atoi(rest); err == nil && idx >= 1 && idx <= n {
			return "kill", idx - 1
		}
		return "exit", 0
	}
	if idx, err := strconv.Atoi(input); err == nil && idx >= 1 && idx <= n {
		return "reattach", idx - 1
	}
	return "exit", 0
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
	if err := runner.Client().ContainerRemove(ctx, session.ContainerID, container_types.RemoveOptions{Force: true}); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove container %s: %v\n", session.ContainerName, err)
	}
	logging.LogExit(logDir, session.ContainerName, result.ExitCode)

	// Clean up DIND sidecar if present.
	if cfg.IsDINDEnabled() {
		stopDINDForSession(ctx, runner.Client(), session)
	}
	// Garbage-collect the shared registry cache if no other sessions are
	// using it. Without this, the cache container outlives every consumer
	// on natural exit from a reattached session. This cleanup path is
	// only reached when the container exited on its own — the detach
	// path returns earlier and deliberately leaves the cache in place.
	if cfg.IsCacheEnabled() {
		dind.MaybeStopCache(ctx, runner.Client())
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

// stopSession stops a running container and its associated DIND sidecar,
// then asks the registry cache to garbage-collect itself if no other cache
// consumers remain. Without the final MaybeStopCache call, killing the
// last consumer through the picker (k<N>) leaves the shared registry cache
// container running indefinitely. MaybeStopCache is safe to invoke
// unconditionally: it no-ops if cache consumers are still running or if
// no cache container exists.
func stopSession(ctx context.Context, cli *client.Client, session *container.RunningSession) {
	stopTimeout := 5
	if err := cli.ContainerStop(ctx, session.ContainerID, container_types.StopOptions{
		Timeout: &stopTimeout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to stop container %s: %v\n", session.ContainerName, err)
	}
	if err := cli.ContainerRemove(ctx, session.ContainerID, container_types.RemoveOptions{Force: true}); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove container %s: %v\n", session.ContainerName, err)
	}
	stopDINDForSession(ctx, cli, session)
	dind.MaybeStopCache(ctx, cli)
}

// dindSessionFilters builds the filter used to locate the DIND sidecar that
// belongs to the given session. It must be scoped by workspace hash as well
// as agent+profile: when parallel sessions for the same agent+profile run
// in different workspaces, each has its own DIND sidecar, and stopping one
// session must not touch the other's DIND.
func dindSessionFilters(session *container.RunningSession) filters.Args {
	return filters.NewArgs(
		filters.Arg("label", container.LabelBase+"=true"),
		filters.Arg("label", container.LabelDIND+"=true"),
		filters.Arg("label", container.LabelAgent+"="+session.AgentName),
		filters.Arg("label", container.LabelProfile+"="+session.Profile),
		filters.Arg("label", container.LabelWorkspace+"="+session.WorkspaceHash),
		filters.Arg("status", "running"),
	)
}

// stopDINDForSession finds and stops the DIND sidecar associated with a session,
// including removing its socket and certs volumes to avoid leaking Docker volumes.
func stopDINDForSession(ctx context.Context, cli *client.Client, session *container.RunningSession) {
	list, err := cli.ContainerList(ctx, container_types.ListOptions{Filters: dindSessionFilters(session)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to list DIND containers for %s/%s: %v\n", session.AgentName, session.Profile, err)
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
		if err := cli.ContainerStop(ctx, c.ID, container_types.StopOptions{Timeout: &stopTimeout}); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to stop DIND container %s: %v\n", c.ID, err)
		}
		if err := cli.ContainerRemove(ctx, c.ID, container_types.RemoveOptions{Force: true}); err != nil {
			fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove DIND container %s: %v\n", c.ID, err)
		}

		// Remove associated volumes. Not-found is fine (previous cleanup pass).
		if containerName != "" {
			_ = cli.VolumeRemove(ctx, containerName+"-socket", true)
			_ = cli.VolumeRemove(ctx, containerName+"-certs", true)
		}
	}

	// Remove session network if no containers remain attached.
	if err := network.RemoveOrphanedForSession(ctx, cli, session.AgentName, session.Profile, session.WorkspaceHash); err != nil {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to remove session network: %v\n", err)
	}
}

// manageAttachByName implements `ai-shim manage attach <container-name>`.
// It looks up the container by exact Docker name, verifies it is a running
// ai-shim container, resolves the session's config from container labels,
// and delegates to handleReattach for the actual attach + cleanup flow.
func manageAttachByName(name string) (int, error) {
	ctx := context.Background()
	runner, err := container.NewRunner(ctx)
	if err != nil {
		return 1, fmt.Errorf("creating container runner: %w", err)
	}
	defer func() { _ = runner.Close() }()

	session, err := container.FindSessionByContainerName(ctx, runner.Client(), name)
	if err != nil {
		return 1, err
	}
	if session == nil {
		return 1, fmt.Errorf("no ai-shim container named %q found\nUse 'ai-shim manage status' to see running containers", name)
	}

	// Resolve config from the container's agent+profile labels so
	// handleReattach can run DIND/cache cleanup on container exit.
	layout := storage.NewLayout(storage.DefaultRoot())
	cfg, err := config.Resolve(layout.ConfigDir, session.AgentName, session.Profile)
	if err != nil {
		// Non-fatal: proceed with zero-value config (cleanup will no-op).
		logging.Debug("resolving config for %s/%s: %v", session.AgentName, session.Profile, err)
		cfg = config.Config{}
	}

	logDir := filepath.Join(layout.Root, "logs")
	return handleReattach(ctx, runner, session, cfg, logDir)
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
