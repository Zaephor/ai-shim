# ai-shim Design Document

## Overview

ai-shim is a Go CLI tool that transparently sandboxes AI coding agents (claude, gemini, pi, etc.) inside Docker containers. The user experience should feel identical to running the agent natively.

## Core Concepts

- **Agent** — A coding assistant CLI (claude, gemini, pi, etc.). Built-in agents are defined in the Go binary with install type, package name, binary path, home directory patterns, and default config.
- **Profile** — An isolated home folder identity. Each profile gets its own persistent storage mounted as the container's home directory.
- **Invocation** — Via symlinks using `_` as delimiter: `claude_work` -> agent=claude, profile=work. All arguments pass through to the agent.
- **Direct mode** — Calling `ai-shim` directly provides management subcommands only (no agent launch).

## Configuration System

### 5-Tier Precedence (lowest to highest)

1. `default` — Global defaults
2. `agent` — Agent-specific (all claude, all gemini, etc.)
3. `profile` — Profile-specific (all agents using "work" profile)
4. `agent-profile` — Specific combination (claude + work)
5. Environment variables — Runtime overrides via `AI_SHIM_*`

### Config File Layout

```
~/.ai-shim/config/
  default.yaml
  agents/
    claude.yaml
    gemini.yaml
  profiles/
    work.yaml
    personal.yaml
  agent-profiles/
    claude_work.yaml
```

### Config Structure

```yaml
# variables are template sources, not injected into the container
variables:
  llm_host: "my-llm-host:8080"

# env vars injected into the container, supports templating
env:
  LLM_ENDPOINT: "https://{{ .llm_host }}/v1"

# container settings
image: "ghcr.io/catthehacker/ubuntu:act-24.04"
hostname: "ai-shim"

# agent runtime (built-in agents have defaults, override here)
version: ""          # pin version, empty = latest
args:                # default args passed to agent CLI
  - "--no-telemetry"

# volumes beyond the automatic storage mounts
volumes:
  - "{{ .storage_shared }}/bin:/usr/local/bin"

# features
dind: false
dind_gpu: false
gpu: false
network_scope: isolated  # global, profile, workspace, profile-workspace, isolated (default)
dind_hostname: ai-shim-dind  # hostname for the DIND sidecar container
dind_mirrors:            # registry mirror URLs (default: mirror.gcr.io)
  - https://mirror.gcr.io
dind_cache: false        # enable pull-through registry cache

# Container resource limits (optional, disabled by default)
# resources:
#   memory: "4g"
#   cpus: "2.0"

# DIND container resource limits (optional, disabled by default)
# dind_resources:
#   memory: "2g"
#   cpus: "1.0"

# cross-agent access
allow_agents: []     # agents whose homes get mounted
isolated: true       # when false, all installed agents visible

# tool provisioning (typed installers + shell escape hatch)
tools:
  act:
    type: tar-extract
    url: "https://github.com/nektos/act/releases/..."
    binary: act
  custom-tool:
    type: custom
    install: |
      curl -L ... | tar xz
      mv bin/tool /usr/local/bin/

# port mappings
ports:
  - "8080:8080"

# additional packages to install in the container
packages:
  - tmux
```

### Merge Behavior

- **Scalars** (image, version, hostname): later tier wins
- **Maps** (env, variables, tools): per-key replace across tiers
- **Lists** (volumes, args, allow_agents): append across tiers
- **Variables**: resolved via Go `text/template` after full merge
- **Template sources** are separated from env vars to prevent leakage into the container environment and to allow templating in volumes, image names, etc.

## Storage Layout

```
~/.ai-shim/
  salt                   # hostname, used for workspace path hashing
  config/                # config files (see above)
  shared/
    bin/                 # common dev tools (act, helm, etc.)
    cache/               # shared download cache
  agents/
    claude/
      bin/               # claude CLI binary, version-scoped
      cache/             # agent-specific install cache (npm/uv)
    gemini/
      bin/
      cache/
  profiles/
    work/
      home/              # mounted as /home/<user> in container
    personal/
      home/
```

### 3-Tier Storage

