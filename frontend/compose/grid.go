package compose

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"

	look "github.com/y3owk1n/fude/frontend/theme"
)

// widths measures clusters as tcell does, so a Grid's cells line up with
// the screen's.
var widths = func() displaywidth.Options {
	if rw := strings.ToLower(os.Getenv("RUNEWIDTH_EASTASIAN")); rw == "1" || rw == "true" || rw == "yes" {
		return displaywidth.Options{EastAsianWidth: true}
	}
	return displaywidth.Options{}
}()

// Grid is a window's cells as composed, kept between frames. It records
// which cells changed since it last painted.
type Grid struct {
	w, h   int
	at     [2]int // the screen cell its top left was last painted at
	cells  []gridCell
	lo, hi []int          // by row, the columns changed, lo > hi for none
	lines  map[int]*shown // by row, what each line was composed from
}

type gridCell struct {
	text  string
	style look.Style
	width int
}

// NewGrid returns a Grid w wide and h tall of blanks in style, every cell
// changed.
func NewGrid(w, h int, style look.Style) *Grid {
	g := &Grid{w: w, h: h, at: [2]int{-1, -1}, cells: make([]gridCell, w*h), lo: make([]int, h), hi: make([]int, h), lines: map[int]*shown{}}
	for i := range g.cells {
		g.cells[i] = gridCell{" ", style, 1}
	}
	g.touchAll()
	return g
}

// Clear makes every cell a blank in style, as the screen under the Grid
// now is, so no cell needs painting. It also forgets the lines composed.
func (g *Grid) Clear(style look.Style) {
	for i := range g.cells {
		g.cells[i] = gridCell{" ", style, 1}
	}
	for y := range g.h {
		g.lo[y], g.hi[y] = g.w, -1
	}
	clear(g.lines)
}

// touchAll counts every cell changed, to paint them all.
func (g *Grid) touchAll() {
	for y := range g.h {
		g.lo[y], g.hi[y] = 0, g.w-1
	}
}

func (g *Grid) touch(x, y int) {
	g.lo[y], g.hi[y] = min(g.lo[y], x), max(g.hi[y], x)
}

// Put puts the first cluster of str at x, y in style, as tcell's Put does,
// and returns the rest of str and the cluster's width. A colour of None
// keeps the cell's.
func (g *Grid) Put(x, y int, str string, style look.Style) (string, int) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return str, 0
	}
	c := &g.cells[y*g.w+x]
	var cl string
	width := 0
	switch {
	case str == c.text && c.width > 0:
		cl, width, str = str, c.width, ""
	case len(str) > 0 && str[0] >= ' ' && str[0] <= '~' && (len(str) == 1 || str[1] < utf8.RuneSelf):
		cl, width, str = str[:1], 1, str[1:]
	default:
		for gs := widths.StringGraphemes(str); width == 0 && gs.Next(); {
			cl += gs.Value()
			width = gs.Width()
			str = str[len(gs.Value()):]
		}
	}
	if style.Fg == look.None {
		style.Fg = c.style.Fg
	}
	if style.Bg == look.None {
		style.Bg = c.style.Bg
	}
	if cl != c.text || width != c.width || style != c.style {
		// A wide cell covers the next, and one that was wide uncovers it.
		g.touch(x, y)
		g.touch(min(x+max(width, c.width, 1)-1, g.w-1), y)
		c.text, c.width, c.style = cl, width, style
	}
	return str, width
}

// Get returns the cluster at x, y, its style and its width, a blank for a
// cell with none, as tcell's Get does.
func (g *Grid) Get(x, y int) (string, look.Style, int) {
	if x < 0 || y < 0 || x >= g.w || y >= g.h {
		return "", look.Style{}, 0
	}
	c := g.cells[y*g.w+x]
	if c.width == 0 || c.text == "" {
		return " ", c.style, 1
	}
	return c.text, c.style, c.width
}

// FillArea puts r in style in every cell of the w by h area at x, y.
func (g *Grid) FillArea(x, y, w, h int, r rune, style look.Style) {
	s := string(r)
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			g.Put(col, row, s, style)
		}
	}
}

// putClipped puts text from x on row y, cut at end, and returns the column
// after it.
func (g *Grid) putClipped(x, y, end int, text string, style look.Style) int {
	for rest := text; rest != "" && x < end; {
		var w int
		rest, w = g.Put(x, y, rest, style)
		x += w
	}
	return x
}

// Size returns how many columns and rows g has. A nil Grid has none, so one
// check tells a frontend to make a window's grid when it has none or when
// the window resized.
func (g *Grid) Size() (int, int) {
	if g == nil {
		return 0, 0
	}
	return g.w, g.h
}

// Paint gives put each cell changed since the last Paint, at its column and
// row counted from x, y. When g moved since then, it gives every cell. put
// does not get the cell a wide cell covers.
func (g *Grid) Paint(x, y int, put func(x, y int, text string, style look.Style)) {
	if g.at != [2]int{x, y} {
		g.at = [2]int{x, y}
		g.touchAll()
	}
	for row := range g.h {
		for col := g.lo[row]; col <= g.hi[row]; {
			c := &g.cells[row*g.w+col]
			put(x+col, y+row, c.text, c.style)
			col += max(c.width, 1)
		}
		g.lo[row], g.hi[row] = g.w, -1
	}
}
