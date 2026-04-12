package config

import "fmt"

// ConfigSources maps config field names to the tier that set them.
// Tier names: "default", "agent:<name>", "profile:<name>",
// "agent-profile:<agent>_<profile>", "env".
type ConfigSources struct {
	Fields map[string]string
}

// NewConfigSources creates an empty ConfigSources.
func NewConfigSources() ConfigSources {
	return ConfigSources{Fields: make(map[string]string)}
}

// Source returns the tier name that set the given field, or "" if unset.
func (s ConfigSources) Source(field string) string {
	return s.Fields[field]
}

// FormatSource returns a display string like " (from default.yaml)" for a field.
func (s ConfigSources) FormatSource(field string) string {
	tier := s.Fields[field]
	if tier == "" {
		return ""
	}
	return fmt.Sprintf(" (from %s)", tier)
}

// computeSources walks through each tier config and records which tier last
// set each field. This mirrors the merge logic: later tiers override earlier.
func computeSources(tiers []namedConfig) ConfigSources {
	sources := NewConfigSources()

	for _, t := range tiers {
		cfg := t.config
		name := t.name

		if cfg.Image != "" {
			sources.Fields["image"] = name
		}
		if cfg.Hostname != "" {
			sources.Fields["hostname"] = name
		}
		if cfg.Version != "" {
			sources.Fields["version"] = name
		}
		if cfg.NetworkScope != "" {
			sources.Fields["network_scope"] = name
		}
		if cfg.DINDHostname != "" {
			sources.Fields["dind_hostname"] = name
		}
		if cfg.SecurityProfile != "" {
			sources.Fields["security_profile"] = name
		}
		if cfg.UpdateInterval != "" {
			sources.Fields["update_interval"] = name
		}
		if cfg.SymlinkDir != "" {
			sources.Fields["symlink_dir"] = name
		}
		if cfg.SelfUpdate != nil {
			sources.Fields["selfupdate"] = name
		}

		if cfg.DIND != nil {
			sources.Fields["dind"] = name
		}
		if cfg.DINDGpu != nil {
			sources.Fields["dind_gpu"] = name
		}
		if cfg.GPU != nil {
			sources.Fields["gpu"] = name
		}
		if cfg.Isolated != nil {
			sources.Fields["isolated"] = name
		}
		if cfg.DINDCache != nil {
			sources.Fields["dind_cache"] = name
		}
		if cfg.DINDTLS != nil {
			sources.Fields["dind_tls"] = name
		}

		if cfg.Resources != nil {
			sources.Fields["resources"] = name
		}
		if cfg.DINDResources != nil {
			sources.Fields["dind_resources"] = name
		}
		if cfg.Git != nil {
			sources.Fields["git"] = name
		}
		if len(cfg.Env) > 0 {
			sources.Fields["env"] = name
		}
		if len(cfg.Variables) > 0 {
			sources.Fields["variables"] = name
		}
		if len(cfg.Args) > 0 {
			sources.Fields["args"] = name
		}
		if len(cfg.Volumes) > 0 {
			sources.Fields["volumes"] = name
		}
		if len(cfg.Ports) > 0 {
			sources.Fields["ports"] = name
		}
		if len(cfg.Packages) > 0 {
			sources.Fields["packages"] = name
		}
		if len(cfg.AllowAgents) > 0 {
			sources.Fields["allow_agents"] = name
		}
		if len(cfg.DINDMirrors) > 0 {
			sources.Fields["dind_mirrors"] = name
		}
		if len(cfg.Tools) > 0 {
			sources.Fields["tools"] = name
		}
		if len(cfg.MCPServers) > 0 {
			sources.Fields["mcp_servers"] = name
		}
	}

	return sources
}

// namedConfig pairs a Config with its tier name for source tracking.
type namedConfig struct {
	name   string
	config Config
}
