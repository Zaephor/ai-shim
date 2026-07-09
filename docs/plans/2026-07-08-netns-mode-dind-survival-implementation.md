# netns_mode + DIND Survival Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the `dind_shared_netns` boolean with a three-way `netns_mode` selector and make the agent container survive DIND sidecar death via a netns-holder container plus DIND auto-restart.

**Architecture:** A minimal "holder" container owns the shared network namespace; both the agent and the DIND sidecar join it via Docker `container:<id>` network mode, so a DIND death no longer destroys the agent's netns. DIND gets `RestartPolicy: unless-stopped` and creates its socket with the agent's group on every start (`dockerd --group`), so an auto-restart yields a reachable daemon.

**Tech Stack:** Go 1.26.4, Docker Engine API (`github.com/docker/docker/client`), testify, existing `internal/{config,dind,container,network}` packages.

## Global Constraints

- Go toolchain pinned: `toolchain go1.26.4` in `go.mod` — do not change.
- Conventional Commits, imperative subject ≤72 chars.
- Full CI gate before a phase is "done": `make ci` (fmt-check/vet/tidy-check/silent-failures/test-race/fuzz/vuln) **and** the e2e job green.
- Repo is pre-1.0 (0.9.0): `dind_shared_netns` / `AI_SHIM_DIND_SHARED_NETNS` are removed with **no** back-compat alias. Migration is documentation-only.
- `netns_mode` values are exactly `agent`, `dind`, `holder`. Default (unset) is `holder`.
- `netns_mode` is only meaningful with DIND enabled; `dind`/`holder` with DIND off is a validation error.
- Do not commit unless the human directs it; the human owns all remote git.

---

## File Structure

- `internal/config/types.go` — remove `DINDSharedNetns`; add `NetnsMode`, `DINDNetnsHolderImage` fields + mode constants + accessors.
- `internal/config/resolver.go` — env parsing: drop `AI_SHIM_DIND_SHARED_NETNS`, add `AI_SHIM_NETNS_MODE` + `AI_SHIM_DIND_NETNS_HOLDER_IMAGE`.
- `internal/config/sources.go` — source-tracking for the two new fields.
- `internal/config/merge.go` — merge the two new fields.
- `internal/config/validate.go` — validate mode enum, DIND-required, reworded ports warning.
- `internal/dind/dind.go` — extract `dockerdArgs`; add `BuildNetnsExtraHosts`; add `JoinNetns` + `AutoRestart` to `Config`; wire `--group`, restart policy, and netns-join into `Start`.
- `internal/dind/holder.go` — **new**: `Holder` type, `HolderConfig`, `holderSpec` (pure), `StartHolder`, `(*Holder).ContainerID/Stop`.
- `cmd/ai-shim/main.go` — orchestrate per-mode launch: build holder before DIND, wire `NetworkMode` for DIND and agent, teardown ordering, cache alias onto the holder.
- `test/e2e/netns_mode_test.go` — **new** (replaces `test/e2e/dind_shared_netns_test.go`): per-mode netns identity + DIND-death survival.
- `README.md`, `configs/` example(s), `CHANGELOG.md` — doc updates + breaking-change note.

---

### Task 1: Config field + accessors

**Files:**
- Modify: `internal/config/types.go` (remove `DINDSharedNetns` at line 38; remove `IsDINDSharedNetns` at line 143; add new fields + accessors)
- Test: `internal/config/types_test.go` (replace `TestIsDINDSharedNetns` at line 52)

**Interfaces:**
- Produces: `config.NetnsModeAgent`, `config.NetnsModeDIND`, `config.NetnsModeHolder` (string consts); `(Config).GetNetnsMode() string`; `(Config).IsNetnsShared() bool`; `(Config).UsesNetnsHolder() bool`; fields `Config.NetnsMode string`, `Config.DINDNetnsHolderImage string`.

- [ ] **Step 1: Write the failing test**

Replace `TestIsDINDSharedNetns` in `internal/config/types_test.go` with:

```go
func TestGetNetnsMode(t *testing.T) {
	assert.Equal(t, config.NetnsModeHolder, config.Config{}.GetNetnsMode(), "unset defaults to holder")
	assert.Equal(t, config.NetnsModeAgent, config.Config{NetnsMode: "agent"}.GetNetnsMode())
	assert.Equal(t, config.NetnsModeDIND, config.Config{NetnsMode: "dind"}.GetNetnsMode())
}

func TestIsNetnsShared(t *testing.T) {
	assert.False(t, config.Config{NetnsMode: "agent"}.IsNetnsShared())
	assert.True(t, config.Config{NetnsMode: "dind"}.IsNetnsShared())
	assert.True(t, config.Config{NetnsMode: "holder"}.IsNetnsShared())
	assert.True(t, config.Config{}.IsNetnsShared(), "default holder is shared")
}

func TestUsesNetnsHolder(t *testing.T) {
	assert.False(t, config.Config{NetnsMode: "agent"}.UsesNetnsHolder())
	assert.False(t, config.Config{NetnsMode: "dind"}.UsesNetnsHolder())
	assert.True(t, config.Config{NetnsMode: "holder"}.UsesNetnsHolder())
	assert.True(t, config.Config{}.UsesNetnsHolder(), "default is holder")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run 'TestGetNetnsMode|TestIsNetnsShared|TestUsesNetnsHolder' -v`
Expected: FAIL — `undefined: config.NetnsModeHolder` / `GetNetnsMode`.

- [ ] **Step 3: Edit `internal/config/types.go`**

Remove the `DINDSharedNetns` field (line 38) and its doc comment (lines 35-37). In its place add:

