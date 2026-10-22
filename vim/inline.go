package vim

import (
	"math"
	"strings"

	"github.com/clipperhouse/displaywidth"
)

// An Inline is text a window shows that the text does not hold. It shows
// before Start when End is Start, as an inlay hint does (B216). Otherwise
// it shows in place of the text from Start to End, and an empty Text hides
// that text (B295). An Inline never spans lines, and a host gives them
// sorted by Start.
type Inline struct {
	Start, End int
	Text       string
}

// inlineText draws a line break in an inline's text as ⏎ and a tab as a
// space, each one column wide.
var inlineText = strings.NewReplacer("\n", "⏎", "\t", " ")

// InlineText returns an inline's text as it draws.
func InlineText(s string) string { return inlineText.Replace(s) }

// walk calls f with each place on the line at start that draws something:
// its position, the screen column it starts at, how wide it is and the
// cluster there, hidden or not, or "" at the line's end. A place
// is the inlines at a position, then the cluster there unless one of them
// hides it. A tab is as wide as it is at its column in the text, hidden
// text counting. f returns false to stop.
func walk(t Text, start, tab int, in []Inline, f func(p, x, w int, c string) bool) {
	walkFrom(t, start, start, 0, tab, in, f, nil, nil)
}

// walkFrom is walk from from on. from is a cluster of the line at start at
// column col, and the screen columns count from col. Given run, once no
// inline is ahead, it calls run in place of f for each run of printable
// ASCII, with where the run begins, its screen column and its length, and
// takes the text plain says is printable ASCII as clusters does.
func walkFrom(t Text, start, from, col, tab int, in []Inline, f func(p, x, w int, c string) bool, run func(p, x, n int) bool, plain func(p int) int) {
	end := LineEnd(t, start)
	k := 0
	for k < len(in) && in[k].Start < from {
		k++
	}
	// at collects the inlines at p, and returns their width and the end of
	// the text they hide.
	at := func(p int) (w, hidden int) {
		for hidden = p; k < len(in) && in[k].Start <= p; k++ {
			w += displaywidth.String(inlineText.Replace(in[k].Text))
			hidden = max(hidden, min(in[k].End, end))
		}
		return w, hidden
	}
	x, hidden, more := col, from, true
	var runs func(p, n int) bool
	if run != nil && k == len(in) {
		runs = func(p, n int) bool {
			more = run(p, x, n)
			x, col = x+n, col+n
			return more
		}
	}
	clusters(t, from, end, func(p int, c string) bool {
		cw := width(c, col, tab)
		col += cw
		if p < hidden {
			return true
		}
		w, h := at(p)
		if h > p {
			hidden, cw = h, 0
		}
		more = f(p, x, w+cw, c)
		x += w + cw
		return more
	}, runs, plain)
	if w, _ := at(end); more && w > 0 {
		f(end, x, w, "")
	}
}

// inlinesOn reports whether any of in is on the line from start to end.
func inlinesOn(in []Inline, start, end int) bool {
	for _, i := range in {
		if i.Start >= start && i.Start <= end {
			return true
		}
	}
	return false
}

// Column returns the screen column pos draws at on the line at start,
// counting from 0, after the inlines before it but not those at it.
func Column(t Text, start, pos, tab int, in []Inline) int {
	if !inlinesOn(in, start, pos) {
		return column(t, start, pos, tab)
	}
	col := 0
	walk(t, start, tab, in, func(p, x, w int, _ string) bool {
		col = x + w
		if p >= pos {
			col = x
		}
		return p < pos
	})
	return col
}

// ColumnStart returns the first place on the line at start that begins at
// or after screen column col, or the line's end. It is never inside text
// an inline hides.
func ColumnStart(t Text, start, col, tab int, in []Inline) int {
	end := LineEnd(t, start)
	if !inlinesOn(in, start, end) {
		return colStart(t, start, col, tab)
	}
	at := end
	walk(t, start, tab, in, func(p, x, _ int, _ string) bool {
		if x >= col {
			at = p
			return false
		}
		return true
	})
	return at
}

