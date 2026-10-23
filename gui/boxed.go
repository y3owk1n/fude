package gui

import (
	"math"
	"slices"

	"github.com/clipperhouse/displaywidth"
	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/compose"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
)

// inset is the room inside a box, in points, and head the height of a
// box's title over its text.
const (
	inset = 10
	head  = 18
)

// statusH is a status bar's height, in points.
const statusH = 22

// boxed is what a box shows, cells or a toast's text. The loop makes it
// whole each frame.
type boxed struct {
	cells  [][]cell
	rules  []rule // drawn over the cells
	cursor cursorAt
	middle bool    // it goes in the middle of the window, not its upper part
	at     *[2]int // where its first cell goes, in cells of the area
	title  string  // over the cells
	pics   []pic   // over the cells
	chosen []rule  // the rows a menu has selected, which take the system's selection
	prompt string  // what a command line's prompt is, command or search, which each system shows as its own symbol
	list   *list   // a menu the window lays out, before any cells
	// A float with the focus has its status line under its text.
	left, right []client.Part
	text        string
	look        theme.Look
}

// rule is a line across w cells from column x of row y.
type rule struct{ x, y, w int }

// palette is the command line in a box, after the system's symbol for its
// prompt, with menu's completions under it as a list (B148, B560).
func palette(line string, cursor int, menu wire.Menu, lk theme.Look) *boxed {
	b := &boxed{look: lk, prompt: "command"}
	if line[:1] != ":" {
		b.prompt = "search"
	}
	b.list = &list{
		typing: true, input: compose.Carets(line[1:]), at: len(compose.Carets(line[1:min(max(cursor, 1), len(line))])),
		items: menu.Items, selected: -1, rowH: listRowH,
	}
	if len(menu.Items) > 0 {
		b.list.selected = menu.Selected
	}
	return b
}

// finder is the finder's box: its query and its list, and the preview
// right of them as the editor lays it out (B130, B150, B560).
func finder(m wire.Menu, lk theme.Look) *boxed {
	lw, dw, h := max(m.W-4, 1), max(m.DocW-4, 0), max(m.H-2, 1)
	b := &boxed{middle: true, look: lk, prompt: "search"}
	note := m.Count
	if m.Title != "" {
		note = m.Title + "   " + m.Count
	}
	b.list = &list{
		typing: true, input: m.Query, at: min(max(m.Cursor-len(m.Prompt), 0), len(m.Query)), note: note,
		items: m.Items, selected: m.Selected, rowH: listRowH, width: float64(lw) * cellW,
	}
	if dw > 0 {
		g := compose.NewGrid(dw, h, lk.Base)
		rows, _ := compose.Wrap(m.Doc, m.DocSpans, dw)
		b.rules, b.chosen = write(g, m.Doc, m.DocSpans, rows[:min(len(rows), h)], lk, 0, 0, dw)
		b.cells = cellsOf(g)
		if p := m.DocImage; p != nil {
			b.pics = []pic{{p.ID, 0, p.Row, min(p.Cols, dw), p.Rows}}
		}
	}
	return b
}

// atCursor returns the boxes of a menu at the cursor and of its
// documentation. The cursor is at column cx and row cy of an area cols wide
// and rows tall. The menu goes under the cursor, or over it when there is
// more room there. It shows at most m.Rows items, scrolled to keep its
// selected item in view (B64, B481). The documentation goes right or left
// of the menu when one side has room for its widest line or 40 columns,
// and under or over the menu otherwise (B227).
func (p *painter) atCursor(m wire.Menu, lk theme.Look, cx, cy, cols, rows int) []*boxed {
	// tall is how many rows of cells n items take.
	tall := func(n int) int { return int(math.Ceil((float64(n)*menuRowH + 8) / cellH)) }
	below, above := rows-cy-2, cy-1
	n := len(m.Items)
	for n > 0 && tall(n) > max(below, above) {
		n--
	}
	if m.Rows > 0 {
		n = min(n, m.Rows)
	}
	if n < 1 {
		return nil
	}
	// width is about how many columns the items take, for the
	// documentation to go beside them.
	width := 0
	for _, it := range m.Items[:min(len(m.Items), 50)] {
		width = max(width, displaywidth.String(it.Label)+displaywidth.String(it.Detail)+6)
	}
	width = min(width, cols-4)
	y := cy + 2
	if below < tall(n) && above > below {
		y = cy - tall(n) - 1
	}
	x := max(min(cx, cols-width-1), 0)
	menu := &boxed{look: lk, at: &[2]int{x, y}, list: &list{items: m.Items, selected: m.Selected, view: n, rowH: menuRowH}}
	if m.Doc == "" {
		return []*boxed{menu}
	}
	_, natural := compose.Wrap(m.Doc, m.DocSpans, 80)
	need := min(natural, 40)
	right, left := cols-x-width-3, x-3
	var doc []compose.Row
	var dw, dx, dy int
	switch {
	case right >= need:
		doc, dw = compose.Wrap(m.Doc, m.DocSpans, min(80, right))
		dx, dy = x+width+3, y
	case left >= need:
		doc, dw = compose.Wrap(m.Doc, m.DocSpans, min(80, left))
		dx, dy = x-dw-3, y
	default:
		doc, dw = compose.Wrap(m.Doc, m.DocSpans, min(80, cols-4))
		dx, dy = max(min(x, cols-dw-1), 0), y+tall(n)+2
		if y < cy {
			dy = y - min(len(doc), 20) - 2
		}
	}
	doc = doc[:min(len(doc), 20, max(rows-dy-1, 1))]
	return []*boxed{menu, textBox(m.Doc, m.DocSpans, doc, lk, dw, dx, dy)}
}

