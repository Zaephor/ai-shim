package invocation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateProfileName_Valid(t *testing.T) {
	valid := []string{
		"work",
		"personal",
		"default",
		"dev-1",
		"my.profile",
		"test_env",
		"a",
		"A",
		"Work123",
		"my-profile",
		"my_profile",
		"name.ext",
		"profile.with.dots",
	}
	for _, name := range valid {
		err := ValidateProfileName(name)
		assert.NoError(t, err, "should accept %q", name)
	}
}

func TestValidateProfileName_Invalid(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"japanese":          "日本語",
		"space":             "Pro File",
		"leading-dash":      "-leading",
		"leading-dot":       ".leading",
		"slash":             "profile/slash",
		"backslash":         "a\\b",
		"semicolon":         "a;b",
		"dollar":            "a$b",
		"backtick":          "a`b",
		"paren":             "a(b",
		"brace":             "a{b",
		"angle":             "a<b",
		"pipe":              "a|b",
		"at":                "a@b",
		"plus":              "a+b",
		"equals":            "a=b",
		"dotdot":            "..",
		"dot":               ".",
		"parent":            "../etc",
		"dollar-substitute": "$(evil)",
	}
	for label, name := range cases {
		err := ValidateProfileName(name)
		assert.Error(t, err, "should reject %s: %q", label, name)
	}
}

func TestValidateProfileName_TooLong(t *testing.T) {
	// 64 chars, should be rejected (limit is 63)
	name := strings.Repeat("a", 64)
	err := ValidateProfileName(name)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "63")

	// 63 chars, should pass
	name = strings.Repeat("a", 63)
	assert.NoError(t, ValidateProfileName(name))
}

func TestValidateAgentName_Valid(t *testing.T) {
	valid := []string{"claude", "claude-code", "gemini", "opencode", "agent1"}
	for _, name := range valid {
		err := ValidateAgentName(name)
		assert.NoError(t, err, "should accept %q", name)
	}
}

func TestValidateAgentName_Invalid(t *testing.T) {
	invalid := []string{"", "Pro Agent", "-leading", ".leading", "agent/x", "日本語"}
	for _, name := range invalid {
		err := ValidateAgentName(name)
		assert.Error(t, err, "should reject %q", name)
	}
}

func TestParseName_RejectsInvalidProfile(t *testing.T) {
	_, _, err := ParseName("claude_日本語")
	assert.Error(t, err)
}

func TestParseName_RejectsInvalidAgent(t *testing.T) {
	_, _, err := ParseName("Pro Agent_work")
	assert.Error(t, err)
}

func TestParseName_RejectsLeadingDashProfile(t *testing.T) {
	_, _, err := ParseName("claude_-bad")
	assert.Error(t, err)
}
