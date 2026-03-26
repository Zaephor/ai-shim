# ai-shim

ai-shim transparently sandboxes AI coding agents inside Docker containers.
The user experience is identical to running the agent natively -- all arguments,
stdin/stdout, and exit codes pass through unchanged.

## Quick Start

```bash
# Build from source
make build

# Create a symlink for claude with the "work" profile
ai-shim manage symlinks create claude-code work ~/bin

# Use it exactly like the native agent
claude-code_work "explain this codebase"

# Or with default profile (no underscore needed)
ai-shim manage symlinks create claude-code default ~/bin
claude-code "explain this codebase"
```

## Supported Agents

ai-shim ships with 9 built-in agent definitions:

| Agent | Install Method | Binary |
|---|---|---|
| claude-code | custom (install script) | `claude` |
| gemini-cli | npm | `gemini` |
| qwen-code | npm | `qwen` |
| codex | npm | `codex` |
| pi | npm | `pi` |
| gsd | npm | `gsd` |
| aider | uv | `aider` |
| goose | custom (install script) | `goose` |
| opencode | npm | `opencode` |

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

image: "ghcr.io/catthehacker/ubuntu:act-24.04"
version: ""
args:
  - "--no-telemetry"

dind: false
gpu: false
network_scope: isolated     # global, profile, workspace, profile-workspace, isolated

# resources:                # optional container resource limits
#   memory: "4g"
#   cpus: "2.0"
dind_hostname: ai-shim-dind # hostname for the DIND sidecar container
dind_mirrors:               # registry mirrors (default: mirror.gcr.io)
  - https://mirror.gcr.io
dind_cache: false            # enable pull-through registry cache

packages:
  - tmux
  - git
```

Scalars (image, version, hostname) use last-wins. Maps (env, variables, tools)
merge per-key. Lists (volumes, args, allow_agents) append across tiers.

See `configs/examples/` for annotated example files and
`docs/plans/2026-03-21-ai-shim-design.md` for the full design document.

## Features

- **Docker sandboxing** -- agents run in isolated containers with persistent
  home directories and deterministic workspace mounts
- **Docker-in-Docker (DIND)** -- opt-in sidecar for agents that need Docker
  access, with Sysbox preferred and privileged fallback
- **GPU passthrough** -- independent toggles for the agent container and DIND
  sidecar (`gpu`, `dind_gpu`)
- **Network scopes** -- configurable network isolation per invocation: global,
  profile, workspace, profile-workspace, or fully isolated (default)
- **Registry mirrors** -- DIND sidecar uses configurable registry mirrors
  (default: `mirror.gcr.io`) for faster and more reliable image pulls
- **Pull-through cache** -- opt-in registry cache for offline-capable and
  faster container image pulls via `dind_cache`
- **Cross-agent access** -- selectively mount other agents' home directories
  via `allow_agents`, or disable isolation entirely
- **Tool provisioning** -- typed installers (tar-extract, binary-download, apt,
  go-install, custom) provision dev tools into the container
- **Template variables** -- Go `text/template` support in env vars, volumes,
  and image names, with variables kept separate from container env
- **Profile isolation** -- each profile gets its own persistent home directory,
  so work and personal contexts stay separate
- **Port mapping** -- forward ports from host to container
- **Package installation** -- install apt packages at container startup
- **Resource limits** -- optional memory and CPU limits for agent and DIND
  containers independently via `resources` and `dind_resources`

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
ai-shim manage config <agent> <profile>
                                # show resolved config
ai-shim manage doctor           # check Docker, storage, config
ai-shim manage dry-run <agent> <profile> [args...]
                                # show container spec without launching
ai-shim manage symlinks list [dir]
                                # list ai-shim symlinks in directory
ai-shim manage symlinks create <agent> [profile] [dir]
                                # create a symlink
ai-shim manage symlinks remove <path>
                                # remove a symlink
ai-shim manage cleanup          # remove orphaned ai-shim containers
ai-shim manage status           # show running ai-shim containers
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
AI_SHIM_NETWORK_SCOPE=<scope>   # override network scope
AI_SHIM_DIND_HOSTNAME=<host>    # override DIND sidecar hostname
AI_SHIM_DIND_CACHE=0/1          # toggle pull-through registry cache
AI_SHIM_VERBOSE=0/1             # enable debug output
```

## Building from Source

Requires Go 1.25+ and Docker.

```bash
git clone https://github.com/ai-shim/ai-shim.git
cd ai-shim
make build              # produces ./ai-shim binary
make test               # run all tests
make test-short         # skip integration tests requiring Docker
make verify             # fmt + vet + lint + test
```

## Contributing

- Use [conventional commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `docs:`, `refactor:`, `test:`, `chore:`
- Run `make setup` to install the commit-msg hook that enforces this
- Run `make verify` before submitting a pull request
- Integration tests use real Docker -- ensure the daemon is running
