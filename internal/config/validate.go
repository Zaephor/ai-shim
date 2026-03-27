package config

import (
	"fmt"
	"strconv"

	"github.com/ai-shim/ai-shim/internal/parse"
)

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
