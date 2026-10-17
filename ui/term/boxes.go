package term

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/syntax"
)

// panel draws a box with a rounded border around w columns and h rows
// from x and y, the border's corner, cleared to the ground. A border line
// sits in the middle of its cells, so a coloured inside would show a seam
// between the line and the fill (B150).
func (c *client) panel(x, y, w, h int) {
	edge := base.Foreground(th.border)
	c.s.FillArea(x+1, y+1, w, h, ' ', base)
	c.s.PutStrStyled(x, y, "╭"+strings.Repeat("─", w)+"╮", edge)
	for i := 1; i <= h; i++ {
		c.s.Put(x, y+i, "│", edge)
		c.s.Put(x+w+1, y+i, "│", edge)
	}
	c.s.PutStrStyled(x, y+h+1, "╰"+strings.Repeat("─", w)+"╯", edge)
}

// rule draws a rule across a panel's inside at row y, joined to its border.
func (c *client) rule(x, y, w int) {
	c.s.PutStrStyled(x, y, "├"+strings.Repeat("─", w)+"┤", base.Foreground(th.border))
}

// commandLine draws the line being typed in a box in the upper middle of
// a screen w columns by h rows, its prompt in the colour of commands or of
// search, with menu's completions under a rule in the same box, and returns
// where its cursor is (B148).
func (c *client) commandLine(w, h int, menu string, spans []syntax.Span) (int, int) {
	line := c.e.Line()
	prompt, rest := line[:1], line[1:]
	inner := max(min(60, w-4), 4)
	left, top := (w-inner-2)/2, max(h/5, 1)
	var items []string
	if menu != "" {
		items = strings.Split(menu, "\n")
	}
	rows := 1
	if len(items) > 0 {
		rows += 1 + len(items)
	}
	c.panel(left, top, inner, rows)
	colour := th.normal
	if prompt == ":" {
		colour = th.cmd
	}
	c.s.Put(left+2, top+1, prompt, base.Foreground(colour).Bold(true))
	// A line longer than the box shows its end, where the cursor usually is.
	before := rest[:c.e.LineCursor()-1]
	skip := 0 // the bytes of rest left out at its start
	for displaywidth.String(before[skip:]) > inner-4 {
		_, n := utf8.DecodeRuneInString(before[skip:])
		skip += n
	}
	c.putClipped(left+4, top+1, left+inner+1, part{rest[skip:], base})
	if len(items) > 0 {
		c.rule(left, top+2, inner)
	}
	sel := -1
	for _, s := range spans {
		if s.Name == "menu.selected" {
			sel = strings.Count(menu[:s.Start], "\n")
		}
	}
	styles := paint(nil, spans, 0, len(menu))
	at := 0 // where the item starts in menu
	for i, it := range items {
		ground := base
		if i == sel {
			ground = colours["menu.selected"]
			c.s.FillArea(left+1, top+3+i, inner, 1, ' ', ground)
			c.s.Put(left, top+3+i, "▌", base.Foreground(th.cmd))
		}
		x := left + 4
		for k, r := range it {
			if x >= left+inner+1 {
				break
			}
			style := ground
			if p := at + k; p < len(styles) && styles[p] == colours["match"] {
				style = colours["match"].Background(ground.GetBackground())
			}
			_, cw := c.s.Put(x, top+3+i, string(r), style)
			x += cw
		}
		at += len(it) + 1
	}
	return left + 4 + displaywidth.String(before[skip:]), top + 1
}

