package shell

import "testing"

func FuzzQuote(f *testing.F) {
	// Seed corpus: interesting shell metacharacters and edge cases.
	f.Add("")
	f.Add("hello")
	f.Add("hello world")
	f.Add("it's")
	f.Add("$(whoami)")
	f.Add("; rm -rf /")
	f.Add("`id`")
	f.Add("a\nb")
	f.Add("$HOME")
	f.Add("'single'")
	f.Add("\"double\"")
	f.Add("back\\slash")
	f.Add("tab\there")
	f.Add("!bang")
	f.Add("#comment")
	f.Add("pipe|cmd")
	f.Add("semi;colon")
	f.Add("(parens)")
	f.Add("{braces}")

	f.Fuzz(func(t *testing.T, input string) {
		result := Quote(input)

		// Property: result should never be empty (empty input gives '').
		if result == "" {
			t.Error("Quote returned empty string")
		}

		// Property: for non-empty input without special chars, result == input.
		// For empty input, result == "''" (length 2).
		if input == "" && result != "''" {
			t.Errorf("Quote(%q) = %q, want \"''\"", input, result)
		}
	})
}
