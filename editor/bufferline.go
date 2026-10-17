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
	for _, b := range e.bufs {
		if b.file.path != "" && b.kind() == "" && e.barred(b) == nil {
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
