package tui

import (
	"os"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"

	look "github.com/y3owk1n/fude/internal/theme"
)

// widths measures clusters as tcell does, so a grid's cells line up with
// the screen's.
var widths = func() displaywidth.Options {
	if rw := strings.ToLower(os.Getenv("RUNEWIDTH_EASTASIAN")); rw == "1" || rw == "true" || rw == "yes" {
		return displaywidth.Options{EastAsianWidth: true}
	}
	return displaywidth.Options{}
}()

// grid is a window's cells as composed, kept between frames. It records
// which cells changed since it last painted.
type grid struct {
	w, h   int
	at     [2]int // the screen cell its top left was last painted at
	frame  int    // the last frame it was drawn in
	cells  []gridCell
	lo, hi []int          // by row, the columns changed, lo > hi for none
	lines  map[int]*shown // by row, what each line was composed from
}

type gridCell struct {
	text  string
	style look.Style
	width int
}

// newGrid returns a grid w wide and h tall of blanks in style, every cell
// changed.
func newGrid(w, h int, style look.Style) *grid {
	g := &grid{w: w, h: h, at: [2]int{-1, -1}, cells: make([]gridCell, w*h), lo: make([]int, h), hi: make([]int, h), lines: map[int]*shown{}}
	for i := range g.cells {
		g.cells[i] = gridCell{" ", style, 1}
	}
	g.touchAll()
	return g
}

// clear makes every cell a blank in style, as the screen under the grid
// now is, so no cell needs painting. It also forgets the lines composed.
func (g *grid) clear(style look.Style) {
	for i := range g.cells {
		g.cells[i] = gridCell{" ", style, 1}
	}
	for y := range g.h {
		g.lo[y], g.hi[y] = g.w, -1
	}
	clear(g.lines)
}

// touchAll counts every cell changed, to paint them all.
func (g *grid) touchAll() {
	for y := range g.h {
		g.lo[y], g.hi[y] = 0, g.w-1
	}
}

func (g *grid) touch(x, y int) {
	g.lo[y], g.hi[y] = min(g.lo[y], x), max(g.hi[y], x)
}

// Put puts the first cluster of str at x, y in style, as tcell's Put does,
// and returns the rest of str and the cluster's width. A colour of None
// keeps the cell's.
func (g *grid) Put(x, y int, str string, style look.Style) (string, int) {
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
func (g *grid) Get(x, y int) (string, look.Style, int) {
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
func (g *grid) FillArea(x, y, w, h int, r rune, style look.Style) {
	s := string(r)
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			g.Put(col, row, s, style)
		}
	}
}

// putClipped puts text from x on row y, cut at end, and returns the column
// after it.
func (g *grid) putClipped(x, y, end int, text string, style look.Style) int {
	for rest := text; rest != "" && x < end; {
		var w int
		rest, w = g.Put(x, y, rest, style)
		x += w
	}
	return x
}

// paint writes the changed cells to s with the grid's top left at x, y,
// every cell when the grid moved since it last painted.
func (g *grid) paint(s tcell.Screen, x, y int) {
	if g.at != [2]int{x, y} {
		g.at = [2]int{x, y}
		g.touchAll()
	}
	// Cells side by side mostly share a style, so paint converts a style to
	// tcell's once for each run of them.
	var last look.Style
	ts := tcellStyle(last)
	for row := range g.h {
		for col := g.lo[row]; col <= g.hi[row]; {
			c := &g.cells[row*g.w+col]
			if c.style != last {
				last, ts = c.style, tcellStyle(c.style)
			}
			s.Put(x+col, y+row, c.text, ts)
			col += max(c.width, 1)
		}
		g.lo[row], g.hi[row] = g.w, -1
	}
}