// toast draws msg in a box at the top right of the windows, which cover
// the columns from left to right. Its lines wrap at half that width. A
// message taller than a third of the h rows ends in an ellipsis (B149,
// B199).
func (c *client) toast(msg string, left, right, h int) {
	longest := 0
	for l := range strings.SplitSeq(msg, "\n") {
		longest = max(longest, utf8.RuneCountInString(l))
	}
	w := right - left
	inner := min(longest+2, max(w/2, 20), w-4)
	rows, _ := wrap(msg, nil, inner-2)
	lines := make([]string, len(rows))
	for i, r := range rows {
		lines[i] = msg[r.start:r.end]
	}
	if limit := max(h/3, 1); len(lines) > limit {
		lines = lines[:limit]
		last := []rune(lines[limit-1])
		lines[limit-1] = string(last[:min(len(last), inner-3)]) + "…"
	}
	x := right - inner - 3
	c.panel(x, 0, inner, len(lines))
	for i, l := range lines {
		c.putClipped(x+2, 1+i, x+inner+1, part{l, base})
	}
}

// float draws f in a bordered box at the cursor, cx and cy, below its line,
// or above it when there is more room there, at most 80 columns and 20 rows.
// Its documentation goes in a box beside it. w and h are the space the
// windows have. While f has the cursor, its rows scroll to show it, and
// float reports where it is.
func (c *client) float(f editor.Float, cx, cy, w, h int) (int, int, bool) {
	if f.W > 2 && f.H > 2 {
		if f.DocW > 2 {
			// The documentation box is as tall as the box beside it.
			doc := f.Doc + strings.Repeat("\n", max(f.H-3-strings.Count(f.Doc, "\n"), 0))
			rows, _ := wrap(doc, f.DocSpans, f.DocW-4)
			c.box(doc, f.DocSpans, rows[:min(len(rows), f.H-2)], f.X+f.W, f.Y, f.DocW-4, -1)
		}
		rows, _ := wrap(f.Text, f.Spans, f.W-4)
		first := scroll(rows, f, f.H-2)
		x, y, ok := c.box(f.Text, f.Spans, rows[first:min(first+f.H-2, len(rows))], f.X, f.Y, f.W-4, f.Cursor)
		if f.Title != "" {
			c.s.PutStrStyled(f.X+2, f.Y, " "+f.Title+" ", base.Foreground(th.muted))
		}
		return x, y, ok
	}
	rows, width := wrap(f.Text, f.Spans, min(80, w-4))
	below, above := h-cy-1, cy
	n := min(len(rows), max(below, above)-2, 20)
	if width < 1 || n < 1 {
		return 0, 0, false
	}
	first := scroll(rows, f, n)
	y := cy + 1
	if below < n+2 && above > below {
		y = cy - n - 2
	}
	x := min(cx, max(w-width-4, 0))
	fx, fy, found := c.box(f.Text, f.Spans, rows[first:min(first+n, len(rows))], x, y, width, f.Cursor)
	// The documentation goes right of the box, or left of it when there is
	// more room there.
	right, left := w-x-width-4, x
	if room := max(right, left) - 4; f.Doc != "" && room >= 10 {
		rows, dw := wrap(f.Doc, f.DocSpans, min(80, room))
		dx := x + width + 4
		if right < left {
			dx = x - dw - 4
		}
		c.box(f.Doc, f.DocSpans, rows[:min(len(rows), h-y-2, 20)], dx, y, dw, -1)
	}
	return fx, fy, found
}

// above draws f in a box above the cursor's line at cx and cy, at most 80
// columns and 10 rows. When it does not fit there and there is more room
// below, it goes below. w and h are the space the windows have.
func (c *client) above(f editor.Float, cx, cy, w, h int) {
	rows, width := wrap(f.Text, f.Spans, min(80, w-4))
	want, up, down := min(len(rows), 10), cy-2, h-cy-3
	n, y := min(want, up), cy-min(want, up)-2
	if up < want && down > up {
		n, y = min(want, down), cy+1
	}
	if width < 1 || n < 1 {
		return
	}
	c.box(f.Text, f.Spans, rows[:n], min(cx, max(w-width-4, 0)), y, width, -1)
}

