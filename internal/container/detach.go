package container

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// DefaultDetachKeys is Ctrl+] then 'd' (telnet escape + confirm).
var DefaultDetachKeys = [2]byte{0x1D, 0x64}

// DetachableReader wraps an io.Reader and watches for a 2-byte detach
// sequence. When the sequence is detected, detachCh is closed and Read
// returns io.EOF without forwarding the escape bytes.
type DetachableReader struct {
	reader  io.Reader
	keys    [2]byte
	timeout time.Duration // max wait between escape bytes

	detachCh   chan struct{}
	detachOnce sync.Once

	mu         sync.Mutex
	sawEscape  bool      // true if first key was seen
	escapeTime time.Time // when the first key arrived
	pending    byte      // buffered first key to forward if second doesn't match
}

// NewDetachableReader creates a DetachableReader with the default keys and
// a 500ms timeout between the two key presses.
func NewDetachableReader(r io.Reader, detachCh chan struct{}) *DetachableReader {
	return NewDetachableReaderWithKeys(r, detachCh, DefaultDetachKeys)
}

// NewDetachableReaderWithKeys creates a DetachableReader with custom keys.
func NewDetachableReaderWithKeys(r io.Reader, detachCh chan struct{}, keys [2]byte) *DetachableReader {
	return &DetachableReader{
		reader:   r,
		keys:     keys,
		timeout:  500 * time.Millisecond,
		detachCh: detachCh,
	}
}

// Read implements io.Reader. It intercepts the detach key sequence and
// returns io.EOF when detach is triggered.
func (d *DetachableReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	d.mu.Lock()
	// If we have a pending escape byte that timed out, emit it first.
	if d.sawEscape && time.Since(d.escapeTime) >= d.timeout {
		d.sawEscape = false
		p[0] = d.pending
		d.mu.Unlock()
		return 1, nil
	}
	d.mu.Unlock()

	// Read from underlying reader.
	buf := make([]byte, len(p))
	n, err := d.reader.Read(buf)
	if n == 0 {
		// Underlying reader returned no data. If we have a pending escape
		// byte, flush it before propagating the error.
		d.mu.Lock()
		if d.sawEscape {
			d.sawEscape = false
			p[0] = d.pending
			d.mu.Unlock()
			return 1, err
		}
		d.mu.Unlock()
		return 0, err
	}

	// Process the bytes, filtering out the detach sequence.
	out := 0
	for i := 0; i < n; i++ {
		b := buf[i]

		d.mu.Lock()
		if !d.sawEscape {
			if b == d.keys[0] {
				// First key of sequence — buffer it.
				d.sawEscape = true
				d.escapeTime = time.Now()
				d.pending = b
				d.mu.Unlock()
				continue
			}
			// Normal byte — forward it.
			d.mu.Unlock()
			if out < len(p) {
				p[out] = b
				out++
			}
		} else {
			// We have a pending escape byte.
			if b == d.keys[1] && time.Since(d.escapeTime) < d.timeout {
				// Detach sequence complete.
				d.sawEscape = false
				d.mu.Unlock()
				d.detachOnce.Do(func() { close(d.detachCh) })
				if out > 0 {
					return out, nil
				}
				return 0, io.EOF
			}
			// Not the second key — forward both the buffered and current byte.
			pending := d.pending
			d.sawEscape = false
			d.mu.Unlock()
			if out < len(p) {
				p[out] = pending
				out++
			}
			if out < len(p) {
				p[out] = b
				out++
			}
		}
	}

	if out > 0 {
		return out, err
	}
	// All bytes were consumed (buffered as escape). If the underlying reader
	// returned an error (including io.EOF), we need to flush the pending byte.
	if err != nil {
		d.mu.Lock()
		if d.sawEscape {
			d.sawEscape = false
			p[0] = d.pending
			d.mu.Unlock()
			return 1, err
		}
		d.mu.Unlock()
		return 0, err
	}
	return 0, nil
}

// ParseDetachKeys parses a detach key string in the format used by Docker:
// "ctrl-X,Y" where X is a letter and Y is a character.
// Examples: "ctrl-],d" (default), "ctrl-p,ctrl-q" (Docker default).
func ParseDetachKeys(s string) ([2]byte, error) {
	parts := strings.SplitN(s, ",", 2)
	if len(parts) != 2 {
		return [2]byte{}, fmt.Errorf("detach keys must be two keys separated by comma, got %q", s)
	}

	var result [2]byte
	for i, part := range parts {
		part = strings.TrimSpace(part)
		b, err := parseKeySpec(part)
		if err != nil {
			return [2]byte{}, fmt.Errorf("invalid key %d: %w", i+1, err)
		}
		result[i] = b
	}
	return result, nil
}

// parseKeySpec parses a single key specifier: "ctrl-X" or a literal character.
func parseKeySpec(s string) (byte, error) {
	if strings.HasPrefix(s, "ctrl-") {
		rest := s[5:]
		if len(rest) != 1 {
			return 0, fmt.Errorf("ctrl- must be followed by a single character, got %q", rest)
		}
		ch := rest[0]
		// Ctrl+letter: ASCII 1-26 for a-z, also handle common symbols.
		if ch >= 'a' && ch <= 'z' {
			return ch - 'a' + 1, nil
		}
		if ch >= 'A' && ch <= 'Z' {
			return ch - 'A' + 1, nil
		}
		// Ctrl+special chars
		switch ch {
		case '[':
			return 0x1B, nil // ESC
		case '\\':
			return 0x1C, nil // FS
		case ']':
			return 0x1D, nil // GS
		case '^':
			return 0x1E, nil // RS
		case '_':
			return 0x1F, nil // US
		default:
			return 0, fmt.Errorf("unsupported ctrl character: %q", string(ch))
		}
	}

	if len(s) == 1 {
		return s[0], nil
	}
	return 0, fmt.Errorf("expected single character or ctrl-X, got %q", s)
}
