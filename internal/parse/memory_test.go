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

func TestMemory_Whitespace(t *testing.T) {
	result, err := Memory("  2g  ")
	require.NoError(t, err)
	assert.Equal(t, int64(2147483648), result)
}

func TestMemory_HugeValue(t *testing.T) {
	// 9999999999999g exceeds int64 range when converted to bytes.
	_, err := Memory("9999999999999g")
	assert.Error(t, err)
}

func TestMemory_FractionalSmallUnit(t *testing.T) {
	result, err := Memory("0.5k")
	require.NoError(t, err)
	assert.Equal(t, int64(512), result)
}

func TestMemory_Zero(t *testing.T) {
	result, err := Memory("0g")
	require.NoError(t, err)
	assert.Equal(t, int64(0), result)
}

func TestMemory_JustNumber(t *testing.T) {
	result, err := Memory("1048576")
	require.NoError(t, err)
	assert.Equal(t, int64(1048576), result, "raw bytes without suffix")
}

func TestMemory_NegativeZero(t *testing.T) {
	// -0 parses to negative zero in float64, but int64(-0.0) == 0
	result, err := Memory("-0g")
	// Could either error (negative check) or return 0
	if err != nil {
		assert.Contains(t, err.Error(), "positive")
	} else {
		assert.Equal(t, int64(0), result)
	}
}
