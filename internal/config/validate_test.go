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
