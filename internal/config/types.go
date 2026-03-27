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
	Variables       map[string]string        `yaml:"variables,omitempty" json:"variables,omitempty"`
	Env             map[string]string        `yaml:"env,omitempty" json:"env,omitempty"`
	Image           string                   `yaml:"image,omitempty" json:"image,omitempty"`
	Hostname        string                   `yaml:"hostname,omitempty" json:"hostname,omitempty"`
	Version         string                   `yaml:"version,omitempty" json:"version,omitempty"`
	Args            []string                 `yaml:"args,omitempty" json:"args,omitempty"`
	Volumes         []string                 `yaml:"volumes,omitempty" json:"volumes,omitempty"`
	Ports           []string                 `yaml:"ports,omitempty" json:"ports,omitempty"`
	Packages        []string                 `yaml:"packages,omitempty" json:"packages,omitempty"`
	NetworkScope    string                   `yaml:"network_scope,omitempty" json:"network_scope,omitempty"`
	DINDHostname    string                   `yaml:"dind_hostname,omitempty" json:"dind_hostname,omitempty"`
	DIND            *bool                    `yaml:"dind,omitempty" json:"dind,omitempty"`
	DINDGpu         *bool                    `yaml:"dind_gpu,omitempty" json:"dind_gpu,omitempty"`
	GPU             *bool                    `yaml:"gpu,omitempty" json:"gpu,omitempty"`
	DINDMirrors     []string                 `yaml:"dind_mirrors,omitempty" json:"dind_mirrors,omitempty"`
	DINDCache       *bool                    `yaml:"dind_cache,omitempty" json:"dind_cache,omitempty"`
	DINDTLS         *bool                    `yaml:"dind_tls,omitempty" json:"dind_tls,omitempty"`
	AllowAgents     []string                 `yaml:"allow_agents,omitempty" json:"allow_agents,omitempty"`
	Isolated        *bool                    `yaml:"isolated,omitempty" json:"isolated,omitempty"`
	MCPServers      map[string]MCPServerDef  `yaml:"mcp_servers,omitempty" json:"mcp_servers,omitempty"`
	Tools           map[string]ToolDef       `yaml:"tools,omitempty" json:"tools,omitempty"`
	Resources       *ResourceLimits          `yaml:"resources,omitempty" json:"resources,omitempty"`
	DINDResources   *ResourceLimits          `yaml:"dind_resources,omitempty" json:"dind_resources,omitempty"`
	Git             *GitConfig               `yaml:"git,omitempty" json:"git,omitempty"`
	NetworkRules    *NetworkRules            `yaml:"network_rules,omitempty" json:"network_rules,omitempty"`
	SecurityProfile string                   `yaml:"security_profile,omitempty" json:"security_profile,omitempty"`
}

// NetworkRules defines egress firewall rules for the container.
type NetworkRules struct {
	AllowedHosts []string `yaml:"allowed_hosts,omitempty" json:"allowed_hosts,omitempty"`
	BlockedHosts []string `yaml:"blocked_hosts,omitempty" json:"blocked_hosts,omitempty"`
	AllowedPorts []string `yaml:"allowed_ports,omitempty" json:"allowed_ports,omitempty"`
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
	Type     string   `yaml:"type" json:"type"`
	URL      string   `yaml:"url,omitempty" json:"url,omitempty"`
	Binary   string   `yaml:"binary,omitempty" json:"binary,omitempty"`
	Files    []string `yaml:"files,omitempty" json:"files,omitempty"`
	Package  string   `yaml:"package,omitempty" json:"package,omitempty"`
	Install  string   `yaml:"install,omitempty" json:"install,omitempty"`
	Checksum string   `yaml:"checksum,omitempty" json:"checksum,omitempty"`
}
