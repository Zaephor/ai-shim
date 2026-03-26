package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestImageDigest_TagOnly(t *testing.T) {
	assert.NoError(t, ImageDigest("ubuntu:24.04"))
	assert.NoError(t, ImageDigest("ghcr.io/catthehacker/ubuntu:act-24.04"))
	assert.NoError(t, ImageDigest("docker:dind"))
	assert.NoError(t, ImageDigest("registry:2"))
}

func TestImageDigest_ValidDigest(t *testing.T) {
	hash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	assert.NoError(t, ImageDigest("ubuntu@sha256:"+hash))
	assert.NoError(t, ImageDigest("ghcr.io/catthehacker/ubuntu@sha256:"+hash))
	assert.NoError(t, ImageDigest("docker@sha256:"+hash))
}

func TestImageDigest_TagAndDigest(t *testing.T) {
	hash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	assert.NoError(t, ImageDigest("ubuntu:24.04@sha256:"+hash))
}

func TestImageDigest_InvalidLength(t *testing.T) {
	err := ImageDigest("ubuntu@sha256:abc123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "64 hex characters")
}

func TestImageDigest_InvalidChars(t *testing.T) {
	err := ImageDigest("ubuntu@sha256:" + "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "64 hex characters")
}

func TestImageDigest_EmptyHash(t *testing.T) {
	err := ImageDigest("ubuntu@sha256:")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty hash")
}

func TestImageDigest_MissingName(t *testing.T) {
	hash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	err := ImageDigest("@sha256:" + hash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing image name")
}

func TestIsDigestPinned(t *testing.T) {
	assert.False(t, IsDigestPinned("ubuntu:24.04"))
	assert.False(t, IsDigestPinned("docker:dind"))
	hash := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	assert.True(t, IsDigestPinned("ubuntu@sha256:"+hash))
}
