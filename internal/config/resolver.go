package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolve loads, merges, and resolves the full 5-tier config for the given
// agent and profile. configDir is the path to ~/.ai-shim/config/.
func Resolve(configDir, agent, profile string) (Config, error) {
	cfg, _, err := ResolveWithSources(configDir, agent, profile)
	return cfg, err
}

// ResolveWithSources is like Resolve but also returns ConfigSources that
// tracks which tier set each field.
func ResolveWithSources(configDir, agentName, profile string) (Config, ConfigSources, error) {
	defaultCfg, err := LoadFile(filepath.Join(configDir, "default.yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading default config: %w", err)
	}

	agentCfg, err := LoadFile(filepath.Join(configDir, "agents", agentName+".yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading agent config: %w", err)
	}

	profileCfg, err := LoadFile(filepath.Join(configDir, "profiles", profile+".yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading profile config: %w", err)
	}

	agentProfileCfg, err := LoadFile(filepath.Join(configDir, "agent-profiles", agentName+"_"+profile+".yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading agent-profile config: %w", err)
	}

	envCfg := loadEnvOverrides()

	tiers := []namedConfig{
		{name: "default.yaml", config: defaultCfg},
		{name: fmt.Sprintf("agent:%s", agentName), config: agentCfg},
		{name: fmt.Sprintf("profile:%s", profile), config: profileCfg},
		{name: fmt.Sprintf("agent-profile:%s_%s", agentName, profile), config: agentProfileCfg},
		{name: "env", config: envCfg},
	}

	configs := make([]Config, len(tiers))
	for i, t := range tiers {
		configs[i] = t.config
	}

	merged := MergeAll(configs...)
	sources := computeSources(tiers)

	resolved, err := ResolveTemplates(merged)
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("resolving templates: %w", err)
	}

	return resolved, sources, nil
}

// loadEnvOverrides reads AI_SHIM_* environment variables and returns a Config
// representing tier 5 overrides. Supported variables:
//   - AI_SHIM_IMAGE         — override container image
//   - AI_SHIM_VERSION       — pin agent version
//   - AI_SHIM_DIND          — toggle DIND sidecar (0/1/true/false)
//   - AI_SHIM_DIND_GPU      — toggle GPU for DIND (0/1/true/false)
//   - AI_SHIM_GPU           — toggle GPU for agent container (0/1/true/false)
//   - AI_SHIM_NETWORK_SCOPE — override network scope
//   - AI_SHIM_DIND_HOSTNAME — override DIND sidecar hostname
//   - AI_SHIM_DIND_CACHE    — toggle pull-through registry cache (0/1/true/false)
//   - AI_SHIM_DIND_TLS      — toggle TLS for DIND socket (0/1/true/false)
//   - AI_SHIM_GIT_NAME      — git user.name for container commits
//   - AI_SHIM_GIT_EMAIL     — git user.email for container commits
//   - AI_SHIM_JSON          — enable JSON output for management commands (0/1)
//   - AI_SHIM_NO_COLOR      — disable colored output (0/1)
func loadEnvOverrides() Config {
	var cfg Config

	if v := os.Getenv("AI_SHIM_IMAGE"); v != "" {
		cfg.Image = v
	}
	if v := os.Getenv("AI_SHIM_VERSION"); v != "" {
		cfg.Version = v
	}
	if v := os.Getenv("AI_SHIM_DIND"); v != "" {
		b := v == "1" || v == "true"
		cfg.DIND = &b
	}
	if v := os.Getenv("AI_SHIM_DIND_GPU"); v != "" {
		b := v == "1" || v == "true"
		cfg.DINDGpu = &b
	}
	if v := os.Getenv("AI_SHIM_GPU"); v != "" {
		b := v == "1" || v == "true"
		cfg.GPU = &b
	}
	if v := os.Getenv("AI_SHIM_NETWORK_SCOPE"); v != "" {
		cfg.NetworkScope = v
	}
	if v := os.Getenv("AI_SHIM_DIND_HOSTNAME"); v != "" {
		cfg.DINDHostname = v
	}
	if v := os.Getenv("AI_SHIM_DIND_CACHE"); v != "" {
		b := v == "1" || v == "true"
		cfg.DINDCache = &b
	}
	if v := os.Getenv("AI_SHIM_DIND_TLS"); v != "" {
		b := v == "1" || v == "true"
		cfg.DINDTLS = &b
	}

	if v := os.Getenv("AI_SHIM_SECURITY_PROFILE"); v != "" {
		cfg.SecurityProfile = v
	}

	gitName := os.Getenv("AI_SHIM_GIT_NAME")
	gitEmail := os.Getenv("AI_SHIM_GIT_EMAIL")
	if gitName != "" || gitEmail != "" {
		cfg.Git = &GitConfig{Name: gitName, Email: gitEmail}
	}

	// Output formatting env vars (checked here for documentation consistency;
	// actual logic lives in internal/color and internal/cli).
	_ = os.Getenv("AI_SHIM_JSON")
	_ = os.Getenv("AI_SHIM_NO_COLOR")

	return cfg
}
