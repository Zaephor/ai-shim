# ai-shim

ai-shim transparently sandboxes AI coding agents inside Docker containers.
The user experience is identical to running the agent natively -- all arguments,
stdin/stdout, and exit codes pass through unchanged.

## Requirements

- **Docker** -- the daemon must be running. ai-shim creates containers to
  run agents. Verify with `docker info`.
- **Go 1.25+** -- only if building from source.

## Installation

**Download a release** (recommended):

```bash
# Download the latest binary for your platform
curl -fsSL https://github.com/Zaephor/ai-shim/releases/latest/download/ai-shim_linux_amd64.tar.gz | tar xz
sudo mv ai-shim /usr/local/bin/
```

**Or install with Go:**

```bash
# NOTE: This must match the Go module path in go.mod (github.com/Zaephor/ai-shim)
# If the module is renamed, this command will need to be updated accordingly
go install github.com/Zaephor/ai-shim/cmd/ai-shim@latest
```

**Or build from source:**

```bash
git clone https://github.com/Zaephor/ai-shim.git
cd ai-shim
make build
sudo cp ai-shim /usr/local/bin/   # or add ./ai-shim to your PATH
```

### Prerequisites

> - Docker daemon running (`docker info`)
> - `~/.local/bin` on PATH: `export PATH="$HOME/.local/bin:$PATH"`
> - API key for your agent (e.g. `ANTHROPIC_API_KEY` for claude-code -- set in `~/.ai-shim/config/agents/claude-code.yaml`)

## Quick Start

```bash
# Install (Linux/macOS, amd64/arm64)
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m); [ "$ARCH" = "x86_64" ] && ARCH="amd64"; [ "$ARCH" = "aarch64" ] && ARCH="arm64"
curl -fsSL "https://github.com/Zaephor/ai-shim/releases/latest/download/ai-shim_${OS}_${ARCH}.tar.gz" | tar xz -C ~/.local/bin/

# Initialize
ai-shim init

# Verify Docker is working
ai-shim manage doctor

# Create agent symlink (replace claude-code/personal with your agent/profile)
ai-shim manage symlinks create claude-code personal

# Launch (first run pulls the container image ~500MB)
claude-code_personal
```

On first launch, ai-shim pulls the container image (~500MB) and installs the
agent. This takes 30-60 seconds. Subsequent launches reuse the cached install.

**Profiles** let you maintain separate configurations (work, personal, etc.).
ai-shim ships with 39 example profiles for common stacks.
Browse them on GitHub at [`configs/examples/profiles/`](configs/examples/profiles/)
or in the source tree if you installed from source. Copy any profile to
`~/.ai-shim/config/profiles/<name>.yaml` and customize it.

## Supported Agents

ai-shim ships with 10 built-in agent definitions:

| Agent | Install Method | Binary |
|---|---|---|
| claude-code | custom (install script) | `claude` |
| copilot-cli | npm | `copilot` |
| gemini-cli | npm | `gemini` |
| qwen-code | npm | `qwen` |
| codex | npm | `codex` |
| pi | npm | `pi` |
| gsd | npm | `gsd` |
| aider | uv | `aider` |
| goose | custom (install script) | `goose` |
| opencode | npm | `opencode` |

### Custom Agents

Users can define their own agents by adding an `agent_def:` block to an agent
config file at `~/.ai-shim/config/agents/<name>.yaml`. The filename (minus
`.yaml`) becomes the agent name.

Supported `agent_def` fields:

| Field | Description |
|---|---|
| `install_type` | `npm`, `uv`, or `custom` |
| `package` | Package name to install (npm/uv package, or ignored for custom) |
| `binary` | Binary name the agent provides |
| `data_dirs` | Directories under `~` to persist (e.g. `".my-agent"`) |
| `data_files` | Files under `~` to persist (e.g. `".my-agent.json"`) |

Example (`~/.ai-shim/config/agents/my-agent.yaml`):

```yaml
agent_def:
  install_type: npm
  package: my-custom-agent-package
  binary: my-agent
  data_dirs:
    - ".my-agent"
  data_files:
    - ".my-agent.json"

env:
  MY_AGENT_API_KEY: "your-key-here"

args:
  - "--no-telemetry"
```

Custom agents appear in `ai-shim manage agents` alongside built-ins. If a
custom agent uses the same filename as a built-in, the custom definition wins.
Symlink creation works the same way: `ai-shim manage symlinks create my-agent personal`.

