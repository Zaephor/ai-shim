# ai-shim Implementation Roadmap

> **For Claude:** Each phase has its own detailed plan document. Use superpowers:executing-plans to implement each phase.

**Goal:** Build ai-shim from the ground up in testable, shippable phases.

**Principle:** Each phase produces a working (if incomplete) binary. No phase should leave the project in a broken state.

---

## Phase 1: Project Scaffolding & Core Config

**Delivers:** Go module, directory structure, CI pipeline, git hooks, config YAML loading with 5-tier merge and template resolution.

**Why first:** Everything depends on config. Getting the merge semantics right with tests establishes the foundation.

**Detailed plan:** `docs/plans/2026-03-21-phase-01-scaffolding-config.md`

---

## Phase 2: Storage, Workspace & Symlink Parsing

**Delivers:** Storage layout creation/resolution at `~/.ai-shim/`, workspace path hashing, symlink name parsing (agent_profile split).

**Why second:** These are pure functions with no Docker dependency — easy to test, and the container phase needs them.

**Detailed plan:** TBD

---

## Phase 3: Agent Registry & Install System

**Delivers:** Built-in agent definitions, npm/uv/binary/custom install types, version pinning, cache-aware install logic.

**Why third:** Agents need storage paths (Phase 2) and config (Phase 1). Install logic runs inside containers but can be unit-tested independently.

**Detailed plan:** TBD

---

## Phase 4: Container Lifecycle (Core Launch)

**Delivers:** Docker client setup, platform abstraction (socket discovery, UID/GID), container creation with mounts/env/hostname, interactive TTY attach, signal passthrough, exit code forwarding, container labeling, orphan cleanup.

**Why fourth:** This is the main event. Needs config, storage, workspace, and agent definitions all wired up. First time we get a real agent running.

**Detailed plan:** TBD

---

## Phase 5: Security & Validation

**Delivers:** Path validation on volume mounts, safe directory checks, pattern-based secret masking in verbose output.

**Why fifth:** Security hardens the container launch path from Phase 4. Easier to test once containers are actually running.

**Detailed plan:** TBD

---

## Phase 6: DIND Sidecar

**Delivers:** DIND container lifecycle (create, health check via events API, cleanup), shared Docker network, Sysbox detection with privileged fallback, DIND-specific GPU toggle.

**Why sixth:** Builds on container lifecycle (Phase 4). Separate phase because DIND is complex and independently testable.

**Detailed plan:** TBD

---

## Phase 7: GPU Support

**Delivers:** NVIDIA GPU detection on Linux, GPU passthrough for agent container and DIND independently, macOS skip logic.

**Why seventh:** Depends on container lifecycle (Phase 4) and DIND (Phase 6). Small, focused phase.

**Detailed plan:** TBD

---

## Phase 8: Tool Provisioning

**Delivers:** Typed installers (binary-download, tar-extract, tar-extract-selective, apt, go-install, custom), checksum verification, cache-aware downloads, progress reporting.

**Why eighth:** Tools install into shared storage (Phase 2) and run during container startup (Phase 4). Can be built and tested independently.

**Detailed plan:** TBD

---

## Phase 9: Cross-Agent Access

**Delivers:** `isolated` flag, `allow_agents` selective home mounts, cross-agent env var injection.

**Why ninth:** Builds on agent registry (Phase 3) and container mounts (Phase 4). Relatively small feature surface.

**Detailed plan:** TBD

---

## Phase 10: CLI Management Commands

**Delivers:** `ai-shim manage` subcommands (agents, profiles, symlinks, cleanup, config, dry-run, doctor), `ai-shim version`.

**Why tenth:** Management commands query the state built by all previous phases. Nice-to-have, not blocking daily use.

**Detailed plan:** TBD

---

## Phase 11: Self-Update

**Delivers:** `ai-shim update` command that checks GitHub releases and replaces the running binary.

**Why eleventh:** Independent feature, no other phase depends on it.

**Detailed plan:** TBD

---

## Phase 12: CI/CD & Release Pipeline

**Delivers:** GitHub Actions workflows (test, build matrix, release), goreleaser config, conventional commit CI check on PR titles.

**Why last:** CI benefits from having all tests written. Goreleaser config needs the final binary structure.

**Detailed plan:** TBD

---

## Phase Dependencies

```
Phase 1 (Config) ─────────────────────────────────────┐
    │                                                  │
Phase 2 (Storage/Workspace/Symlink) ──────────────┐    │
    │                                              │    │
Phase 3 (Agent Registry/Install) ─────────────┐   │    │
    │                                          │   │    │
Phase 4 (Container Lifecycle) ─────────────────┤   │    │
    │          │           │                   │   │    │
Phase 5    Phase 6      Phase 8               │   │    │
(Security) (DIND)       (Tool Provision)      │   │    │
               │                               │   │    │
           Phase 7                             │   │    │
           (GPU)                               │   │    │
                                               │   │    │
Phase 9 (Cross-Agent) ────────────────────────┘   │    │
                                                   │    │
Phase 10 (CLI Management) ───────────────────────┘    │
                                                       │
Phase 11 (Self-Update) ──────────────────────────────┘

Phase 12 (CI/CD) ── depends on all above
```

Note: Phases 5, 6, 7, 8 can be worked in any order after Phase 4.
