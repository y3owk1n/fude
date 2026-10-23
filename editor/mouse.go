package editor

import (
	"slices"
	"unicode/utf8"

	"github.com/y3owk1n/fude/vim"
)

// Click gives the window with the id the focus and puts its cursor at the
// cell at column x and row y of its text, gutter included, in cells as
// Place counts them (B552). A cell in the gutter means its row's first
// character, and one past a row's text its last. A row at or past the
// window's height, such as its status line's, only gives it the focus.
// A click ends visual mode and keeps insert mode. While the command line,
// the finder or a question shows, it does nothing.
func (e *Session) Click(id, x, y int) {
	w := e.windowOf(id)
	if w == nil || e.busy() {
		return
	}
	insert := e.vim.Mode() == vim.Insert
	if insert || e.vim.Mode() == vim.Visual {
		e.Feed("<Esc>")
	}
	e.win = w
	if y < w.h && w.buf.term == nil {
		e.clicked = e.posAt(w, x, y, insert)
		e.setCursor(e.clicked)
	}
	if insert {
		e.Feed("i")
	}
	e.reveal()
}

// Drag selects as v does from where the last click put the cursor to the
// cell at column x and row y of the window with the id, which the click
// gave the focus (B552).
func (e *Session) Drag(id, x, y int) {
	w := e.windowOf(id)
	if w == nil || w != e.win || e.busy() || w.buf.term != nil {
		return
	}
	if e.vim.Mode() != vim.Visual {
		if e.vim.Mode() == vim.Insert {
			e.Feed("<Esc>")
		}
		e.setCursor(e.clicked)
		e.Feed("v")
	}
	e.setCursor(e.posAt(w, x, min(max(y, 0), max(w.h-1, 0)), false))
	e.reveal()
}

// Scroll moves the view of the window with the id n lines down, or up
// for a negative n, as <C-e> and <C-y> do, and its cursor the least it
// must to stay in the view. The focus stays where it is (B552).
func (e *Session) Scroll(id, n int) {
	w := e.windowOf(id)
	if w == nil || w.buf.term != nil {
		return
	}
	t := w.buf.text.Snapshot()
	top := e.topOf(w)
	for ; n > 0; n-- {
		next, ok := w.nextLine(t, top)
		if !ok {
			break
		}
		top = next
	}
	for ; n < 0 && top > 0; n++ {
		top = w.prevLine(t, top)
	}
	w.buf.text.MoveMark(w.top, top)
	// The cursor keeps its column on the nearest line in view, past
	// scroll_off lines from either edge.
	ranges, _ := e.view(w, t, top, e.tabWidth(w.buf))
	first, last := t.LineOf(top), t.LineOf(ranges[len(ranges)-1].End)
	off := min(w.opts.ScrollOff, (max(w.h, 1)-1)/2)
	c := w.pos()
	line := min(max(t.LineOf(c), first+off), max(last-off, first))
	if line != t.LineOf(c) {
		ls := t.LineStart(line)
		end := vim.LineEnd(t, ls)
		if end > ls && e.vim.Mode() != vim.Insert {
			end--
		}
		w.buf.text.MoveMark(w.cursor, min(ls+c-vim.LineStart(t, c), end))
	}
}

// busy reports whether the command line, the finder or a question takes
// the keys, which a click leaves alone.
func (e *Session) busy() bool {
	return e.vim.Mode() == vim.Cmdline || e.finder != nil || e.ask != nil
}

// windowOf returns the window with the id, or nil.
func (e *Session) windowOf(id int) *window {
	ws := e.windows()
	if i := slices.IndexFunc(ws, func(w *window) bool { return w.id == id }); i >= 0 {
		return ws[i]
	}
	return nil
}

// posAt returns the place in w's text that the cell at column x and row y
// of w shows, gutter included. A cell in the gutter, or in a row that
// decorations put above or below a line, is the start of its row. A cell
// past the end of a row's text is its last character, or with end set the
// end of the line when the row is its last.
func (e *Session) posAt(w *window, x, y int, end bool) int {
	t, tab := w.buf.text.Snapshot(), e.tabWidth(w.buf)
	top := e.topOf(w)
	_, d := e.view(w, t, top, tab)
	column := func(ls, p int) int {
		if len(d.inlines) == 0 {
			return w.buf.column(t, ls, p, tab)
		}
		return vim.Column(t, ls, p, tab, d.inlines)
	}
	row := 0
	for p, from := top, w.firstRow(); ; {
		above, below := 0, 0
		for _, v := range d.virtual[t.LineOf(p)] {
			if v.Above {
				above++
			} else {
				below++
			}
		}
		starts := w.starts(t, p, tab, d)
		for len(starts) > 1 && starts[1] <= from {
			starts = starts[1:]
		}
		next, more := w.nextLine(t, p)
		if y >= row+above+len(starts)+below && more {
			row += above + len(starts) + below
			p, from = next, next
			continue
		}
		r := min(max(y-row-above, 0), len(starts)-1)
		start, stop := starts[r], vim.LineEnd(t, p)
		last := r == len(starts)-1
		if !last {
			stop = starts[r+1]
		}
		col := max(x-w.gutter(), 0)
		if !w.opts.Wrap {
			col += w.left
		} else {
			col += column(p, start)
			if start > p {
				col -= w.hang(t, p, tab, d.show)
			}
		}
		// The character that covers the column is the one before the
		// first that starts after it.
		at := stop
		if col < column(p, stop) {
			at = vim.ColumnStart(t, p, col+1, tab, d.inlines)
		} else if end && last {
			return stop
		}
		if at > start {
			_, n := utf8.DecodeLastRuneInString(t.Slice(start, at))
			at -= n
		}
		return max(at, start)
	}
}
