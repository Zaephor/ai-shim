package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSingleChoice(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty defaults to reattach", "", "reattach"},
		{"y", "y", "reattach"},
		{"yes", "yes", "reattach"},
		{"Y uppercase", "Y", "reattach"},
		{"YES uppercase", "YES", "reattach"},
		{"whitespace around yes", "  yes  ", "reattach"},
		{"n", "n", "exit"},
		{"no", "no", "exit"},
		{"NO uppercase", "NO", "exit"},
		{"new", "new", "new"},
		{"NEW uppercase", "NEW", "new"},
		{"p", "p", "parallel"},
		{"parallel", "parallel", "parallel"},
		{"PARALLEL uppercase", "PARALLEL", "parallel"},
		{"kill not valid in single", "kill", "exit"},
		{"garbage", "garbage", "exit"},
		{"number not valid in single", "1", "exit"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseSingleChoice(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestParseMultiChoice(t *testing.T) {
	type result struct {
		action string
		index  int
	}
	tests := []struct {
		name  string
		input string
		n     int
		want  result
	}{
		{"empty exits", "", 3, result{"exit", 0}},
		{"n exits", "n", 3, result{"exit", 0}},
		{"no exits", "no", 3, result{"exit", 0}},
		{"new", "new", 3, result{"new", 0}},
		{"NEW uppercase", "NEW", 3, result{"new", 0}},
		{"p parallel", "p", 3, result{"parallel", 0}},
		{"parallel", "parallel", 3, result{"parallel", 0}},
		{"PARALLEL uppercase", "PARALLEL", 3, result{"parallel", 0}},
		{"1 reattaches idx 0", "1", 3, result{"reattach", 0}},
		{"2 reattaches idx 1", "2", 3, result{"reattach", 1}},
		{"3 reattaches idx 2", "3", 3, result{"reattach", 2}},
		{"whitespace around 2", "  2  ", 3, result{"reattach", 1}},
		{"0 out of range", "0", 3, result{"exit", 0}},
		{"N+1 out of range", "4", 3, result{"exit", 0}},
		{"negative out of range", "-1", 3, result{"exit", 0}},
		{"k1 kills idx 0", "k1", 3, result{"kill", 0}},
		{"k 2 kills idx 1", "k 2", 3, result{"kill", 1}},
		{"kill 3 kills idx 2", "kill 3", 3, result{"kill", 2}},
		{"K1 uppercase kills", "K1", 3, result{"kill", 0}},
		{"KILL 2 uppercase", "KILL 2", 3, result{"kill", 1}},
		{"k0 out of range", "k0", 3, result{"exit", 0}},
		{"k N+1 out of range", "k 4", 3, result{"exit", 0}},
		{"k garbage", "kfoo", 3, result{"exit", 0}},
		{"garbage", "garbage", 3, result{"exit", 0}},
		{"whitespace around k1", "  k1  ", 3, result{"kill", 0}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action, idx := parseMultiChoice(tc.input, tc.n)
			assert.Equal(t, tc.want.action, action)
			assert.Equal(t, tc.want.index, idx)
		})
	}
}
