package editor

import (
	"cmp"

	"github.com/y3owk1n/fude/frontend/wire"
)

// Buffers returns what the buffer line shows: the buffers a module lists,
// but those a guest may not show, with the current window's lit (B258,
// B331).
func (e *Session) Buffers() []wire.Tab {
	// While a float or the finder is open, the window under it is the
	// current one.
	current := e.under()
	var tabs []wire.Tab
	for _, t := range e.tabs {
		if b, err := e.buffer(t.Buffer); err == nil && t.Buffer != 0 && e.barred(b) == nil {
			tabs = append(tabs, wire.Tab{Number: t.Buffer, Name: t.Name, Current: b == current.buf, Modified: b.modified()})
		}
	}
	return tabs
}

// after returns the first listed buffer at or after index i of the
// buffers, wrapping, past an empty buffer with no file while another is
// listed, or the buffer at i when none is listed (B89).
func (e *Session) after(i int) *buffer {
	i = min(i, len(e.bufs)-1)
	var empty *buffer
	for k := range e.bufs {
		b := e.bufs[(i+k)%len(e.bufs)]
		switch {
		case b.kind() != "" || e.barred(b) != nil:
		case b.blank():
			empty = cmp.Or(empty, b)
		default:
			return b
		}
	}
	return cmp.Or(empty, e.bufs[i])
}