```go
	// NetnsMode selects which container owns the network namespace the agent
	// uses: "agent" (agent keeps its own netns on the bridge), "dind" (agent
	// joins the DIND sidecar's netns), or "holder" (a dedicated holder owns
	// the netns and both agent and DIND join it, so DIND death does not
	// destroy the agent's networking). Default: "holder". Only meaningful
	// when DIND is enabled.
	NetnsMode string `yaml:"netns_mode,omitempty" json:"netns_mode,omitempty"`
	// DINDNetnsHolderImage overrides the image used for the netns holder in
	// "holder" mode. Empty reuses the DIND image (no extra pull). The image
	// must provide a `sleep` binary.
	DINDNetnsHolderImage string `yaml:"dind_netns_holder_image,omitempty" json:"dind_netns_holder_image,omitempty"`
```

Remove `IsDINDSharedNetns` (line 143). Add, near the other DIND accessors:

```go
// Netns mode values. Default (unset) is NetnsModeHolder.
const (
	NetnsModeAgent  = "agent"
	NetnsModeDIND   = "dind"
	NetnsModeHolder = "holder"
)

// GetNetnsMode returns the configured netns mode, defaulting to holder.
// Only meaningful when DIND is enabled; callers must gate on IsDINDEnabled().
func (c Config) GetNetnsMode() string {
	if c.NetnsMode == "" {
		return NetnsModeHolder
	}
	return c.NetnsMode
}

// IsNetnsShared reports whether the agent shares another container's netns
// (true for dind and holder modes).
func (c Config) IsNetnsShared() bool {
	m := c.GetNetnsMode()
	return m == NetnsModeDIND || m == NetnsModeHolder
}

// UsesNetnsHolder reports whether a dedicated netns holder container is used.
func (c Config) UsesNetnsHolder() bool {
	return c.GetNetnsMode() == NetnsModeHolder
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run 'TestGetNetnsMode|TestIsNetnsShared|TestUsesNetnsHolder' -v`
Expected: PASS. (Other packages will not compile yet — that is fixed in Tasks 2-3 and 7.)

- [ ] **Step 5: Commit**

```bash
git add internal/config/types.go internal/config/types_test.go
git commit -m "feat(config): add netns_mode selector, drop dind_shared_netns field"
```

---

### Task 2: Env, source-tracking, merge

**Files:**
- Modify: `internal/config/resolver.go` (drop `AI_SHIM_DIND_SHARED_NETNS` at lines 218-220 and the doc line 171; add two env reads)
- Modify: `internal/config/sources.go` (replace the `DINDSharedNetns` block at lines 98-100)
- Modify: `internal/config/merge.go` (replace the `DINDSharedNetns` block at lines 48-50)
- Test: `internal/config/resolver_test.go`, `internal/config/merge_test.go`

**Interfaces:**
- Consumes: `Config.NetnsMode`, `Config.DINDNetnsHolderImage` (Task 1).

- [ ] **Step 1: Write the failing tests**

In `internal/config/merge_test.go`, replace `TestMerge_DINDSharedNetnsLastWins` with:

```go
func TestMerge_NetnsModeLastWins(t *testing.T) {
	base := config.Config{NetnsMode: "dind"}
	over := config.Config{NetnsMode: "holder"}
	assert.Equal(t, "holder", config.Merge(base, over).NetnsMode, "explicit override wins")
	assert.Equal(t, "dind", config.Merge(base, config.Config{}).NetnsMode, "empty override preserves base")
}

func TestMerge_HolderImageLastWins(t *testing.T) {
	base := config.Config{DINDNetnsHolderImage: "a"}
	over := config.Config{DINDNetnsHolderImage: "b"}
	assert.Equal(t, "b", config.Merge(base, over).DINDNetnsHolderImage)
	assert.Equal(t, "a", config.Merge(base, config.Config{}).DINDNetnsHolderImage)
}
```

