package invocation

import "testing"

func FuzzParseName(f *testing.F) {
	// Seed corpus: valid names, edge cases, and adversarial inputs.
	f.Add("claude-code")
	f.Add("claude-code_work")
	f.Add("opencode_default")
	f.Add("")
	f.Add("_")
	f.Add("agent_")
	f.Add("_profile")
	f.Add("a_b_c")
	f.Add("../../../etc")
	f.Add("; rm -rf /")
	f.Add("name_with_multiple_underscores")
	f.Add("simple")
	f.Add("a_b")

	f.Fuzz(func(t *testing.T, input string) {
		agent, profile, err := ParseName(input)
		if err == nil {
			if agent == "" {
				t.Errorf("ParseName(%q) returned empty agent", input)
			}
			if profile == "" {
				t.Errorf("ParseName(%q) returned empty profile", input)
			}
		}
		// Property: should never panic (implicit).
		_ = agent
		_ = profile
	})
}
