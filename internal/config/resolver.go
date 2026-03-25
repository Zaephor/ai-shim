package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// Resolve loads, merges, and resolves the full 5-tier config for the given
// agent and profile. configDir is the path to ~/.ai-shim/config/.
func Resolve(configDir, agent, profile string) (Config, error) {
	defaultCfg, err := LoadFile(filepath.Join(configDir, "default.yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading default config: %w", err)
	}

	agentCfg, err := LoadFile(filepath.Join(configDir, "agents", agent+".yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading agent config: %w", err)
	}

	profileCfg, err := LoadFile(filepath.Join(configDir, "profiles", profile+".yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading profile config: %w", err)
	}

	agentProfileCfg, err := LoadFile(filepath.Join(configDir, "agent-profiles", agent+"_"+profile+".yaml"))
	if err != nil {
		return Config{}, fmt.Errorf("loading agent-profile config: %w", err)
	}

	envCfg := loadEnvOverrides()

	merged := MergeAll(defaultCfg, agentCfg, profileCfg, agentProfileCfg, envCfg)

	resolved, err := ResolveTemplates(merged)
	if err != nil {
		return Config{}, fmt.Errorf("resolving templates: %w", err)
	}

	return resolved, nil
}

// loadEnvOverrides reads AI_SHIM_* environment variables and returns a Config
// representing tier 5 overrides.
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

	return cfg
}
