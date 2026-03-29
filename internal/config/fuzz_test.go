package config

import "testing"

func FuzzParseUpdateInterval(f *testing.F) {
	// Seed corpus: valid keywords, day shorthand, durations, and invalid inputs.
	f.Add("")
	f.Add("always")
	f.Add("never")
	f.Add("1d")
	f.Add("7d")
	f.Add("24h")
	f.Add("30m")
	f.Add("1h30m")
	f.Add("invalid")
	f.Add("0")
	f.Add("-1")
	f.Add("999999d")
	f.Add("0.5d")
	f.Add("1.5h")
	f.Add("  always  ")
	f.Add("d")
	f.Add("3600s")

	f.Fuzz(func(t *testing.T, input string) {
		result, err := ParseUpdateInterval(input)
		if err == nil {
			// Valid parse: result should be >= -1 (IntervalNever).
			if result < -1 {
				t.Errorf("ParseUpdateInterval(%q) = %d, want >= -1", input, result)
			}
		}
		// Property: should never panic (implicit).
	})
}
