package editor

import (
	"math"
	"slices"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// places keeps places on one long line of a buffer's text with their
// display columns, so a walk of the line starts from the nearest one before
// where it goes in place of the line's start. The places before an edit
// stay through it.
type places struct {
	version kernel.Version
	key     [4]int   // the line's start, then what the places depend on
	marks   [][2]int // places, in order, and their columns
}

// sync makes c the places of b's text t for key.
func (c *places) sync(b *buffer, t kernel.Snapshot, key [4]int) {
	if c.version != t.Version() {
		keep := 0
		if changes, ok := b.text.Since(c.version); ok {
			first := t.Len()
			for _, edits := range changes {
				for _, e := range edits {
					first = min(first, e.Start)
				}
			}
			if c.key[0] < first {
				keep, _ = slices.BinarySearchFunc(c.marks, first, byPlace)
			}
		}
		c.version, c.marks = t.Version(), c.marks[:keep]
	}
	if c.key != key {
		c.key, c.marks = key, c.marks[:0]
	}
}

// byPlace orders a kept place by where it is.
func byPlace(m [2]int, p int) int { return m[0] - p }

// keep adds the place p at column x.
func (c *places) keep(p, x int) {
	i, found := slices.BinarySearchFunc(c.marks, p, byPlace)
	if !found {
		c.marks = slices.Insert(c.marks, i, [2]int{p, x})
	}
}

// before returns the last place at or before p, or the line's start.
func (c *places) before(p int) [2]int {
	i, _ := slices.BinarySearchFunc(c.marks, p+1, byPlace)
	if i == 0 {
		return [2]int{c.key[0], 0}
	}
	return c.marks[i-1]
}

// column returns the display column of pos, a cluster's start, on the line
// at start of b's text t.
func (b *buffer) column(t kernel.Snapshot, start, pos, tab int) int {
	if pos-start <= clip {
		return vim.Column(t, start, pos, tab, nil)
	}
	c := &b.columns
	c.sync(b, t, [4]int{start, tab})
	from := c.before(pos)
	x := vim.ColumnFrom(t, from[0], from[1], pos, tab, c.keep)
	if pos-from[0] > clip {
		c.keep(pos, x)
	}
	return x
}

// columnStart returns the first cluster at or after display column col on
// the line at start of b's text t, or the line's end, and its column.
func (b *buffer) columnStart(t kernel.Snapshot, start, col, tab int) (int, int) {
	if vim.LineEnd(t, start)-start <= clip {
		return vim.ColumnStartFrom(t, start, 0, col, tab, nil)
	}
	c := &b.columns
	c.sync(b, t, [4]int{start, tab})
	i, _ := slices.BinarySearchFunc(c.marks, col+1, func(m [2]int, x int) int { return m[1] - x })
	from := [2]int{start, 0}
	if i > 0 {
		from = c.marks[i-1]
	}
	p, x := vim.ColumnStartFrom(t, from[0], from[1], col, tab, c.keep)
	if p-from[0] > clip {
		c.keep(p, x)
	}
	return p, x
}

// wrap returns how w wraps the line at start, and the rows kept of a line
// without inlines, or nil.
func (w *window) wrap(t kernel.Snapshot, start, tab int, d drawn) (vim.Wrap, *places) {
	wr := vim.Wrap{Cols: w.cols(), Hang: w.hang(t, start, tab, d.show), Tab: tab, In: d.inlines, Words: w.anchor != "" || w.opts.LineBreak}
	if len(d.inlines) > 0 || vim.LineEnd(t, start)-start <= clip {
		return wr, nil
	}
	words := 0
	if wr.Words {
		words = 1
	}
	c := &w.buf.rows
	c.sync(w.buf, t, [4]int{start, wr.Cols, wr.Hang, tab<<1 | words})
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
// row kept before pos.
func (w *window) rowsTo(t kernel.Snapshot, start, pos, keep, tab int, d drawn) []int {
	wr, c := w.wrap(t, start, tab, d)
	from, mark := [2]int{start, 0}, func(int, int) {}
	if c != nil {
		from, mark = c.before(pos), c.keep
	}
	starts := vim.RowsFrom(t, start, from[0], from[1], math.MaxInt, pos, wr, mark)
	if len(starts) > 1 && starts[len(starts)-1] > pos {
		starts = starts[:len(starts)-1]
	}
	return starts[max(len(starts)-keep, 0):]
}
