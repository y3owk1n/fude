package editor

import (
	"slices"

	"github.com/y3owk1n/fude/core/kernel"
)

// fold returns the first and last line of the outermost of the closed
// folds that holds line in w's buffer, or line twice (B224). A fold an edit
// emptied holds nothing.
func (w *window) fold(t kernel.Snapshot, line int) (int, int) {
	b := w.buf.text
	first, last := line, line
	for _, f := range w.buf.folds {
		s, e := t.LineOf(b.Mark(f.start)), t.LineOf(b.Mark(f.end))
		if e > s && line >= s && line <= e && (s < first || e > last) {
			first, last = min(first, s), max(last, e)
		}
	}
	return first, last
}

// openFoldAt opens the closed fold that holds line of b past its first
// line, as a cursor landing inside it asks (B224). The fold is a module's
// decoration, which goes as if the module had removed it.
func (e *Session) openFoldAt(b *buffer, t kernel.Snapshot, line int) {
	for _, f := range b.folds {
		s, end := t.LineOf(b.text.Mark(f.start)), t.LineOf(b.text.Mark(f.end))
		if line <= s || line > end {
			continue
		}
		for owner, ds := range e.decos {
			if i := slices.IndexFunc(ds, func(d decoration) bool { return d.start == f.start && d.end == f.end }); i >= 0 {
				ds[i].remove()
				e.decos[owner] = slices.Delete(ds, i, i+1)
			}
		}
	}
}
