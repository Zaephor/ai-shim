package invocation

import (
	"fmt"
	"strings"
)

// ParseName splits a symlink name into agent and profile using underscore as
// the delimiter. Only the first underscore is used for splitting — dashes are
// allowed in both agent and profile names.
func ParseName(name string) (agent, profile string, err error) {
	if name == "" {
		return "", "", fmt.Errorf("empty invocation name")
	}
	idx := strings.IndexByte(name, '_')
	if idx < 0 {
		if err := ValidateAgentName(name); err != nil {
			return "", "", err
		}
		return name, "default", nil
	}
	agent = name[:idx]
	profile = name[idx+1:]
	if agent == "" {
		return "", "", fmt.Errorf("empty agent name in %q", name)
	}
	if profile == "" {
		return "", "", fmt.Errorf("empty profile name in %q", name)
	}
	if err := ValidateAgentName(agent); err != nil {
		return "", "", err
	}
	if err := ValidateProfileName(profile); err != nil {
		return "", "", err
	}
	return agent, profile, nil
}
