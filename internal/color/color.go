package color

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	bold   = "\033[1m"
)

// Colorer provides ANSI color formatting methods that respect a disabled flag.
type Colorer struct {
	enabled bool
}

// New creates a Colorer. Pass true to enable color output, false to disable.
func New(enabled bool) Colorer {
	return Colorer{enabled: enabled}
}

// Green wraps text in green ANSI codes if color is enabled.
func (c Colorer) Green(s string) string {
	if !c.enabled {
		return s
	}
	return fmt.Sprintf("%s%s%s", green, s, reset)
}

// Red wraps text in red ANSI codes if color is enabled.
func (c Colorer) Red(s string) string {
	if !c.enabled {
		return s
	}
	return fmt.Sprintf("%s%s%s", red, s, reset)
}

// Yellow wraps text in yellow ANSI codes if color is enabled.
func (c Colorer) Yellow(s string) string {
	if !c.enabled {
		return s
	}
	return fmt.Sprintf("%s%s%s", yellow, s, reset)
}

// Bold wraps text in bold ANSI codes if color is enabled.
func (c Colorer) Bold(s string) string {
	if !c.enabled {
		return s
	}
	return fmt.Sprintf("%s%s%s", bold, s, reset)
}

// Enabled returns true if color output should be used, based on TTY detection
// and the AI_SHIM_NO_COLOR / NO_COLOR environment variables.
func Enabled() bool {
	// Respect NO_COLOR convention (https://no-color.org/)
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("AI_SHIM_NO_COLOR") != "" {
		return false
	}
	// Check if stderr is a terminal
	return isTerminal(int(os.Stderr.Fd()))
}

// isTerminal returns true if the file descriptor is a terminal.
func isTerminal(fd int) bool {
	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	return err == nil
}
