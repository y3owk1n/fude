package vim

import (
	"strings"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
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
// its position, the screen column it starts at, and how wide it is. A place
// is the inlines at a position, then the cluster there unless one of them
// hides it. A tab is as wide as it is at its column in the text, hidden
// text counting. f returns false to stop.
func walk(t Text, start, tab int, in []Inline, f func(p, x, w int) bool) {
	line := t.Slice(start, LineEnd(t, start))
	end := start + len(line)
	k := 0
	for k < len(in) && in[k].Start < start {
		k++
	}
	x, col := 0, 0
	g := graphemes.FromString(line)
	more := g.Next()
	for {
		p := end
		if more {
			p = start + g.Start()
		}
		w, hidden := 0, p
		for ; k < len(in) && in[k].Start <= p; k++ {
			w += displaywidth.String(inlineText.Replace(in[k].Text))
			hidden = max(hidden, min(in[k].End, end))
		}
		if hidden > p {
			for more && start+g.Start() < hidden {
				col += width(g.Value(), col, tab)
				more = g.Next()
			}
			if !f(p, x, w) {
				return
			}
			x += w
			continue
		}
		if !more {
			if w > 0 {
				f(p, x, w)
			}
			return
		}
		cw := width(g.Value(), col, tab)
		if !f(p, x, w+cw) {
			return
		}
		x, col = x+w+cw, col+cw
		more = g.Next()
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
	walk(t, start, tab, in, func(p, x, w int) bool {
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
	walk(t, start, tab, in, func(p, x, _ int) bool {
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

// Rows returns where each screen row of the line at start begins when it
// wraps at cols columns, the rows after the first starting hang columns
// in: start, then each place that does not fit on the row before it.
func Rows(t Text, start, cols, hang, tab int, in []Inline) []int {
	rows := []int{start}
	end := LineEnd(t, start)
	line := t.Slice(start, end)
	// No cluster is wider than its bytes, and no tab wider than tab, so a
	// line this short fits on one row.
	if len(line)+(tab-1)*strings.Count(line, "\t") <= cols && !inlinesOn(in, start, end) {
		return rows
	}
	x, left := 0, 0 // left is the column the row's text starts at
	walk(t, start, tab, in, func(p, _, w int) bool {
		if x > left && x+w > cols {
			rows, x, left = append(rows, p), hang, hang
		}
		x += w
		return true
	})
	return rows
}

// WordRows is Rows that ends a row after its last space that fits, or
// where Rows does on a row with none, as a float wraps its text.
func WordRows(t Text, start, cols, hang, tab int, in []Inline) []int {
	rows := Rows(t, start, cols, hang, tab, in)
	if len(rows) == 1 {
		return rows
	}
	rows = rows[:1]
	// brk is just after the row's last space, and brkX its column.
	x, brk, brkX, left := 0, -1, 0, 0
	walk(t, start, tab, in, func(p, _, w int) bool {
		for x > left && x+w > cols {
			if brk > rows[len(rows)-1] {
				rows, x, brk = append(rows, brk), hang+x-brkX, -1
			} else {
				rows, x = append(rows, p), hang
			}
			left = hang
		}
		x += w
		if t.Slice(p, min(p+1, t.Len())) == " " {
			brk, brkX = p+1, x
		}
		return true
	})
	return rows
}
