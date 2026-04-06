//go:build unix

package cli

import "syscall"

// freeBytes returns the number of bytes available to non-privileged users
// on the filesystem containing path. It uses statfs(2).
func freeBytes(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	// Bavail is in units of Bsize. Cast deliberately: on linux Bavail is
	// uint64, on darwin it is uint64; Bsize varies (int32/int64). Use
	// uint64 product, which fits any realistic disk.
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
