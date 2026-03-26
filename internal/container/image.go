package container

import "github.com/ai-shim/ai-shim/internal/parse"

// ValidateImageDigest checks that if an image reference contains a @sha256: digest,
// the digest is exactly 64 hex characters. Returns nil for tag-only references.
func ValidateImageDigest(image string) error {
	return parse.ImageDigest(image)
}

// IsDigestPinned returns true if the image reference uses a @sha256: digest.
func IsDigestPinned(image string) bool {
	return parse.IsDigestPinned(image)
}
