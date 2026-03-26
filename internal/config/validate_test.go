package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateNetworkScope_Valid(t *testing.T) {
	for _, scope := range []string{"", "global", "profile", "workspace", "profile-workspace", "isolated"} {
		assert.NoError(t, ValidateNetworkScope(scope), "scope %q should be valid", scope)
	}
}

func TestValidateNetworkScope_Invalid(t *testing.T) {
	assert.Error(t, ValidateNetworkScope("banana"))
}

func TestConfig_Validate_NoWarnings(t *testing.T) {
	cfg := Config{NetworkScope: "global"}
	warnings := cfg.Validate()
	assert.Empty(t, warnings)
}

func TestConfig_Validate_InvalidNetworkScope(t *testing.T) {
	cfg := Config{NetworkScope: "banana"}
	warnings := cfg.Validate()
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "invalid network_scope")
}

func TestValidate_ResourceLimits(t *testing.T) {
	cfg := Config{Resources: &ResourceLimits{Memory: "2gb", CPUs: "abc"}}
	warnings := cfg.Validate()
	assert.Len(t, warnings, 2, "should warn about invalid memory and cpus")
}

func TestValidate_ValidResourceLimits(t *testing.T) {
	cfg := Config{Resources: &ResourceLimits{Memory: "2g", CPUs: "1.5"}}
	warnings := cfg.Validate()
	assert.Empty(t, warnings)
}

func TestValidate_ValidImageDigest(t *testing.T) {
	cfg := Config{Image: "ubuntu@sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"}
	warnings := cfg.Validate()
	assert.Empty(t, warnings)
}

func TestValidate_InvalidImageDigest(t *testing.T) {
	cfg := Config{Image: "ubuntu@sha256:tooshort"}
	warnings := cfg.Validate()
	assert.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "64 hex characters")
}

func TestValidate_TagOnlyImage(t *testing.T) {
	cfg := Config{Image: "ubuntu:24.04"}
	warnings := cfg.Validate()
	assert.Empty(t, warnings)
}

func TestValidate_EmptyImage(t *testing.T) {
	cfg := Config{}
	warnings := cfg.Validate()
	assert.Empty(t, warnings)
}
