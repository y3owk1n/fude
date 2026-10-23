package term

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// panel draws a box with a rounded border around w columns and h rows
// from x and y, the border's corner, cleared to the ground. A border line
// sits in the middle of its cells, so a coloured inside would show a seam
// between the line and the fill (B150).
func (c *client) panel(x, y, w, h int) {
	c.img.box(x, y, w+2, h+2)
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
	before := carets(rest[:c.e.LineCursor()-1])
	rest = carets(rest)
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
	styles := paint(nil, spans, []wire.Range{{End: len(menu) - 1}})
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

// confirm draws q, a yes or no question, in a box in the middle of a
// screen w columns by h rows, with the keys that answer it under a rule
// (B362).
func (c *client) confirm(q string, w, h int) {
	longest := 0
	for l := range strings.SplitSeq(q, "\n") {
		longest = max(longest, displaywidth.String(l))
	}
	inner := max(min(max(longest, 30)+2, 60, w-4), 4)
	rows, _ := wrap(q, nil, inner-2)
	left, top := (w-inner-2)/2, max((h-len(rows)-4)/2, 0)
	c.panel(left, top, inner, len(rows)+2)
	for i, r := range rows {
		c.putClipped(left+2, top+1+i, left+inner+1, part{q[r.start:r.end], base.Bold(true)})
	}
	y := top + 1 + len(rows)
	c.rule(left, y, inner)
	key, label := base.Foreground(th.cmd).Bold(true), base.Foreground(th.muted)
	x := left + 2
	for _, p := range []part{{"y", key}, {" yes   ", label}, {"n", key}, {" no", label}} {
		x = c.putClipped(x, y+1, left+inner+1, p)
	}
}

// menu draws f in a bordered box at the cursor, cx and cy, below its line,
// or above it when there is more room there, at most 80 columns and 20 rows.
// Its documentation goes in a box beside it. The windows cover the columns
// from lo to hi, which :center narrows, and h rows (B199). While f has
// the cursor, its rows scroll to show it, and
// menu reports where it is.
func (c *client) menu(f wire.Menu, cx, cy, lo, hi, h int) (int, int, bool) {
	if f.W > 2 && f.H > 2 {
		if f.DocW > 2 {
			// The documentation box is as tall as the box beside it.
			doc := f.Doc + strings.Repeat("\n", max(f.H-3-strings.Count(f.Doc, "\n"), 0))
			rows, _ := wrap(doc, f.DocSpans, f.DocW-4)
			c.box(doc, f.DocSpans, rows[:min(len(rows), f.H-2)], f.X+f.W, f.Y, f.DocW-4, -1)
			if p := f.DocImage; p != nil {
				c.picture(*p, f.X+f.W+2, f.Y+1, f.DocW-4)
			}
		}
		n := f.H - 2
		all, _ := wrap(f.Text, f.Spans, f.W-4)
		first := c.scroll(all, f, n)
		x, y, ok := c.box(f.Text, f.Spans, all[first:min(first+n, len(all))], f.X, f.Y, f.W-4, f.Cursor)
		if f.Title != "" {
			c.s.PutStrStyled(f.X+2, f.Y, " "+f.Title+" ", base.Foreground(th.muted))
		}
		return x, y, ok
	}
	rows, width := wrap(f.Text, f.Spans, min(80, hi-lo-4))
	below, above := h-cy-1, cy
	n := min(len(rows), max(below, above)-2)
	if f.Rows > 0 {
		n = min(n, f.Rows)
	}
	if width < 1 || n < 1 {
		return 0, 0, false
	}
	first := c.scroll(rows, f, n)
	y := cy + 1
	if below < n+2 && above > below {
		y = cy - n - 2
	}
	x := max(min(cx, hi-width-4), lo)
	fx, fy, found := c.box(f.Text, f.Spans, rows[first:min(first+n, len(rows))], x, y, width, f.Cursor)
	if f.Doc == "" {
		return fx, fy, found
	}
	// The documentation goes right of the box, or left of it, when one side
	// has room for its widest line or 40 columns. Otherwise it goes under
	// the box, or over it when there is more room there, as wide as the
	// windows (B227).
	_, natural := wrap(f.Doc, f.DocSpans, 80)
	need := min(natural, 40)
	right, left := hi-x-width-8, x-lo-4
	switch {
	case right >= need:
		rows, dw := wrap(f.Doc, f.DocSpans, min(80, right))
		c.box(f.Doc, f.DocSpans, rows[:min(len(rows), h-y-2, 20)], x+width+4, y, dw, -1)
	case left >= need:
		rows, dw := wrap(f.Doc, f.DocSpans, min(80, left))
		c.box(f.Doc, f.DocSpans, rows[:min(len(rows), h-y-2, 20)], x-dw-4, y, dw, -1)
	default:
		rows, dw := wrap(f.Doc, f.DocSpans, min(80, hi-lo-4))
		dx := max(min(x, hi-dw-4), lo)
		// It leaves the cursor's line in view, whichever side of it the box
		// is on.
		top, bottom := cy, y+n+2
		if y < cy {
			top, bottom = y, cy+1
		}
		dy, fit := bottom, h-bottom-2
		if top > h-bottom {
			fit = top - 2
			dy = top - min(len(rows), fit, 20) - 2
		}
		if fit >= 1 {
			c.box(f.Doc, f.DocSpans, rows[:min(len(rows), fit, 20)], dx, dy, dw, -1)
		}
	}
	return fx, fy, found
}

// scroll returns the first of rows to show in a box n rows tall. With the
// cursor in f, it is the first that keeps the cursor's row showing. A menu
// without the cursor starts where the last frame showed it, and moves only
// as far as it must to show its selected item (B64).
func (c *client) scroll(rows []row, f wire.Menu, n int) int {
	// rowOf is the row that shows the byte at p.
	rowOf := func(p int) int {
		i, _ := slices.BinarySearchFunc(rows, p, func(r row, p int) int { return cmp.Compare(r.start, p+1) })
		return max(i-1, 0)
	}
	if f.Cursor < 0 {
		i := slices.IndexFunc(f.Spans, func(s syntax.Span) bool { return s.Name == "menu.selected" })
		if i < 0 {
			c.menuTop = 0
			return 0
		}
		sel := rowOf(f.Spans[i].Start)
		c.menuTop = max(min(max(c.menuTop, sel-n+1), sel, len(rows)-n), 0)
		return c.menuTop
	}
	cur := rowOf(f.Cursor)
	return min(max(cur-n+1, 0), cur)
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
	styles := paint(nil, spans, []wire.Range{{End: len(text) - 1}})
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

// floating draws v, a float, in a border titled by its name, with a column
// of room each side of its text and its foot under a rule over the bottom
// of the border, and reports where its cursor is (D55).
func (c *client) floating(v wire.Window, sel vim.Selection, visual bool) (int, int, bool) {
	var foot []string
	if v.Foot != "" {
		foot = strings.Split(v.Foot, "\n")
	}
	rows := v.H
	if len(foot) > 0 {
		rows += len(foot) + 1
	}
	c.panel(v.X-2, v.Y-1, v.W+2, rows)
	if v.Name != "" {
		c.putClipped(v.X, v.Y-1, v.X+v.W, part{" " + v.Name + " ", base.Foreground(th.muted)})
	}
	x, y, ok := c.window(v, v.X, sel, visual)
	// The float with the focus has its status line in its bottom border,
	// without its name, which the top border shows.
	if v.Current {
		left, right := c.statusParts(v, base, false)
		c.putParts(left, append([]part{{" ", base}}, right...), v.X-1, v.Y+rows, v.W+2)
	}
	if len(foot) == 0 {
		return x, y, ok
	}
	c.rule(v.X-2, v.Y+v.H, v.W+2)
	styles := paint(nil, v.FootSpans, []wire.Range{{End: len(v.Foot) - 1}})
	at := 0
	for i, line := range foot {
		col := v.X
		for rest := line; rest != "" && col < v.X+v.W; {
			s := base
			if p := at + len(line) - len(rest); p < len(styles) {
				s = styles[p]
			}
			next, w := c.s.Put(col, v.Y+v.H+1+i, rest, s)
			if len(next) == len(rest) {
				break
			}
			rest, col = next, col+w
		}
		at += len(line) + 1
	}
	return x, y, ok
}

// floatRows draws a rule across row of v, a float, when the line at pos is
// a markup.rule one.
func (c *client) floatRows(v wire.Window, pos, row int) {
	if slices.ContainsFunc(v.Highlights, func(s syntax.Span) bool { return s.Name == "markup.rule" && s.Start == pos }) ||
		slices.ContainsFunc(v.Decorations, func(s syntax.Span) bool { return s.Name == "markup.rule" && s.Start == pos }) {
		c.rule(v.X-2, v.Y+row, v.W+2)
	}
}
