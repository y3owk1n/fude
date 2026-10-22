package editor

import (
	"math"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// places keeps places on one long line of a buffer's text with their
// display columns, so a walk of the line starts from the nearest one before
// where it goes in place of the line's start. The places before an edit
// stay through it.
type places struct {
	version kernel.Version
	key     [4]int // the line's start, then what the places depend on
	marks   []kept // in order
}

// kept is a place on a long line and the display column it begins at.
// plain is whether only printable ASCII lies between the place before it
// and it.
type kept struct {
	p, x  int
	plain bool
}

// sync makes c the places of b's text t for key. Given tab, they are
// columns with tabs that wide, and those after the edits move with the
// text.
func (c *places) sync(b *buffer, t kernel.Snapshot, key [4]int, tab int) {
	if c.version != t.Version() {
		keep, moved := 0, []kept(nil)
		if changes, ok := b.text.Since(c.version); ok {
			first := t.Len()
			for _, edits := range changes {
				for _, e := range edits {
					first = min(first, e.Start)
				}
			}
			if c.key[0] <= first {
				keep, _ = slices.BinarySearchFunc(c.marks, first, byPlace)
			}
			if tab > 0 && c.key[0] <= first {
				moved = c.moved(t, changes, keep, tab)
			}
		}
		c.version, c.marks = t.Version(), append(c.marks[:keep], moved...)
	}
	if c.key != key {
		c.key, c.marks = key, c.marks[:0]
	}
}

// moved returns the places that lie past every change since c's version,
// each change a list of edits, where they are in t. A walk from the place
// before the edits, the keep-th, gives the first its column. Each later one
// moves its column as far while only plain ASCII lies between, where no tab
// can change its width. A typed line break ends the line before them, so
// then none move.
func (c *places) moved(t kernel.Snapshot, changes [][]kernel.Edit, keep, tab int) []kept {
	for _, edits := range changes {
		for _, e := range edits {
			if strings.Contains(e.Text, "\n") {
				return nil
			}
		}
	}
	var out []kept
	for _, m := range c.marks[keep:] {
		for _, edits := range changes {
			if m.p <= edits[len(edits)-1].End {
				m.p = -1
				break
			}
			for _, e := range edits {
				m.p += len(e.Text) - (e.End - e.Start)
			}
		}
		if m.p >= 0 {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		return nil
	}
	from := kept{p: c.key[0]}
	if keep > 0 {
		from = c.marks[keep-1]
	}
	// The walk keeps the places it passes, and whether the text it read
	// last is plain.
	var walked []kept
	last := from.p
	x := vim.ColumnFrom(t, from.p, from.x, out[0].p, tab, func(p, x int, plain bool) {
		walked, last = append(walked, kept{p, x, plain}), p
	})
	dx := x - out[0].x
	out[0].x, out[0].plain = x, plainText(t.Slice(last, out[0].p))
	for i := 1; i < len(out); i++ {
		if !out[i].plain && dx%tab != 0 {
			return append(walked, out[:i]...)
		}
		out[i].x += dx
	}
	return append(walked, out...)
}

// plainText reports whether s is only printable ASCII.
func plainText(s string) bool {
	for i := range len(s) {
		if s[i] < ' ' || s[i] >= 0x7f {
			return false
		}
	}
	return true
}

// byPlace orders a kept place by where it is.
func byPlace(m kept, p int) int { return m.p - p }

// keep adds the place p at column x, with whether only plain ASCII lies
// between the place before it and it.
func (c *places) keep(p, x int, plain bool) {
	i, found := slices.BinarySearchFunc(c.marks, p, byPlace)
	if !found {
		c.marks = slices.Insert(c.marks, i, kept{p, x, plain})
	}
}

// before returns the last place at or before p, or the line's start.
func (c *places) before(p int) kept {
	i, _ := slices.BinarySearchFunc(c.marks, p+1, byPlace)
	if i == 0 {
		return kept{p: c.key[0]}
	}
	return c.marks[i-1]
}

// plain returns where the printable ASCII known from p goes on to.
func (c *places) plain(p int) int {
	i, _ := slices.BinarySearchFunc(c.marks, p+1, byPlace)
	q := p
	for ; i < len(c.marks) && c.marks[i].plain; i++ {
		q = c.marks[i].p
	}
	return q
}

// columnsOf returns the columns b keeps of the line at start of its text t.
func (b *buffer) columnsOf(t kernel.Snapshot, start, tab int) *places {
	b.columns.sync(b, t, [4]int{start, tab}, tab)
	return &b.columns
}

// column returns the display column of pos, a cluster's start, on the line
// at start of b's text t.
func (b *buffer) column(t kernel.Snapshot, start, pos, tab int) int {
	if pos-start <= clip {
		return vim.Column(t, start, pos, tab, nil)
	}
	c := b.columnsOf(t, start, tab)
	from := c.before(pos)
	x := vim.ColumnFrom(t, from.p, from.x, pos, tab, c.keep)
	if pos-from.p > clip {
		c.keep(pos, x, false)
	}
	return x
}

// columnStart returns the first cluster at or after display column col on
// the line at start of b's text t, or the line's end, and its column.
func (b *buffer) columnStart(t kernel.Snapshot, start, col, tab int) (int, int) {
	if vim.LineEnd(t, start)-start <= clip {
		return vim.ColumnStartFrom(t, start, 0, col, tab, nil)
	}
	c := b.columnsOf(t, start, tab)
	i, _ := slices.BinarySearchFunc(c.marks, col+1, func(m kept, x int) int { return m.x - x })
	from := kept{p: start}
	if i > 0 {
		from = c.marks[i-1]
	}
	p, x := vim.ColumnStartFrom(t, from.p, from.x, col, tab, c.keep)
	if p-from.p > clip {
		c.keep(p, x, false)
	}
	return p, x
}

// wrap returns how w wraps the line at start, and the rows kept of a line
// without inlines, or nil. A walk of a long line takes the printable ASCII
// the columns kept know of unread.
func (w *window) wrap(t kernel.Snapshot, start, tab int, d drawn) (vim.Wrap, *places) {
	wr := vim.Wrap{Cols: w.cols(), Hang: w.hang(t, start, tab, d.show), Tab: tab, In: d.inlines, Words: w.anchor != "" || w.opts.LineBreak}
	if len(d.inlines) > 0 || vim.LineEnd(t, start)-start <= clip {
		return wr, nil
	}
	wr.Plain = w.buf.columnsOf(t, start, tab).plain
	words := 0
	if wr.Words {
		words = 1
	}
	c := &w.buf.rows
	c.sync(w.buf, t, [4]int{start, wr.Cols, wr.Hang, tab<<1 | words}, 0)
	return wr, c
}

// rowsFrom returns where each of w's rows of the line at start begins, from
// the row at from on: at most n, and none after the row after the one that
// holds to.
func (w *window) rowsFrom(t kernel.Snapshot, start, from, n, to, tab int, d drawn) []int {
	wr, c := w.wrap(t, start, tab, d)
	if c == nil {
		return vim.RowsFrom(t, start, from, vim.Column(t, start, from, tab, nil), n, to, wr, nil)
	}
	return vim.RowsFrom(t, start, from, w.buf.column(t, start, from, tab), n, to, wr, c.keep)
}

// rowsTo returns where the row of w's line at start that holds pos begins,
// after the starts of as many as keep-1 rows before it. A line without
// inlines keeps where its rows begin, so the walk starts from the nearest
// row kept before pos with enough rows after it.
func (w *window) rowsTo(t kernel.Snapshot, start, pos, keep, tab int, d drawn) []int {
	wr, c := w.wrap(t, start, tab, d)
	for back := pos; ; {
		from, mark := kept{p: start}, func(int, int, bool) {}
		if c != nil {
			from, mark = c.before(back), c.keep
			// A row of printable ASCII takes as many characters as it has
			// columns, so the walk starts a few rows before pos's.
			first, next := max(wr.Cols, 1), max(wr.Cols-wr.Hang, 1)
			if from.p > start {
				first = next
			}
			if q := min(wr.Plain(from.p)-1, pos) - (keep+1)*next; q > from.p+first && !wr.Words {
				r := from.p + first + (q-from.p-first)/next*next
				from = kept{p: r, x: from.x + r - from.p}
			}
		}
		starts := vim.RowsFrom(t, start, from.p, from.x, math.MaxInt, pos, wr, mark)
		if len(starts) > 1 && starts[len(starts)-1] > pos {
			starts = starts[:len(starts)-1]
		}
		if len(starts) >= keep || from.p == start {
			return starts[max(len(starts)-keep, 0):]
		}
		back = from.p - 1
	}
}
