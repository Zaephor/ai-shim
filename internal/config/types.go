package config

const (
	// DefaultImage is the default container image used when none is configured.
	DefaultImage = "ghcr.io/catthehacker/ubuntu:act-24.04"
)

// ResourceLimits defines optional container resource constraints.
type ResourceLimits struct {
	Memory string `yaml:"memory,omitempty" json:"memory,omitempty"` // e.g. "512m", "2g", "4g"
	CPUs   string `yaml:"cpus,omitempty" json:"cpus,omitempty"`     // decimal CPU count, e.g. "1.0", "2.5"
}

// Config represents the fully resolved configuration for an agent+profile invocation.
type Config struct {
	Variables    map[string]string       `yaml:"variables,omitempty" json:"variables,omitempty"`
	Env          map[string]string       `yaml:"env,omitempty" json:"env,omitempty"`
	Image        string                  `yaml:"image,omitempty" json:"image,omitempty"`
	Hostname     string                  `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Version      string                  `yaml:"version,omitempty" json:"version,omitempty"`
	Args         []string                `yaml:"args,omitempty" json:"args,omitempty"`
	Volumes      []string                `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Ports        []string                `yaml:"ports,omitempty" json:"ports,omitempty"`
	Packages     []string                `yaml:"packages,omitempty" json:"packages,omitempty"`
	NetworkScope string                  `yaml:"network_scope,omitempty" json:"network_scope,omitempty"`
	DINDHostname string                  `yaml:"dind_hostname,omitempty" json:"dind_hostname,omitempty"`
	DIND         *bool                   `yaml:"dind,omitempty" json:"dind,omitempty"`
	DINDGpu      *bool                   `yaml:"dind_gpu,omitempty" json:"dind_gpu,omitempty"`
	GPU          *bool                   `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	DINDMirrors  []string                `yaml:"dind_mirrors,omitempty" json:"dind_mirrors,omitempty"`
	DINDCache    *bool                   `yaml:"dind_cache,omitempty" json:"dind_cache,omitempty"`
	DINDTLS      *bool                   `yaml:"dind_tls,omitempty" json:"dind_tls,omitempty"`
	AllowAgents  []string                `yaml:"allow_agents,omitempty" json:"allow_agents,omitempty"`
	Isolated     *bool                   `yaml:"isolated,omitempty" json:"isolated,omitempty"`
	MCPServers   map[string]MCPServerDef `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
	// MCPServersOrder holds mcp_servers names in the order they appear in
	// YAML so the MCP_SERVERS JSON blob handed to agents can be emitted in
	// declaration order rather than Go map-iteration (random) order.
	// Populated alongside yaml.Unmarshal in LoadFile/LoadFileStrict — not a
	// YAML input field itself.
	MCPServersOrder []string           `yaml:"-" json:"-"`
	Tools           map[string]ToolDef `yaml:"tools,omitempty" json:"tools,omitempty"`
	// ToolsOrder holds tool names in the order they appear in YAML so the
	// provisioning script can install them in declaration order. Populated
	// by Config.UnmarshalYAML — not a YAML input field itself.
	ToolsOrder      []string        `yaml:"-" json:"-"`
	Resources       *ResourceLimits `yaml:"resources,omitempty" json:"resources,omitempty"`
	DINDResources   *ResourceLimits `yaml:"dind_resources,omitempty" json:"dind_resources,omitempty"`
	Git             *GitConfig      `yaml:"git,omitempty" json:"git,omitempty"`
	SecurityProfile string          `yaml:"security_profile,omitempty" json:"security_profile,omitempty"`
	UpdateInterval  string          `yaml:"update_interval,omitempty" json:"update_interval,omitempty"`
	// Extends is the name of another profile to inherit from.
	// When set, the referenced profile is loaded first and this profile's
	// settings are merged on top (child overrides parent). Chaining is
	// supported (A extends B extends C); circular references and chains
	// deeper than 10 are rejected.
	Extends string `yaml:"extends,omitempty" json:"extends,omitempty"`
	// EnvFile is an optional path to a dotenv-style file whose KEY=VALUE
	// pairs are loaded into the container environment. Variables from
	// env_file are applied before the env: map so explicit env: entries
	// win on conflict. Tilde (~) is expanded. Missing file = hard error.
	EnvFile string `yaml:"env_file,omitempty" json:"env_file,omitempty"`
	// SymlinkDir is the directory where `manage symlinks create` installs
	// agent symlinks. Tilde (~) is expanded to the user's home directory.
	// Read from ~/.ai-shim/config/default.yaml only (global preference);
	// not resolved through the 5-tier agent/profile stack. Defaults to
	// $HOME/.local/bin when unset.
	SymlinkDir string `yaml:"symlink_dir,omitempty" json:"symlink_dir,omitempty"`
	// SelfUpdate configures the `ai-shim update` command: which GitHub
	// repository to check for releases, the API base URL (for Enterprise),
	// and whether pre-release versions should be considered.
	SelfUpdate *SelfUpdateConfig `yaml:"selfupdate,omitempty" json:"selfupdate,omitempty"`
}

