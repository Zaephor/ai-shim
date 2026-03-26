package container

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateImageDigest_TagOnly(t *testing.T) {
	assert.NoError(t, ValidateImageDigest("ubuntu:24.04"))
	assert.NoError(t, ValidateImageDigest("ghcr.io/catthehacker/ubuntu:act-24.04"))
	assert.NoError(t, ValidateImageDigest("docker:dind"))
	assert.NoError(t, ValidateImageDigest("registry:2"))
}

func TestValidateImageDigest_ValidDigest(t *testing.T) {
	assert.NoError(t, ValidateImageDigest("ubuntu@sha256:"+validHash()))
	assert.NoError(t, ValidateImageDigest("ghcr.io/catthehacker/ubuntu@sha256:"+validHash()))
	assert.NoError(t, ValidateImageDigest("docker@sha256:"+validHash()))
}

func TestValidateImageDigest_TagAndDigest(t *testing.T) {
	// Docker allows tag+digest; we validate the digest portion
	assert.NoError(t, ValidateImageDigest("ubuntu:24.04@sha256:"+validHash()))
}

func TestValidateImageDigest_InvalidLength(t *testing.T) {
	err := ValidateImageDigest("ubuntu@sha256:abc123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "64 hex characters")
}

func TestValidateImageDigest_InvalidChars(t *testing.T) {
	err := ValidateImageDigest("ubuntu@sha256:" + "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "64 hex characters")
}

func TestValidateImageDigest_EmptyHash(t *testing.T) {
	err := ValidateImageDigest("ubuntu@sha256:")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty hash")
}

func TestValidateImageDigest_MissingName(t *testing.T) {
	err := ValidateImageDigest("@sha256:" + validHash())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing image name")
}

func TestValidateImageDigest_TooLong(t *testing.T) {
	err := ValidateImageDigest("ubuntu@sha256:" + validHash() + "extra")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "64 hex characters")
}

func TestIsDigestPinned(t *testing.T) {
	assert.False(t, IsDigestPinned("ubuntu:24.04"))
	assert.False(t, IsDigestPinned("docker:dind"))
	assert.True(t, IsDigestPinned("ubuntu@sha256:"+validHash()))
	assert.True(t, IsDigestPinned("ghcr.io/foo@sha256:"+validHash()))
}

func validHash() string {
	return "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
}
