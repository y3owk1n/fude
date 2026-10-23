package gui

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/compose"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// point, size and rect are in points, laid out as the system's own
// structs, so they pass to it as they are.
type (
	point struct{ X, Y float64 }
	size  struct{ W, H float64 }
	rect  struct {
		Origin point
		Size   size
	}
)

// pad is the room around the text, in points.
const pad = 6

type cell struct {
	text  string
	width int
	style theme.Style
}

type cursorAt struct {
	x, y  int
	shape int // 0 a block, 1 a bar, 2 an underline
	shown bool
}

// frame is what the loop hands the main thread after each change.
type frame struct {
	title   string
	layout  wire.Split // the zoomed window alone while one is
	key     string     // says what layout is, to tell when it changes
	wins    []shown
	floats  map[int]*boxed // by window id, and -1 and -2 for a menu and its documentation
	tabs    strip
	look    theme.Look     // the windows', dim under a modal box
	panel   *boxed         // the command line or the finder, nil for neither
	toast   *boxed         // the message, nil for none
	confirm string         // the question, "" for none
	asked   int            // counts each change of the question, to tell a new one
	copied  *string        // to put on the system's clipboard
	alert   string         // to tell the user of while the window is not in front, "" for none
	ground  theme.RGB      // the theme's own ground, for the window's look
	edited  bool           // a window shown has changes
	file    string         // the current window's file, "" for none
	busy    bool           // a status line turns a spinner, so frames go on
	pngs    map[int][]byte // by id, the images the main thread lacks
	freed   []int          // the images the session dropped
}

// shown is a window of the layout as a frame brings it, with the cells
// that changed, the cursor and its status line's parts.
type shown struct {
	id, w, h    int
	put         []placed
	cursor      cursorAt
	left, right []client.Part
	current     bool
	offers      []string
	pics        []pic
	scroll      [3]int // its first line shown, its rows and its text's lines
}

type placed struct {
	x, y int
	cell
}

// strip is what the tab strip in the title bar shows. It has the buffers
// as tabs and the tab pages at its right, the current page in the colour
// of mode. With neither it shows the title.
type strip struct {
	buffers []wire.Tab
	pages   []wire.Page
	mode    string
	title   string
}

// painter turns a session into the cells that changed, keeping each
// window's grid between frames.
type painter struct {
	comp    compose.Composer
	grids   map[int]*compose.Grid
	had     map[int]bool // the windows of the last frame's layout
	sent    map[int]bool // the images the main thread has
	name    string       // the current window's name, and its file
	file    string
	frame   int
	lookKey string
	modal   bool
	// lit is the theme's look, dim the windows' under a modal box, and
	// boxed lit on dim's ground for the box (B151).
	lit, dim, boxed theme.Look
}

