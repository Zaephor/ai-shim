//go:build !windows

package platform

import "os"

func getIDs() (uid, gid int) {
	return os.Getuid(), os.Getgid()
}
