package term

import (
	"strings"
	"unicode/utf8"

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
	before := []rune(rest[:c.e.LineCursor()-1])
	room := inner - 4
	skip := max(len(before)-room, 0)
	c.putClipped(left+4, top+1, left+inner+1, part{string([]rune(rest)[skip:]), base})
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
	return left + 4 + len(before) - skip, top + 1
}

// toast draws msg in a box in the top right corner of a screen w columns
// wide and h rows tall. Its lines wrap at half the screen's width. A
// message taller than a third of the rows ends in an ellipsis (B149).
func (c *client) toast(msg string, w, h int) {
	longest := 0
	for l := range strings.SplitSeq(msg, "\n") {
		longest = max(longest, utf8.RuneCountInString(l))
	}
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
	x := w - inner - 3
	c.panel(x, 0, inner, len(lines))
	for i, l := range lines {
		c.putClipped(x+2, 1+i, x+inner+1, part{l, base})
	}
}
