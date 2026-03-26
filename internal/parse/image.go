package parse

import (
	"fmt"
	"regexp"
	"strings"
)

var hexPattern = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ImageDigest validates that if an image reference contains a @sha256: digest,
// the digest is exactly 64 hex characters. Returns nil for tag-only references.
func ImageDigest(image string) error {
	idx := strings.Index(image, "@sha256:")
	if idx < 0 {
		return nil // no digest, nothing to validate
	}

	if idx == 0 {
		return fmt.Errorf("invalid image reference %q: missing image name before digest", image)
	}

	hash := image[idx+len("@sha256:"):]
	if hash == "" {
		return fmt.Errorf("invalid image digest %q: empty hash after sha256:", image)
	}
	if !hexPattern.MatchString(hash) {
		return fmt.Errorf("invalid image digest %q: sha256 hash must be exactly 64 hex characters", image)
	}

	return nil
}

// IsDigestPinned returns true if the image reference uses a @sha256: digest.
func IsDigestPinned(image string) bool {
	return strings.Contains(image, "@sha256:")
}
