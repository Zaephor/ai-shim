package config

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMerge_AllFieldsHandled uses reflection to verify that every field in the
// Config struct is propagated by Merge(). This prevents the class of bug where
// a new field is added to the struct but not to Merge() — like UpdateInterval
// was before it was caught.
//
// For each field, we set a non-zero value in `over`, merge with an empty `base`,
// and verify the result has the non-zero value. If a field is added to Config
// but not to Merge(), this test will catch it.
func TestMerge_AllFieldsHandled(t *testing.T) {
	// Build an `over` Config with every field set to a non-zero value.
	over := Config{
		Variables:       map[string]string{"k": "v"},
		Env:             map[string]string{"E": "V"},
		Image:           "test-image",
		Hostname:        "test-host",
		Version:         "1.0.0",
		Args:            []string{"--arg"},
		Volumes:         []string{"/a:/b"},
		Ports:           []string{"8080:80"},
		Packages:        []string{"curl"},
		NetworkScope:    "profile",
		DINDHostname:    "dind-host",
		DIND:            boolPtr(true),
		DINDGpu:         boolPtr(true),
		GPU:             boolPtr(true),
		DINDMirrors:     []string{"mirror"},
		DINDCache:       boolPtr(true),
		DINDTLS:         boolPtr(true),
		AllowAgents:     []string{"agent"},
		Isolated:        boolPtr(false),
		MCPServers:      map[string]MCPServerDef{"s": {Command: "cmd"}},
		MCPServersOrder: []string{"s"},
		Tools:           map[string]ToolDef{"t": {Type: "apt"}},
		ToolsOrder:      []string{"t"},
		Resources:       &ResourceLimits{Memory: "4g", CPUs: "2.0"},
		DINDResources:   &ResourceLimits{Memory: "2g", CPUs: "1.0"},
		Git:             &GitConfig{Name: "User", Email: "u@e.com"},
		SecurityProfile: "strict",
		UpdateInterval:  "7d",
		Extends:         "base",
		EnvFile:         "~/.ai-shim/secrets/test.env",
		SymlinkDir:      "~/.local/bin",
		SelfUpdate:      &SelfUpdateConfig{Repository: "test/repo", APIURL: "https://test", Enabled: boolPtr(true), Prerelease: boolPtr(true)},
	}

	result := Merge(Config{}, over)

	// Use reflection to check every exported field in Config has a non-zero value
	rv := reflect.ValueOf(result)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		val := rv.Field(i)
		assert.False(t, val.IsZero(),
			"Config field %q was not propagated by Merge() — add it to the Merge function",
			field.Name)
	}
}

// TestLoadEnvOverrides_AllEnvVarsDocumented verifies that every AI_SHIM_* env
// var handled in loadEnvOverrides produces a non-zero Config field. This ensures
// env var overrides are wired through to the config correctly.
func TestLoadEnvOverrides_AllEnvVarsDocumented(t *testing.T) {
	// Set every known AI_SHIM_* env var
	envVars := map[string]string{
		"AI_SHIM_IMAGE":                 "env-image",
		"AI_SHIM_VERSION":               "1.0.0",
		"AI_SHIM_DIND":                  "1",
		"AI_SHIM_DIND_GPU":              "1",
		"AI_SHIM_GPU":                   "1",
		"AI_SHIM_NETWORK_SCOPE":         "profile",
		"AI_SHIM_DIND_HOSTNAME":         "dind-host",
		"AI_SHIM_DIND_CACHE":            "1",
		"AI_SHIM_DIND_TLS":              "1",
		"AI_SHIM_SECURITY_PROFILE":      "strict",
		"AI_SHIM_UPDATE_INTERVAL":       "7d",
		"AI_SHIM_GIT_NAME":              "Test User",
		"AI_SHIM_GIT_EMAIL":             "test@example.com",
		"AI_SHIM_SELFUPDATE_REPOSITORY": "owner/repo",
		"AI_SHIM_SELFUPDATE_API_URL":    "https://api.github.com",
		"AI_SHIM_SELFUPDATE_ENABLED":    "1",
		"AI_SHIM_SELFUPDATE_PRERELEASE": "1",
	}
	for k, v := range envVars {
		t.Setenv(k, v)
	}

	cfg := loadEnvOverrides()

	// Verify each env var produced a non-zero field
	assert.Equal(t, "env-image", cfg.Image, "AI_SHIM_IMAGE")
	assert.Equal(t, "1.0.0", cfg.Version, "AI_SHIM_VERSION")
	assert.True(t, cfg.IsDINDEnabled(), "AI_SHIM_DIND")
	assert.True(t, cfg.IsDINDGPUEnabled(), "AI_SHIM_DIND_GPU")
	assert.True(t, cfg.IsGPUEnabled(), "AI_SHIM_GPU")
	assert.Equal(t, "profile", cfg.NetworkScope, "AI_SHIM_NETWORK_SCOPE")
	assert.Equal(t, "dind-host", cfg.DINDHostname, "AI_SHIM_DIND_HOSTNAME")
	assert.True(t, cfg.IsCacheEnabled(), "AI_SHIM_DIND_CACHE")
	assert.True(t, cfg.IsDINDTLSEnabled(), "AI_SHIM_DIND_TLS")
	assert.Equal(t, "strict", cfg.SecurityProfile, "AI_SHIM_SECURITY_PROFILE")
	assert.Equal(t, "7d", cfg.UpdateInterval, "AI_SHIM_UPDATE_INTERVAL")
	require.NotNil(t, cfg.Git, "AI_SHIM_GIT_NAME/EMAIL")
	assert.Equal(t, "Test User", cfg.Git.Name, "AI_SHIM_GIT_NAME")
	assert.Equal(t, "test@example.com", cfg.Git.Email, "AI_SHIM_GIT_EMAIL")
	require.NotNil(t, cfg.SelfUpdate, "AI_SHIM_SELFUPDATE_*")
	assert.Equal(t, "owner/repo", cfg.SelfUpdate.Repository, "AI_SHIM_SELFUPDATE_REPOSITORY")
	assert.Equal(t, "https://api.github.com", cfg.SelfUpdate.APIURL, "AI_SHIM_SELFUPDATE_API_URL")
	assert.True(t, *cfg.SelfUpdate.Enabled, "AI_SHIM_SELFUPDATE_ENABLED")
	assert.True(t, *cfg.SelfUpdate.Prerelease, "AI_SHIM_SELFUPDATE_PRERELEASE")
}

