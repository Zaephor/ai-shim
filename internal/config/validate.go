package config

import "fmt"

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

	return warnings
}
