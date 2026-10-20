package term

import (
	"cmp"
	imgcolor "image/color"

	"github.com/charmbracelet/x/ansi"
	"github.com/gdamore/tcell/v3/color"
	"github.com/y3owk1n/fude/wire"
)

// cells draws a terminal window's screen in the program's colours, then
// its separator and status line, and reports where the program's cursor
// is (B205).
func (c *client) cells(v wire.Window, left int) (int, int, bool) {
	c.s.FillArea(v.X, v.Y, v.W, v.H-1, ' ', base)
	for y, row := range v.Cells[:min(len(v.Cells), v.H-1)] {
		for x, cell := range row[:min(len(row), v.W)] {
			// The second column of a wide character is the first's.
			if cell.Width == 0 {
				continue
			}
			style := base.Bold(cell.Bold).Dim(cell.Faint).Italic(cell.Italic).Reverse(cell.Reverse).StrikeThrough(cell.Strike)
			if fg, ok := termColour(cell.Fg); ok {
				style = style.Foreground(fg)
			}
			if bg, ok := termColour(cell.Bg); ok {
				style = style.Background(bg)
			}
			if cell.Underline {
				style = style.Underline(true)
			}
			c.s.Put(v.X+x, v.Y+y, cmp.Or(cell.Text, " "), style)
		}
	}
	c.edge(v, left)
	return v.X + v.CellX, v.Y + v.CellY, v.Current && v.CellX >= 0
}

// termColour returns a terminal cell's colour as tcell draws it, or false
// for the theme's. The 256 indexed colours are the outer terminal's own.
func termColour(c imgcolor.Color) (color.Color, bool) {
	switch c := c.(type) {
	case nil:
		return 0, false
	case ansi.BasicColor:
		return color.PaletteColor(int(c)), true
	case ansi.IndexedColor:
		return color.PaletteColor(int(c)), true
	}
	return color.FromImageColor(c), true
}
