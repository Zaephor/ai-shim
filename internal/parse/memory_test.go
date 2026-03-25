package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemory_ValidValues(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"1g", 1073741824},
		{"512m", 536870912},
		{"1024k", 1048576},
		{"2.5g", 2684354560},
		{"100", 100},
	}
	for _, tt := range tests {
		result, err := Memory(tt.input)
		require.NoError(t, err, "input: %s", tt.input)
		assert.Equal(t, tt.expected, result, "input: %s", tt.input)
	}
}

func TestMemory_InvalidValues(t *testing.T) {
	tests := []string{"", "abc", "2gb", "-1g", "g"}
	for _, input := range tests {
		_, err := Memory(input)
		assert.Error(t, err, "input %q should be invalid", input)
	}
}

func TestMemory_CaseInsensitive(t *testing.T) {
	r1, _ := Memory("2G")
	r2, _ := Memory("2g")
	assert.Equal(t, r1, r2)
}
