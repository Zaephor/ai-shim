package testutil

import "testing"

func TestSkipIfNoDocker_CIMode(t *testing.T) {
	// Just verify the function exists and doesn't panic when Docker IS available
	// (CI mode behavior tested implicitly by CI pipeline itself)
	SkipIfNoDocker(t)
}
