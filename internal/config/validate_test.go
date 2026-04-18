package config

import (
	"strings"
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

func TestConfig_Validate_NoErrors(t *testing.T) {
	cfg := Config{NetworkScope: "global"}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestConfig_Validate_InvalidNetworkScope(t *testing.T) {
	cfg := Config{NetworkScope: "banana"}
	errs := cfg.Validate()
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "invalid network_scope")
}

func TestValidate_ResourceLimits(t *testing.T) {
	cfg := Config{Resources: &ResourceLimits{Memory: "2gb", CPUs: "abc"}}
	errs := cfg.Validate()
	assert.Len(t, errs, 2, "should report invalid memory and cpus")
}

func TestValidate_ValidResourceLimits(t *testing.T) {
	cfg := Config{Resources: &ResourceLimits{Memory: "2g", CPUs: "1.5"}}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_ValidImageDigest(t *testing.T) {
	cfg := Config{Image: "ubuntu@sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_InvalidImageDigest(t *testing.T) {
	cfg := Config{Image: "ubuntu@sha256:tooshort"}
	errs := cfg.Validate()
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "64 hex characters")
}

func TestValidate_TagOnlyImage(t *testing.T) {
	cfg := Config{Image: "ubuntu:24.04"}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_EmptyImage(t *testing.T) {
	cfg := Config{}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidateSecurityProfile_Valid(t *testing.T) {
	for _, profile := range []string{"", "default", "strict", "none"} {
		assert.NoError(t, ValidateSecurityProfile(profile), "profile %q should be valid", profile)
	}
}

func TestValidateSecurityProfile_Invalid(t *testing.T) {
	assert.Error(t, ValidateSecurityProfile("banana"))
}

func TestValidate_InvalidSecurityProfile(t *testing.T) {
	cfg := Config{SecurityProfile: "invalid"}
	errs := cfg.Validate()
	assert.Len(t, errs, 1)
	assert.Contains(t, errs[0], "invalid security_profile")
}

func TestValidate_ValidSecurityProfile(t *testing.T) {
	cfg := Config{SecurityProfile: "strict"}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := Config{
		NetworkScope:    "banana",
		SecurityProfile: "invalid",
		Image:           "ubuntu@sha256:tooshort",
		Resources:       &ResourceLimits{Memory: "2gb", CPUs: "abc"},
	}
	errs := cfg.Validate()
	assert.Len(t, errs, 5, "should report all validation errors: network_scope, image digest, 2x resource limits, security_profile")
}

func TestValidate_DINDResourceLimits(t *testing.T) {
	cfg := Config{DINDResources: &ResourceLimits{Memory: "bad", CPUs: "bad"}}
	errs := cfg.Validate()
	assert.Len(t, errs, 2, "should report invalid dind_resources")
	assert.Contains(t, errs[0], "dind_resources")
}

func TestValidate_UpdateInterval_Valid(t *testing.T) {
	for _, val := range []string{"", "always", "never", "1d", "7d", "24h", "30m"} {
		cfg := Config{UpdateInterval: val}
		errs := cfg.Validate()
		assert.Empty(t, errs, "update_interval %q should be valid", val)
	}
}

func TestValidate_UpdateInterval_Invalid(t *testing.T) {
	for _, val := range []string{"xyz", "1x", "-1d", "107000000000000d"} {
		cfg := Config{UpdateInterval: val}
		errs := cfg.Validate()
		assert.NotEmpty(t, errs, "update_interval %q should be invalid", val)
		assert.Contains(t, errs[0], "interval", "error for %q should mention interval", val)
	}
}

func TestValidate_Ports_Valid(t *testing.T) {
	cfg := Config{Ports: []string{"8080:80", "3000:3000"}}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_Ports_Invalid(t *testing.T) {
	tests := []struct {
		port string
		want string
	}{
		{"invalid", "expected host:container"},
		{"abc:80", "invalid host port"},
		{"8080:abc", "invalid container port"},
	}
	for _, tt := range tests {
		cfg := Config{Ports: []string{tt.port}}
		errs := cfg.Validate()
		assert.NotEmpty(t, errs, "port %q should be invalid", tt.port)
		assert.Contains(t, errs[0], tt.want, "error for %q", tt.port)
	}
}

func TestValidate_Tools_Valid(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"act": {Type: "binary-download", Binary: "act", URL: "https://example.com/act"},
	}}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_Tools_InvalidType(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"bad": {Type: "unknown-type", Binary: "bad"},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "unknown type")
}

func TestValidate_Tools_MissingBinary(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"nobinary": {Type: "binary-download", URL: "https://example.com/tool"},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "missing binary")
}

func TestValidate_Tools_CustomAllowsMissingBinary(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"script": {Type: "custom", Install: "echo hello"},
	}}
	errs := cfg.Validate()
	assert.Empty(t, errs, "custom tool type should not require binary")
}

