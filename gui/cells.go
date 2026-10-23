package gui

import (
	"math"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/frontend/theme"
)

// drawCells draws cells and the cursor on them, the first cell at o. A run
// of one-column ASCII in one style draws as one string, and any other cell
// alone at its own column, so the font's fallbacks for other characters
// never shift the row. A cell whose ground is clear draws none, so what is
// under it shows, and the text of chosen rows is the system's selected
// text.
func drawCells(cells [][]cell, cursor cursorAt, o point, clear *theme.RGB, chosen []rule) {
	picked := func(x, y int) bool {
		return slices.ContainsFunc(chosen, func(r rule) bool { return r.y == y && x >= r.x && x < r.x+r.w })
	}
	ascii := func(c cell) bool { return c.width == 1 && len(c.text) == 1 && c.text[0] >= ' ' && c.text[0] <= '~' }
	for y, row := range cells {
		for x := 0; x < len(row); {
			c := row[x]
			n := 1
			for ascii(c) && x+n < len(row) && ascii(row[x+n]) && row[x+n].style == c.style {
				n++
			}
			r := at(x, y, max(n, c.width), o)
			if clear == nil || c.style.Bg != *clear {
				fillRGB(c.style.Bg, r)
			}
			var text strings.Builder
			for _, o := range row[x : x+n] {
				text.WriteString(o.text)
			}
			if t := strings.TrimRight(text.String(), " "); t != "" {
				colour := ""
				if picked(x, y) {
					colour = "alternateSelectedControlTextColor"
				}
				drawString(t, c.style, colour, r.Origin)
			}
			decorate(c.style, r)
			x += max(n, c.width)
		}
	}
	if c := cursor; c.shown && c.y >= 0 && c.x >= 0 && c.y < len(cells) && c.x < len(cells[c.y]) {
		r := at(c.x, c.y, 1, o)
		switch c.shape {
		case 1:
			r.Size.W = 2
		case 2:
			r.Origin.Y, r.Size.H = r.Origin.Y+cellH-2, 2
		}
		fillRGB(cells[c.y][c.x].style.Fg, r)
		if under := cells[c.y][c.x]; c.shape == 0 && strings.TrimSpace(under.text) != "" {
			drawString(under.text, theme.Style{Fg: under.style.Bg}, "", r.Origin)
		}
	}
}

// decorate draws the lines s asks for over the text in r: a strike through
// its middle and an underline under it. An underline with a colour of its
// own, as a diagnostic's has, is wavy.
func decorate(s theme.Style, r rect) {
	if s.Strike {
		fillRGB(s.Fg, rect{point{r.Origin.X, r.Origin.Y + math.Floor(cellH/2)}, size{r.Size.W, 1}})
	}
	switch {
	case !s.Underline:
	case s.UnderSet:
		wavy(s.Under, r.Origin.X, r.Origin.X+r.Size.W, r.Origin.Y+cellH-2)
	default:
		fillRGB(s.Fg, rect{point{r.Origin.X, r.Origin.Y + cellH - 2}, size{r.Size.W, 1}})
	}
}
