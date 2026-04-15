package container

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// seqWriter is an io.Writer that stamps every Write call with the next value
// of a shared monotonic counter. The sequence numbers recorded here can be
// compared against those recorded by seqResizer to verify ordering between
// the two streams of activity.
type seqWriter struct {
	counter *atomic.Int32
	seqs    []int32
	bytes   []byte
}

func (w *seqWriter) Write(p []byte) (int, error) {
	w.seqs = append(w.seqs, w.counter.Add(1))
	w.bytes = append(w.bytes, p...)
	return len(p), nil
}

// seqResizer is a containerResizer that stamps every Resize call with the
// next value of the same shared counter used by seqWriter, recording both
// the sequence number and the (width, height) it was called with.
type seqResizer struct {
	counter *atomic.Int32
	seqs    []int32
	calls   []resizeCall
}

func (r *seqResizer) Resize(_ context.Context, id string, width, height uint) error {
	r.seqs = append(r.seqs, r.counter.Add(1))
	r.calls = append(r.calls, resizeCall{id: id, width: width, height: height})
	return nil
}

// TestPrepareReattachTerminal_ANSIPrecedesResize is the core ordering
// invariant: the ANSI clear/home/cursor-on sequence must be flushed to the
// host terminal BEFORE any ContainerResize fires. The inner TUI redraws on
// the SIGWINCH provoked by forceResize, so if the host canvas hasn't been
// cleared yet, the redraw paints over stale glyphs. A shared atomic counter
// stamped on every Write and Resize makes the order directly assertable.
func TestPrepareReattachTerminal_ANSIPrecedesResize(t *testing.T) {
	var counter atomic.Int32
	w := &seqWriter{counter: &counter}
	r := &seqResizer{counter: &counter}

	prepareReattachTerminal(context.Background(), w, r, "cid", 80, 24)

	if !assert.NotEmpty(t, w.seqs, "writer should have received the ANSI reset bytes") {
		return
	}
	if !assert.Len(t, r.seqs, 2, "resizer should have been called exactly twice") {
		return
	}

	// The first write must happen strictly before the first resize.
	assert.Less(t, w.seqs[0], r.seqs[0],
		"ANSI reset must be emitted before the first ContainerResize (writer seq=%d, first resize seq=%d)",
		w.seqs[0], r.seqs[0])

	// And every write must precede every resize — we never interleave more
	// bytes after forceResize has started.
	maxWrite := w.seqs[len(w.seqs)-1]
	assert.Less(t, maxWrite, r.seqs[0],
		"all ANSI bytes must be flushed before any resize fires (last write seq=%d, first resize seq=%d)",
		maxWrite, r.seqs[0])

	// Resize calls themselves must be in order h+1 then h, and the counter
	// confirms no reordering happened between them either.
	assert.Less(t, r.seqs[0], r.seqs[1],
		"resize calls must be issued in order (h+1 before h)")
	assert.Equal(t, uint(25), r.calls[0].height, "first resize is height+1")
	assert.Equal(t, uint(24), r.calls[1].height, "second resize is height")
}

// TestPrepareReattachTerminal_ResizeCountIsTwo pins the contract that the
// reattach helper issues exactly two resize calls. A single resize is a
// no-op against Docker when the dimensions haven't changed — the doubled
// call is the whole point of the helper — and a third would be wasted I/O.
// This guards against future refactors that might accidentally collapse the
// toggle or add an extra restore pass.
func TestPrepareReattachTerminal_ResizeCountIsTwo(t *testing.T) {
	var counter atomic.Int32
	w := &seqWriter{counter: &counter}
	r := &seqResizer{counter: &counter}

	prepareReattachTerminal(context.Background(), w, r, "cid", 120, 40)

	assert.Len(t, r.calls, 2, "reattach must produce exactly two resize calls")
	assert.Equal(t, resizeCall{id: "cid", width: 120, height: 41}, r.calls[0])
	assert.Equal(t, resizeCall{id: "cid", width: 120, height: 40}, r.calls[1])
}

// TestPrepareReattachTerminal_NoSizeDoesNotInvokeResizer documents the
// complement of the reattach contract: when the host terminal size is
// unknown we skip the whole helper — no ANSI, no resize. This parallels
// the "non-reattach" path where prepareReattachTerminal is never called
// (the caller falls through to a single resizeContainer), and guarantees
// we never issue the double-resize without also clearing the canvas.
func TestPrepareReattachTerminal_NoSizeDoesNotInvokeResizer(t *testing.T) {
	var counter atomic.Int32
	w := &seqWriter{counter: &counter}
	r := &seqResizer{counter: &counter}

	prepareReattachTerminal(context.Background(), w, r, "cid", 0, 0)

	assert.Zero(t, counter.Load(), "no writes and no resizes should have been recorded")
	assert.Empty(t, w.seqs)
	assert.Empty(t, r.seqs)
	assert.Empty(t, r.calls)
}
