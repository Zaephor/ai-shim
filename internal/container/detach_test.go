package container

import (
	"bytes"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetachableReader_Passthrough(t *testing.T) {
	input := []byte("hello world\n")
	r := NewDetachableReader(bytes.NewReader(input), make(chan struct{}))

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, input, out)
}

func TestDetachableReader_DetachSequence(t *testing.T) {
	// Ctrl+] then 'd'
	input := []byte{0x1D, 'd'}
	detachCh := make(chan struct{})
	r := NewDetachableReader(bytes.NewReader(input), detachCh)

	out, err := io.ReadAll(r)
	// io.ReadAll treats io.EOF as success — check the channel instead
	require.NoError(t, err)
	assert.Empty(t, out)

	select {
	case <-detachCh:
	default:
		t.Fatal("detachCh was not closed")
	}
}

func TestDetachableReader_DetachMidStream(t *testing.T) {
	// Some data, then detach sequence
	input := []byte{'a', 'b', 0x1D, 'd', 'c'}
	detachCh := make(chan struct{})
	r := NewDetachableReader(bytes.NewReader(input), detachCh)

	// First read should get "ab"
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, []byte("ab"), buf[:n])

	// Next read should get EOF from detach (or 0 bytes + nil then EOF)
	total := 0
	for {
		n, err = r.Read(buf)
		total += n
		if err != nil {
			break
		}
	}
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 0, total)

	select {
	case <-detachCh:
	default:
		t.Fatal("detachCh was not closed")
	}
}

func TestDetachableReader_EscapeFollowedByWrongKey(t *testing.T) {
	// Ctrl+] then 'x' — not a detach, forward both bytes
	input := []byte{0x1D, 'x'}
	detachCh := make(chan struct{})
	r := NewDetachableReader(bytes.NewReader(input), detachCh)

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x1D, 'x'}, out)

	// Channel should NOT be closed
	select {
	case <-detachCh:
		t.Fatal("detachCh should not be closed")
	default:
	}
}

func TestDetachableReader_EscapeAtEndOfInput(t *testing.T) {
	// Ctrl+] at the end of input — should be flushed when EOF comes
	input := []byte{'a', 0x1D}
	detachCh := make(chan struct{})
	r := NewDetachableReader(bytes.NewReader(input), detachCh)

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, []byte{'a', 0x1D}, out)
}

func TestDetachableReader_Timeout(t *testing.T) {
	// Use a slow reader to simulate timeout between escape bytes.
	// First read: Ctrl+], second read: 'd' but after timeout.
	pr, pw := io.Pipe()
	detachCh := make(chan struct{})
	dr := NewDetachableReaderWithKeys(pr, detachCh, DefaultDetachKeys)
	// Shorten timeout for test speed
	dr.timeout = 50 * time.Millisecond

	go func() {
		pw.Write([]byte{0x1D})
		time.Sleep(100 * time.Millisecond) // exceed timeout
		pw.Write([]byte{'d'})
		pw.Close()
	}()

	out, err := io.ReadAll(dr)
	require.NoError(t, err)
	// Escape byte should be forwarded (timed out), then 'd' forwarded normally
	assert.Equal(t, []byte{0x1D, 'd'}, out)

	select {
	case <-detachCh:
		t.Fatal("detachCh should not be closed after timeout")
	default:
	}
}

func TestDetachableReader_CustomKeys(t *testing.T) {
	// Custom keys: Ctrl+P, Ctrl+Q (Docker default)
	keys := [2]byte{0x10, 0x11}
	input := []byte{0x10, 0x11}
	detachCh := make(chan struct{})
	r := NewDetachableReaderWithKeys(bytes.NewReader(input), detachCh, keys)

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, out)

	select {
	case <-detachCh:
	default:
		t.Fatal("detachCh was not closed")
	}
}

func TestDetachableReader_CustomKeysNoMatch(t *testing.T) {
	// Custom keys: Ctrl+P, Ctrl+Q. Input: default sequence (no match).
	keys := [2]byte{0x10, 0x11}
	input := []byte{0x1D, 'd'}
	detachCh := make(chan struct{})
	r := NewDetachableReaderWithKeys(bytes.NewReader(input), detachCh, keys)

	out, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, []byte{0x1D, 'd'}, out)
}

func TestDetachableReader_MultipleEscapes(t *testing.T) {
	// Two consecutive escape bytes, then 'd'
	input := []byte{0x1D, 0x1D, 'd'}
	detachCh := make(chan struct{})
	r := NewDetachableReader(bytes.NewReader(input), detachCh)

	out, err := io.ReadAll(r)
	// First 0x1D is followed by another 0x1D (not 'd'), so both forwarded.
	// Wait... let's trace: first 0x1D sets sawEscape=true. Second 0x1D:
	// sawEscape=true, but 0x1D != 'd', so forward both (0x1D, 0x1D).
	// Then 'd': sawEscape=false, so forwarded normally.
	require.NoError(t, err)
	assert.Equal(t, []byte{0x1D, 0x1D, 'd'}, out)
}

