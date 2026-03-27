package color

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGreen(t *testing.T) {
	c := New(true)
	assert.Equal(t, "\033[32mhello\033[0m", c.Green("hello"))
}

func TestRed(t *testing.T) {
	c := New(true)
	assert.Equal(t, "\033[31mfail\033[0m", c.Red("fail"))
}

func TestYellow(t *testing.T) {
	c := New(true)
	assert.Equal(t, "\033[33mwarn\033[0m", c.Yellow("warn"))
}

func TestBold(t *testing.T) {
	c := New(true)
	assert.Equal(t, "\033[1mtitle\033[0m", c.Bold("title"))
}

func TestNoColor(t *testing.T) {
	c := New(false)
	assert.Equal(t, "hello", c.Green("hello"))
	assert.Equal(t, "fail", c.Red("fail"))
	assert.Equal(t, "warn", c.Yellow("warn"))
	assert.Equal(t, "title", c.Bold("title"))
}

func TestNoColor_NoANSICodes(t *testing.T) {
	c := New(false)
	for _, fn := range []func(string) string{c.Green, c.Red, c.Yellow, c.Bold} {
		result := fn("text")
		assert.NotContains(t, result, "\033[", "should not contain ANSI escape codes when color disabled")
	}
}

func TestEnabled_RespectsNO_COLOR(t *testing.T) {
	t.Setenv("AI_SHIM_NO_COLOR", "1")
	assert.False(t, Enabled(), "AI_SHIM_NO_COLOR=1 should disable color")
}

func TestEnabled_RespectsNO_COLOR_Convention(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	assert.False(t, Enabled(), "NO_COLOR=1 should disable color")
}
