package term

import (
	"strings"
	"unicode/utf8"
)

// panel draws a box with a rounded border around w columns and h rows
// from x and y, the border's corner, filled with the surface colour.
func (c *client) panel(x, y, w, h int) {
	edge := base.Background(th.surface).Foreground(th.border)
	c.s.FillArea(x, y, w+2, h+2, ' ', base.Background(th.surface))
	c.s.PutStrStyled(x, y, "╭"+strings.Repeat("─", w)+"╮", edge)
	for i := 1; i <= h; i++ {
		c.s.Put(x, y+i, "│", edge)
		c.s.Put(x+w+1, y+i, "│", edge)
	}
	c.s.PutStrStyled(x, y+h+1, "╰"+strings.Repeat("─", w)+"╯", edge)
}

// commandLine draws the line being typed in a box in the upper middle of
// a screen w columns by h rows, its prompt in the colour of commands or of
// search, and returns where its cursor is and the box's left column and
// bottom row, for the completion menu to go under (B148).
func (c *client) commandLine(w, h int) (cx, cy, left, bottom int) {
	line := c.e.Line()
	prompt, rest := line[:1], line[1:]
	inner := max(min(60, w-4), 4)
	left, top := (w-inner-2)/2, max(h/5, 1)
	c.panel(left, top, inner, 1)
	colour := th.normal
	if prompt == ":" {
		colour = th.cmd
	}
	surface := base.Background(th.surface)
	c.s.Put(left+2, top+1, prompt, surface.Foreground(colour).Bold(true))
	// A line longer than the box shows its end, where the cursor usually is.
	before := []rune(rest[:c.e.LineCursor()-1])
	room := inner - 4
	skip := max(len(before)-room, 0)
	c.putClipped(left+4, top+1, left+inner+1, part{string([]rune(rest)[skip:]), surface})
	return left + 4 + len(before) - skip, top + 1, left, top + 2
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
		c.putClipped(x+2, 1+i, x+inner+1, part{l, base.Background(th.surface)})
	}
}