All other config fields (`env`, `args`, `tools`, `mcp_servers`, etc.) work
identically to built-in agent overrides. See
[`configs/examples/agents/custom-agent.yaml`](configs/examples/agents/custom-agent.yaml)
for a full annotated example.

## Configuration

ai-shim uses a 5-tier YAML configuration system. Each tier overrides the
previous:

1. **default** -- global defaults (`~/.ai-shim/config/default.yaml`)
2. **agent** -- agent-specific (`~/.ai-shim/config/agents/claude-code.yaml`)
3. **profile** -- profile-specific (`~/.ai-shim/config/profiles/work.yaml`)
4. **agent-profile** -- combination (`~/.ai-shim/config/agent-profiles/claude-code_work.yaml`)
5. **environment** -- runtime overrides via `AI_SHIM_*` variables

Example configuration:

```yaml
env:
  ANTHROPIC_API_KEY: "sk-ant-..."

# Image reference; supports tag or @sha256: digest pinning
image: "ghcr.io/catthehacker/ubuntu:act-24.04"
# image: "ghcr.io/catthehacker/ubuntu@sha256:abcdef1234567890..."

hostname: "ai-shim"         # container hostname
version: ""                  # pin agent to specific version
update_interval: "1d"        # how often to check for updates (always/never/1d/7d/24h)
args:
  - "--no-telemetry"

dind: false
gpu: false
# dind_gpu: false                # GPU passthrough for DIND sidecar
# kvm: false                     # KVM passthrough for agent container
# dind_kvm: false                # KVM passthrough for DIND sidecar
network_scope: isolated     # global, profile, workspace, profile-workspace, isolated

# Git identity for commits made inside the container
git:
  name: "Your Name"
  email: "you@example.com"

# Home directory isolation (default: true).
# When true, only agent-specific data dirs/files are mounted.
# When false, the entire profile home directory is mounted.
# isolated: true

# resources:                # optional container resource limits
#   memory: "4g"
#   cpus: "2.0"
# dind_resources:
#   memory: 2g
#   cpus: "1.0"
dind_hostname: ai-shim-dind # hostname for the DIND sidecar container
dind_mirrors:               # registry mirrors (default: mirror.gcr.io)
  - https://mirror.gcr.io
dind_cache: false            # enable pull-through registry cache

# Cross-agent access: mount other agents' bins AND data directories
# allow_agents:
#   - gemini-cli

# apt-installed packages. Requires the container to run as root OR the
# non-root agent user to have passwordless sudo. When neither is true the
# entrypoint fails loudly with a hint to convert these to self-contained
# tools: (binary-download / tar-extract / custom). Prefer tools: when the
# base image does not grant root.
packages:
  - tmux
  - git

# Security profile: default, strict, or none
# security_profile: default

# Enable TLS for the DIND Docker socket
# dind_tls: false

# MCP servers exposed to the agent (injected as MCP_SERVERS env var)
# mcp_servers:
#   filesystem:
#     command: npx
#     args: ["@modelcontextprotocol/server-filesystem", "/workspace"]
```

Scalars (image, version, hostname) use last-wins. Maps (env, variables, tools)
merge per-key. Lists (volumes, args, allow_agents) append across tiers.

See `configs/examples/` for annotated example files and
`docs/plans/2026-03-21-ai-shim-design.md` for the full design document.

## Features

- **Docker sandboxing** -- agents run in isolated containers with persistent
  home directories and deterministic workspace mounts
- **Home directory isolation** -- in isolated mode (default), only
  agent-specific data directories and files are mounted into the container;
  set `isolated: false` to mount the full profile home instead
- **Docker-in-Docker (DIND)** -- opt-in sidecar for agents that need Docker
  access, with Sysbox preferred and privileged fallback
- **GPU passthrough** -- independent toggles for the agent container and DIND
  sidecar (`gpu`, `dind_gpu`)
- **KVM passthrough** -- independent toggles for the agent container and DIND
  sidecar (`kvm`, `dind_kvm`); grants access to the host KVM hypervisor device,
  use only with trusted workloads
- **Network scopes** -- configurable network isolation per invocation: global,
  profile, workspace, profile-workspace, or fully isolated (default)
- **Registry mirrors** -- DIND sidecar uses configurable registry mirrors
  (default: `mirror.gcr.io`) for faster and more reliable image pulls
- **Pull-through cache** -- opt-in registry cache for offline-capable and
  faster container image pulls via `dind_cache`
