package invocation

import (
	"errors"
	"fmt"
	"regexp"
)

// profileNamePattern matches names that are safe to use as Docker container
// name components and as filesystem path segments. The first character must be
// alphanumeric (matching Docker's container name requirement), and subsequent
// characters may be alphanumeric, underscore, dash, or dot.
var profileNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// maxProfileNameLen caps profile names well under Docker's 253-character
// container name limit, leaving headroom for the agent prefix and any
// hostname/container suffixes the runtime adds.
const maxProfileNameLen = 63

// ValidateProfileName returns an error if the name contains characters that
// would break Docker container naming or create ambiguous filesystem paths.
// Valid: ASCII letters, digits, underscore, dash, dot. Must not start with a
// dash or dot.
func ValidateProfileName(name string) error {
	if name == "" {
		return errors.New("profile name cannot be empty")
	}
	if len(name) > maxProfileNameLen {
		return fmt.Errorf("profile name %q exceeds %d characters", name, maxProfileNameLen)
	}
	if !profileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q (allowed: ASCII letters, digits, '.', '_', '-'; must start with a letter or digit)", name)
	}
	return nil
}

// ValidateAgentName applies the same character rules as ValidateProfileName to
// agent names. Agent names share the container-naming and filesystem-path
// concerns since they appear alongside profile names in container identifiers.
func ValidateAgentName(name string) error {
	if name == "" {
		return errors.New("agent name cannot be empty")
	}
	if len(name) > maxProfileNameLen {
		return fmt.Errorf("agent name %q exceeds %d characters", name, maxProfileNameLen)
	}
	if !profileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid agent name %q (allowed: ASCII letters, digits, '.', '_', '-'; must start with a letter or digit)", name)
	}
	return nil
}
