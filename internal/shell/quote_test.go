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