// Hang returns the columns the rows after the first of the line at start
// begin with when it wraps at cols: its indent when indent is set, kept to
// leave 20 columns of text, then show's width. The text keeps at least one
// column (B489).
func Hang(t Text, start, cols, tab int, indent bool, show string) int {
	n := displaywidth.String(show)
	if indent {
		n += min(column(t, start, firstNonBlank(t, start, LineEnd(t, start)), tab), max(cols-n-20, 0))
	}
	return max(min(n, cols-1), 0)
}

// Rows returns where each of the first n screen rows of the line at start
// begins when it wraps at cols columns, the rows after the first starting
// hang columns in: start, then each place that does not fit on the row
// before it.
func Rows(t Text, start, n, cols, hang, tab int, in []Inline) []int {
	return RowsFrom(t, start, start, 0, n, math.MaxInt, Wrap{Cols: cols, Hang: hang, Tab: tab, In: in}, nil)
}

// WordRows is Rows that ends a row after its last space that fits, or
// where Rows does on a row with none, as a float wraps its text.
func WordRows(t Text, start, n, cols, hang, tab int, in []Inline) []int {
	return RowsFrom(t, start, start, 0, n, math.MaxInt, Wrap{Cols: cols, Hang: hang, Tab: tab, In: in, Words: true}, nil)
}

// Wrap is how a line wraps: at Cols columns, the rows after its first Hang
// columns in, with tabs Tab columns wide and the inlines In, and with Words
// after a row's last space that fits. Plain, when set, returns where the
// printable ASCII from a place goes on to as far as the caller knows, so a
// walk of the line takes it unread.
type Wrap struct {
	Cols, Hang, Tab int
	In              []Inline
	Words           bool
	Plain           func(p int) int
}

// RowsFrom is Rows from from on, a row of the line at start that begins at
// display column col. It returns at most n rows, and stops after the row
// that follows the one holding to. Given mark, it tells mark of the rows it
// finds, marks bytes apart, and the screen columns they begin at.
func RowsFrom(t Text, start, from, col, n, to int, wr Wrap, mark func(p, x int, plain bool)) []int {
	cols, hang, tab, in, words := wr.Cols, wr.Hang, wr.Tab, wr.In, wr.Words
	rows := []int{from}
	end := LineEnd(t, start)
	// No cluster is wider than its bytes, and no tab wider than tab, so a
	// line this short fits on one row.
	if from == start && end-start <= cols && !inlinesOn(in, start, end) {
		if line := t.Slice(start, end); len(line)+(tab-1)*strings.Count(line, "\t") <= cols {
			return rows
		}
	}
	x, left := 0, 0 // x is the column on the row, and left where its text starts
	if from > start {
		x, left = hang, hang
	}
	note, spoil := marker(from, mark)
	// add starts a row at p, at screen column at, or reports false once
	// there are n or the last starts after to.
	add := func(p, at int) bool {
		if len(rows) == n || rows[len(rows)-1] > to {
			return false
		}
		rows = append(rows, p)
		note(p, at)
		return true
	}
	// brk is just after the row's last space, brkX its column on the row
	// and brkAt its screen column.
	brk, brkX, brkAt := -1, 0, 0
	var run func(p, at, k int) bool
	if !words {
		// Each character of a run is one column wide, so a row takes as
		// many as fit.
		run = func(p, at, k int) bool {
			for k > 0 {
				if x > left && x+1 > cols {
					if !add(p, at) {
						return false
					}
					x, left = hang, hang
				}
				m := max(cols-x, 0)
				if x == left {
					m = max(m, 1)
				}
				m = min(m, k)
				x, p, at, k = x+m, p+m, at+m, k-m
			}
			return true
		}
	}
	walkFrom(t, start, from, col, tab, in, func(p, at, w int, c string) bool {
		for x > left && x+w > cols {
			if words && brk > rows[len(rows)-1] {
				if !add(brk, brkAt) {
					return false
				}
				x, brk = hang+x-brkX, -1
			} else {
				if !add(p, at) {
					return false
				}
				x = hang
			}
			left = hang
		}
		x += w
		if words && c == " " {
			brk, brkX, brkAt = p+1, x, at+w
		}
		if !printable(c) {
			spoil()
		}
		return true
	}, run, wr.Plain)
	return rows
}