// SelfUpdateConfig controls the behaviour of `ai-shim update`.
type SelfUpdateConfig struct {
	// Repository is the GitHub owner/repo to check for releases.
	// Default: "Zaephor/ai-shim".
	Repository string `yaml:"repository,omitempty" json:"repository,omitempty"`
	// APIURL is the GitHub API base URL. Override for GitHub Enterprise
	// (e.g. "https://ghe.example.com/api/v3").
	// Default: "https://api.github.com".
	APIURL string `yaml:"api_url,omitempty" json:"api_url,omitempty"`
	// Enabled controls whether `ai-shim update` is allowed. When false
	// the command refuses and hints that self-update is disabled.
	// Default: true (nil treated as enabled).
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// Prerelease allows pre-release versions to be considered for update.
	// When true, `ai-shim update` may offer a pre-release as the latest.
	// Default: false (nil treated as disabled).
	Prerelease *bool `yaml:"prerelease,omitempty" json:"prerelease,omitempty"`
}

// IsSelfUpdateEnabled returns true if self-update is allowed (default: true).
func (c Config) IsSelfUpdateEnabled() bool {
	return c.SelfUpdate == nil || c.SelfUpdate.Enabled == nil || *c.SelfUpdate.Enabled
}

// IsSelfUpdatePrerelease returns true if pre-release versions are allowed.
func (c Config) IsSelfUpdatePrerelease() bool {
	return c.SelfUpdate != nil && c.SelfUpdate.Prerelease != nil && *c.SelfUpdate.Prerelease
}

// GitConfig defines git user identity for commits inside the container.
type GitConfig struct {
	Name  string `yaml:"name,omitempty" json:"name,omitempty"`
	Email string `yaml:"email,omitempty" json:"email,omitempty"`
}

// IsDINDEnabled returns true if DIND is explicitly enabled.
func (c Config) IsDINDEnabled() bool { return c.DIND != nil && *c.DIND }

// IsGPUEnabled returns true if GPU is explicitly enabled.
func (c Config) IsGPUEnabled() bool { return c.GPU != nil && *c.GPU }

// IsDINDGPUEnabled returns true if DIND GPU is explicitly enabled.
func (c Config) IsDINDGPUEnabled() bool { return c.DINDGpu != nil && *c.DINDGpu }

// IsCacheEnabled returns true if pull-through cache is explicitly enabled.
func (c Config) IsCacheEnabled() bool { return c.DINDCache != nil && *c.DINDCache }

// IsDINDTLSEnabled returns true if DIND TLS is explicitly enabled.
func (c Config) IsDINDTLSEnabled() bool { return c.DINDTLS != nil && *c.DINDTLS }

// IsIsolated returns true if agent isolation is enabled (default: true).
func (c Config) IsIsolated() bool { return c.Isolated == nil || *c.Isolated }

// GetImage returns the configured image or the default.
func (c Config) GetImage() string {
	if c.Image != "" {
		return c.Image
	}
	return DefaultImage
}

// GetHostname returns the configured hostname or the default.
func (c Config) GetHostname() string {
	if c.Hostname != "" {
		return c.Hostname
	}
	return "ai-shim"
}

// MCPServerDef defines an MCP (Model Context Protocol) server to expose to the agent.
type MCPServerDef struct {
	Command string            `yaml:"command"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
}

// ToolDef defines a tool to provision in the container.
type ToolDef struct {
	Type       string   `yaml:"type" json:"type"`
	URL        string   `yaml:"url,omitempty" json:"url,omitempty"`
	Binary     string   `yaml:"binary,omitempty" json:"binary,omitempty"`
	Files      []string `yaml:"files,omitempty" json:"files,omitempty"`
	Package    string   `yaml:"package,omitempty" json:"package,omitempty"`
	Install    string   `yaml:"install,omitempty" json:"install,omitempty"`
	Checksum   string   `yaml:"checksum,omitempty" json:"checksum,omitempty"`
	DataDir    bool     `yaml:"data_dir,omitempty" json:"data_dir,omitempty"`       // request persistent dir mount
	CacheScope string   `yaml:"cache_scope,omitempty" json:"cache_scope,omitempty"` // "global" (default), "profile", "agent"
	EnvVar     string   `yaml:"env_var,omitempty" json:"env_var,omitempty"`         // env var name exported with mount path
}
