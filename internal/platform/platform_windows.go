//go:build windows

package platform

func getIDs() (uid, gid int) {
	return 0, 0
}