In `internal/config/resolver_test.go` add (adjust to the file's existing env-test helper style — set env, call the resolver entrypoint used by neighboring tests, assert):

```go
func TestResolver_NetnsModeEnv(t *testing.T) {
	t.Setenv("AI_SHIM_NETNS_MODE", "dind")
	t.Setenv("AI_SHIM_DIND_NETNS_HOLDER_IMAGE", "busybox:latest")
	cfg := config.Config{}
	config.ApplyEnvOverrides(&cfg) // use whatever the file's other env tests call
	assert.Equal(t, "dind", cfg.NetnsMode)
	assert.Equal(t, "busybox:latest", cfg.DINDNetnsHolderImage)
}
```

> Note: if the resolver's env entrypoint is not `ApplyEnvOverrides`, match the exact function the existing `AI_SHIM_DIND_*` tests exercise in `resolver_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestMerge_NetnsModeLastWins|TestMerge_HolderImageLastWins|TestResolver_NetnsModeEnv' -v`
Expected: FAIL (compile error: `DINDSharedNetns` still referenced; new fields not wired).

- [ ] **Step 3: Edit `internal/config/merge.go`**

Replace lines 48-50 (`if over.DINDSharedNetns != nil { ... }`) with:

```go
	if over.NetnsMode != "" {
		result.NetnsMode = over.NetnsMode
	}
	if over.DINDNetnsHolderImage != "" {
		result.DINDNetnsHolderImage = over.DINDNetnsHolderImage
	}
```

- [ ] **Step 4: Edit `internal/config/resolver.go`**

Remove the doc line 171 (`AI_SHIM_DIND_SHARED_NETNS ...`) and replace lines 218-220 (`if b := parseBoolEnv("AI_SHIM_DIND_SHARED_NETNS"); ...`) with:

```go
	if v := os.Getenv("AI_SHIM_NETNS_MODE"); v != "" {
		cfg.NetnsMode = v
	}
	if v := os.Getenv("AI_SHIM_DIND_NETNS_HOLDER_IMAGE"); v != "" {
		cfg.DINDNetnsHolderImage = v
	}
```

Add matching doc lines near line 171 in the comment block:

```go
//   - AI_SHIM_NETNS_MODE — netns owner: agent|dind|holder (default holder)
//   - AI_SHIM_DIND_NETNS_HOLDER_IMAGE — image for the holder (default: DIND image)
```

- [ ] **Step 5: Edit `internal/config/sources.go`**

Replace lines 98-100 (`if cfg.DINDSharedNetns != nil { ... }`) with:

```go
		if cfg.NetnsMode != "" {
			sources.Fields["netns_mode"] = name
		}
		if cfg.DINDNetnsHolderImage != "" {
			sources.Fields["dind_netns_holder_image"] = name
		}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'TestMerge_NetnsModeLastWins|TestMerge_HolderImageLastWins|TestResolver_NetnsModeEnv' -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/config/resolver.go internal/config/sources.go internal/config/merge.go internal/config/merge_test.go internal/config/resolver_test.go
git commit -m "feat(config): wire netns_mode env, source-tracking, and merge"
```

---

### Task 3: Config validation

**Files:**
- Modify: `internal/config/validate.go` (lines 65-67 ports warning; add mode validation)
- Test: `internal/config/validate_test.go` (replace lines 34-51 shared-netns tests)

**Interfaces:**
- Consumes: `Config.GetNetnsMode`, `Config.IsNetnsShared`, `Config.IsDINDEnabled` (Tasks 1).

- [ ] **Step 1: Write the failing tests**

In `internal/config/validate_test.go`, replace `TestValidate_SharedNetnsWithPortsWarns` and `TestValidate_SharedNetnsOffWithPortsNoWarn` with:

```go
func TestValidate_NetnsSharedWithPortsWarns(t *testing.T) {
	cfg := config.Config{DIND: testutil.BoolPtr(true), NetnsMode: "holder", Ports: []string{"8080:80"}}
	warns := config.Validate(cfg) // match the existing Validate signature used by neighbors
	found := false
	for _, e := range warns {
		if strings.Contains(e, "published ports are ignored") {
			found = true
		}
	}
	assert.True(t, found, "expected ports-ignored warning under shared netns")
}

func TestValidate_NetnsAgentWithPortsNoWarn(t *testing.T) {
	cfg := config.Config{DIND: testutil.BoolPtr(true), NetnsMode: "agent", Ports: []string{"8080:80"}}
	for _, e := range config.Validate(cfg) {
		assert.NotContains(t, e, "published ports are ignored")
	}
}

func TestValidate_InvalidNetnsModeErrors(t *testing.T) {
	cfg := config.Config{DIND: testutil.BoolPtr(true), NetnsMode: "bogus"}
	err := config.ValidateStrict(cfg) // match the file's error-returning validator
	require.Error(t, err)
	assert.Contains(t, err.Error(), "netns_mode")
}

func TestValidate_SharedNetnsRequiresDIND(t *testing.T) {
	cfg := config.Config{DIND: testutil.BoolPtr(false), NetnsMode: "holder"}
	err := config.ValidateStrict(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires DIND")
}
```

> Note: `validate.go` currently returns warnings (`[]string`) at line 65. Determine whether hard errors go through a separate returning path. If the package has only a warnings channel, make the two error cases append to warnings and assert via the warnings slice instead — but prefer a hard error if an error path exists. Match the real API before finalizing the test.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run 'TestValidate_Netns|TestValidate_SharedNetnsRequiresDIND|TestValidate_InvalidNetnsMode' -v`
Expected: FAIL (compile: old test names / `DINDSharedNetns` gone; new validation absent).

- [ ] **Step 3: Edit `internal/config/validate.go`**

Replace the ports-warning block (lines 65-67) with the reworded, mode-based version:

```go
	if c.IsDINDEnabled() && c.IsNetnsShared() && len(c.Ports) > 0 {
		warnings = append(warnings, "netns_mode shares the network namespace and ports are set: published ports are ignored under a shared netns (they must be published on the netns owner; set netns_mode: agent to publish them from the agent)")
	}
```

Add mode validation in the error-returning validator (mirror how the file rejects other bad enum values, e.g. `security_profile`/`network_scope`):

```go
	switch c.NetnsMode {
	case "", NetnsModeAgent, NetnsModeDIND, NetnsModeHolder:
		// valid
	default:
		return fmt.Errorf("invalid netns_mode %q: must be agent, dind, or holder", c.NetnsMode)
	}
	if c.NetnsMode != "" && c.NetnsMode != NetnsModeAgent && !c.IsDINDEnabled() {
		return fmt.Errorf("netns_mode %q requires DIND to be enabled", c.NetnsMode)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run 'TestValidate_Netns|TestValidate_SharedNetnsRequiresDIND|TestValidate_InvalidNetnsMode' -v`
Expected: PASS.

- [ ] **Step 5: Verify the whole config package compiles clean**

Run: `go test ./internal/config/...`
Expected: PASS (all config tests green; no lingering `DINDSharedNetns` references).

- [ ] **Step 6: Commit**

```bash
git add internal/config/validate.go internal/config/validate_test.go
git commit -m "feat(config): validate netns_mode enum and DIND requirement"
```

---

### Task 4: Extract `dockerdArgs` and `BuildNetnsExtraHosts` in dind

This is a pure refactor that also exposes the extra-hosts logic so the holder (Task 6) and main (Task 7) can reuse it. No behavior change.

**Files:**
- Modify: `internal/dind/dind.go` (extract from the Cmd-building block ~lines 125-133 and the ExtraHosts block ~lines 197-206)
- Test: `internal/dind/dind_test.go`

**Interfaces:**
- Produces: `dockerdArgs(cfg Config) []string` (unexported, pure); `BuildNetnsExtraHosts(ctx context.Context, cli *client.Client, networkID, cacheAddr string) []string` (exported).

- [ ] **Step 1: Write the failing test**

Add to `internal/dind/dind_test.go`:

```go
func TestDockerdArgs(t *testing.T) {
	assert.Nil(t, dockerdArgs(Config{}), "no mirrors, no cache, no socket gid -> nil")

	got := dockerdArgs(Config{CacheAddr: "http://cache:5000", Mirrors: []string{"https://m1"}, SocketGID: 2000})
	assert.Equal(t, []string{
		"--registry-mirror=http://cache:5000",
		"--registry-mirror=https://m1",
		"--group=2000",
	}, got, "cache mirror first, then mirrors, then --group")
}

func TestBuildNetnsExtraHosts_NoCache(t *testing.T) {
	assert.Equal(t, []string{"host.docker.internal:host-gateway"},
		BuildNetnsExtraHosts(context.Background(), nil, "", ""))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dind/ -run 'TestDockerdArgs|TestBuildNetnsExtraHosts_NoCache' -v`
Expected: FAIL — `undefined: dockerdArgs` / `BuildNetnsExtraHosts`.

- [ ] **Step 3: Add the two functions to `internal/dind/dind.go`**

```go
// dockerdArgs builds the leading-dash Cmd args passed to the DIND entrypoint:
// registry mirrors (cache first, highest priority), then --group so the socket
// is created with the agent's group on every daemon start (survives restart).
func dockerdArgs(cfg Config) []string {
	var cmd []string
	if cfg.CacheAddr != "" {
		cmd = append(cmd, "--registry-mirror="+cfg.CacheAddr)
	}
	for _, mirror := range cfg.Mirrors {
		cmd = append(cmd, "--registry-mirror="+mirror)
	}
	if cfg.SocketGID != 0 {
		cmd = append(cmd, "--group="+strconv.Itoa(cfg.SocketGID))
	}
	return cmd
}

// BuildNetnsExtraHosts returns the /etc/hosts entries the netns owner needs.
// In shared-netns modes these live on the owner (DIND or the holder) and are
// inherited by every joiner. Always maps host.docker.internal; adds the
// registry-cache alias pointing at the bridge gateway when a cache is set.
func BuildNetnsExtraHosts(ctx context.Context, cli *client.Client, networkID, cacheAddr string) []string {
	hosts := []string{"host.docker.internal:host-gateway"}
	if cacheAddr != "" && networkID != "" {
		if gwIP, _ := networkGatewayIP(ctx, cli, networkID); gwIP != "" {
			hosts = append(hosts, CacheHostAlias+":"+gwIP)
		}
	}
	return hosts
}
```

Now replace the inline Cmd block (~lines 125-133) with `cmd := dockerdArgs(cfg)` and replace the inline ExtraHosts block (~lines 197-206) with `extraHosts := BuildNetnsExtraHosts(ctx, cli, cfg.NetworkID, cfg.CacheAddr)`. Ensure `strconv` is imported (it already is — used for CPU parsing).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dind/ -run 'TestDockerdArgs|TestBuildNetnsExtraHosts_NoCache' -v`
Expected: PASS.

- [ ] **Step 5: Confirm no behavior drift**

Run: `go test ./internal/dind/ -short`
Expected: PASS (existing short tests still green).

> Behavior change to note for review: `dockerdArgs` now appends `--group=<gid>` whenever `SocketGID != 0`. This is intended (durable socket group) and is exercised end-to-end in Task 8. The one-shot post-start `chgrp` in `Start` stays as a fallback.

- [ ] **Step 6: Commit**

```bash
git add internal/dind/dind.go internal/dind/dind_test.go
git commit -m "refactor(dind): extract dockerdArgs and BuildNetnsExtraHosts, add durable --group"
```

---

### Task 5: DIND `JoinNetns` + `AutoRestart`

**Files:**
- Modify: `internal/dind/dind.go` (`Config` struct ~lines 48-79; `Start` container/host config build ~lines 168-233)
- Test: `internal/dind/dind_test.go`

**Interfaces:**
- Consumes: `dockerdArgs`, `BuildNetnsExtraHosts` (Task 4).
- Produces: `Config.JoinNetns string`, `Config.AutoRestart bool`; when `JoinNetns != ""`, `Start` sets `NetworkMode = container:<JoinNetns>`, omits bridge attach, `ExtraHosts`, and `Hostname`; when `AutoRestart`, `Start` sets `RestartPolicy{Name: RestartPolicyUnlessStopped}`.

- [ ] **Step 1: Write the failing test**

Add to `internal/dind/dind_test.go` a pure test on a spec-builder helper. First we introduce the helper (Step 3); the test asserts its output:

```go
func TestDINDHostConfig_JoinNetnsAndRestart(t *testing.T) {
	// Bridge mode: NetworkMode is the network ID, ExtraHosts present, no restart.
	h := dindNetworkHostConfig(Config{NetworkID: "netabc", AutoRestart: false},
		[]string{"host.docker.internal:host-gateway"})
	assert.Equal(t, container.NetworkMode("netabc"), h.NetworkMode)
	assert.NotEmpty(t, h.ExtraHosts)
	assert.Empty(t, h.RestartPolicy.Name)

	// Join mode: NetworkMode is container:<id>, no ExtraHosts, restart set.
	j := dindNetworkHostConfig(Config{JoinNetns: "holderid", AutoRestart: true},
		[]string{"host.docker.internal:host-gateway"})
	assert.Equal(t, container.NetworkMode("container:holderid"), j.NetworkMode)
	assert.Nil(t, j.ExtraHosts)
	assert.Equal(t, container.RestartPolicyUnlessStopped, j.RestartPolicy.Name)
}

func TestDINDContainerHostname_ClearedWhenJoining(t *testing.T) {
	assert.Equal(t, "", dindHostname(Config{JoinNetns: "x", Hostname: "dind"}))
	assert.Equal(t, "dind", dindHostname(Config{Hostname: "dind"}))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dind/ -run 'TestDINDHostConfig_JoinNetnsAndRestart|TestDINDContainerHostname_ClearedWhenJoining' -v`
Expected: FAIL — `undefined: dindNetworkHostConfig` / `dindHostname`.

- [ ] **Step 3: Edit `internal/dind/dind.go`**

Add the two fields to `Config` (after `SocketGID`):

```go
	// JoinNetns, when set, makes the sidecar join the given container's
	// network namespace (NetworkMode = container:<id>) instead of attaching
	// to the bridge. Used in holder mode. Joining clears the bridge attach,
	// ExtraHosts, and Hostname (inherited from the netns owner).
	JoinNetns string
	// AutoRestart applies RestartPolicy unless-stopped so a dead daemon
	// (e.g. OOM) is restarted into its surviving netns.
	AutoRestart bool
```

Add two pure helpers:

```go
// dindHostname returns the hostname to set on the container config; a netns
// joiner must not set its own hostname (Docker rejects it), so it is cleared.
func dindHostname(cfg Config) string {
	if cfg.JoinNetns != "" {
		return ""
	}
	return cfg.Hostname
}

// dindNetworkHostConfig fills the network-related fields of the HostConfig.
// In join mode the sidecar shares the owner's netns (so no bridge, no
// ExtraHosts); otherwise it attaches to the bridge with the given ExtraHosts.
func dindNetworkHostConfig(cfg Config, extraHosts []string) *container.HostConfig {
	hc := &container.HostConfig{}
	if cfg.JoinNetns != "" {
		hc.NetworkMode = container.NetworkMode("container:" + cfg.JoinNetns)
	} else {
		hc.NetworkMode = container.NetworkMode(cfg.NetworkID)
		hc.ExtraHosts = extraHosts
	}
	if cfg.AutoRestart {
		hc.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyUnlessStopped}
	}
	return hc
}
```

Now rewire `Start`:
- Replace `containerCfg.Hostname: cfg.Hostname` with `Hostname: dindHostname(cfg)`.
- Compute `extraHosts := BuildNetnsExtraHosts(ctx, cli, cfg.NetworkID, cfg.CacheAddr)` (only meaningful in bridge mode; harmless otherwise).
- Replace the inline `hostCfg := &container.HostConfig{ Privileged: true, NetworkMode: ..., Mounts: mounts, ExtraHosts: extraHosts }` construction so it starts from `dindNetworkHostConfig(cfg, extraHosts)` and then sets `Privileged`, `Mounts`, and the existing Sysbox/GPU/KVM/Resources fields on that struct.

Concretely, the base construction becomes:

```go
	hostCfg := dindNetworkHostConfig(cfg, extraHosts)
	hostCfg.Privileged = true
	hostCfg.Mounts = mounts
```

(Sysbox flips `Privileged=false` + sets `Runtime` further down — leave that block as-is; it mutates `hostCfg`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/dind/ -run 'TestDINDHostConfig_JoinNetnsAndRestart|TestDINDContainerHostname_ClearedWhenJoining' -v`
Expected: PASS.

- [ ] **Step 5: Run the dind short suite**

Run: `go test ./internal/dind/ -short`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/dind/dind.go internal/dind/dind_test.go
git commit -m "feat(dind): support joining a netns owner and auto-restart policy"
```

---

### Task 6: Netns holder container

**Files:**
- Create: `internal/dind/holder.go`
- Test: `internal/dind/holder_test.go`

**Interfaces:**
- Consumes: `ai_container.Runner`, `container` (docker) types.
- Produces:
  - `type Holder struct { ... }`
  - `type HolderConfig struct { Image, Name, Hostname, NetworkID string; ExtraHosts []string; Labels map[string]string }`
  - `holderSpec(cfg HolderConfig) (*container.Config, *container.HostConfig)` (unexported, pure)
  - `StartHolder(ctx context.Context, runner *ai_container.Runner, cfg HolderConfig) (*Holder, error)`
  - `(*Holder).ContainerID() string`, `(*Holder).Stop(ctx context.Context) error`

- [ ] **Step 1: Write the failing test**

Create `internal/dind/holder_test.go`:

```go
package dind

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
)

func TestHolderSpec(t *testing.T) {
	cc, hc := holderSpec(HolderConfig{
		Image:      "docker:dind",
		Hostname:   "ai-shim-dind",
		NetworkID:  "netabc",
		ExtraHosts: []string{"host.docker.internal:host-gateway"},
		Labels:     map[string]string{"k": "v"},
	})
	assert.Equal(t, "docker:dind", cc.Image)
	assert.Equal(t, "ai-shim-dind", cc.Hostname)
	assert.Equal(t, []string{"sleep", "infinity"}, []string(cc.Entrypoint))
	assert.Equal(t, "v", cc.Labels["k"])
	assert.Equal(t, container.NetworkMode("netabc"), hc.NetworkMode)
	assert.Equal(t, []string{"host.docker.internal:host-gateway"}, hc.ExtraHosts)
	// The holder is never auto-restarted: a restart yields a new netns.
	assert.Empty(t, hc.RestartPolicy.Name)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/dind/ -run TestHolderSpec -v`
Expected: FAIL — `undefined: holderSpec` / `HolderConfig`.

- [ ] **Step 3: Create `internal/dind/holder.go`**

```go
package dind

import (
	"context"
	"fmt"

	ai_container "github.com/Zaephor/ai-shim/internal/container"
	"github.com/docker/docker/api/types/container"
)

// Holder is a minimal container that owns a network namespace shared by the
// agent and the DIND sidecar (both join it via container: network mode).
// Because the holder is a bare `sleep` process it effectively never dies, so
// the shared netns survives DIND death/restart. It is intentionally NOT given
// a restart policy: a restart would create a fresh netns, defeating the point.
type Holder struct {
	client      *ai_container.Runner
	containerID string
}

// HolderConfig configures the netns holder. Because container: joiners inherit
// the owner's /etc/hosts, /etc/resolv.conf, and hostname, all netns-scoped
// settings for the agent and DIND live here.
type HolderConfig struct {
	Image      string // must provide a `sleep` binary; caller resolves the default
	Name       string
	Hostname   string
	NetworkID  string
	ExtraHosts []string
	Labels     map[string]string
}

// holderSpec builds the container and host configs for the holder. Pure; no
// Docker calls, so it is unit-tested directly.
func holderSpec(cfg HolderConfig) (*container.Config, *container.HostConfig) {
	cc := &container.Config{
		Image:      cfg.Image,
		Hostname:   cfg.Hostname,
		Labels:     cfg.Labels,
		Entrypoint: []string{"sleep", "infinity"},
	}
	hc := &container.HostConfig{
		NetworkMode: container.NetworkMode(cfg.NetworkID),
		ExtraHosts:  cfg.ExtraHosts,
	}
	return cc, hc
}

// StartHolder pulls the image if needed, then creates and starts the holder.
// Readiness is simply "started": the netns exists once the container is
// running, and a sleep process needs no health probe.
func StartHolder(ctx context.Context, runner *ai_container.Runner, cfg HolderConfig) (*Holder, error) {
	if cfg.Image == "" {
		return nil, fmt.Errorf("netns holder image must not be empty")
	}
	cli := runner.Client()
	if err := runner.EnsureImage(ctx, cfg.Image); err != nil {
		return nil, fmt.Errorf("preparing netns holder image %s: %w", cfg.Image, err)
	}
	cc, hc := holderSpec(cfg)
	resp, err := cli.ContainerCreate(ctx, cc, hc, nil, nil, cfg.Name)
	if err != nil {
		return nil, fmt.Errorf("creating netns holder: %w", err)
	}
	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		_ = cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, fmt.Errorf("starting netns holder: %w", err)
	}
	return &Holder{client: runner, containerID: resp.ID}, nil
}

// ContainerID returns the holder's container ID (the netns join target).
func (h *Holder) ContainerID() string { return h.containerID }

// Stop force-removes the holder container.
func (h *Holder) Stop(ctx context.Context) error {
	return h.client.Client().ContainerRemove(ctx, h.containerID, container.RemoveOptions{Force: true})
}
```

> Confirm the `ai_container.Runner` accessor name for the docker client is `.Client()` (it is used that way throughout `dind.go`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/dind/ -run TestHolderSpec -v`
Expected: PASS.

- [ ] **Step 5: Vet + build the package**

Run: `go vet ./internal/dind/ && go build ./internal/dind/`
Expected: no output (success).

- [ ] **Step 6: Commit**

```bash
git add internal/dind/holder.go internal/dind/holder_test.go
git commit -m "feat(dind): add netns holder container"
```

---

### Task 7: Orchestrate per-mode launch in main

**Files:**
- Modify: `cmd/ai-shim/main.go` (the DIND block ~lines 1204-1356; the shared-netns block ~lines 1343-1356)
- Test: covered by Task 8 e2e (this task is wiring across an already-tested surface; no isolated unit test is added — `main` orchestration is validated end-to-end).

**Interfaces:**
- Consumes: `cfg.GetNetnsMode`, `cfg.UsesNetnsHolder`, `cfg.IsNetnsShared`, `cfg.DINDNetnsHolderImage` (Tasks 1); `dind.StartHolder`, `dind.HolderConfig`, `dind.BuildNetnsExtraHosts`, `dind.Config{JoinNetns, AutoRestart}` (Tasks 4-6); `(*dind.Holder).ContainerID/Stop`.

- [ ] **Step 1: Add holder creation before DIND start**

Inside `if cfg.IsDINDEnabled() {`, after the network is ensured and `cacheAddr` is resolved but **before** `dind.Start`, insert:

```go
		// In holder mode a dedicated container owns the shared netns; both the
		// agent and DIND join it, so a DIND death does not destroy the agent's
		// networking. The holder carries all netns-scoped config (bridge,
		// ExtraHosts, hostname) because container: joiners inherit them.
		var netnsHolder *dind.Holder
		var dindJoinTarget string
		if cfg.UsesNetnsHolder() {
			holderImage := cfg.DINDNetnsHolderImage
			if holderImage == "" {
				holderImage = dind.DefaultImage // reuse the DIND image (no extra pull)
			}
			holder, err := dind.StartHolder(ctx, runner, dind.HolderConfig{
				Image:      holderImage,
				Name:       spec.Name + "-netns",
				Hostname:   dindHostname,
				NetworkID:  netHandle.ID,
				ExtraHosts: dind.BuildNetnsExtraHosts(ctx, runner.Client(), netHandle.ID, cacheAddr),
				Labels:     dindHolderLabels(spec.Labels),
			})
			if err != nil {
				return 1, fmt.Errorf("starting netns holder: %w", err)
			}
			netnsHolder = holder
			dindJoinTarget = holder.ContainerID()
			defer func() {
				if detached {
					return // preserve holder for reattach
				}
				if err := netnsHolder.Stop(ctx); err != nil {
					fmt.Fprintf(os.Stderr, "ai-shim: warning: failed to stop netns holder: %v\n", err)
				}
			}()
		}
```

Add a small labels helper near the other helpers in `main.go` (holder gets a distinct role so `manage status`/`cleanup` see it):

```go
// dindHolderLabels copies the session labels and marks the container as the
// netns holder so status/cleanup queries can distinguish it.
func dindHolderLabels(base map[string]string) map[string]string {
	m := make(map[string]string, len(base)+1)
	for k, v := range base {
		m[k] = v
	}
	m[container.LabelRole] = "netns-holder"
	return m
}
```

> Confirm `container.LabelRole` is the exported label key used for `"agent"`/`"dind"` (it is referenced as `ai_container.LabelRole` in `dind.go`; in `main.go` the package is imported as `container`).

- [ ] **Step 2: Wire DIND to join the holder + auto-restart**

In the `dind.Start(ctx, runner, dind.Config{...})` literal, add:

```go
			JoinNetns:   dindJoinTarget, // "" in agent/dind modes -> bridge as before
			AutoRestart: cfg.IsNetnsShared(), // survive OOM in dind + holder modes
```

In holder mode `dindJoinTarget` is the holder ID; in agent/dind modes it is `""` so DIND attaches to the bridge exactly as today. `AutoRestart` is on for both shared modes (dind mode benefits too: DIND self-restarts, though the agent still can't recover a lost netns without a holder — documented).

- [ ] **Step 3: Replace the agent shared-netns block**

Replace the whole `if cfg.IsDINDSharedNetns() { ... }` block (lines 1343-1356) with a mode switch that points the agent at the correct netns owner:

```go
		// Point the agent at the netns owner. In dind mode it joins the DIND
		// sidecar; in holder mode it joins the holder; in agent mode it keeps
		// its own bridge netns. A container: joiner cannot attach a bridge, set
		// its own hostname, or publish ports, so clear those.
		var agentNetnsOwner string
		switch cfg.GetNetnsMode() {
		case config.NetnsModeDIND:
			agentNetnsOwner = sidecar.ContainerID()
		case config.NetnsModeHolder:
			agentNetnsOwner = netnsHolder.ContainerID()
		}
		if agentNetnsOwner != "" {
			spec.NetworkMode = "container:" + agentNetnsOwner
			spec.NetworkID = ""
			spec.Hostname = ""
			if len(spec.Ports) > 0 || len(spec.ExposedPorts) > 0 {
				fmt.Fprintf(os.Stderr, "ai-shim: warning: published ports are ignored under a shared netns (netns_mode=%s); set netns_mode: agent to publish ports from the agent\n", cfg.GetNetnsMode())
				spec.Ports = nil
				spec.ExposedPorts = nil
			}
		}
```

Ensure `config` is imported in `main.go` (it is — used throughout).

- [ ] **Step 4: Build**

Run: `go build ./...`
Expected: success. Fix any remaining references to the removed `IsDINDSharedNetns` / `DINDSharedNetns` symbols the compiler flags.

- [ ] **Step 5: Sanity run — agent mode (no holder, ports work as before)**

Run (requires Docker):
```bash
go run ./cmd/ai-shim manage doctor >/dev/null 2>&1 || true
AI_SHIM_NETNS_MODE=agent go run ./cmd/ai-shim run --help >/dev/null
```
Expected: no panic; help prints. (Full behavioral coverage is Task 9.)

- [ ] **Step 6: Commit**

```bash
git add cmd/ai-shim/main.go
git commit -m "feat: orchestrate netns_mode launch with holder and DIND auto-restart"
```

---

### Task 8: e2e — per-mode netns identity + DIND-death survival

**Files:**
- Delete: `test/e2e/dind_shared_netns_test.go`
- Create: `test/e2e/netns_mode_test.go`

**Interfaces:**
- Consumes: `dind.Start`, `dind.StartHolder`, `dind.Config{JoinNetns, AutoRestart}`, `network.EnsureNetwork`, `container.Runner`, `testutil` helpers (mirror the deleted file's setup).

- [ ] **Step 1: Remove the obsolete test**

Run: `git rm test/e2e/dind_shared_netns_test.go`

- [ ] **Step 2: Write the new e2e test**

Create `test/e2e/netns_mode_test.go`. Reuse the netns-inode comparison technique from the deleted file (`readlink /proc/1/ns/net` inside each container; equal inode == shared netns). Two tests:

```go
package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Zaephor/ai-shim/internal/container"
	"github.com/Zaephor/ai-shim/internal/dind"
	"github.com/Zaephor/ai-shim/internal/network"
	"github.com/Zaephor/ai-shim/internal/testutil"
	dtypes "github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// netnsInode returns the network-namespace inode of PID 1 in the container.
func netnsInode(t *testing.T, runner *container.Runner, ctx context.Context, id string) string {
	t.Helper()
	code, out, _, err := testutil.ExecCapture(ctx, runner, id, []string{"readlink", "/proc/1/ns/net"})
	require.NoError(t, err)
	require.Equal(t, 0, code)
	return strings.TrimSpace(string(out))
}

// TestNetnsMode_HolderSharesWithBoth proves the holder pattern: the agent
// stand-in and the DIND sidecar both share the holder's network namespace.
func TestNetnsMode_HolderSharesWithBoth(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow netns holder test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, dind.DefaultImage))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-holder-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(ctx) })

	holder, err := dind.StartHolder(ctx, runner, dind.HolderConfig{
		Image:     dind.DefaultImage,
		Name:      fmt.Sprintf("ai-shim-test-holder-h-%d", time.Now().UnixNano()),
		Hostname:  "holder",
		NetworkID: netHandle.ID,
		Labels:    labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Stop(context.Background()) })

	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-holder-d-%d", time.Now().UnixNano()),
		NetworkID:     netHandle.ID,
		JoinNetns:     holder.ContainerID(),
		AutoRestart:   true,
		Labels:        labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cctx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		_ = sidecar.Stop(cctx)
	})

	holderNs := netnsInode(t, runner, ctx, holder.ContainerID())
	dindNs := netnsInode(t, runner, ctx, sidecar.ContainerID())
	assert.Equal(t, holderNs, dindNs, "DIND must share the holder's netns")
}

// TestNetnsMode_HolderSurvivesDINDDeath is the load-bearing survival guarantee:
// killing DIND must NOT change the holder's netns, and DIND (unless-stopped)
// must come back sharing the same netns.
func TestNetnsMode_HolderSurvivesDINDDeath(t *testing.T) {
	testutil.SkipIfNoDocker(t)
	if testing.Short() {
		t.Skip("skipping slow netns survival test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	runner, err := container.NewRunner(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { runner.Close() })
	require.NoError(t, runner.EnsureImage(ctx, dind.DefaultImage))

	labels := map[string]string{container.LabelBase: "true"}
	netName := fmt.Sprintf("ai-shim-test-surv-%d", time.Now().UnixNano())
	netHandle, err := network.EnsureNetwork(ctx, runner.Client(), netName, labels)
	require.NoError(t, err)
	t.Cleanup(func() { _ = netHandle.Remove(ctx) })

	holder, err := dind.StartHolder(ctx, runner, dind.HolderConfig{
		Image: dind.DefaultImage, Name: fmt.Sprintf("ai-shim-test-surv-h-%d", time.Now().UnixNano()),
		NetworkID: netHandle.ID, Labels: labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = holder.Stop(context.Background()) })

	sidecar, err := dind.Start(ctx, runner, dind.Config{
		ContainerName: fmt.Sprintf("ai-shim-test-surv-d-%d", time.Now().UnixNano()),
		NetworkID:     netHandle.ID, JoinNetns: holder.ContainerID(), AutoRestart: true, Labels: labels,
	})
	require.NoError(t, err)
	t.Cleanup(func() {
		cctx, c := context.WithTimeout(context.Background(), 30*time.Second)
		defer c()
		_ = sidecar.Stop(cctx)
	})

	before := netnsInode(t, runner, ctx, holder.ContainerID())

	// Kill DIND hard (simulate OOM).
	require.NoError(t, runner.Client().ContainerKill(ctx, sidecar.ContainerID(), "KILL"))

	// The holder's netns must be unchanged immediately after DIND dies.
	after := netnsInode(t, runner, ctx, holder.ContainerID())
	assert.Equal(t, before, after, "holder netns must survive DIND death")

	// unless-stopped must bring DIND back sharing the same netns.
	require.Eventually(t, func() bool {
		insp, err := runner.Client().ContainerInspect(ctx, sidecar.ContainerID())
		if err != nil || !insp.State.Running {
			return false
		}
		return netnsInode(t, runner, ctx, sidecar.ContainerID()) == before
	}, 90*time.Second, 2*time.Second, "DIND should auto-restart into the surviving netns")
}
```

> `testutil.ExecCapture` — use whatever exec helper the repo already has (the deleted test used one to run `readlink`). If none is exported, add a thin helper in `internal/testutil` mirroring `dind.Sidecar.exec`, or use the docker client's exec API inline. Match existing patterns before finalizing.

- [ ] **Step 3: Run the new e2e tests**

Run: `go test ./test/e2e/ -run 'TestNetnsMode_' -v`
Expected: PASS (requires a working Docker daemon; skips under `-short`).

- [ ] **Step 4: Commit**

```bash
git add test/e2e/netns_mode_test.go
git commit -m "test(e2e): cover netns_mode holder sharing and DIND-death survival"
```

---

### Task 9: Docs + CHANGELOG breaking-change note

**Files:**
- Modify: `README.md` (any `dind_shared_netns` mention; DIND/netns section)
- Modify: `configs/` example YAML that references `dind_shared_netns`
- Modify: `CHANGELOG.md` (breaking-change entry)

- [ ] **Step 1: Find every stale reference**

Run: `grep -rn "dind_shared_netns\|DIND_SHARED_NETNS\|IsDINDSharedNetns" --include='*.go' --include='*.md' --include='*.yaml' --include='*.yml' .`
Expected: only test/impl already migrated in Tasks 1-8; the remaining hits are docs/config examples to fix. There must be **zero** hits in `.go` files after this step's edits.

- [ ] **Step 2: Update docs**

Replace `dind_shared_netns: true|false` references with `netns_mode: agent|dind|holder`, documenting: default `holder`; the survival guarantee; ports must be published on the netns owner (future); `agent` == old `false`, `dind` == old `true`.

- [ ] **Step 3: Add the CHANGELOG breaking-change note**

Add under the unreleased/next section (match the repo's release-please conventions):

```markdown
### ⚠ BREAKING CHANGES

* **dind:** `dind_shared_netns` (and `AI_SHIM_DIND_SHARED_NETNS`) are removed. Use `netns_mode` instead: `dind_shared_netns: true` → `netns_mode: dind`, `dind_shared_netns: false` → `netns_mode: agent`. The new default is `netns_mode: holder`, which runs a netns-holder container so the agent survives DIND death.
```

- [ ] **Step 4: Verify no stale references remain**

Run: `grep -rn "dind_shared_netns\|DIND_SHARED_NETNS\|IsDINDSharedNetns" . || echo "clean"`
Expected: `clean` (or only the CHANGELOG migration note that intentionally names the old key).

- [ ] **Step 5: Commit**

```bash
git add README.md configs/ CHANGELOG.md
git commit -m "docs: document netns_mode and note dind_shared_netns removal"
```

---

## Final Gate

- [ ] **Run the full local CI mirror**

Run: `make ci`
Expected: all sub-targets green (fmt-check, vet, tidy-check, silent-failures, test-race, fuzz, vuln).

- [ ] **Run the e2e job**

Run: `make e2e-ci` (or the exact e2e target the repo defines)
Expected: exit 0, including `TestNetnsMode_HolderSurvivesDINDDeath`.

Only after both are green is the work unit complete. Do not claim CI-green from partial local checks.

---

## Self-Review notes (author)

- **Spec coverage:** config surface → Tasks 1-3; topology/holder-anchor → Tasks 5-7; DIND survival (restart + `--group`) → Tasks 4-5, 7; lifecycle/teardown → Task 7; testing → Tasks 8; docs/CHANGELOG → Task 9. Future-enhancement items are intentionally not implemented.
- **Restart window:** per the spec correction, no ai-shim-side reconnect-retry task exists; it is documented future work (docker-CLI wrapper).
- **Type consistency:** accessor names `GetNetnsMode`/`IsNetnsShared`/`UsesNetnsHolder`, consts `NetnsMode{Agent,DIND,Holder}`, dind fields `JoinNetns`/`AutoRestart`, holder `StartHolder`/`HolderConfig`/`ContainerID`/`Stop` are used identically across Tasks 1-8.
- **Verify-before-final gaps flagged inline** (marked with `>` notes): exact resolver env-entrypoint name, `Validate` vs error-returning validator API, `testutil` exec helper name, `container.LabelRole` key. Each must be confirmed against the real code during execution rather than assumed.