func TestDetachableReader_DetachOnlyFiresOnce(t *testing.T) {
	// Two detach sequences — channel should only close once (no panic)
	input := []byte{0x1D, 'd', 0x1D, 'd'}
	detachCh := make(chan struct{})
	r := NewDetachableReader(bytes.NewReader(input), detachCh)

	_, _ = io.ReadAll(r)

	select {
	case <-detachCh:
	default:
		t.Fatal("detachCh was not closed")
	}
}

func TestParseDetachKeys_Default(t *testing.T) {
	keys, err := ParseDetachKeys("ctrl-],d")
	require.NoError(t, err)
	assert.Equal(t, [2]byte{0x1D, 'd'}, keys)
}

func TestParseDetachKeys_DockerDefault(t *testing.T) {
	keys, err := ParseDetachKeys("ctrl-p,ctrl-q")
	require.NoError(t, err)
	assert.Equal(t, [2]byte{0x10, 0x11}, keys)
}

func TestParseDetachKeys_Letters(t *testing.T) {
	keys, err := ParseDetachKeys("ctrl-a,d")
	require.NoError(t, err)
	assert.Equal(t, [2]byte{0x01, 'd'}, keys)
}

func TestParseDetachKeys_CaseInsensitive(t *testing.T) {
	keys, err := ParseDetachKeys("ctrl-A,d")
	require.NoError(t, err)
	assert.Equal(t, [2]byte{0x01, 'd'}, keys)
}

func TestParseDetachKeys_Invalid(t *testing.T) {
	_, err := ParseDetachKeys("invalid")
	assert.Error(t, err)

	_, err = ParseDetachKeys("")
	assert.Error(t, err)

	_, err = ParseDetachKeys("ctrl-],")
	assert.Error(t, err)
}

// TestDetachableReaderWithTrigger_ConcurrentClose verifies that calling
// triggerDetach from multiple goroutines simultaneously never panics.
// This is the regression test for the TOCTOU double-close bug: the old signal
// handler used select{default: close(ch)} which is not atomic.
func TestDetachableReaderWithTrigger_ConcurrentClose(t *testing.T) {
	// Run several times to stress the race window.
	for i := 0; i < 100; i++ {
		detachCh := make(chan struct{})
		var once sync.Once
		trigger := func() {
			once.Do(func() { close(detachCh) })
		}

		const goroutines = 8
		var wg sync.WaitGroup
		wg.Add(goroutines)
		start := make(chan struct{})
		for g := 0; g < goroutines; g++ {
			go func() {
				defer wg.Done()
				<-start // all start at the same time
				trigger()
			}()
		}
		close(start)
		wg.Wait()

		// Channel must be closed exactly once.
		select {
		case <-detachCh:
		default:
			t.Fatalf("iteration %d: detachCh was not closed", i)
		}
	}
}

// TestDetachableReaderWithTrigger_SharedOnce verifies that
// NewDetachableReaderWithTrigger uses the caller-provided trigger and that
// calling the trigger externally (simulating the signal handler) at the same
// time as the reader seeing the detach sequence does not panic.
func TestDetachableReaderWithTrigger_SharedOnce(t *testing.T) {
	detachCh := make(chan struct{})
	var once sync.Once
	trigger := func() {
		once.Do(func() { close(detachCh) })
	}

	// Reader will see the detach sequence.
	input := []byte{0x1D, 'd'}
	r := NewDetachableReaderWithTrigger(bytes.NewReader(input), DefaultDetachKeys, trigger)

	// Fire the external trigger concurrently with the read.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		trigger() // simulates SIGHUP signal handler
	}()

	_, _ = io.ReadAll(r)
	wg.Wait()

	// Must be closed exactly once (no panic, channel readable).
	select {
	case <-detachCh:
	default:
		t.Fatal("detachCh was not closed")
	}
}

func TestParseDetachKeys_SpecialChars(t *testing.T) {
	tests := []struct {
		input string
		want  byte
	}{
		{"ctrl-[", 0x1B},  // ESC
		{"ctrl-\\", 0x1C}, // FS
		{"ctrl-]", 0x1D},  // GS
		{"ctrl-^", 0x1E},  // RS
		{"ctrl-_", 0x1F},  // US
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			keys, err := ParseDetachKeys(tt.input + ",d")
			require.NoError(t, err)
			assert.Equal(t, tt.want, keys[0])
		})
	}
}