// TestComputeSources_AllFieldsTracked verifies that computeSources tracks every
// field that Merge handles. Uses the same fully-populated Config as the merge
// test — if a field is merged but not source-tracked, the annotation will be
// empty in `ai-shim manage config`.
func TestComputeSources_AllFieldsTracked(t *testing.T) {
	over := Config{
		Variables:       map[string]string{"k": "v"},
		Env:             map[string]string{"E": "V"},
		Image:           "test-image",
		Hostname:        "test-host",
		Version:         "1.0.0",
		Args:            []string{"--arg"},
		Volumes:         []string{"/a:/b"},
		Ports:           []string{"8080:80"},
		Packages:        []string{"curl"},
		NetworkScope:    "profile",
		DINDHostname:    "dind-host",
		DIND:            boolPtr(true),
		DINDGpu:         boolPtr(true),
		GPU:             boolPtr(true),
		DINDMirrors:     []string{"mirror"},
		DINDCache:       boolPtr(true),
		DINDTLS:         boolPtr(true),
		AllowAgents:     []string{"agent"},
		Isolated:        boolPtr(false),
		MCPServers:      map[string]MCPServerDef{"s": {Command: "cmd"}},
		MCPServersOrder: []string{"s"},
		Tools:           map[string]ToolDef{"t": {Type: "apt"}},
		ToolsOrder:      []string{"t"},
		Resources:       &ResourceLimits{Memory: "4g", CPUs: "2.0"},
		DINDResources:   &ResourceLimits{Memory: "2g", CPUs: "1.0"},
		Git:             &GitConfig{Name: "User", Email: "u@e.com"},
		SecurityProfile: "strict",
		UpdateInterval:  "7d",
		Extends:         "base",
		EnvFile:         "~/.ai-shim/secrets/test.env",
		SymlinkDir:      "~/.local/bin",
		SelfUpdate:      &SelfUpdateConfig{Repository: "test/repo", APIURL: "https://test", Enabled: boolPtr(true), Prerelease: boolPtr(true)},
	}

	tiers := []namedConfig{{name: "test-tier", config: over}}
	sources := computeSources(tiers)

	// Every merged field should be tracked in sources
	rv := reflect.ValueOf(over)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		yamlTag := field.Tag.Get("yaml")
		if yamlTag == "" || yamlTag == "-" {
			// yamlTag "-" means the field is explicitly not serialized
			// (e.g. computed/derived state such as ToolsOrder or
			// MCPServersOrder); it has no corresponding source tier.
			continue
		}
		// Extract the yaml key name (before comma)
		key := yamlTag
		if idx := len(key); idx > 0 {
			if ci := 0; ci < len(key) {
				for ci < len(key) && key[ci] != ',' {
					ci++
				}
				key = key[:ci]
			}
		}
		assert.Equal(t, "test-tier", sources.Fields[key],
			"computeSources does not track field %q (yaml: %q) — add it to computeSources",
			field.Name, key)
	}
}

func boolPtr(b bool) *bool { return &b }
