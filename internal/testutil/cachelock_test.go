package testutil

import (
	"testing"
	"time"
)

// TestLockRegistryCache_Exclusive verifies a second holder blocks until the
// first releases. flock(2) locks belong to the open file description, so two
// acquisitions in one process conflict the same way two test binaries do.
func TestLockRegistryCache_Exclusive(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	release, err := lockRegistryCache()
	if err != nil {
		t.Fatal(err)
	}

	acquired := make(chan func())
	errs := make(chan error, 1)
	go func() {
		r, err := lockRegistryCache()
		if err != nil {
			errs <- err
			return
		}
		acquired <- r
	}()

	select {
	case r := <-acquired:
		r()
		t.Fatal("second lock acquired while first was held")
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(200 * time.Millisecond):
	}

	release()

	select {
	case release2 := <-acquired:
		release2()
	case err := <-errs:
		t.Fatal(err)
	case <-time.After(5 * time.Second):
		t.Fatal("second lock not acquired after first was released")
	}
}
