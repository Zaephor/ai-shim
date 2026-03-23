package workspace

import (
	"crypto/sha256"
	"fmt"
)

// HashPath returns a deterministic 12-character hex hash of the hostname + path.
func HashPath(hostname, absPath string) string {
	h := sha256.Sum256([]byte(hostname + absPath))
	return fmt.Sprintf("%x", h)[:12]
}

// ContainerWorkdir returns the container-side working directory for a host path.
func ContainerWorkdir(hostname, absPath string) string {
	return "/workspace/" + HashPath(hostname, absPath)
}
