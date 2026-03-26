package config

// ResourceLimits defines optional container resource constraints.
type ResourceLimits struct {
	Memory string `yaml:"memory,omitempty"` // e.g. "512m", "2g", "4g"
	CPUs   string `yaml:"cpus,omitempty"`   // decimal CPU count, e.g. "1.0", "2.5"
}

// Config represents the fully resolved configuration for an agent+profile invocation.
type Config struct {
	Variables   map[string]string  `yaml:"variables,omitempty"`
	Env         map[string]string  `yaml:"env,omitempty"`
	Image       string             `yaml:"image,omitempty"`
	Hostname    string             `yaml:"hostname,omitempty"`
	Version     string             `yaml:"version,omitempty"`
	Args        []string           `yaml:"args,omitempty"`
	Volumes     []string           `yaml:"volumes,omitempty"`
	Ports       []string           `yaml:"ports,omitempty"`
	Packages    []string           `yaml:"packages,omitempty"`
	NetworkScope string             `yaml:"network_scope,omitempty"` // global, profile, workspace, profile-workspace, isolated (default)
	DINDHostname string             `yaml:"dind_hostname,omitempty"`
	DIND        *bool              `yaml:"dind,omitempty"`
	DINDGpu     *bool              `yaml:"dind_gpu,omitempty"`
	GPU         *bool              `yaml:"gpu,omitempty"`
	DINDMirrors []string           `yaml:"dind_mirrors,omitempty"` // registry mirror URLs
	DINDCache   *bool              `yaml:"dind_cache,omitempty"`   // enable pull-through cache
	AllowAgents []string           `yaml:"allow_agents,omitempty"`
	Isolated    *bool              `yaml:"isolated,omitempty"`
	Tools         map[string]ToolDef `yaml:"tools,omitempty"`
	Resources     *ResourceLimits    `yaml:"resources,omitempty"`      // agent container limits
	DINDResources *ResourceLimits    `yaml:"dind_resources,omitempty"` // DIND container limits
}

// IsDINDEnabled returns true if DIND is explicitly enabled.
func (c Config) IsDINDEnabled() bool { return c.DIND != nil && *c.DIND }

// IsGPUEnabled returns true if GPU is explicitly enabled.
func (c Config) IsGPUEnabled() bool { return c.GPU != nil && *c.GPU }

// IsDINDGPUEnabled returns true if DIND GPU is explicitly enabled.
func (c Config) IsDINDGPUEnabled() bool { return c.DINDGpu != nil && *c.DINDGpu }

// IsCacheEnabled returns true if pull-through cache is explicitly enabled.
func (c Config) IsCacheEnabled() bool { return c.DINDCache != nil && *c.DINDCache }

// IsIsolated returns true if agent isolation is enabled (default: true).
func (c Config) IsIsolated() bool { return c.Isolated == nil || *c.Isolated }

// GetImage returns the configured image or the default.
func (c Config) GetImage() string {
	if c.Image != "" {
		return c.Image
	}
	return "ghcr.io/catthehacker/ubuntu:act-24.04"
}

// GetHostname returns the configured hostname or the default.
func (c Config) GetHostname() string {
	if c.Hostname != "" {
		return c.Hostname
	}
	return "ai-shim"
}

// ToolDef defines a tool to provision in the container.
type ToolDef struct {
	Type     string   `yaml:"type"`
	URL      string   `yaml:"url,omitempty"`
	Binary   string   `yaml:"binary,omitempty"`
	Files    []string `yaml:"files,omitempty"`
	Package  string   `yaml:"package,omitempty"`
	Install  string   `yaml:"install,omitempty"`
	Checksum string   `yaml:"checksum,omitempty"`
}
