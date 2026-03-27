//go:build darwin

package color

import "golang.org/x/sys/unix"

// isTerminal returns true if the file descriptor is a terminal.
func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	return err == nil
}
