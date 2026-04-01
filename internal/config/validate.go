package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ai-shim/ai-shim/internal/parse"
)

// validToolTypes are the recognized tool provisioning types.
var validToolTypes = map[string]bool{
	"binary-download":       true,
	"tar-extract":           true,
	"tar-extract-selective": true,
	"apt":                   true,
	"go-install":            true,
	"custom":                true,
}

// validateImageDigest checks for valid @sha256: format if present.
func validateImageDigest(image string) []string {
	if image == "" {
		return nil
	}
	if err := parse.ImageDigest(image); err != nil {
		return []string{err.Error()}
	}
	return nil
}

// ValidateNetworkScope checks if a network scope value is valid.
func ValidateNetworkScope(scope string) error {
	valid := map[string]bool{
		"":                  true, // empty = default (isolated)
		"global":            true,
		"profile":           true,
		"workspace":         true,
		"profile-workspace": true,
		"isolated":          true,
	}
	if !valid[scope] {
		return fmt.Errorf("invalid network_scope %q (valid: global, profile, workspace, profile-workspace, isolated)", scope)
	}
	return nil
}

// Validate checks the resolved config for common mistakes.
func (c Config) Validate() []string {
	var warnings []string

	if err := ValidateNetworkScope(c.NetworkScope); err != nil {
		warnings = append(warnings, err.Error())
	}

	warnings = append(warnings, validateImageDigest(c.Image)...)
	warnings = append(warnings, validateResourceLimits("resources", c.Resources)...)
	warnings = append(warnings, validateResourceLimits("dind_resources", c.DINDResources)...)
	warnings = append(warnings, validateSecurityProfile(c.SecurityProfile)...)
	warnings = append(warnings, validateUpdateInterval(c.UpdateInterval)...)
	warnings = append(warnings, validatePorts(c.Ports)...)
	warnings = append(warnings, validateTools(c.Tools)...)
	warnings = append(warnings, validateMCPServers(c.MCPServers)...)

	return warnings
}

// ValidateSecurityProfile checks if a security profile value is valid.
func ValidateSecurityProfile(profile string) error {
	valid := map[string]bool{
		"":        true,
		"default": true,
		"strict":  true,
		"none":    true,
	}
	if !valid[profile] {
		return fmt.Errorf("invalid security_profile %q (valid: default, strict, none)", profile)
	}
	return nil
}

func validateSecurityProfile(profile string) []string {
	if err := ValidateSecurityProfile(profile); err != nil {
		return []string{err.Error()}
	}
	return nil
}

func validateUpdateInterval(interval string) []string {
	if interval == "" {
		return nil
	}
	if _, err := ParseUpdateInterval(interval); err != nil {
		return []string{err.Error()}
	}
	return nil
}

// validatePorts checks that port mappings have the expected host:container format.
func validatePorts(ports []string) []string {
	var warnings []string
	for _, p := range ports {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			warnings = append(warnings, fmt.Sprintf("invalid port mapping %q: expected host:container format", p))
			continue
		}
		if _, err := strconv.Atoi(parts[0]); err != nil {
			warnings = append(warnings, fmt.Sprintf("invalid host port in %q: %v", p, err))
		}
		if _, err := strconv.Atoi(parts[1]); err != nil {
			warnings = append(warnings, fmt.Sprintf("invalid container port in %q: %v", p, err))
		}
	}
	return warnings
}

// validateTools checks that tool definitions have valid types and required fields.
func validateTools(tools map[string]ToolDef) []string {
	var warnings []string
	for name, td := range tools {
		if !validToolTypes[td.Type] {
			warnings = append(warnings, fmt.Sprintf("tool %q: unknown type %q (valid: binary-download, tar-extract, tar-extract-selective, apt, go-install, custom)", name, td.Type))
		}
		if td.Binary == "" && td.Type != "custom" {
			warnings = append(warnings, fmt.Sprintf("tool %q: missing binary name", name))
		}
	}
	return warnings
}

// validateMCPServers checks that MCP server definitions have required fields.
func validateMCPServers(servers map[string]MCPServerDef) []string {
	var warnings []string
	for name, srv := range servers {
		if srv.Command == "" {
			warnings = append(warnings, fmt.Sprintf("mcp_server %q: missing command", name))
		}
	}
	return warnings
}

// validateResourceLimits checks that resource limit values are parseable.
func validateResourceLimits(name string, r *ResourceLimits) []string {
	if r == nil {
		return nil
	}
	var warnings []string
	if r.Memory != "" {
		if _, err := parse.Memory(r.Memory); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s.memory: %v", name, err))
		}
	}
	if r.CPUs != "" {
		if _, err := strconv.ParseFloat(r.CPUs, 64); err != nil {
			warnings = append(warnings, fmt.Sprintf("%s.cpus: invalid value %q", name, r.CPUs))
		}
	}
	return warnings
}
