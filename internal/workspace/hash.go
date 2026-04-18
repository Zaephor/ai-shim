package workspace

import (
	"crypto/sha256"
	"fmt"
)

// HashPath returns a deterministic 12-character hex hash of the hostname + path.
// absPath always begins with "/" and hostname never contains "/", so the
// concatenation is unambiguous without an explicit separator. Do not add one:
// changing the hash breaks every existing container label and agent-side
// project directory keyed on the old value (e.g. Claude's per-cwd memory).
func HashPath(hostname, absPath string) string {
	h := sha256.Sum256([]byte(hostname + absPath))
	return fmt.Sprintf("%x", h)[:12]
}

// ContainerWorkdir returns the container-side working directory for a host path.
func ContainerWorkdir(hostname, absPath string) string {
	return "/workspace/" + HashPath(hostname, absPath)
}
