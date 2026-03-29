package parse

import (
	"strings"
	"testing"
)

func FuzzImageDigest(f *testing.F) {
	// Seed corpus: tag references, valid/invalid digests, edge cases.
	f.Add("ubuntu:latest")
	f.Add("ubuntu@sha256:" + strings.Repeat("a", 64))
	f.Add("ubuntu@sha256:tooshort")
	f.Add("")
	f.Add("@sha256:")
	f.Add("image@sha256:" + strings.Repeat("g", 64))
	f.Add("registry.io/org/image:v1.2.3")
	f.Add("registry.io/org/image@sha256:" + strings.Repeat("0", 64))
	f.Add("image@sha256:" + strings.Repeat("F", 64))
	f.Add("@sha256:" + strings.Repeat("b", 64))
	f.Add("img@sha256:")

	f.Fuzz(func(t *testing.T, input string) {
		// Both functions should never panic.
		_ = ImageDigest(input)
		_ = IsDigestPinned(input)

		// Property: if IsDigestPinned is true, the input contains @sha256:.
		if IsDigestPinned(input) && !strings.Contains(input, "@sha256:") {
			t.Errorf("IsDigestPinned(%q) = true but input has no @sha256:", input)
		}
	})
}
