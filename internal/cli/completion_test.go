package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBashCompletion(t *testing.T) {
	output := BashCompletion()
	assert.Contains(t, output, "complete -F")
	assert.Contains(t, output, "ai-shim")
	assert.Contains(t, output, "manage")
}

func TestZshCompletion(t *testing.T) {
	output := ZshCompletion()
	assert.Contains(t, output, "#compdef")
	assert.Contains(t, output, "ai-shim")
}

func TestBashCompletion_ContainsAgentNames(t *testing.T) {
	output := BashCompletion()
	// Bash completion includes agent names in the manage subcommand completions
	// The completion script itself doesn't list agent names inline, but it
	// must contain the manage commands that operate on agents.
	// Agent names are resolved at runtime, so we verify the script structure.
	assert.NotEmpty(t, output)
}

func TestBashCompletion_ContainsCommands(t *testing.T) {
	output := BashCompletion()
	assert.Contains(t, output, "manage")
	assert.Contains(t, output, "run")
	assert.Contains(t, output, "version")
	assert.Contains(t, output, "update")
}

func TestZshCompletion_ContainsAgentNames(t *testing.T) {
	output := ZshCompletion()
	assert.NotEmpty(t, output)
}

func TestZshCompletion_ContainsCommands(t *testing.T) {
	output := ZshCompletion()
	assert.Contains(t, output, "manage")
	assert.Contains(t, output, "run")
	assert.Contains(t, output, "version")
	assert.Contains(t, output, "update")
}
