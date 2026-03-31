package shell

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuote_Empty(t *testing.T) {
	assert.Equal(t, "''", Quote(""))
}

func TestQuote_Simple(t *testing.T) {
	assert.Equal(t, "hello", Quote("hello"))
}

func TestQuote_WithSpaces(t *testing.T) {
	assert.Equal(t, "'hello world'", Quote("hello world"))
}

func TestQuote_WithSingleQuote(t *testing.T) {
	assert.Equal(t, "'it'\"'\"'s'", Quote("it's"))
}

func TestQuote_WithDoubleQuote(t *testing.T) {
	assert.Equal(t, "'say \"hi\"'", Quote(`say "hi"`))
}

func TestQuote_WithDollarSign(t *testing.T) {
	assert.Equal(t, "'$HOME'", Quote("$HOME"))
}

func TestQuote_WithBacktick(t *testing.T) {
	assert.Equal(t, "'`cmd`'", Quote("`cmd`"))
}

func TestQuote_WithSemicolon(t *testing.T) {
	assert.Equal(t, "'foo; rm -rf /'", Quote("foo; rm -rf /"))
}

func TestQuote_WithPipe(t *testing.T) {
	assert.Equal(t, "'a|b'", Quote("a|b"))
}

func TestQuote_NoSpecialChars(t *testing.T) {
	// Alphanumeric, dots, hyphens, underscores, slashes — no quoting needed
	inputs := []string{"foo", "foo-bar", "foo_bar", "foo.bar", "/usr/bin/foo", "v1.2.3"}
	for _, s := range inputs {
		assert.Equal(t, s, Quote(s), "should not quote %q", s)
	}
}

func TestQuote_NullByte(t *testing.T) {
	// Null bytes in shell strings cause truncation. Quote wraps them but
	// doesn't strip them — the caller should validate input before quoting.
	q := Quote("hello\x00world")
	assert.Contains(t, q, "\x00", "null byte is preserved (not stripped)")
}

func TestQuote_ControlChars(t *testing.T) {
	// Control characters like BEL, ESC, carriage return
	for _, c := range []string{"\x07", "\x1b", "\r", "\x01"} {
		q := Quote("a" + c + "b")
		// These don't trigger quoting (not in the special char list),
		// but they're passed through unchanged
		assert.Contains(t, q, c, "control char should be preserved")
	}
}

func TestQuote_Unicode(t *testing.T) {
	// Unicode should pass through unmodified
	assert.Equal(t, "héllo", Quote("héllo"))
	assert.Equal(t, "日本語", Quote("日本語"))
	// Unicode with spaces should be quoted
	assert.Equal(t, "'hello 世界'", Quote("hello 世界"))
}

func TestQuote_MultipleQuotes(t *testing.T) {
	// Multiple single quotes in a row
	q := Quote("'''")
	assert.NotContains(t, q, "'''", "raw triple quotes should be escaped")
	// The result should be a valid shell expression
	assert.True(t, len(q) > 3, "escaped quotes should be longer")
}

func TestQuote_SpecialChars(t *testing.T) {
	specials := []string{
		"hello world", "a\tb", "a\nb", `a"b`, "a'b",
		"a\\b", "a$b", "a`b", "a!b", "a#b", "a&b",
		"a|b", "a;b", "a(b", "a)b", "a{b", "a}b",
		"a[b", "a]b", "a<b", "a>b", "a?b", "a*b", "a~b",
	}
	for _, s := range specials {
		q := Quote(s)
		assert.NotEqual(t, s, q, "should quote %q", s)
		assert.True(t, len(q) > len(s), "quoted should be longer for %q", s)
	}
}
