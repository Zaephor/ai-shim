//go:build !unix

package cli

import "errors"

// freeBytes is unsupported on non-unix platforms; returns an error so the
// caller skips the disk-space pre-flight check.
func freeBytes(path string) (uint64, error) {
	_ = path
	return 0, errors.New("freeBytes: statfs not supported on this platform")
}
