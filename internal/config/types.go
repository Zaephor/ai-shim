package config

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
	AllowAgents []string           `yaml:"allow_agents,omitempty"`
	Isolated    *bool              `yaml:"isolated,omitempty"`
	Tools       map[string]ToolDef `yaml:"tools,omitempty"`
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
