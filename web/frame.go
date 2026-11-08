package web

import (
	"fmt"
	"sync"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/compose"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// frame is what the page gets after each change, as JSON.
type frame struct {
	Full    bool              `json:"full,omitempty"`   // the page drops the cells it shows first
	Colors  map[string]string `json:"colors,omitempty"` // the theme's, with Full
	Styles  []style           `json:"styles"`           // what each run's style indexes
	Wins    []win             `json:"wins"`
	Mode    string            `json:"mode"`
	Title   string            `json:"title,omitempty"`
	Tabs    []wire.Tab        `json:"tabs,omitempty"`
	Pages   []wire.Page       `json:"pages,omitempty"`
	Line    *cmdline          `json:"line,omitempty"`
	Menu    *wire.Menu        `json:"menu,omitempty"`
	Doc     [][]any           `json:"doc,omitempty"` // the finder's preview as runs, as a window's
	Message string            `json:"message,omitempty"`
	Confirm string            `json:"confirm,omitempty"`
	Modal   bool              `json:"modal,omitempty"` // the windows dim under a box (B151)
	Copy    *string           `json:"copy,omitempty"`  // for the system's clipboard
	Ended   bool              `json:"ended,omitempty"`
	Moved   bool              `json:"moved,omitempty"` // another page shows the session now
}

// win is a window, or a float, at its place in cells of the screen, with
// the runs of cells that changed. A float sends all its cells each frame.
type win struct {
	ID      int           `json:"id"`
	X       int           `json:"x"`
	Y       int           `json:"y"`
	W       int           `json:"w"`
	H       int           `json:"h"`
	Float   bool          `json:"float,omitempty"`
	Name    string        `json:"name,omitempty"`
	Runs    [][]any       `json:"runs"`             // x, y, columns, style and text
	Pics    [][5]int      `json:"pics,omitempty"`   // id, x, y, columns and rows of each image, over the cells (B303)
	Cursor  *[3]int       `json:"cursor,omitempty"` // x, y and shape: 0 a block, 1 a bar, 2 an underline
	Current bool          `json:"current,omitempty"`
	Left    []client.Part `json:"left,omitempty"`
	Right   []client.Part `json:"right,omitempty"`
}

type cmdline struct {
	Text   string `json:"text"`
	Cursor int    `json:"cursor"`
}

type style struct {
	Fg     string `json:"fg"`
	Bg     string `json:"bg"`
	Bold   bool   `json:"b,omitempty"`
	Italic bool   `json:"i,omitempty"`
	Under  string `json:"u,omitempty"` // the underline's colour, "" for none
	Strike bool   `json:"s,omitempty"`
}

// painter turns a session into frames, keeping each window's grid between
// them so a frame holds only the cells that changed. Nil grids send every
// cell.
type painter struct {
	comp    compose.Composer
	grids   map[int]*compose.Grid
	frame   int
	lookKey string
	look    theme.Look
	boxed   theme.Look // look on the tinted ground a modal box has (B151)
	styles  map[theme.Style]int
	pngs    *sync.Map // where the page gets each image's PNG, by id
}

func (p *painter) paint(s client.Session) frame {
	p.frame++
	var f frame
	th := s.Theme()
	own, restyled := s.Styles()
	if key := fmt.Sprint(th.Name, restyled, th.Base16, th.Colors); key != p.lookKey || p.grids == nil {
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
		ground := t
		ground.Bg = t.Tint().Bg
		p.look, p.boxed = theme.LookOf(t), theme.LookOf(ground)
		same := func(c theme.RGB) theme.RGB { return c }
		p.look.Restyle(own, same)
		p.boxed.Restyle(own, same)
		p.lookKey, p.grids = key, map[int]*compose.Grid{}
		f.Full, f.Colors = true, colors(t)
	}
	p.styles = map[theme.Style]int{}
	_, f.Mode = client.ModeName(s)
	f.Title, f.Tabs, f.Pages, f.Message, f.Confirm = s.Title(), s.Buffers(), s.Pages(), compose.Carets(s.Message()), s.Confirm()
	menu, menus := s.Menu()
	if s.Mode() == vim.Cmdline {
		f.Line = &cmdline{Text: compose.Carets(s.Line()), Cursor: len(compose.Carets(s.Line()[:s.LineCursor()]))}
	}
	if menus {
		f.Menu = &menu
		if menu.Anchor == "middle" && menu.DocW > 4 {
			f.Doc = p.doc(menu)
		}
	}
	f.Modal = s.Mode() == vim.Cmdline && !s.Substituting() || menus && menu.Anchor == "middle" || f.Confirm != ""
	if text, ok := s.Clipboard(); ok {
		f.Copy = &text
	}
	sel, visual := s.Selection()
	shown := map[int]bool{}
	for _, id := range s.Freed() {
		p.pngs.Delete(id)
	}
	for _, v := range s.Windows() {
		var cursors []int
		if v.Current {
			cursors = s.Cursors()
		}
		o := compose.Options{
			Look: p.look, Frame: p.frame, Insert: s.Mode() == vim.Insert, Cursors: cursors,
			Selection: sel, Visual: visual && v.Current, Tint: p.look.Theme.Normal,
		}
		w := win{ID: v.ID, X: v.X, Y: v.Y, W: v.W, H: v.H, Float: v.Float != "", Current: v.Current}
		g := p.grids[v.ID]
		if gw, gh := g.Size(); g == nil || w.Float || gw != v.W || gh != v.H {
			g = compose.NewGrid(v.W, v.H, p.look.Base)
		}
		if w.Float {
			w.Name = v.Name
		} else {
			p.grids[v.ID] = g
			shown[v.ID] = true
			w.Left, w.Right = client.StatusParts(s, v, true)
		}
		var out compose.Composed
		if v.Cells != nil {
			out = compose.Terminal(g, v, p.look)
		} else {
			out = p.comp.Compose(g, v, o)
			w.Pics = p.pics(s, v, out)
		}
		if out.Found && v.Current {
			w.Cursor = &[3]int{out.X, out.Y, shape(s)}
		}
		w.Runs = p.runs(g)
		f.Wins = append(f.Wins, w)
	}
	// A window hidden behind a zoom or on another tab page draws whole when
	// it shows again.
	for id := range p.grids {
		if !shown[id] {
			delete(p.grids, id)
		}
	}
	f.Styles = make([]style, len(p.styles))
	for st, i := range p.styles {
		f.Styles[i] = styleOf(st)
	}
	return f
}

// runs are the cells of g that changed, a row's one-column ASCII cells of
// one style together and any other cell alone, so the page's font never
// shifts a row.
func (p *painter) runs(g *compose.Grid) [][]any {
	var out [][]any
	ascii := func(text string, width int) bool {
		return width == 1 && len(text) == 1 && text[0] >= ' ' && text[0] <= '~'
	}
	var last []any // the run the last cell joined, while it may join more
	lastEnd, lastY := -1, -1
	g.Paint(0, 0, func(x, y int, text string, width int, st theme.Style) {
		i, ok := p.styles[st]
		if !ok {
			i = len(p.styles)
			p.styles[st] = i
		}
		if last != nil && ascii(text, width) && x == lastEnd && y == lastY && last[3] == i {
			last[2] = last[2].(int) + 1
			last[4] = last[4].(string) + text
			lastEnd++
			return
		}
		run := []any{x, y, width, i, text}
		out = append(out, run)
		last, lastEnd, lastY = nil, -1, -1
		if ascii(text, width) {
			last, lastEnd, lastY = run, x+1, y
		}
	})
	return out
}

// pics are the images out says v shows, each once, with its top left cell,
// which may be above the first row shown. Each one's PNG goes in the store
// the first time it shows.
func (p *painter) pics(s client.Session, v wire.Window, out compose.Composed) [][5]int {
	var ps [][5]int
	seen := map[[2]int]bool{}
	for _, r := range out.Images {
		k := [2]int{r.Line, r.Row.Image}
		if seen[k] {
			continue
		}
		seen[k] = true
		rows := 0
		for _, o := range v.Virtual[r.Line] {
			if o.Image == r.Row.Image {
				rows++
			}
		}
		ps = append(ps, [5]int{r.Row.Image, v.Gutter, r.Y - r.Row.Row, min(r.Row.Cols, v.W-v.Gutter), rows})
		if _, ok := p.pngs.Load(r.Row.Image); !ok {
			if png, ok := s.Image(r.Row.Image); ok {
				p.pngs.Store(r.Row.Image, png)
			}
		}
	}
	return ps
}

// doc is the finder's preview in its box, inside its border and a column
// each side, styled as the editor names its spans.
func (p *painter) doc(m wire.Menu) [][]any {
	w, h := m.DocW-4, max(m.H-2, 1)
	g := compose.NewGrid(w, h, p.boxed.Base)
	rows, _ := compose.Wrap(m.Doc, m.DocSpans, w)
	styles := compose.Styles(nil, m.DocSpans, []wire.Range{{End: len(m.Doc) - 1}}, p.boxed)
	rule := p.boxed.Base
	rule.Fg = p.boxed.Theme.Border
	for y, r := range rows[:min(len(rows), h)] {
		if r.Rule {
			g.FillArea(0, y, w, 1, '─', rule)
			continue
		}
		for i, col := r.Start, 0; i < r.End && col < w; {
			if m.Doc[i] == '\t' {
				i, col = i+1, col+4
				continue
			}
			st := styles[i]
			st.Bg = p.boxed.Base.Bg
			rest, n := g.Put(col, y, m.Doc[i:r.End], st)
			i, col = r.End-len(rest), col+n
		}
	}
	return p.runs(g)
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

func hex(c theme.RGB) string { return fmt.Sprintf("#%06x", uint32(c)) }

func styleOf(st theme.Style) style {
	out := style{Fg: hex(st.Fg), Bg: hex(st.Bg), Bold: st.Bold, Italic: st.Italic, Strike: st.Strike}
	switch {
	case st.Underline && st.UnderSet:
		out.Under = hex(st.Under)
	case st.Underline:
		out.Under = out.Fg
	}
	return out
}

// colors are t's colours by role, which the page's chrome takes.
func colors(t theme.Theme) map[string]string {
	return map[string]string{
		"bg": hex(t.Bg), "surface": hex(t.Surface), "raised": hex(t.Raised), "border": hex(t.Border),
		"text": hex(t.Text), "muted": hex(t.Muted), "faint": hex(t.Faint), "selection": hex(t.Selection),
		"normal": hex(t.Normal), "insert": hex(t.Insert), "visual": hex(t.Visual), "command": hex(t.Cmd),
		"error": hex(t.Error), "warn": hex(t.Warn), "box": hex(t.Tint().Bg),
	}
}
