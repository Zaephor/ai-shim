# Image Digest Pinning Design

## Goal

Support SHA256 digest pinning for all three image types (agent container, DIND sidecar, registry cache) using the standard Docker `@sha256:` format. No new config fields.

## Approach

Users specify digests in the existing `image` config field or `AI_SHIM_IMAGE` env var using standard Docker format: `ghcr.io/catthehacker/ubuntu@sha256:abc123...`. Docker SDK already handles this format natively. We add format validation and doctor reporting.

Default images remain tag-based. Users opt in to pinning via config.

## Scope

**Changes:**
- Format validation for `@sha256:` image strings (64 hex chars)
- Validation in `Config.Validate()` for agent image, at resolution points for DIND/cache
- `ai-shim doctor` reports pinning status for all three images
- Fix duplicate default image constant (`GetImage()` fallback should use `container.DefaultImage`)

**No changes:**
- No new config fields
- `EnsureImage()` and Docker SDK work with digests as-is
- 5-tier config merge unchanged (image is scalar, last-wins)
- `AI_SHIM_IMAGE` env var works with digests naturally
- Default images stay as tags

## Validation

Shared helper in `internal/container/` validates image digest format:
- Triggered only when image string contains `@sha256:`
- Verifies exactly 64 hex characters after `sha256:`
- Clear error message on invalid format
- Called from `Config.Validate()` and DIND/cache resolution points

## Doctor Output

Reports pinning status for all three images:

```
agent image: ghcr.io/catthehacker/ubuntu:act-24.04 (tag)
dind image:  docker:dind (tag, default)
cache image: registry:2 (tag, default)
```

Or if pinned:

```
agent image: ghcr.io/catthehacker/ubuntu@sha256:a1b2c3... (pinned)
```

Labels: `(tag)` for tag-based, `(pinned)` for digest, `(default)` suffix when unoverridden.

## Testing

- Unit: digest format validation (valid, invalid length, invalid hex, tag-only, empty)
- Unit: doctor pinning status labels for each image type
- Integration: `Config.Validate()` catches bad digests, passes good ones
- No E2E changes needed -- Docker validates actual digest at pull time

## Decisions

- All three images support digests (agent, DIND, cache)
- Standard Docker `@sha256:` format in existing image field, no new fields
- Defaults remain tag-based, users opt in
- Format validation only (64 hex chars), no registry verification
- Doctor reports informational pinning status, no warnings for unpinned