func TestValidate_MCPServers_Valid(t *testing.T) {
	cfg := Config{MCPServers: map[string]MCPServerDef{
		"fs": {Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem"}},
	}}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_MCPServers_EmptyCommand(t *testing.T) {
	cfg := Config{MCPServers: map[string]MCPServerDef{
		"bad": {Command: ""},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	assert.Contains(t, errs[0], "missing command")
}

func TestValidate_Tools_DataDirWithoutEnvVar(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"nvm": {Type: "custom", Install: "echo hello", DataDir: true, EnvVar: ""},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "data_dir") && strings.Contains(e, "env_var") {
			found = true
		}
	}
	assert.True(t, found, "should report data_dir without env_var error")
}

func TestValidate_Tools_DataDirWithEnvVar(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"nvm": {Type: "custom", Install: "echo hello", DataDir: true, EnvVar: "NVM_DIR"},
	}}
	errs := cfg.Validate()
	assert.Empty(t, errs)
}

func TestValidate_Tools_EnvVarWithoutDataDir(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"nvm": {Type: "custom", Install: "echo hello", EnvVar: "MY_VAR", DataDir: false},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "env_var") && strings.Contains(e, "data_dir") {
			found = true
		}
	}
	assert.True(t, found, "should report env_var without data_dir error")
}

func TestValidate_Tools_InvalidCacheScope(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"nvm": {Type: "custom", Install: "echo hello", DataDir: true, EnvVar: "NVM_DIR", CacheScope: "invalid"},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "cache_scope") {
			found = true
		}
	}
	assert.True(t, found, "should report invalid cache_scope error")
}

func TestValidate_Tools_ValidCacheScopes(t *testing.T) {
	for _, scope := range []string{"", "global", "profile", "agent"} {
		cfg := Config{Tools: map[string]ToolDef{
			"nvm": {Type: "custom", Install: "echo hello", DataDir: true, EnvVar: "NVM_DIR", CacheScope: scope},
		}}
		errs := cfg.Validate()
		assert.Empty(t, errs, "cache_scope %q should be valid", scope)
	}
}

func TestValidate_Tools_BinaryDownload_NoURL(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"mytool": {Type: "binary-download", Binary: "mytool"},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "binary-download") && strings.Contains(e, "url") {
			found = true
		}
	}
	assert.True(t, found, "should warn that binary-download type needs a url")
}

func TestValidate_Tools_TarExtract_NoURL(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"mytool": {Type: "tar-extract", Binary: "mytool"},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "tar-extract") && strings.Contains(e, "url") {
			found = true
		}
	}
	assert.True(t, found, "should warn that tar-extract type needs a url")
}

func TestValidate_Tools_Custom_NoInstallOrPackage(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"mytool": {Type: "custom"},
	}}
	errs := cfg.Validate()
	assert.NotEmpty(t, errs)
	found := false
	for _, e := range errs {
		if strings.Contains(e, "custom") && strings.Contains(e, "install or package") {
			found = true
		}
	}
	assert.True(t, found, "should warn that custom type needs install or package")
}

func TestValidate_Tools_Custom_WithInstall_NoWarning(t *testing.T) {
	cfg := Config{Tools: map[string]ToolDef{
		"mytool": {Type: "custom", Install: "echo hello"},
	}}
	errs := cfg.Validate()
	// Should have no warning about install/package since Install is set
	for _, e := range errs {
		assert.NotContains(t, e, "install or package", "should not warn about missing install/package when Install is set")
	}
}