- **Image digest pinning** -- pin images to a specific `@sha256:` digest for
  reproducible builds; `ai-shim manage doctor` reports pinning status
- **Git user config** -- configure `git user.name` and `user.email` for
  commits made inside the container via the `git:` config block or
  `AI_SHIM_GIT_NAME`/`AI_SHIM_GIT_EMAIL` env vars
- **Cross-agent access** -- selectively grant access to other agents' binaries
  and data directories via `allow_agents`, or disable isolation entirely
  with `isolated: false`
- **Agent data registry** -- each built-in agent defines its `DataDirs` and
  `DataFiles` (e.g. `~/.claude`, `~/.claude.json`), which are persisted
  per-profile and shared when cross-agent access is granted
- **Tool provisioning** -- typed installers (tar-extract, tar-extract-selective,
  binary-download, apt, go-install, custom) provision dev tools into the container
- **Template variables** -- Go `text/template` support in env vars, volumes,
  and image names, with variables kept separate from container env
- **Profile isolation** -- each profile gets its own persistent home directory,
  so work and personal contexts stay separate.

  Note: running the same profile in multiple terminals simultaneously is not
  recommended -- the profile home directory is shared and concurrent writes
  from multiple agents (git config, shell history, package caches) may
  conflict. Use separate profiles for parallel sessions.
- **Port mapping** -- forward ports from host to container
- **Package installation** -- install apt packages at container startup
- **Resource limits** -- optional memory and CPU limits for agent and DIND
  containers independently via `resources` and `dind_resources`
- **MCP server configuration** -- define MCP servers in `mcp_servers:` config
  blocks; injected into containers as a `MCP_SERVERS` JSON env var
- **Custom agent definitions** -- add `agent_def:` to agent YAML files to
  define new agents or override built-in definitions
- **Config source annotation** -- `ai-shim manage config` shows which
  configuration tier set each value (e.g. "from agents/claude-code.yaml")
- **Security profiles** -- `security_profile: default|strict|none` controls
  Linux capability dropping for the container
- **JSON output** -- set `AI_SHIM_JSON=1` for machine-readable JSON output
  from management commands
- **Colorized output** -- management output uses ANSI colors; disable with
  `AI_SHIM_NO_COLOR=1` or the standard `NO_COLOR` variable
- **Session detach/reattach** -- TTY sessions are persistent: press
  **Ctrl+], d** to detach without stopping the container, then re-invoke
  the same symlink to reattach. Customizable via `AI_SHIM_DETACH_KEYS`.
  Use `ai-shim manage attach` / `ai-shim manage stop` for explicit control.
  DIND sidecars and networks are preserved across detach/reattach cycles.
- **Container exec** -- `ai-shim manage exec <name> <command...>` runs a
  command in a running ai-shim container
- **Watch mode** -- `ai-shim manage watch <agent> [profile]` auto-restarts
  an agent on crash, controlled by `AI_SHIM_WATCH_RETRIES` (default 3)
- **Agent version management** -- `ai-shim manage agent-versions` lists
  installed versions; `ai-shim manage reinstall <agent>` forces a reinstall
- **Profile switching** -- `ai-shim manage switch-profile <profile>` sets the
  default profile for subsequent invocations
- **DIND TLS** -- `dind_tls: true` enables TLS-secured Docker socket for the
  DIND sidecar
- **DIND health check** -- DIND sidecar startup includes automatic readiness
  polling before the agent container launches
- **Parallel sessions** -- multiple concurrent sessions for the same
  agent+profile+workspace; picker to select which to reattach; `k<N>` to
  kill individual sessions
- **DIND workspace sharing** -- sidecar sees the agent's workspace and tool
  caches at matching paths so `docker -v` works inside the sandbox
- **YAML declaration order** -- tools and MCP servers are provisioned/injected
  in the order they appear in config, not random map iteration
- **Dev build identification** -- `ai-shim version` shows `dev-<commit-hash>`
  for builds without release tags
- **Terminal reset on reattach** -- clear screen and forced SIGWINCH on
  reconnect so TUI redraws cleanly
- **Non-root package gate** -- `packages:` detects uid, routes through sudo
  when available, fails with actionable diagnostic when neither root nor sudo

## CLI Reference

### Symlink Invocation (agent launch)

```bash
claude-code_work [args...]      # launch claude-code with work profile
gemini-cli_personal --verbose   # launch gemini-cli, --verbose passed through
aider                           # launch aider with default profile
```

Symlink names use `_` as the delimiter: `<agent>_<profile>`. Without an
underscore, the `default` profile is used.

