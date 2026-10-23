package gui

import (
	"math"
	"slices"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/ebitengine/purego/objc"

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

// boxed is what a box shows, cells or a toast's text. The loop makes it
// whole each frame.
type boxed struct {
	cells  [][]cell
	rules  []rule // drawn over the cells
	split  int    // the column a divider goes down, 0 for none
	cursor cursorAt
	middle bool    // it goes in the middle of the window, not its upper part
	at     *[2]int // where its first cell goes, in cells of the area
	title  string  // over the cells
	// A float with the focus has its status line under its text.
	left, right []client.Part
	text        string
	look        theme.Look
}

// rule is a line across w cells from column x of row y.
type rule struct{ x, y, w int }

// palette is the command line in a box, its prompt in the colour of
// commands or of search, with menu's completions under a rule (B148).
func palette(line string, cursor int, menu wire.Menu, lk theme.Look, cols int) *boxed {
	w := max(min(60, cols-6), 8)
	var items []compose.Row
	if menu.Text != "" {
		items, _ = compose.Wrap(menu.Text, menu.Spans, math.MaxInt)
	}
	rows := 1
	if len(items) > 0 {
		rows += 1 + len(items)
	}
	g := compose.NewGrid(w, rows, lk.Base)
	prompt := line[:1]
	colour := lk.Theme.Normal
	if prompt == ":" {
		colour = lk.Theme.Cmd
	}
	g.Put(0, 0, prompt, theme.Style{Fg: colour, Bg: lk.Base.Bg, Bold: true})
	// A line longer than the box shows its end, where the cursor usually is.
	before, rest := compose.Carets(line[1:min(max(cursor, 1), len(line))]), compose.Carets(line[1:])
	skip := 0
	for displaywidth.String(before[skip:]) > w-3 {
		_, n := utf8.DecodeRuneInString(before[skip:])
		skip += n
	}
	b := &boxed{look: lk}
	write(g, rest[skip:], nil, []compose.Row{{End: len(rest) - skip}}, lk, 2, 0, w-2, -1)
	b.cursor = cursorAt{x: 2 + displaywidth.String(before[skip:]), shape: 1, shown: true}
	if len(items) > 0 {
		b.rules = []rule{{0, 1, w}}
		write(g, menu.Text, menu.Spans, items, lk, 0, 2, w, -1)
	}
	b.cells = cellsOf(g)
	return b
}

// finder is the finder's box, as wide and tall as the editor makes it, its
// list left of a divider and the preview right of it (B130, B150).
func finder(m wire.Menu, lk theme.Look) *boxed {
	lw, dw, h := max(m.W-4, 1), max(m.DocW-4, 0), max(m.H-2, 1)
	total := lw
	if dw > 0 {
		total += 3 + dw
	}
	g := compose.NewGrid(total, h, lk.Base)
	b := &boxed{middle: true, look: lk}
	rows, _ := compose.Wrap(m.Text, m.Spans, lw)
	b.rules, b.cursor = write(g, m.Text, m.Spans, rows[:min(len(rows), h)], lk, 0, 0, lw, m.Cursor)
	b.cursor.shape = 1
	// The title heads the rule under the query.
	if m.Title != "" && len(b.rules) > 0 {
		r := &b.rules[0]
		n := min(displaywidth.String(m.Title), r.w-1)
		muted := lk
		muted.Base.Fg = lk.Theme.Muted
		write(g, m.Title, nil, []compose.Row{{End: len(m.Title)}}, muted, r.x, r.y, n, -1)
		r.x, r.w = r.x+n+1, r.w-n-1
	}
	if dw > 0 {
		rows, _ := compose.Wrap(m.Doc, m.DocSpans, dw)
		rules, _ := write(g, m.Doc, m.DocSpans, rows[:min(len(rows), h)], lk, lw+3, 0, dw, -1)
		b.rules, b.split = append(b.rules, rules...), lw+1
	}
	b.cells = cellsOf(g)
	return b
}

// atCursor returns the boxes of a menu at the cursor and of its
// documentation. The cursor is at column cx and row cy of an area cols wide
// and rows tall. The menu goes under the cursor, or over it when there is
// more room there. It shows at most m.Rows rows, scrolled to keep its
// selected item in view (B64, B481). The documentation goes right or left
// of the menu when one side has room for its widest line or 40 columns,
// and under or over the menu otherwise (B227).
func (p *painter) atCursor(m wire.Menu, lk theme.Look, cx, cy, cols, rows int) []*boxed {
	all, width := compose.Wrap(m.Text, m.Spans, min(80, cols-4))
	below, above := rows-cy-2, cy-1
	n := min(len(all), max(below, above))
	if m.Rows > 0 {
		n = min(n, m.Rows)
	}
	if width < 1 || n < 1 {
		return nil
	}
	if i := slices.IndexFunc(m.Spans, func(s wire.Span) bool { return s.Name == "menu.selected" }); i >= 0 {
		sel := slices.IndexFunc(all, func(r compose.Row) bool { return r.End >= m.Spans[i].Start })
		p.menuTop = max(min(max(p.menuTop, sel-n+1), sel, len(all)-n), 0)
	} else {
		p.menuTop = 0
	}
	y := cy + 2
	if below < n && above > below {
		y = cy - n - 1
	}
	x := max(min(cx, cols-width-1), 0)
	menu := textBox(m.Text, m.Spans, all[p.menuTop:p.menuTop+n], lk, width, x, y)
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
		dx, dy = max(min(x, cols-dw-1), 0), y+n+2
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
	b.rules, _ = write(g, text, spans, rows, lk, 0, 0, w, -1)
	b.cells = cellsOf(g)
	return b
}

// write puts rows of text, styled by spans in lk, in g from column x and
// row y, cut at w columns. A row that a menu.selected span starts takes
// that span's ground, and a rule row is a rule. It returns the rules and
// where the byte at cursor is.
func write(g *compose.Grid, text string, spans []wire.Span, rows []compose.Row, lk theme.Look, x, y, w, cursor int) ([]rule, cursorAt) {
	styles := compose.Styles(nil, spans, []wire.Range{{End: len(text) - 1}}, lk)
	selected, _ := lk.Name("menu.selected")
	var rules []rule
	var at cursorAt
	for i, r := range rows {
		if r.Rule {
			rules = append(rules, rule{x, y + i, w})
			continue
		}
		ground := lk.Base.Bg
		if slices.ContainsFunc(spans, func(s wire.Span) bool {
			return s.Name == "menu.selected" && s.Start <= r.Start && r.Start < s.End
		}) {
			ground = selected.Bg
			g.FillArea(x, y+i, w, 1, ' ', theme.Style{Fg: lk.Base.Fg, Bg: ground})
		}
		col := x
		for p := r.Start; p <= r.End && col < x+w; {
			// The end of a wrapped row is the start of the next one.
			if p == cursor && (p < r.End || i == len(rows)-1 || rows[i+1].Start != p) {
				at = cursorAt{x: col, y: y + i, shown: true}
			}
			if p == r.End {
				break
			}
			if text[p] == '\t' {
				p, col = p+1, col+4
				continue
			}
			st := styles[p]
			st.Bg = ground
			rest, n := g.Put(col, y+i, text[p:r.End], st)
			p, col = r.End-len(rest), col+n
		}
	}
	return rules, at
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

// box is a panel over a window's text, which shows the palette or the
// finder, or a toast.
type box struct {
	view objc.ID
	*boxed
}

// newBox makes a hidden box in view, with rounded corners and a shadow.
func newBox(view objc.ID) *box {
	v := msgInitView(objc.ID(boxClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	v.Send(sel("setWantsLayer:"), 1)
	l := v.Send(sel("layer"))
	msgSetFloat(l, sel("setCornerRadius:"), 10)
	msgSetFloat(l, sel("setBorderWidth:"), 1)
	shadow := cls("NSShadow").Send(sel("alloc")).Send(sel("init"))
	msgSetFloat(shadow, sel("setShadowBlurRadius:"), 18)
	msgSetSize(shadow, sel("setShadowOffset:"), size{0, -6})
	shadow.Send(sel("setShadowColor:"), msgColor(cls("NSColor"), sel("colorWithSRGBRed:green:blue:alpha:"), 0, 0, 0, 0.35))
	v.Send(sel("setShadow:"), shadow)
	shadow.Send(sel("release"))
	v.Send(sel("setHidden:"), 1)
	view.Send(sel("addSubview:"), v)
	b := &box{view: v}
	boxes[v] = b
	return b
}

// show makes b show what, sized and placed in view, whose area under the
// title bar starts top points down. A palette goes in the upper middle,
// the finder in the middle and a float at its place. A toast goes in the
// area's top right corner, where it fades in (B149). A nil what hides b.
func (b *box) show(what *boxed, view objc.ID, top float64) {
	shown := b.boxed != nil
	b.boxed = what
	if what == nil {
		b.view.Send(sel("setHidden:"), 1)
		return
	}
	bounds := msgRect(view, sel("bounds")).Size
	var r rect
	if what.text != "" {
		// It wraps at half the window's width, and a third of its height
		// ends in an ellipsis.
		limit := size{max(bounds.W/2, 240) - 2*inset, max(bounds.H/3-2*inset, cellH)}
		need := msgBounding(nsString(what.text), sel("boundingRectWithSize:options:attributes:context:"), size{limit.W, math.MaxFloat32}, lineFragments, textAttrs(toastFont, what.look.Base.Fg), 0).Size
		r.Size = size{math.Ceil(min(need.W, limit.W)) + 2*inset, math.Ceil(min(need.H, limit.H)) + 2*inset}
		r.Origin = point{bounds.W - r.Size.W - 2*pad, top + 2*pad}
	} else {
		r.Size = size{float64(len(what.cells[0]))*cellW + 2*inset, float64(len(what.cells))*cellH + 2*inset + what.head() + what.foot()}
		r.Origin = point{math.Round((bounds.W - r.Size.W) / 2), math.Round(bounds.H / 5)}
		switch {
		case what.middle:
			r.Origin.Y = math.Round((bounds.H - r.Size.H) / 2)
		case what.at != nil:
			// It stays in the area.
			r.Origin.X = max(min(float64(what.at[0])*cellW-inset, bounds.W-r.Size.W), 0)
			r.Origin.Y = max(min(top+float64(what.at[1])*cellH-inset-what.head(), bounds.H-r.Size.H), top)
		}
	}
	msgSetRect(b.view, sel("setFrame:"), r)
	l := b.view.Send(sel("layer"))
	l.Send(sel("setBackgroundColor:"), nsColor(what.look.Base.Bg).Send(sel("CGColor")))
	l.Send(sel("setBorderColor:"), nsColor(what.look.Theme.Border).Send(sel("CGColor")))
	if !shown {
		if what.text != "" {
			msgSetFloat(b.view, sel("setAlphaValue:"), 0)
			msgSetFloat(b.view.Send(sel("animator")), sel("setAlphaValue:"), 1)
		}
		b.view.Send(sel("setHidden:"), 0)
	}
	b.view.Send(sel("setNeedsDisplay:"), 1)
}

// The options that lay a toast's text out in lines, the last one cut with
// an ellipsis.
const lineFragments = 1 | 1<<5

func drawBox(self objc.ID, _ objc.SEL, _ rect) {
	b := boxes[self]
	if b == nil || b.boxed == nil {
		return
	}
	if b.text != "" {
		r := msgRect(self, sel("bounds"))
		r = rect{point{inset, inset}, size{r.Size.W - 2*inset, r.Size.H - 2*inset}}
		msgDrawIn(nsString(b.text), sel("drawWithRect:options:attributes:context:"), r, lineFragments, textAttrs(toastFont, b.look.Base.Fg), 0)
		return
	}
	bounds := msgRect(self, sel("bounds")).Size
	if b.title != "" {
		run{text: b.title, font: uiFont, fg: b.look.Theme.Muted}.draw(inset, inset/2, head)
	}
	origin := point{inset, inset + b.head()}
	drawCells(b.cells, b.cursor, origin)
	nsColor(b.look.Theme.Border).Send(sel("setFill"))
	for _, ru := range b.rules {
		r := at(ru.x, ru.y, ru.w, origin)
		rectFill(rect{point{r.Origin.X, r.Origin.Y + math.Floor(cellH/2)}, size{r.Size.W, 1}})
	}
	if b.split > 0 {
		r := at(b.split, 0, 1, origin)
		rectFill(rect{point{r.Origin.X + math.Floor(cellW/2), r.Origin.Y}, size{1, float64(len(b.cells)) * cellH}})
	}
	if f := b.foot(); f > 0 {
		drawParts(b.left, b.right, b.look, true, rect{point{0, bounds.H - f}, size{bounds.W, f}})
	}
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

// ask shows q in a sheet on the window. Its buttons and the keys y and n
// answer it as y and n do in the window (B362). apply closes the sheet
// when the question goes away unanswered.
func (w *window) ask(q string) {
	alert := cls("NSAlert").Send(sel("alloc")).Send(sel("init"))
	alert.Send(sel("setMessageText:"), nsString(q))
	for _, b := range [][2]string{{"Yes", "y"}, {"No", "n"}} {
		alert.Send(sel("addButtonWithTitle:"), nsString(b[0])).Send(sel("setKeyEquivalent:"), nsString(b[1]))
	}
	var done objc.Block
	done = objc.NewBlock(func(_ objc.Block, answer int) {
		switch answer {
		case 1000:
			w.keys <- "y"
		case 1001:
			w.keys <- "n"
		}
		if w.sheet == alert {
			w.sheet = 0
		}
		alert.Send(sel("release"))
		done.Release()
	})
	w.sheet = alert
	alert.Send(sel("beginSheetModalForWindow:completionHandler:"), w.win, done)
}
