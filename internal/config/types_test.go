package config

import (
	"testing"

	"github.com/Zaephor/ai-shim/internal/testutil"
	"github.com/stretchr/testify/assert"
)

func TestIsDINDEnabled(t *testing.T) {
	assert.False(t, Config{}.IsDINDEnabled(), "nil should be false")
	assert.True(t, Config{DIND: testutil.BoolPtr(true)}.IsDINDEnabled())
	assert.False(t, Config{DIND: testutil.BoolPtr(false)}.IsDINDEnabled())
}

func TestIsGPUEnabled(t *testing.T) {
	assert.False(t, Config{}.IsGPUEnabled(), "nil should be false")
	assert.True(t, Config{GPU: testutil.BoolPtr(true)}.IsGPUEnabled())
	assert.False(t, Config{GPU: testutil.BoolPtr(false)}.IsGPUEnabled())
}

func TestIsDINDGPUEnabled(t *testing.T) {
	assert.False(t, Config{}.IsDINDGPUEnabled(), "nil should be false")
	assert.True(t, Config{DINDGpu: testutil.BoolPtr(true)}.IsDINDGPUEnabled())
	assert.False(t, Config{DINDGpu: testutil.BoolPtr(false)}.IsDINDGPUEnabled())
}

func TestIsCacheEnabled(t *testing.T) {
	assert.False(t, Config{}.IsCacheEnabled(), "nil should be false")
	assert.True(t, Config{DINDCache: testutil.BoolPtr(true)}.IsCacheEnabled())
	assert.False(t, Config{DINDCache: testutil.BoolPtr(false)}.IsCacheEnabled())
}

func TestIsIsolated(t *testing.T) {
	assert.True(t, Config{}.IsIsolated(), "nil should default to true")
	assert.True(t, Config{Isolated: testutil.BoolPtr(true)}.IsIsolated())
	assert.False(t, Config{Isolated: testutil.BoolPtr(false)}.IsIsolated())
}

func TestGetImage(t *testing.T) {
	assert.Equal(t, "ghcr.io/catthehacker/ubuntu:act-24.04", Config{}.GetImage())
	assert.Equal(t, "custom:latest", Config{Image: "custom:latest"}.GetImage())
}

func TestGetHostname(t *testing.T) {
	assert.Equal(t, "ai-shim", Config{}.GetHostname())
	assert.Equal(t, "custom", Config{Hostname: "custom"}.GetHostname())
}

func TestIsDINDTLSEnabled(t *testing.T) {
	assert.False(t, Config{}.IsDINDTLSEnabled(), "nil should be false")
	assert.True(t, Config{DINDTLS: testutil.BoolPtr(true)}.IsDINDTLSEnabled())
	assert.False(t, Config{DINDTLS: testutil.BoolPtr(false)}.IsDINDTLSEnabled())
}