// scroll returns the first of rows to show in a box n rows tall. It is f's
// top row, moved so the cursor's row shows when f has the cursor. A menu
// without the cursor shows its selected item (B64).
func scroll(rows []row, f editor.Float, n int) int {
	// rowOf is the row that shows the byte at p.
	rowOf := func(p int) int {
		i, _ := slices.BinarySearchFunc(rows, p, func(r row, p int) int { return cmp.Compare(r.start, p+1) })
		return max(i-1, 0)
	}
	if f.Cursor < 0 {
		i := slices.IndexFunc(f.Spans, func(s syntax.Span) bool { return s.Name == "menu.selected" })
		if i < 0 {
			return 0
		}
		return max(rowOf(f.Spans[i].Start)-n+1, 0)
	}
	cur := rowOf(f.Cursor)
	return min(max(rowOf(f.Top), cur-n+1), cur)
}

// row is one row of a box: bytes of its text, or a rule across it.
type row struct {
	start, end int
	rule       bool
}

// wrap splits text into rows at most limit columns wide and returns them
// with the widest one's width. A row ends after the last space that fits,
// or at the limit when none does. A markup.rule line is a rule.
func wrap(text string, spans []syntax.Span, limit int) ([]row, int) {
	var rows []row
	width := 0
	for at := 0; at <= len(text); {
		end := strings.IndexByte(text[at:], '\n')
		if end < 0 {
			end = len(text) - at
		}
		end += at
		if slices.ContainsFunc(spans, func(s syntax.Span) bool { return s.Name == "markup.rule" && s.Start == at }) {
			rows = append(rows, row{at, end, true})
		} else {
			start, col := at, 0
			brk, brkCol := start, 0 // just after the row's last space, and its column
			for i, r := range text[at:end] {
				cw := 1
				if r == '\t' {
					cw = 4
				}
				if col+cw > limit {
					if brk == start {
						brk, brkCol = at+i, col
					}
					rows = append(rows, row{start, brk, false})
					start, col = brk, col-brkCol
				}
				col += cw
				width = max(width, col)
				if r == ' ' {
					brk, brkCol = at+i+1, col
				}
			}
			rows = append(rows, row{start, end, false})
		}
		at = end + 1
	}
	return rows, width
}

// box draws rows of text, styled by spans, in a panel at x and y, width
// columns wide inside it, and reports where the byte at cursor is. A row a
// selected item starts takes the raised colour, with an accent bar for its
// left border (B150).
func (c *client) box(text string, spans []syntax.Span, rows []row, x, y, width, cursor int) (int, int, bool) {
	styles := paint(nil, spans, 0, len(text))
	style := base
	fx, fy, found := 0, 0, false
	// The text has a column of room on either side inside the border.
	c.panel(x, y, width+2, len(rows))
	x++
	for i, r := range rows {
		ground := base.GetBackground()
		if r.rule {
			c.rule(x-1, y+1+i, width+2)
		}
		if slices.ContainsFunc(spans, func(s syntax.Span) bool {
			return s.Name == "menu.selected" && s.Start <= r.start && r.start < s.End
		}) {
			ground = th.raised
			c.s.FillArea(x, y+1+i, width+2, 1, ' ', style.Background(ground))
			c.s.Put(x-1, y+1+i, "▌", base.Foreground(th.normal))
		}
		col := 0
		for rest := text[r.start:r.end]; ; {
			p := r.end - len(rest)
			// The end of a wrapped row is the start of the next one.
			if p == cursor && (rest != "" || i == len(rows)-1 || rows[i+1].start != p) {
				fx, fy, found = x+1+min(col, width-1), y+1+i, true
			}
			if r.rule || rest == "" {
				break
			}
			s := style
			if p < len(styles) {
				s = styles[p]
			}
			s = s.Background(ground)
			if rest[0] == '\t' {
				col, rest = col+4, rest[1:]
				continue
			}
			next, cw := c.s.Put(x+1+col, y+1+i, rest, s)
			// Put draws nothing off the screen, so the row stops there.
			if len(next) == len(rest) {
				break
			}
			rest, col = next, col+cw
		}
	}
	return fx, fy, found
}
