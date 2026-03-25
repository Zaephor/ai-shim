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
