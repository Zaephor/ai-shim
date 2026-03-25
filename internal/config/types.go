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
