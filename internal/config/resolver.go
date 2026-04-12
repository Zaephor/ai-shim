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
	var allWarnings []string

	defaultCfg, warnings, err := LoadFileStrict(filepath.Join(configDir, "default.yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading default config: %w", err)
	}
	allWarnings = append(allWarnings, warnings...)

	agentCfg, warnings, err := LoadFileStrict(filepath.Join(configDir, "agents", agentName+".yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading agent config: %w", err)
	}
	allWarnings = append(allWarnings, warnings...)

	profileCfg, warnings, err := LoadFileStrict(filepath.Join(configDir, "profiles", profile+".yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading profile config: %w", err)
	}
	allWarnings = append(allWarnings, warnings...)

	agentProfileCfg, warnings, err := LoadFileStrict(filepath.Join(configDir, "agent-profiles", agentName+"_"+profile+".yaml"))
	if err != nil {
		return Config{}, ConfigSources{}, fmt.Errorf("loading agent-profile config: %w", err)
	}
	allWarnings = append(allWarnings, warnings...)

	// Print unknown key warnings to stderr
	for _, w := range allWarnings {
		fmt.Fprintf(os.Stderr, "ai-shim: warning: unknown config key: %s\n", w)
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
//   - AI_SHIM_SECURITY_PROFILE — container security profile (default/strict/none)
//   - AI_SHIM_UPDATE_INTERVAL  — agent update interval (always/never/1d/7d/24h)
//   - AI_SHIM_SELFUPDATE_REPOSITORY — GitHub owner/repo for self-update
//   - AI_SHIM_SELFUPDATE_API_URL    — GitHub API base URL (for Enterprise)
//   - AI_SHIM_SELFUPDATE_ENABLED    — enable/disable self-update (0/1/true/false)
//   - AI_SHIM_SELFUPDATE_PRERELEASE — include pre-releases (0/1/true/false)
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
	if v := os.Getenv("AI_SHIM_UPDATE_INTERVAL"); v != "" {
		cfg.UpdateInterval = v
	}

	gitName := os.Getenv("AI_SHIM_GIT_NAME")
	gitEmail := os.Getenv("AI_SHIM_GIT_EMAIL")
	if gitName != "" || gitEmail != "" {
		cfg.Git = &GitConfig{Name: gitName, Email: gitEmail}
	}

	// Self-update env vars.
	suRepo := os.Getenv("AI_SHIM_SELFUPDATE_REPOSITORY")
	suAPI := os.Getenv("AI_SHIM_SELFUPDATE_API_URL")
	suEnabled := os.Getenv("AI_SHIM_SELFUPDATE_ENABLED")
	suPrerelease := os.Getenv("AI_SHIM_SELFUPDATE_PRERELEASE")
	if suRepo != "" || suAPI != "" || suEnabled != "" || suPrerelease != "" {
		if cfg.SelfUpdate == nil {
			cfg.SelfUpdate = &SelfUpdateConfig{}
		}
		if suRepo != "" {
			cfg.SelfUpdate.Repository = suRepo
		}
		if suAPI != "" {
			cfg.SelfUpdate.APIURL = suAPI
		}
		if suEnabled != "" {
			b := suEnabled == "1" || suEnabled == "true"
			cfg.SelfUpdate.Enabled = &b
		}
		if suPrerelease != "" {
			b := suPrerelease == "1" || suPrerelease == "true"
			cfg.SelfUpdate.Prerelease = &b
		}
	}

	// Output formatting env vars (checked here for documentation consistency;
	// actual logic lives in internal/color and internal/cli).
	_ = os.Getenv("AI_SHIM_JSON")
	_ = os.Getenv("AI_SHIM_NO_COLOR")

	return cfg
}

// LoadEnvSelfUpdate returns self-update config from AI_SHIM_SELFUPDATE_*
// environment variables, or nil if none are set. Exported so the `update`
// command handler — which doesn't use the full 5-tier resolver — can apply
// env overrides on top of the default.yaml selfupdate block.
func LoadEnvSelfUpdate() *SelfUpdateConfig {
	repo := os.Getenv("AI_SHIM_SELFUPDATE_REPOSITORY")
	apiURL := os.Getenv("AI_SHIM_SELFUPDATE_API_URL")
	enabled := os.Getenv("AI_SHIM_SELFUPDATE_ENABLED")
	prerelease := os.Getenv("AI_SHIM_SELFUPDATE_PRERELEASE")
	if repo == "" && apiURL == "" && enabled == "" && prerelease == "" {
		return nil
	}
	su := &SelfUpdateConfig{}
	if repo != "" {
		su.Repository = repo
	}
	if apiURL != "" {
		su.APIURL = apiURL
	}
	if enabled != "" {
		b := enabled == "1" || enabled == "true"
		su.Enabled = &b
	}
	if prerelease != "" {
		b := prerelease == "1" || prerelease == "true"
		su.Prerelease = &b
	}
	return su
}
