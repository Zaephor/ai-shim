package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUpdateInterval(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    int64
		wantErr bool
	}{
		{name: "always", input: "always", want: IntervalAlways},
		{name: "never", input: "never", want: IntervalNever},
		{name: "empty string defaults to 1 day", input: "", want: IntervalDefault},
		{name: "1d", input: "1d", want: 86400},
		{name: "7d", input: "7d", want: 604800},
		{name: "30d", input: "30d", want: 2592000},
		{name: "0.5d", input: "0.5d", want: 43200},
		{name: "24h", input: "24h", want: 86400},
		{name: "1h30m", input: "1h30m", want: 5400},
		{name: "30m", input: "30m", want: 1800},
		{name: "whitespace trimmed always", input: " always ", want: IntervalAlways},
		{name: "invalid", input: "invalid", wantErr: true},
		{name: "abc", input: "abc", wantErr: true},
		{name: "d without number", input: "d", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUpdateInterval(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
