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
	for i, it := range items {
		style := base
		if i == sel {
			style = colours["menu.selected"]
			c.s.FillArea(left+1, top+3+i, inner, 1, ' ', style)
			c.s.Put(left, top+3+i, "▌", base.Foreground(th.cmd))
		}
		c.putClipped(left+4, top+3+i, left+inner+1, part{it, style})
	}
	return left + 4 + len(before) - skip, top + 1
}

// toast draws msg in a box in the top right corner of a screen w columns
// wide, its lines cut to half the screen (B149).
func (c *client) toast(msg string, w int) {
	lines := strings.Split(msg, "\n")
	inner := 0
	for _, l := range lines {
		inner = max(inner, utf8.RuneCountInString(l))
	}
	inner = min(inner+2, max(w/2, 20), w-4)
	x := w - inner - 3
	c.panel(x, 0, inner, len(lines))
	for i, l := range lines {
		c.putClipped(x+2, 1+i, x+inner+1, part{l, base})
	}
}
