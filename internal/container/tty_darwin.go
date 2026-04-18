//go:build darwin

package container

import (
	"os"

	"golang.org/x/sys/unix"
)

// makeRaw puts the terminal into raw mode and returns a function to restore it.
// Returns nil if stdin is not a terminal.
func makeRaw() func() {
	fd := int(os.Stdin.Fd())
	oldState, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return nil
	}
	raw := *oldState
	raw.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Cflag &^= unix.CSIZE | unix.PARENB
	raw.Cflag |= unix.CS8
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return nil
	}
	return func() {
		_ = unix.IoctlSetTermios(fd, unix.TIOCSETA, oldState)
	}
}

// getTerminalSize returns the current terminal dimensions, or 0,0 if unavailable.
func getTerminalSize() (width, height uint) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		return 0, 0
	}
	return uint(ws.Col), uint(ws.Row)
}
