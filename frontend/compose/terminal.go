package compose

import (
	"cmp"
	"image/color"

	"github.com/charmbracelet/x/ansi"

	look "github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
)

// Terminal puts a terminal window's screen in g, in lk's colours where a
// cell has none of its own, and returns where its program's cursor is
// (B205).
func Terminal(g *Grid, v wire.Window, lk look.Look) Composed {
	for y, row := range v.Cells[:min(len(v.Cells), v.H)] {
		for x, c := range row[:min(len(row), v.W)] {
			// The second column of a wide character is the first's.
			if c.Width == 0 {
				continue
			}
			st := look.Style{
				Fg: termRGB(c.Fg, lk, lk.Base.Fg), Bg: termRGB(c.Bg, lk, lk.Base.Bg),
				Bold: c.Bold, Italic: c.Italic, Underline: c.Underline, Strike: c.Strike,
			}
			if c.Faint {
				st.Fg = look.Mix(st.Fg, st.Bg, 0.5)
			}
			if c.Reverse {
				st.Fg, st.Bg = st.Bg, st.Fg
			}
			g.Put(x, y, cmp.Or(c.Text, " "), st)
		}
	}
	return Composed{X: v.CellX, Y: v.CellY, Found: v.CellX >= 0}
}

// termRGB is a terminal cell's colour, or none when the cell has no colour
// of its own. The sixteen basic colours are the theme's, so a program's
// colours match it.
func termRGB(c color.Color, lk look.Look, none look.RGB) look.RGB {
	t := lk.Theme
	basic := [16]look.RGB{
		look.Mix(t.Bg, t.Text, 0.2), t.Error, t.Insert, t.Warn, t.Normal, t.Visual, t.Hint, t.Muted,
		t.Faint, t.Error, t.String, t.Number, t.Function, t.Keyword, t.Type, t.Text,
	}
	switch c := c.(type) {
	case nil:
		return none
	case ansi.BasicColor:
		return basic[c&15]
	case ansi.IndexedColor:
		if c < 16 {
			return basic[c]
		}
	}
	r, g, b, _ := c.RGBA()
	return look.RGB(r>>8<<16 | g>>8<<8 | b>>8)
}
