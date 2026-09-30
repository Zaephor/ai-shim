package testutil

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// LockRegistryCache serializes tests that create, remove, or inspect the DIND
// registry cache container (dind.CacheContainerName) or its uses-cache
// consumers. The cache has a single fixed name per daemon, and `go test`
// runs packages as parallel processes against the same daemon, so without
// this lock one package's test can force-remove another's live cache. The
// lock is a flock(2) on a file in os.TempDir(), shared by every test binary
// in one `go test` invocation, and is released when the test finishes.
func LockRegistryCache(t *testing.T) {
	t.Helper()
	release, err := lockRegistryCache()
	if err != nil {
		t.Fatalf("locking registry cache: %v", err)
	}
	t.Cleanup(release)
}

func lockRegistryCache() (func(), error) {
	path := filepath.Join(os.TempDir(), "ai-shim-test-registry-cache.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o666)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
