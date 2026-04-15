package container

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeResizer records every Resize call made against it and optionally returns
// a stubbed error. It lets tests drive prepareReattachTerminal without a real
// Docker daemon.
type fakeResizer struct {
	calls []resizeCall
	err   error
}

type resizeCall struct {
	id     string
	width  uint
	height uint
}

func (f *fakeResizer) Resize(_ context.Context, id string, width, height uint) error {
	f.calls = append(f.calls, resizeCall{id: id, width: width, height: height})
	return f.err
}

// TestReattachResetSequence pins the exact ANSI bytes that the reattach path
// emits. These bytes are load-bearing: changing them would regress the visual
// fix documented in commit 0bcd4d4.
func TestReattachResetSequence(t *testing.T) {
	assert.Equal(t, "\033[2J\033[H\033[?25h", reattachResetSequence,
		"reattach reset sequence must clear screen, home cursor, and show cursor")
}

// TestPrepareReattachTerminal_EmitsAnsiAndDoubleResize verifies that on reattach
// we (a) write the ANSI reset sequence exactly once and (b) call Resize twice,
// first at height+1 then at height, to provoke a SIGWINCH inside the container
// even when the host terminal hasn't actually changed size.
func TestPrepareReattachTerminal_EmitsAnsiAndDoubleResize(t *testing.T) {
	var out bytes.Buffer
	resizer := &fakeResizer{}
	ctx := context.Background()

	prepareReattachTerminal(ctx, &out, resizer, "container-123", 80, 24)

	assert.Equal(t, reattachResetSequence, out.String(),
		"ANSI reset sequence should be written exactly once")

	if assert.Len(t, resizer.calls, 2, "expected two resize calls to force SIGWINCH") {
		assert.Equal(t, resizeCall{id: "container-123", width: 80, height: 25}, resizer.calls[0],
			"first resize must toggle through height+1")
		assert.Equal(t, resizeCall{id: "container-123", width: 80, height: 24}, resizer.calls[1],
			"second resize must restore the real height")
	}
}

// TestPrepareReattachTerminal_NoSizeIsNoOp verifies the existing guard: when
// the host terminal cannot report its size (width or height is zero), the
// function must not emit the ANSI sequence and must not call Resize. Writing
// the escape bytes to a non-tty or resizing to a zero dimension would be at
// best wasted syscalls and at worst a visible glitch.
func TestPrepareReattachTerminal_NoSizeIsNoOp(t *testing.T) {
	cases := []struct {
		name   string
		width  uint
		height uint
	}{
		{"zero width", 0, 24},
		{"zero height", 80, 0},
		{"both zero", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			resizer := &fakeResizer{}
			ctx := context.Background()

			prepareReattachTerminal(ctx, &out, resizer, "container-123", tc.width, tc.height)

			assert.Empty(t, out.String(),
				"no ANSI bytes should be written when terminal size is unknown")
			assert.Empty(t, resizer.calls,
				"no resize calls should be made when terminal size is unknown")
		})
	}
}