func (p *painter) paint(s client.Session, cols, rows int) frame {
	p.frame++
	th := s.Theme()
	own, restyled := s.Styles()
	if key := themeKey(th, restyled); key != p.lookKey {
		t, ok := theme.Themes[th.Name]
		if !ok {
			t = theme.Themes["dark"]
		}
		if th.Base16 != nil {
			t = theme.FromBase16(*th.Base16)
		}
		if th.Colors != nil {
			t = t.With(*th.Colors)
		}
		p.lit, p.dim = theme.LookOf(t), theme.LookOf(t.Tint())
		ground := t
		ground.Bg = p.dim.Theme.Bg
		p.boxed = theme.LookOf(ground)
		same := func(c theme.RGB) theme.RGB { return c }
		p.lit.Restyle(own, same)
		p.boxed.Restyle(own, same)
		p.dim.Restyle(own, func(c theme.RGB) theme.RGB { return theme.Mix(c, p.dim.Theme.Bg, 0.55) })
		p.lookKey, p.grids = key, nil
	}
	menu, floats := s.Menu()
	question := s.Confirm()
	cmdline := s.Mode() == vim.Cmdline
	// A :s being typed shows its preview in the windows, so they stay lit.
	modal := cmdline && !s.Substituting() || floats && menu.Anchor == "middle" || question != ""
	if p.grids == nil || modal != p.modal {
		p.grids, p.modal = map[int]*compose.Grid{}, modal
	}
	look, boxLook := p.lit, p.lit
	if modal {
		look, boxLook = p.dim, p.boxed
	}
	wins := s.Windows()
	layout := s.Layout()
	for _, v := range wins {
		if v.Zoomed {
			layout = wire.Split{Window: v.ID}
		}
	}
	_, mode := client.ModeName(s)
	f := frame{
		title: s.Title(), layout: layout, key: fmt.Sprint(layout), floats: map[int]*boxed{}, pngs: map[int][]byte{}, ground: p.lit.Theme.Bg,
		tabs: strip{buffers: s.Buffers(), pages: s.Pages(), mode: mode}, look: look, confirm: question,
	}
	in := map[int]bool{}
	for _, id := range leaves(layout) {
		in[id] = true
	}
	sel, visual := s.Selection()
	floated := false // a float has the focus
	cx, cy := 0, 0   // where the cursor is in the area
	for _, v := range wins {
		if v.Float == "" && !in[v.ID] {
			continue
		}
		// The rows past the text show nothing, as a window's do.
		v.FillChars.Eob = " "
		var cursors []int
		if v.Current {
			cursors = s.Cursors()
		}
		o := compose.Options{
			Look: look, Frame: p.frame, Insert: s.Mode() == vim.Insert, Cursors: cursors,
			Selection: sel, Visual: visual && v.Current, Tint: look.Theme.Normal,
		}
		at := cursorAt{shape: shape(s)}
		// draw draws v in g: its text, or a terminal's screen.
		draw := func(g *compose.Grid, o compose.Options) compose.Composed {
			if v.Cells != nil {
				return termCells(g, v, o.Look)
			}
			return p.comp.Compose(g, v, o)
		}
		if v.Float != "" {
			// A float shows lit in a box of its own, composed whole.
			o.Look, o.Tint = boxLook, boxLook.Theme.Normal
			g := compose.NewGrid(v.W, v.H, boxLook.Base)
			out := draw(g, o)
			b := &boxed{look: boxLook, at: &[2]int{v.X, v.Y}, title: v.Name, cells: cellsOf(g), pics: pics(v, out)}
			// Its foot shows under a rule, and its status line under that
			// while it has the focus (D55, B159).
			if v.Foot != "" {
				foot, _ := compose.Wrap(v.Foot, v.FootSpans, v.W)
				fg := compose.NewGrid(v.W, len(foot)+1, boxLook.Base)
				write(fg, v.Foot, v.FootSpans, foot, boxLook, 0, 1, v.W)
				b.rules = []rule{{0, v.H, v.W}}
				b.cells = append(b.cells, cellsOf(fg)...)
			}
			if v.Current {
				b.left, b.right = client.StatusParts(s, v, false)
				f.busy = f.busy || turning(b.right)
			}
			if out.Found && v.Current {
				at.x, at.y, at.shown = out.X, out.Y, true
				b.cursor, floated = at, true
				cx, cy = v.X+out.X, v.Y+out.Y
			}
			f.floats[v.ID] = b
			continue
		}
		// A window new to the layout has a new pane, which takes every cell.
		g := p.grids[v.ID]
		if gw, gh := g.Size(); g == nil || !p.had[v.ID] || gw != v.W || gh != v.H {
			g = compose.NewGrid(v.W, v.H, look.Base)
			p.grids[v.ID] = g
		}
		out := draw(g, o)
		sh := shown{id: v.ID, w: v.W, h: v.H, current: v.Current, offers: v.Offers, scroll: [3]int{v.Text.LineOf(v.Top), v.H, v.Text.Lines()}}
		g.Paint(0, 0, func(x, y int, text string, width int, st theme.Style) {
			sh.put = append(sh.put, placed{x, y, cell{text, width, st}})
		})
		if out.Found && v.Current {
			at.x, at.y, at.shown = out.X, out.Y, true
			sh.cursor = at
			if !floated {
				cx, cy = v.X+out.X, v.Y+out.Y
			}
		}
		sh.left, sh.right = client.StatusParts(s, v, true)
		f.edited = f.edited || v.Modified
		// A name looks for its file on disk once.
		if v.Current && v.Name != p.name {
			p.name, p.file = v.Name, file(v.Name)
		}
		f.busy = f.busy || turning(sh.right)
		sh.pics = pics(v, out)
		f.wins = append(f.wins, sh)
	}
	p.had = in
	switch {
	case cmdline:
		// The completion menu is part of the command line's box.
		if !floats {
			menu = wire.Menu{}
		}
		f.panel = palette(s.Line(), s.LineCursor(), menu, boxLook)
	case floats && menu.Anchor == "middle":
		f.panel = finder(menu, boxLook)
	case floats && menu.Anchor == "cursor":
		for i, b := range p.atCursor(menu, boxLook, cx, cy, cols, rows) {
			f.floats[-1-i] = b
		}
	case floats && menu.Anchor == "bottom":
		f.floats[-1] = keyHints(menu, boxLook)
	}
	if msg := s.Message(); msg != "" {
		f.toast = &boxed{text: compose.Carets(msg), look: boxLook}
	}
	if text, ok := s.Clipboard(); ok {
		f.copied = &text
	}
	f.alert, _ = s.Alert()
	var all []pic
	for _, sh := range f.wins {
		all = append(all, sh.pics...)
	}
	for _, b := range f.floats {
		all = append(all, b.pics...)
	}
	if f.panel != nil {
		all = append(all, f.panel.pics...)
	}
	p.images(s, &f, all)
	f.file = p.file
	// While the palette, the finder or a question shows, no window shows
	// the cursor. A float with the focus shows it, and the window it opened
	// from does not.
	if f.panel != nil || question != "" || floated {
		for i := range f.wins {
			f.wins[i].cursor.shown = false
		}
	}
	if f.panel != nil || question != "" {
		for _, b := range f.floats {
			b.cursor.shown = false
		}
	}
	return f
}

// themeKey says what the look is made from, to know when it changes.
func themeKey(th wire.Theme, restyled int) string {
	key := fmt.Sprint(th.Name, restyled)
	if th.Base16 != nil {
		key += fmt.Sprint(*th.Base16)
	}
	if th.Colors != nil {
		key += fmt.Sprintf("%+v", *th.Colors)
	}
	return key
}

// shape is the cursor's shape: a bar in insert mode, an underline in
// replace mode and while an operator waits, and a block otherwise (B402).
func shape(s client.Session) int {
	switch {
	case s.Mode() == vim.Insert:
		return 1
	case s.Mode() == vim.Replace, s.Pending():
		return 2
	}
	return 0
}

// at is where the n cells from column x of row y go, the first cell at o.
func at(x, y, n int, o point) rect {
	return rect{point{o.X + float64(x)*cellW, o.Y + float64(y)*cellH}, size{float64(n) * cellW, cellH}}
}

// file is the absolute path of a window's file, or "" when it names none
// on disk.
func file(name string) string {
	path, err := filepath.Abs(name)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// dark reports whether c is a dark ground.
func dark(c theme.RGB) bool {
	r, g, b := float64(c>>16&0xff), float64(c>>8&0xff), float64(c&0xff)
	return 0.299*r+0.587*g+0.114*b < 128
}