### Management Commands

```bash
ai-shim init                    # initialize ai-shim configuration
ai-shim run <agent> [profile] [-- args...]
                                # launch agent without creating a symlink
ai-shim version                 # print version
ai-shim update                  # check for updates
ai-shim completion <bash|zsh>   # generate shell completion script

ai-shim manage agents           # list all built-in agents
ai-shim manage profiles         # list configured profiles
ai-shim manage config <agent> [profile]
                                # show resolved config (profile defaults to "default")
ai-shim manage doctor           # check Docker, storage, config
ai-shim manage dry-run <agent> <profile> [args...]
                                # show container spec without launching
ai-shim manage symlinks list [dir]
                                # list ai-shim symlinks in directory
ai-shim manage symlinks create <agent> [profile] [dir]
                                # create a symlink
                                # agent/profile names: ASCII letters/digits and
                                # '._-' only, must start with a letter or digit,
                                # max 63 chars (matches Docker container naming)
ai-shim manage symlinks remove <path>
                                # remove a symlink
ai-shim manage cleanup          # remove orphaned ai-shim containers
ai-shim manage status           # show running ai-shim containers
ai-shim manage logs [agent] [profile]
                                # show launch/exit log or container logs
ai-shim manage exec <name> <cmd...>
                                # run command in a running container
ai-shim manage attach <agent> [profile]
                                # reattach to a detached session
ai-shim manage stop <agent> [profile]
                                # stop a running session
ai-shim manage watch <agent> [profile]
                                # restart agent on crash with retries
ai-shim manage agent-versions   # show installed agent versions
ai-shim manage reinstall <agent>
                                # force reinstall an agent
ai-shim manage switch-profile <profile>
                                # set the default profile
ai-shim manage backup <profile> [path]
                                # backup profile to tar.gz
ai-shim manage restore <profile> <archive>
                                # restore profile from tar.gz backup
ai-shim manage disk-usage       # show storage usage breakdown
```

### Environment Variable Overrides

```
AI_SHIM_IMAGE=<image>           # override container image
AI_SHIM_VERSION=<ver>           # pin agent version
AI_SHIM_DIND=0/1                # toggle DIND sidecar
AI_SHIM_DIND_GPU=0/1            # toggle GPU for DIND
AI_SHIM_GPU=0/1                 # toggle GPU for agent container
AI_SHIM_DIND_KVM=0/1            # toggle KVM passthrough for DIND
AI_SHIM_KVM=0/1                 # toggle KVM passthrough for agent container
AI_SHIM_NETWORK_SCOPE=<scope>   # override network scope
AI_SHIM_DIND_HOSTNAME=<host>    # override DIND sidecar hostname
AI_SHIM_DIND_CACHE=0/1          # toggle pull-through registry cache
AI_SHIM_DIND_TLS=0/1            # toggle TLS for DIND socket
AI_SHIM_SECURITY_PROFILE=<p>    # security profile (default/strict/none)
AI_SHIM_UPDATE_INTERVAL=<i>    # agent update interval (always/never/1d/7d/24h)
AI_SHIM_GIT_NAME=<name>         # git user.name for container commits
AI_SHIM_GIT_EMAIL=<email>       # git user.email for container commits
AI_SHIM_VERBOSE=0/1             # enable debug output
AI_SHIM_JSON=0/1                # JSON output for management commands
AI_SHIM_NO_COLOR=0/1            # disable colored output
AI_SHIM_WATCH_RETRIES=<n>       # max restarts for watch mode (default 3)
AI_SHIM_DETACH_KEYS=<keys>      # detach key sequence (default ctrl-],d)
```

## Development

```bash
make build              # produces ./ai-shim binary
make test               # run all tests (requires Docker)
make test-short         # skip integration tests requiring Docker
make verify             # fmt + vet + lint + test
make setup              # install lefthook pre-commit hooks
```

## Releases

Releases are automated with [release-please](https://github.com/googleapis/release-please)
and [GoReleaser](https://goreleaser.com/). Merging conventional commits to `main`
triggers a release PR; merging the release PR builds and publishes binaries for
linux/darwin on amd64/arm64.

## Contributing

- Use [conventional commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
- Run `make setup` to install the commit-msg hook that enforces this
- Run `make verify` before submitting a pull request
- Integration tests use real Docker -- ensure the daemon is running

## License

ai-shim is licensed under the [GNU Affero General Public License v3.0](LICENSE).
