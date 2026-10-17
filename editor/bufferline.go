package editor

import (
	"path/filepath"
	"slices"
)

// Tab is a buffer as the buffer line shows it (B258).
type Tab struct {
	Number            int
	Name              string
	Current, Modified bool
}

// Buffers returns what the buffer line shows: the open buffers that have
// files, once two or more are, else nothing (B258).
func (e *Session) Buffers() []Tab {
	if !e.cfg.BufferLine {
		return nil
	}
	var bufs []*buffer
	bases := map[string]int{}
	for _, b := range e.listed() {
		if b.file.path != "" {
			bufs = append(bufs, b)
			bases[filepath.Base(b.file.path)]++
		}
	}
	if len(bufs) < 2 {
		return nil
	}
	// While a float or the finder is open, the window under it is the
	// current one.
	current := e.win
	if !slices.Contains(e.root.windows(), current) && e.under != nil {
		current = e.under
	}
	tabs := make([]Tab, len(bufs))
	for i, b := range bufs {
		name := filepath.Base(b.file.path)
		if bases[name] > 1 {
			name = filepath.Join(filepath.Base(filepath.Dir(b.file.path)), name)
		}
		tabs[i] = Tab{b.id, name, b == current.buf, b.modified()}
	}
	return tabs
}

// listed returns the buffers a user edits, which :bn and :bp go through:
// not help, lists, listings, terminals or the windows fude writes, and none
// a guest may not show (B89).
func (e *Session) listed() []*buffer {
	return slices.DeleteFunc(slices.Clone(e.bufs), func(b *buffer) bool { return b.kind() != "" || e.barred(b) != nil })
}

// stepBuffer shows the listed buffer n after the current one, wrapping at
// either end, as :bn and :bp do (B89). From a buffer not listed, :bn goes
// to the first and :bp to the last.
func (e *Session) stepBuffer(n int) {
	bufs := e.listed()
	if len(bufs) == 0 {
		return
	}
	at := slices.Index(bufs, e.win.buf)
	switch {
	case at >= 0:
		at = ((at+n)%len(bufs) + len(bufs)) % len(bufs)
	case n > 0:
		at = 0
	default:
		at = len(bufs) - 1
	}
	e.show(bufs[at])
}

// after returns the first listed buffer at or after index i of the
// buffers, wrapping, or the buffer at i when none is listed (B89).
func (e *Session) after(i int) *buffer {
	i = min(i, len(e.bufs)-1)
	for k := range e.bufs {
		if b := e.bufs[(i+k)%len(e.bufs)]; b.kind() == "" && e.barred(b) == nil {
			return b
		}
	}
	return e.bufs[i]
}
