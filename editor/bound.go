package editor

import (
	"maps"
	"slices"

	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// level puts the top line, cursor and first column of every other bound
// window level with the current window's, when it is bound. The rows that
// decorations draw between lines count, so a line stays beside the line a
// diff pairs it with (B523).
func (e *Session) level() {
	cur := e.win
	if !cur.bound {
		return
	}
	t := cur.buf.text.Snapshot()
	v := e.shown(cur, 0, t.Len()).virtual
	line, ls := t.LineOf(cur.pos()), vim.LineStart(t, cur.pos())
	top, row := rowOf(v, t.LineOf(e.Top()))+cur.filled(), rowOf(v, line)+above(v[line])
	col := vim.Column(t, ls, cur.pos(), e.tabWidth(cur.buf), nil)
	for _, w := range e.root.windows() {
		if w == cur || !w.bound {
			continue
		}
		ot := w.buf.text.Snapshot()
		ov := e.shown(w, 0, ot.Len()).virtual
		l := lineAt(ov, top, ot.Lines())
		w.buf.text.MoveMark(w.top, ot.LineStart(l))
		w.fill, w.fillAt = min(max(top-rowOf(ov, l), 0), above(ov[l])), ot.LineStart(l)
		start, tab := ot.LineStart(lineAt(ov, row, ot.Lines())), e.tabWidth(w.buf)
		last := max(vim.Column(ot, start, vim.LineEnd(ot, start), tab, nil)-1, 0)
		w.buf.text.MoveMark(w.cursor, vim.ColumnStart(ot, start, min(col, last), tab, nil))
		w.left = cur.left
	}
}

// filled is how many of the rows above w's top line it hides, which it
// does until its top line moves.
func (w *window) filled() int {
	if w.fillAt != w.buf.text.Mark(w.top) {
		return 0
	}
	return w.fill
}

// unfilled returns v, by line the rows w draws besides its lines, without
// those above its top line, top, that it hides.
func (w *window) unfilled(v map[int][]wire.Virtual, top int) map[int][]wire.Virtual {
	n := w.filled()
	if n == 0 || v == nil {
		return v
	}
	rows := v[top]
	v[top] = nil
	for _, r := range rows {
		if r.Above && n > 0 {
			n--
			continue
		}
		v[top] = append(v[top], r)
	}
	return v
}

// above is how many of rows draw above their line.
func above(rows []wire.Virtual) int {
	n := 0
	for _, r := range rows {
		if r.Above {
			n++
		}
	}
	return n
}

// rowOf returns the first row of line, the rows drawn above it included,
// in a window whose lines draw v's rows besides their own.
func rowOf(v map[int][]wire.Virtual, line int) int {
	row := line
	for l, rows := range v {
		if l < line {
			row += len(rows)
		}
	}
	return row
}

// lineAt returns the line of the n lines whose rows hold row, as rowOf
// counts them.
func lineAt(v map[int][]wire.Virtual, row, n int) int {
	extra := 0
	for _, l := range slices.Sorted(maps.Keys(v)) {
		if l+extra > row {
			break
		}
		if row <= l+extra+len(v[l]) {
			return l
		}
		extra += len(v[l])
	}
	return min(max(row-extra, 0), n-1)
}
