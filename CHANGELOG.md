# Changelog

## [0.5.0](https://github.com/Zaephor/ai-shim/compare/v0.4.1...v0.5.0) (2026-04-29)


### Features

* **cli:** move env vars to `help env` subcommand ([6ecaf67](https://github.com/Zaephor/ai-shim/commit/6ecaf67cceb96732ec4ec76194774436032d1772))

## [0.4.1](https://github.com/Zaephor/ai-shim/compare/v0.4.0...v0.4.1) (2026-04-29)


### Bug Fixes

* **dind:** reap orphaned networks on session teardown ([2561b6c](https://github.com/Zaephor/ai-shim/commit/2561b6c93060610b621995a61be2d7ff93325c64))

## [0.4.0](https://github.com/Zaephor/ai-shim/compare/v0.3.0...v0.4.0) (2026-04-28)


### Features

* **cli:** add workspace dir and dind columns to manage status ([982c2b7](https://github.com/Zaephor/ai-shim/commit/982c2b773b73dc34aab74684d2237d92833bc6ea))
* **container:** scope gsd project state to prevent orphan detection ([262c1dc](https://github.com/Zaephor/ai-shim/commit/262c1dc773b0c44cd8599ffd4ef431b61c060cdd))


### Bug Fixes

* **container:** exclude dind containers from reattach session lookup ([c07ac51](https://github.com/Zaephor/ai-shim/commit/c07ac513502c32f093fb9b9f81712becc6b36570))

## [0.3.0](https://github.com/Zaephor/ai-shim/compare/v0.2.0...v0.3.0) (2026-04-26)


### Features

* **cli:** add manage delete-profile for clean profile removal ([8472bb1](https://github.com/Zaephor/ai-shim/commit/8472bb18886ce4be653d0d4e381040e9f3f7e763))
* **cli:** add manage warm to pre-populate agent and tool caches ([29d67bb](https://github.com/Zaephor/ai-shim/commit/29d67bb624de5c4f74088a88383453f4cf0fb293))
* **cli:** document user-defined agents and add manage attach ([ab10f14](https://github.com/Zaephor/ai-shim/commit/ab10f14b7559b2dea080de34c1d8eb6b02f5d89a))
* **cli:** embed commit hash in dev builds via runtime/debug ([9bdd5cc](https://github.com/Zaephor/ai-shim/commit/9bdd5cccc09be606d3c8f953e8b4a68de9805935))
* **config:** load environment variables from a dotenv-style env_file ([571e823](https://github.com/Zaephor/ai-shim/commit/571e82367a71f9a1ffa6e4f377209c9feac8554c))
* **config:** preserve YAML order of mcp_servers through merge and injection ([a1ddcf3](https://github.com/Zaephor/ai-shim/commit/a1ddcf33c9880df741c59481f76371ccf4c98bb7))
* **config:** profile inheritance via extends field ([14871af](https://github.com/Zaephor/ai-shim/commit/14871af7d50736bc01d23ed775c18e14a5d4f9bc))
* **config:** provision profile tools in YAML declaration order ([09b347e](https://github.com/Zaephor/ai-shim/commit/09b347e86b108f177edb00b156e1f30afdc795e3))
* **container:** add FindSessionByContainerName for direct attach ([7cce7bb](https://github.com/Zaephor/ai-shim/commit/7cce7bbbb1c435da3fb53a5bdfb5d377354804fe))
* **container:** add KVM passthrough support ([39105c4](https://github.com/Zaephor/ai-shim/commit/39105c487c68a15e08919ea8aace4dbfe51be01f))
* **dind:** share agent workspace and cache dir with DIND sidecar ([d132e14](https://github.com/Zaephor/ai-shim/commit/d132e14141a2843f49815d07aad9b7356b6fd9c8))
* **install:** bracket provisioning with progress messages ([3d2a301](https://github.com/Zaephor/ai-shim/commit/3d2a301329574d4d265381b0954fbe5e1161b0ac))
* **make:** add dev target for hash-identified builds without version ldflags ([c3cf358](https://github.com/Zaephor/ai-shim/commit/c3cf358b5e15f3abfceee40f716a2b6a2c842857))
* **profiles/python:** add pre-commit as a uv-managed tool ([2df2aff](https://github.com/Zaephor/ai-shim/commit/2df2aff251c8a2171b8e61df81c77aff1c15902b))
* **profiles:** persist shell-function version managers via ~/.bashrc ([816ed9c](https://github.com/Zaephor/ai-shim/commit/816ed9ccfea07521158dd954aca915abf0aeb7ce))
* **runagent:** offer parallel session alongside existing one ([788b4e2](https://github.com/Zaephor/ai-shim/commit/788b4e227b9e5f835b77978697388090aa5a03e0))
* **runner:** persist container output on exit for post-mortem debugging ([e1ee3a8](https://github.com/Zaephor/ai-shim/commit/e1ee3a80239336d7fb105911fb80c8119df47343))


### Bug Fixes

* address static code review findings across 20 packages ([c468e7d](https://github.com/Zaephor/ai-shim/commit/c468e7d88e9dea9ef2bae7718c5c858a2bc3e824))
* **builder:** gate apt package install on root or sudo availability ([67fe17a](https://github.com/Zaephor/ai-shim/commit/67fe17adca996f49d8669a642232d96dde60378d))
* **cli:** clear cache markers on reinstall for custom install types ([6df2e78](https://github.com/Zaephor/ai-shim/commit/6df2e78f1d25d2334702ceb6a8fbda4b00a16453))
* **config:** merge tool definitions field-by-field ([7cb58cc](https://github.com/Zaephor/ai-shim/commit/7cb58cc55de3bcfee1893cd03a0ff5578a3fc171))
* **config:** warn when env_var is set without data_dir ([1a34085](https://github.com/Zaephor/ai-shim/commit/1a34085ca9685fc47c78a67486a7e68b0dbe8db4))
* **container:** accept caller-provided Pwd in BuildSpec ([2a63543](https://github.com/Zaephor/ai-shim/commit/2a635436373c2ddad4190edbc6db16c67ce9141b))
* **container:** add role label so picker excludes DIND and cache containers ([e41ad80](https://github.com/Zaephor/ai-shim/commit/e41ad80e80f84b316d213d58ecb62d9aedee3e86))
* **container:** fail the build when a tool's persistent cache dir cannot be created ([19f4d4e](https://github.com/Zaephor/ai-shim/commit/19f4d4e000b038c5e81b473f30a70daad792d4e4))
* **dind:** resolve registry cache on Linux, add version label, remove unused cache bind ([686d180](https://github.com/Zaephor/ai-shim/commit/686d180c34e1e93954a9428f1c1949c088ea97d2))
* gofmt drift and errcheck/staticcheck lint findings ([0dd0dc6](https://github.com/Zaephor/ai-shim/commit/0dd0dc64d020d63c36b76d2a0e467f243dee27bb))
* **install:** cache custom install-type by update interval ([87830f9](https://github.com/Zaephor/ai-shim/commit/87830f945bf87c27a19c2e281e20eb5b468b6b63))
* **profiles:** add idempotency guards and pin unstable URLs ([060d38b](https://github.com/Zaephor/ai-shim/commit/060d38b45198afaecb987efa6ff584b3ac3fa54a))
* **provision:** propagate errors from go-install tool type ([f811160](https://github.com/Zaephor/ai-shim/commit/f811160d54206162ad3f49f01609e69c547ee837))
* **runagent:** tighten session lifecycle and propagate tool caches to DIND ([511abc6](https://github.com/Zaephor/ai-shim/commit/511abc6cab4efb1e203db50c218fb267748e78d3))
* **runner:** reset terminal on reattach and force inner TUI redraw ([2213548](https://github.com/Zaephor/ai-shim/commit/2213548c77bb29e13eaa081f6f0568250f7350fe))
* **workspace:** revert hash input separator to preserve existing workspaces ([eda308f](https://github.com/Zaephor/ai-shim/commit/eda308f11e83a87291ca644dad5c44842cdf2c8a))

## [0.2.0](https://github.com/Zaephor/ai-shim/compare/v0.1.0...v0.2.0) (2026-04-12)


### Features

* **agents:** add GitHub Copilot CLI as built-in agent ([0e70c6c](https://github.com/Zaephor/ai-shim/commit/0e70c6ca32ed04af5736d1391efd7fe69082f93d))
* **cli:** default symlink dir to ~/.local/bin with config override ([c4acf45](https://github.com/Zaephor/ai-shim/commit/c4acf45f97a5fb81ba19df94b4959b7d626cdb8d))
* **container:** add session detach/reattach support ([de59ef8](https://github.com/Zaephor/ai-shim/commit/de59ef8150df889d50863d0ac562db9c8d63dd07))
* **selfupdate:** configurable repo, version injection, prerelease ([20a078d](https://github.com/Zaephor/ai-shim/commit/20a078d6d1c9c156a10d42f91996079af70ba585))
* **tools:** add data_dir, cache_scope, and env_var for persistent tool directories ([61174cd](https://github.com/Zaephor/ai-shim/commit/61174cd35f120c3ecd1706439636c7bc61256d19))


### Bug Fixes

* **ci:** chain goreleaser from release-please, upload only missing assets ([aa611d6](https://github.com/Zaephor/ai-shim/commit/aa611d63e575886233622256a4719532dfd68ffa))
* **cli:** surface errors from container stop, inspect, and cleanup paths ([ce2d136](https://github.com/Zaephor/ai-shim/commit/ce2d136b611bff20bb6a1faf2797e047c55f8785))
* **cli:** warn on custom agent parse errors, fix ShowConfig and NetworkRemove ([4a629eb](https://github.com/Zaephor/ai-shim/commit/4a629eb019353347e2a3244c77e73e17fd213197))
* **container:** attach before start to avoid fast-exit hang ([54d1bf1](https://github.com/Zaephor/ai-shim/commit/54d1bf1c72bb72ed2a94753bf6b843494862e41b))
* **container:** prevent detachCh double-close panic and reject invalid resource limits ([d37e1b0](https://github.com/Zaephor/ai-shim/commit/d37e1b078bf5ff321e2b489c0d57bf4ca02ff4d9))
* **container:** wait for removal on non-persistent containers ([44b5152](https://github.com/Zaephor/ai-shim/commit/44b5152ce6fd9597bf34247c1f4bf6e3753e44f7))
* **container:** warn on saveExitLog/parsePorts/signal errors, document stdin leak ([1123740](https://github.com/Zaephor/ai-shim/commit/112374069180fb7a27b0875b7eba753dbe2d07a4))
* **dind:** chgrp socket to agent GID and drain exec before inspect ([58b601b](https://github.com/Zaephor/ai-shim/commit/58b601bf571dd4936f45be943110bf7ac81172a9))
* **dind:** pull registry cache image before starting container ([26826cc](https://github.com/Zaephor/ai-shim/commit/26826cc05ce94ceae67d6825ea35ede461bb68dd))
* **dind:** TLS cert permissions, volume cleanup, resource limit errors ([f1190d4](https://github.com/Zaephor/ai-shim/commit/f1190d4c219a33b646a87e1a88993c1130300497))
* **install:** fix uv PATH ordering causing reinstall every launch ([aad90e6](https://github.com/Zaephor/ai-shim/commit/aad90e6ac4220776fddbc2971ccece9ddf69cf24))
* **selfupdate:** extract binary from tar.gz archive ([51e752b](https://github.com/Zaephor/ai-shim/commit/51e752ba13d41494375720ce6d097c155283677b))

## 0.1.0 (2026-04-09)


### Features

* add agent-versions and reinstall manage subcommands ([6c5e991](https://github.com/Zaephor/ai-shim/commit/6c5e99138c4046c1593fd1557b347abe9aeafb6c))
* add AGPL-3.0 license ([6b4baa9](https://github.com/Zaephor/ai-shim/commit/6b4baa9aaa9555c90cd2544215bf8070da91dc4a))
* add colorized terminal output for status and doctor commands ([268289b](https://github.com/Zaephor/ai-shim/commit/268289b3c9a198a76c7853d013fda2adf3c4c649))
* add config profile switching with current-profile fallback ([58dbf65](https://github.com/Zaephor/ai-shim/commit/58dbf65076160466a40424f6084df2fd072e3694))
* add config source annotation to ShowConfig output ([4477492](https://github.com/Zaephor/ai-shim/commit/44774921d4197eae2aed1eee932ba13472d8bb37))
* add configurable egress firewall rules for containers ([8a404af](https://github.com/Zaephor/ai-shim/commit/8a404afba32ce5e8222cf2f90e84e1bcf9f03d04))
* add configurable home directory isolation per agent ([b71df3c](https://github.com/Zaephor/ai-shim/commit/b71df3cb2b5d652b1ffd4ccf752e8de1baf6fe59))
* add container exec command for running commands in ai-shim containers ([7a19784](https://github.com/Zaephor/ai-shim/commit/7a19784aab75f367ab6a1ef094196765fd90fe2c))
* add custom agent definitions from config YAML ([c6d38e6](https://github.com/Zaephor/ai-shim/commit/c6d38e63c3bf48ffa8345c1f82996a3aaf7b1db4))
* add git user config for container commits ([c6b1e84](https://github.com/Zaephor/ai-shim/commit/c6b1e841256a4cfcf58717ba5c8b5e7c0a275b75))
* add image digest pinning support ([b882f4f](https://github.com/Zaephor/ai-shim/commit/b882f4f99573baf8d68f6772b6d7061eba235d77))
* add JSON output for management commands via AI_SHIM_JSON=1 ([5142a73](https://github.com/Zaephor/ai-shim/commit/5142a73badbbd53ddc20562b91502b9df985fe58))
* add MCP server configuration support ([37e0b28](https://github.com/Zaephor/ai-shim/commit/37e0b28db0854913aae188f11b46fa5264e7a129))
* add one-off agent launch and automatic exit logging to stderr ([b0f6b63](https://github.com/Zaephor/ai-shim/commit/b0f6b634d85fc1eecf07ff4a173ab81fad1df74e))
* add resource limits, self-update, first-run, status, logging, and validation ([14b32f1](https://github.com/Zaephor/ai-shim/commit/14b32f1c4fdf4cef13c2320faadea32d335cbc3e))
* add security profiles and validation for network rules ([6ae8029](https://github.com/Zaephor/ai-shim/commit/6ae802928aee8dbba959fe36fadda8cc578953cb))
* add standardized container naming and configurable DIND hostname ([d59a3a4](https://github.com/Zaephor/ai-shim/commit/d59a3a48830e0b81bb4583fd1760ec6d0f503f2f))
* add watch mode for automatic agent restart on crash ([c4fe8f1](https://github.com/Zaephor/ai-shim/commit/c4fe8f123286ed0643cbb8d7973779917a535462))
* **agent:** add built-in agent registry with 8 agents ([ad19d2b](https://github.com/Zaephor/ai-shim/commit/ad19d2bb3aef7302256c684e770ee1ef4e2b02ab))
* **cli:** add bash and zsh shell completion scripts ([508e19e](https://github.com/Zaephor/ai-shim/commit/508e19e48c2326b64e05f3c551824e58dc2a276e))
* **cli:** add management commands (agents, profiles, config, doctor) ([c662e6e](https://github.com/Zaephor/ai-shim/commit/c662e6ec5c49019ee6cc8b2c13c5e91dbf2ef6bf))
* **cli:** add profile backup/restore and disk usage reporting ([8112666](https://github.com/Zaephor/ai-shim/commit/8112666c4c3f78ce6a6794441eb632d9e63b9ed2))
* **cli:** add symlinks, dry-run, cleanup commands and complete update ([d23ffff](https://github.com/Zaephor/ai-shim/commit/d23ffff04e9e1b7274a8099401c04fce7a20a0d5))
* **cli:** seed example configs during ai-shim init ([95667d1](https://github.com/Zaephor/ai-shim/commit/95667d1c73cfe14d8cb3358af32a3d54737ea0a8))
* **config:** add 5-tier config merge engine ([b4af9c2](https://github.com/Zaephor/ai-shim/commit/b4af9c2016e1fcb47cc40bbbdef073d9239ebc5b))
* **config:** add 5-tier resolver with env var overrides ([839c572](https://github.com/Zaephor/ai-shim/commit/839c5725240286757f47a0943246e1179e9716bb))
* **config:** add config types and YAML file loader ([1578191](https://github.com/Zaephor/ai-shim/commit/1578191bccd66a7433f9c6be248995894e7e3cab))
* **config:** add template resolution for variables ([ab2bc87](https://github.com/Zaephor/ai-shim/commit/ab2bc87d89ed6c92b492b56750f6dbac6130377f))
* **config:** add version pinning and update_interval for agents ([278900d](https://github.com/Zaephor/ai-shim/commit/278900dc006f83988ef9dbc19055b95d253d208d))
* **config:** warn on unknown YAML config keys ([72d3e49](https://github.com/Zaephor/ai-shim/commit/72d3e492463375b262544379cc93b6c64bda43c5))
* **container:** add container spec builder from resolved config ([8d72026](https://github.com/Zaephor/ai-shim/commit/8d72026e8e0a246b06a0c7b08d4e3307e669a8ae))
* **container:** add cross-agent mount generation for agent isolation ([fb7e1b2](https://github.com/Zaephor/ai-shim/commit/fb7e1b209554ea93fb3bcdb2857d9b0a8ac0a870))
* **container:** add Docker container runner with lifecycle management ([c6c9e35](https://github.com/Zaephor/ai-shim/commit/c6c9e354b68585f117cad1b02f54f978c290404a))
* **container:** add image pre-pull and improve Docker error messages ([88b494f](https://github.com/Zaephor/ai-shim/commit/88b494f70c92e2b18e4b1e86b04fc523d825e8cd))
* **container:** detect home directory from Docker image inspection ([94dfbce](https://github.com/Zaephor/ai-shim/commit/94dfbce6f88690eed00d91e17136546315545a61))
* **dind:** add DIND sidecar lifecycle management ([1c858d5](https://github.com/Zaephor/ai-shim/commit/1c858d55df12768f1d353c8b8fe0947820ef0b85))
* **dind:** add health check to wait for DIND daemon readiness ([b6e0f93](https://github.com/Zaephor/ai-shim/commit/b6e0f938b68cd9fe62e41271b3798c627c9aee62))
* **dind:** add optional TLS support for DIND socket communication ([0fa8a0e](https://github.com/Zaephor/ai-shim/commit/0fa8a0eacc51affb864565e007cc6062120ab239))
* **dind:** add registry mirrors and reference-counted pull-through cache ([f8394c5](https://github.com/Zaephor/ai-shim/commit/f8394c579286bed59c1954ddc7b4ba08435ab9f1))
* enhance status command and add real agent E2E tests ([0383eb8](https://github.com/Zaephor/ai-shim/commit/0383eb80a5d4f971e9a4c309aee32813e853efd7))
* expand config validation, SECURITY.md, README overhaul ([1c2bdc7](https://github.com/Zaephor/ai-shim/commit/1c2bdc7bd2d03a74ac05f70b9130ec9c35208ad6))
* **install:** add entrypoint script generator for agent install/launch ([df713bc](https://github.com/Zaephor/ai-shim/commit/df713bcb3d01621489da91b032a5a5caea060e24))
* **install:** cache agent installs in persistent bind mounts ([c4a4c36](https://github.com/Zaephor/ai-shim/commit/c4a4c36b35bfd72ae97a00f39a9b6fbbfa30b8db))
* **invocation:** add symlink name parsing with underscore delimiter ([c1bc05d](https://github.com/Zaephor/ai-shim/commit/c1bc05d7925c99ef13e0068414d07409f6bf543c))
* **network:** add configurable network scopes and fix agent-DIND connectivity ([5f275b2](https://github.com/Zaephor/ai-shim/commit/5f275b20bc9bbda26ea3bd4b78934d0b70eb40c6))
* **platform:** add NVIDIA GPU detection for Linux ([a7395b1](https://github.com/Zaephor/ai-shim/commit/a7395b12b3aedd50a11f090ef57be7d4d76018e3))
* **platform:** add platform detection for socket, UID, hostname ([3cd4577](https://github.com/Zaephor/ai-shim/commit/3cd4577951410c817fba661c56d01dd127c78f0e))
* **provision:** add tool provisioning with typed installers ([b44cc3d](https://github.com/Zaephor/ai-shim/commit/b44cc3ddc77de26cfe8d66bf5ca990c86056c14f))
* **security:** add path validation and safe directory checks ([8d97b0b](https://github.com/Zaephor/ai-shim/commit/8d97b0b8ebf77fcbfc0c543b55c62403a5af9809))
* **security:** add secret masking with pattern-based detection ([dc56208](https://github.com/Zaephor/ai-shim/commit/dc562087ed575460244dd7d48b4347779deb70b3))
* **selfupdate:** add self-update version checking and asset resolution ([abd5818](https://github.com/Zaephor/ai-shim/commit/abd5818ae98ec7acdb27da9bd7e4a6985951c125))
* **storage:** add storage layout manager with path resolution ([feb31de](https://github.com/Zaephor/ai-shim/commit/feb31de0ee7ee0a170c2b695813ffbb828e1ec1d))
* UX improvements — profiles, logging, manage logs command ([72478b8](https://github.com/Zaephor/ai-shim/commit/72478b89a3cc45ab0556877752c75f6cbdc04676))
* wire main entrypoint to full container launch flow ([66bacab](https://github.com/Zaephor/ai-shim/commit/66bacabfdf25a395dda8a9a071e0911314a1d5a2))
* wire up main entrypoint with direct vs symlink detection ([72a76ff](https://github.com/Zaephor/ai-shim/commit/72a76ff52c387f22ba3f8bd16fc7783fcba4cb2b))
* **workspace:** add deterministic path hashing for container workdir ([0b08075](https://github.com/Zaephor/ai-shim/commit/0b08075fbb2d4ad7bdeabf68a0f39b6825abe24f))


### Bug Fixes

* add opencode agent, fix mount path consistency and cross-agent logic ([42e70b4](https://github.com/Zaephor/ai-shim/commit/42e70b491e9b119becd6231d5942a6903cc3cd09))
* CI modernization — Node 24 actions, go-version-file, license enforcement ([24feb0c](https://github.com/Zaephor/ai-shim/commit/24feb0c243d2a9bb13f79f2b6ccf0167cbfca8a7))
* **ci:** auto-skip unfixable govulncheck findings ([7db2161](https://github.com/Zaephor/ai-shim/commit/7db21612511838bd03e859162e9893704c360ac8))
* **ci:** e2e-macos best-effort with longer timeout ([8f0b134](https://github.com/Zaephor/ai-shim/commit/8f0b134fa0369372810cdcf44d81c4c5319159c0))
* **ci:** filter known Docker SDK vulns in govulncheck ([f8aa7dc](https://github.com/Zaephor/ai-shim/commit/f8aa7dc50f3f6021beee6ec374970f0925414e5c))
* **ci:** fix Go version mismatch, add build verification and quality checks ([2658186](https://github.com/Zaephor/ai-shim/commit/26581863d3840eef768742cd94af9c3ffc74ad97))
* **ci:** fix gofmt, cross-platform build, and linter compatibility ([94b2ffa](https://github.com/Zaephor/ai-shim/commit/94b2ffa6d128499b01efd2739a29a31567bec0bc))
* **ci:** Go 1.25.8, fuzz overflow fix, GoReleaser v2, Colima socket, coverage boost ([a9dee79](https://github.com/Zaephor/ai-shim/commit/a9dee79564e8c0bffb137f0621d071bd3b8c8ef3))
* **ci:** unblock e2e and e2e-macos ([c1e2028](https://github.com/Zaephor/ai-shim/commit/c1e2028f78ae99dbd76f1f2f7aa1b21e37419e94))
* **cli:** display dind_gpu in ShowConfig/DryRun, remove dead DINDSocketVolume field ([755a4ce](https://github.com/Zaephor/ai-shim/commit/755a4cee87b4e1d9d95f14e48161d57e75769220))
* comprehensive review fixes — tests, merge, security, macOS CI ([233d6c3](https://github.com/Zaephor/ai-shim/commit/233d6c32d648c4ec593d90267b3f5a78fa474f27))
* **container:** add signal forwarding to container process ([19aa096](https://github.com/Zaephor/ai-shim/commit/19aa0967c6b4362e039348bdb878a917cc3c9bc8))
* **container:** add TTY raw mode and terminal resize handling ([ac8f736](https://github.com/Zaephor/ai-shim/commit/ac8f7365522b11a8ceb7b0f15a6d3c9fd2f68949))
* **container:** harden builder with validation, warnings, and error handling ([52a262b](https://github.com/Zaephor/ai-shim/commit/52a262b449feec48b9bc65a9f25e11e98e4eb8db))
* **container:** pass through host TERM and COLORTERM to container ([de482f1](https://github.com/Zaephor/ai-shim/commit/de482f1697370d26d94dad546fdd2695ebcf58e1))
* **container:** wire tool provisioning, volumes, cross-agent, and packages into builder ([ff1338e](https://github.com/Zaephor/ai-shim/commit/ff1338e7707de1aab768b2e2e4bf8a3e5af52d50))
* **deps:** bump go directive to 1.25.9 for stdlib CVE fixes ([345dd39](https://github.com/Zaephor/ai-shim/commit/345dd3962ba9fe2b01be18242e28dda709580584))
* **dind:** cache on host network via host.docker.internal, prioritize as first mirror ([4343ee9](https://github.com/Zaephor/ai-shim/commit/4343ee993fef6f45432421ed63dbf29b977f31b1))
* **dind:** fix cache test for DooD environments ([43b4d7f](https://github.com/Zaephor/ai-shim/commit/43b4d7f7e890748c5535ff0834974706568c6e94))
* **e2e:** container HOME env, context stop, macOS Intel runner ([60c5766](https://github.com/Zaephor/ai-shim/commit/60c5766795731e565a8e556364e82e4421489248))
* **e2e:** pre-create agent data dirs before BuildSpec in tests ([eb17b80](https://github.com/Zaephor/ai-shim/commit/eb17b80dffc034625fdcf4c61a60c10f23541d67))
* eliminate all silent failure patterns across install, provision, and CLI ([9d2c756](https://github.com/Zaephor/ai-shim/commit/9d2c756a5f3b0a83490a0bc625f0bed8bfca2b68))
* eliminate silent failures in error handling across multiple packages ([b8e5ee8](https://github.com/Zaephor/ai-shim/commit/b8e5ee840a88dbccd25cf9c56dc99fc7aa31ef28))
* extend cleanup for networks/volumes, add image check to doctor, document resources ([1b99abe](https://github.com/Zaephor/ai-shim/commit/1b99abe668422ef50e88acdfc18433932d8965b3))
* goroutine leak, HTTP timeouts, CI supply chain hardening ([af6c155](https://github.com/Zaephor/ai-shim/commit/af6c155612f15f806accc316886fe52c1d21ea1a))
* handle rand.Read errors, os.Getwd errors, standardize hash length ([0723fc4](https://github.com/Zaephor/ai-shim/commit/0723fc4352aeeb434bef2694b10faf237aade716))
* harden edge cases in storage, network, and cache ([13a2241](https://github.com/Zaephor/ai-shim/commit/13a2241dbe4ec04e755874686e5b4288518847c4))
* heavy review round — concurrency, validation, errors, robustness ([d166e40](https://github.com/Zaephor/ai-shim/commit/d166e40237e2af6cfedfbefacff81d233d496781))
* **install:** bootstrap uv and harden custom installer entrypoints ([fda8ded](https://github.com/Zaephor/ai-shim/commit/fda8ded813fe7014818319b7467007fa16702a66))
* **platform:** resolve merge conflicts from GPU detection integration ([9799297](https://github.com/Zaephor/ai-shim/commit/9799297c0af5fba4c97d9aa083e2069521821750))
* profile home mount, UpdateInterval merge, macOS test skip ([b6a053f](https://github.com/Zaephor/ai-shim/commit/b6a053f3eec7fdc0eda931cd8b5fbb5e185c69f4))
* resolve all lint errors and pre-existing test failures ([f637746](https://github.com/Zaephor/ai-shim/commit/f63774617500f077bc9da023d30103e915618425))
* review round 2 — CI timeouts, env masking, docs gaps ([5520e8f](https://github.com/Zaephor/ai-shim/commit/5520e8ff20a6a274dc6a69da6fc225132fbdc66b))
* **security:** prevent shell injection and path traversal vulnerabilities ([d7d3c5d](https://github.com/Zaephor/ai-shim/commit/d7d3c5de3f9b88974ad6c3bb04fb093394cd69ca))
* **security:** quote Binary field in entrypoint exec command ([b5db92d](https://github.com/Zaephor/ai-shim/commit/b5db92de468600fd5197c95a9aa491f916b7d2b3))
* **security:** quote Package/Version in entrypoint, validate checksums ([e9550d3](https://github.com/Zaephor/ai-shim/commit/e9550d3a79c4768c39e3aa64f83e089c4548a264))
* **test:** data race in concurrent network creation test ([16e8bbd](https://github.com/Zaephor/ai-shim/commit/16e8bbd754fb9557b051706756e4451cf91b51f7))
* **test:** ensure Docker images are pulled before E2E tests ([0677fb8](https://github.com/Zaephor/ai-shim/commit/0677fb8dcb95aa4698549ba37626aa4a3904af01))
* **test:** fix all pre-existing test failures and add update_interval to DryRun ([87bb0b4](https://github.com/Zaephor/ai-shim/commit/87bb0b4ddfdf825b43e1e20cd7d5344c7db35a10))
* **test:** prevent runAgent test from hanging in CI ([be542ba](https://github.com/Zaephor/ai-shim/commit/be542ba8996df417326ae862aeb85a50120cbc05))
* **test:** skip Docker tests in short mode to fix CI unit test job ([663f684](https://github.com/Zaephor/ai-shim/commit/663f6847792ba5d65ada767ad6fecac6b9064668))
* use os.Args[0] for symlink detection instead of os.Executable ([8b13d7f](https://github.com/Zaephor/ai-shim/commit/8b13d7ffafd5abfbf5cb6c079afd305bfb156c52))
* wire CLI dispatch, security validation, DIND, and self-update into main ([d6af821](https://github.com/Zaephor/ai-shim/commit/d6af82193dd594593b9ca099206cba2fc291a29e))


### Miscellaneous Chores

* target v0.1.0 for initial release ([87f49a2](https://github.com/Zaephor/ai-shim/commit/87f49a21c9c91c081f6cb30411d6b0999a68e91e))