- **shared/** — Common dev tools available to all agents (act, helm, kubectl)
- **agents/<name>/** — Agent runtime binaries and install caches, version-scoped
- **profiles/<name>/** — Container home directory, agent configs, project memory

## Workspace Mapping

The host's PWD is mounted into the container at `/workspace/<hash>`, where `<hash>` is the first 12 characters of `SHA256(hostname + absolute_host_path)`.

- `/home/user/projects/myapp` -> `/workspace/a1b2c3d4e5f6`
- Deterministic: same host path always maps to the same container path
- The hostname acts as a salt for added privacy, preventing hash reversal
- Agents with project-scoped memory key off this path, so different projects don't collide
- Host filesystem structure is not exposed to the agent

### Container Mount Summary

| Host path | Container path | Purpose |
|---|---|---|
| `~/.ai-shim/shared/bin` | `/usr/local/bin` (or similar) | Common tools |
| `~/.ai-shim/agents/<agent>/bin` | Agent-specific PATH entry | Agent runtime |
| `~/.ai-shim/profiles/<profile>/home` | `/home/<user>` | Persistent home |
| Host PWD | `/workspace/<hash>` | Project files |

## Container Lifecycle

### Startup Sequence (symlink invocation)

1. Parse symlink name — split on `_` -> agent + profile
2. Resolve config — merge 5 tiers, resolve templates
3. Ensure storage paths exist
4. Run tool provisioning (if any tools need install/update)
5. Run agent install/update (check cache, install if needed)
6. Optionally start DIND sidecar container
7. Launch main container:
   - Attach interactively (`-it`, auto-detected)
   - Mount storage paths and workspace
   - Set hostname, env vars, working directory
   - Entrypoint runs the agent binary with merged default args + passthrough args
8. ai-shim blocks, watching main container via Docker events API

### Shutdown Sequence

- **Normal exit** — Main container exits (user quit the agent normally). ai-shim removes DIND if running, exits with the agent's exit code.
- **ai-shim killed** — Containers become orphaned. All containers are labeled with `ai-shim` metadata so `ai-shim manage cleanup` can find and remove orphans.

### Signal Handling

Signals are passed through transparently to the container. ai-shim does not intercept Ctrl+C or other signals — the agent handles them natively. The user should not notice they are inside a sandbox.

### DIND Sidecar

- Launched before main container, shares a Docker network
- Toggleable via config (`dind: true/false`) and env var (`AI_SHIM_DIND=0/1`)
- Sysbox runtime used if available, falls back to privileged mode
- Default image: `docker:dind`

### GPU Support

- Independent toggles for agent container (`gpu`) and DIND sidecar (`dind_gpu`)
- Auto-detects available GPUs when enabled
- Overridable via `AI_SHIM_GPU` and `AI_SHIM_DIND_GPU` env vars

## Agent Definitions

### Built-in Agents

Defined in the Go binary as structs:

```go
type Definition struct {
    Name        string   // "claude-code", "gemini-cli", "pi"
    InstallType string   // "npm", "uv", "binary", "custom"
    Package     string   // "@google/gemini-cli", etc.
    Binary      string   // "claude", "gemini", etc.
    HomePaths   []string // [".claude", ".claude.json"]
}
```

Users never need to define these for supported agents. Config tiers can override any field.

### Built-in Agent Registry

| Agent | InstallType | Package / Source | Binary | HomePaths |
|---|---|---|---|---|
| claude-code | custom | `curl -fsSL https://claude.ai/install.sh \| bash` | `claude` | `.claude`, `.claude.json` |
| gemini-cli | npm | `@google/gemini-cli` | `gemini` | `.gemini` |
| qwen-code | npm | `@qwen-code/qwen-code` | `qwen` | `.qwen` |
| codex | npm | `@openai/codex` | `codex` | `.codex` |
| pi | npm | `@mariozechner/pi-coding-agent` | `pi` | `.pi` |
| gsd | npm | `gsd-pi` | `gsd` | `.gsd` |
| aider | uv | `aider-chat` | `aider` | `.aider` |
| goose | custom | `curl -fsSL .../download_cli.sh \| bash` | `goose` | `.config/goose` |
| opencode | npm | `opencode-ai` | `opencode` | `.config/opencode` |

### Install Types

| Type | Mechanism | Cache location |
|---|---|---|
| `npm` | `npm install -g` | `agents/<name>/cache/npm` |
| `uv` | `uv tool install` | `agents/<name>/cache/uv` |
| `binary` | Direct download | `agents/<name>/bin/` |
| `custom` | Shell script | varies |

### Version Pinning

Set `version` in any config tier. Agent install checks cached version against desired, updates only when mismatched.

## Tool Provisioning

Typed installers for common patterns, with a shell escape hatch for edge cases.

| Type | Behavior |
|---|---|
| `binary-download` | Download single binary to shared/bin |
| `tar-extract` | Download tarball, extract named binary |
| `tar-extract-selective` | Extract binary + specific supporting files |
| `apt` | apt-get install inside container |
| `go-install` | `go install` |
| `custom` | Shell snippet escape hatch |

Tool definitions support an optional `checksum` field (SHA256) for download verification. When cached tools exist and match the expected version, downloads are skipped (offline-friendly).

## Cross-Agent Access

When an agent can call other agents from inside the container:

- **Non-isolated mode** (`isolated: false`): All installed agents visible via shared profile storage. Mirrors convenience of having both agents available after first launch.
- **Isolated mode** (`isolated: true`, default): Only agents listed in `allow_agents` have their home paths mounted. Prevents credential leakage.
- Agent binaries live in agent-scoped storage (not shared bin) to avoid version conflicts between agents.
- When cross-agent access is granted, ai-shim mounts the allowed agent's home paths and injects their env vars (API keys, endpoints) from the config tiers.

## CLI Interface

### Symlink Invocation (agent launch)

```
claude_work [args...]          # launch claude with work profile
gemini_personal --verbose      # launch gemini, --verbose passed through
pi_work                        # launch pi with work profile
```

### Direct Invocation (management only)

```
ai-shim manage
  agents          # list built-in and configured agents
  profiles        # list profiles
  symlinks        # list/create/remove symlinks
  cleanup         # remove orphaned containers
  config          # show resolved config for an agent_profile pair
  dry-run         # show full Docker container spec without launching
  doctor          # check Docker, socket, permissions, images, config

ai-shim update                 # self-update from GitHub releases
ai-shim version                # print version info
```

### Environment Variable Overrides

```
AI_SHIM_IMAGE=<image>          # override container image
AI_SHIM_VERSION=<ver>          # pin agent version
AI_SHIM_DIND=0/1               # toggle DIND sidecar
AI_SHIM_DIND_GPU=0/1           # toggle GPU for DIND
AI_SHIM_GPU=0/1                # toggle GPU for agent container
AI_SHIM_NETWORK_SCOPE=<scope>  # override network scope
AI_SHIM_DIND_HOSTNAME=<host>   # override DIND sidecar hostname
AI_SHIM_DIND_CACHE=0/1         # toggle pull-through registry cache
```

ai-shim produces no output by default (transparent sandbox). Errors print to stderr.

## Security

- **Secret masking** — Pattern-based detection in verbose/debug output. Mask env var names matching `*KEY*`, `*SECRET*`, `*TOKEN*`, `*PASSWORD*`, `*CREDENTIAL*`, `*AUTH*` and values matching known API key patterns.
- **Path validation** — Volume mount sources are validated: no path traversal (`../`), no mounting sensitive host paths (`/etc`, `/var/run` except Docker socket).
- **Safe directory check** — Refuse to run from dangerous directories (`/`, `$HOME` root, `/etc`).
- **Rootless execution** — Agent containers run as the invoking user's UID/GID. Files created inside the container are owned by the host user. DIND sidecar runs as root (privileged or Sysbox) due to daemon requirements.

## Platform Abstraction

- **Docker socket discovery** — `/var/run/docker.sock` (Linux) vs `~/.docker/run/docker.sock` or Colima paths (macOS)
- **User ID mapping** — Explicit `uid:gid` passthrough on Linux; Docker Desktop handles mapping on macOS
- **GPU detection** — NVIDIA on Linux only; skipped on macOS

## Testing

Integration tests with real components are strongly preferred over mocks. Tests should use real Docker API, real filesystem, real config files.

| Tier | Scope | Docker Required |
|---|---|---|
| Unit | Config parsing, path validation, template resolution, symlink parsing | No |
| Integration | Container lifecycle, volume mounts, storage layout, tool provisioning | Yes |
| E2E | Full symlink invocation to agent running in container | Yes |

CI runs unit tests on all platforms, integration and E2E on Linux (GitHub Actions runners have Docker).

## CI/CD & Release

### GitHub Actions Workflows

- **Test** — On PR/push: `go test ./...`, lint (`golangci-lint`), vet. Integration tests require Docker.
- **Build** — Matrix: `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`
- **Release** — On tag push (`v*`): goreleaser builds binaries, creates GitHub release, updates brew tap

### Versioning & Commits

- Conventional commits enforced at the git hook level (commit-msg hook)
- Hook distributed in `.githooks/commit-msg`, activated via `core.hooksPath`
- Lightweight CI check on PR titles for squash-merge workflows
- Semver tags trigger releases via goreleaser

## Project Structure

```
ai-shim/
  cmd/
    ai-shim/
      main.go              # entrypoint, symlink detection
  internal/
    agent/                 # built-in agent definitions
    cli/                   # management subcommands (agents, profiles, doctor, etc.)
    config/                # YAML loading, tier merge, templating
    container/             # Docker client, container lifecycle
    dind/                  # DIND sidecar management
    install/               # agent install types (npm, uv, binary)
    invocation/            # symlink name parsing (agent + profile extraction)
    network/               # Docker network creation and scoping
    provision/             # tool provisioning (typed installers)
    storage/               # storage layout, path resolution
    testutil/              # shared test helpers (Docker availability checks)
    workspace/             # workspace hashing, mount generation
    selfupdate/            # self-update from GitHub releases
    platform/              # platform abstraction (socket, uid, gpu detection)
    security/              # path validation, secret masking, safe dir checks
  configs/                 # example/default config files
```

## Design Decisions Summary

| Decision | Choice |
|---|---|
| Language | Go, targeting linux/darwin on amd64/arm64 |
| Config format | YAML |
| Config precedence | default -> agent -> profile -> agent-profile -> env vars |
| Template system | Go `text/template`, variables separate from env |
| Symlink delimiter | `_` (underscore) |
| Storage root | `~/.ai-shim/` |
| Storage tiers | shared, agent-scoped, profile-scoped |
| Workspace path | `/workspace/<SHA256(hostname + abs_path)[:12]>` |
| Default images | `ghcr.io/catthehacker/ubuntu:act-24.04` (agent), `docker:dind` (sidecar) |
| Hostname | Configurable, defaults to `ai-shim` |
| Agent install | Built-in definitions (npm/uv/binary/custom), overridable via config |
| Tool provisioning | Typed installers + custom shell escape hatch |
| DIND | Opt-in per invocation, Sysbox preferred, privileged fallback |
| DIND socket sharing | Volume-mounted Unix socket (not TCP), exposed at `/var/run/dind/docker.sock` |
| Network scope | Configurable isolation: global, profile, workspace, profile-workspace, isolated (default) |
| Registry mirrors | Default `mirror.gcr.io`, configurable via `dind_mirrors` |
| Pull-through cache | Opt-in registry cache for offline/faster pulls via `dind_cache` |
| GPU | Independent toggles for agent container and DIND |
| Cross-agent access | `allow_agents` selective home mounts, `isolated` flag |
| Signal handling | Transparent passthrough, ai-shim watches container exit |
| TTY | Auto-detect, allocate when stdin is a TTY |
| ai-shim output | Silent by default, errors to stderr |
| Self-update | `ai-shim update` via GitHub releases |
| Commit enforcement | Local git hook (commit-msg), lightweight CI check on PR titles |
| Versioning | Conventional commits, semver, goreleaser |
| Secret masking | Pattern-based detection in debug output |
| Path validation | Block traversal and sensitive host paths |
| Safe directory | Refuse to run from `/`, `$HOME`, `/etc` |
| Rootless | Agent container runs as invoking user's UID/GID |
| Platform | Abstracted socket discovery, UID mapping, GPU detection |
| Port mapping | Configurable per tier, appended across tiers |
| Dry-run | `ai-shim manage dry-run` shows full container spec |
| Doctor | `ai-shim manage doctor` checks prerequisites |
| Tool caching | Skip downloads when cache is warm (offline-friendly) |
| Resource limits | Optional memory/CPU limits for agent and DIND containers independently |