// keyHints is the box of the key hints, across the bottom of the area
// where the editor puts it. Its title is the keys being typed (B172).
func keyHints(m wire.Menu, lk theme.Look) *boxed {
	rows, _ := compose.Wrap(m.Text, m.Spans, max(m.W-4, 1))
	b := textBox(m.Text, m.Spans, rows[:min(len(rows), max(m.H-2, 1))], lk, max(m.W-4, 1), m.X+2, m.Y+1)
	b.title = m.Title
	return b
}

// textBox is rows of text, styled by spans in lk, in a box w columns wide
// whose first cell goes at column x and row y of the area.
func textBox(text string, spans []wire.Span, rows []compose.Row, lk theme.Look, w, x, y int) *boxed {
	g := compose.NewGrid(w, max(len(rows), 1), lk.Base)
	b := &boxed{look: lk, at: &[2]int{x, y}}
	b.rules, b.chosen = write(g, text, spans, rows, lk, 0, 0, w)
	b.cells = cellsOf(g)
	return b
}

// write puts rows of text, styled by spans in lk, in g from column x and
// row y, cut at w columns. A rule row is a rule. It returns the rules, and
// the rows that a menu.selected span starts, which draw as the system's
// selection.
func write(g *compose.Grid, text string, spans []wire.Span, rows []compose.Row, lk theme.Look, x, y, w int) (rules, chosen []rule) {
	styles := compose.Styles(nil, spans, []wire.Range{{End: len(text) - 1}}, lk)
	for i, r := range rows {
		if r.Rule {
			rules = append(rules, rule{x, y + i, w})
			continue
		}
		if slices.ContainsFunc(spans, func(s wire.Span) bool {
			return s.Name == "menu.selected" && s.Start <= r.Start && r.Start < s.End
		}) {
			chosen = append(chosen, rule{x, y + i, w})
		}
		col := x
		for p := r.Start; p < r.End && col < x+w; {
			if text[p] == '\t' {
				p, col = p+1, col+4
				continue
			}
			st := styles[p]
			st.Bg = lk.Base.Bg
			rest, n := g.Put(col, y+i, text[p:r.End], st)
			p, col = r.End-len(rest), col+n
		}
	}
	return rules, chosen
}

// cellsOf returns g's cells.
func cellsOf(g *compose.Grid) [][]cell {
	w, h := g.Size()
	cells := make([][]cell, h)
	for y := range cells {
		cells[y] = make([]cell, w)
	}
	g.Paint(0, 0, func(x, y int, text string, width int, st theme.Style) {
		cells[y][x] = cell{text, width, st}
	})
	return cells
}

// cellsOrigin is where b's first cell is: past its title, or right of its
// list, where the finder's preview goes.
func (b *boxed) cellsOrigin() point {
	if l := b.list; l != nil {
		return point{8 + l.width + 12, inputH + inset}
	}
	return point{inset, inset + b.head()}
}

// head is the height of b's title, and foot of its status line.
func (b *boxed) head() float64 {
	if b.title == "" {
		return 0
	}
	return head
}

func (b *boxed) foot() float64 {
	if b.left == nil && b.right == nil {
		return 0
	}
	return statusH
}
