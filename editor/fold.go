package editor

import (
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// fold is a closed fold: marks at the start of its first line, the one
// that shows, and of its last (B224).
type fold struct {
	first, last kernel.MarkID
}

// indentCol returns the column of line's first non-blank, or false for a
// line of blanks.
func indentCol(t kernel.Snapshot, line, tab int) (int, bool) {
	ls := t.LineStart(line)
	text := t.Slice(ls, vim.LineEnd(t, ls))
	rest := strings.TrimLeft(text, " \t")
	if rest == "" {
		return 0, false
	}
	return vim.Column(t, ls, ls+len(text)-len(rest), tab), true
}

// foldEnd returns the last line of the fold line starts: the lines after
// it that are blank or indented more, without the blank ones they end with.
// It is false when no line after it is indented more.
func foldEnd(t kernel.Snapshot, line, tab int) (int, bool) {
	ind, ok := indentCol(t, line, tab)
	if !ok {
		return 0, false
	}
	last := line
	for l := line + 1; l < t.Lines(); l++ {
		col, ok := indentCol(t, l, tab)
		if !ok {
			continue
		}
		if col <= ind {
			break
		}
		last = l
	}
	return last, last > line
}

// fold returns the first and last line of the outermost of w's closed
// folds that holds line, or line twice. A fold an edit emptied is dropped.
func (w *window) fold(t kernel.Snapshot, line int) (int, int) {
	b := w.buf.text
	first, last := line, line
	for i := 0; i < len(w.folds); i++ {
		f := w.folds[i]
		s, e := t.LineOf(b.Mark(f.first)), t.LineOf(b.Mark(f.last))
		if e <= s {
			b.DeleteMark(f.first)
			b.DeleteMark(f.last)
			w.folds = append(w.folds[:i], w.folds[i+1:]...)
			i--
			continue
		}
		if line >= s && line <= e && (s < first || e > last) {
			first, last = min(first, s), max(last, e)
		}
	}
	return first, last
}

// closeFold closes the fold that starts on line, when there is one.
func (w *window) closeFold(t kernel.Snapshot, line, tab int) bool {
	last, ok := foldEnd(t, line, tab)
	if !ok {
		return false
	}
	w.openFold(t, line)
	b := w.buf.text
	w.folds = append(w.folds, fold{b.SetMark(t.LineStart(line), kernel.Left), b.SetMark(t.LineStart(last), kernel.Left)})
	return true
}

// openFold opens the closed fold that holds line, and reports whether
// there was one.
func (w *window) openFold(t kernel.Snapshot, line int) bool {
	first, _ := w.fold(t, line)
	b := w.buf.text
	for i, f := range w.folds {
		if t.LineOf(b.Mark(f.first)) == first {
			b.DeleteMark(f.first)
			b.DeleteMark(f.last)
			w.folds = append(w.folds[:i], w.folds[i+1:]...)
			return true
		}
	}
	return false
}

// foldAct runs zc, zo, za, zR or zM in the current window (B224). zc closes
// the fold the cursor's line starts, or else the one it is in, zo opens the
// closed fold it is in, and za does one or the other. zR opens every fold,
// and zM closes every outermost one.
func (e *Session) foldAct(act string) {
	w, t := e.win, e.Text()
	line := t.LineOf(e.Cursor())
	switch act {
	case "open":
		w.openFold(t, line)
	case "toggle":
		if !w.openFold(t, line) {
			e.closeAround(t, line)
		}
	case "close":
		e.closeAround(t, line)
	case "openAll":
		for len(w.folds) > 0 {
			f := w.folds[0]
			w.openFold(t, t.LineOf(w.buf.text.Mark(f.first)))
		}
	case "closeAll":
		for l := 0; l < t.Lines(); l++ {
			if w.closeFold(t, l, e.tabWidth(w.buf)) {
				_, l = w.fold(t, l)
			}
		}
	}
	if first, _ := w.fold(t, line); first != line {
		e.setCursor(t.LineStart(first))
	}
	e.reveal()
}

// closeAround closes the fold line starts, or else the innermost one that
// holds it: the one started by the nearest line above with less indent.
func (e *Session) closeAround(t kernel.Snapshot, line int) {
	tab := e.tabWidth(e.win.buf)
	if e.win.closeFold(t, line, tab) {
		return
	}
	ind, ok := indentCol(t, line, tab)
	for l := line - 1; l >= 0; l-- {
		if col, text := indentCol(t, l, tab); text && (!ok || col < ind) {
			if e.win.closeFold(t, l, tab) {
				return
			}
		}
	}
	e.message = "no fold here"
}
