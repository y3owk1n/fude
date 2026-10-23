package gui

import (
	"math"
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/compose"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
)

// inset is the room inside a box, in points.
const inset = 10

// boxed is what a box shows, cells or a toast's text. The loop makes it
// whole each frame.
type boxed struct {
	cells  [][]cell
	rules  []rule // drawn over the cells
	split  int    // the column a divider goes down, 0 for none
	cursor cursorAt
	middle bool    // it goes in the middle of the window, not its upper part
	at     *[2]int // a float's place, in cells of the area
	text   string
	look   theme.Look
}

// rule is a line across w cells from column x of row y.
type rule struct{ x, y, w int }

// palette is the command line in a box, its prompt in the colour of
// commands or of search, with menu's completions under a rule (B148).
func palette(line string, cursor int, menu wire.Menu, lk theme.Look, cols int) *boxed {
	w := max(min(60, cols-6), 8)
	items := 0
	if menu.Text != "" {
		items = strings.Count(menu.Text, "\n") + 1
	}
	rows := 1
	if items > 0 {
		rows += 1 + items
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
	write(g, rest[skip:], nil, lk, 2, 0, w-2, 1, -1)
	b.cursor = cursorAt{x: 2 + displaywidth.String(before[skip:]), shape: 1, shown: true}
	if items > 0 {
		b.rules = []rule{{0, 1, w}}
		write(g, menu.Text, menu.Spans, lk, 0, 2, w, items, -1)
	}
	b.fill(g)
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
	b.rules, b.cursor = write(g, m.Text, m.Spans, lk, 0, 0, lw, h, m.Cursor)
	b.cursor.shape = 1
	// The title heads the rule under the query.
	if m.Title != "" && len(b.rules) > 0 {
		r := &b.rules[0]
		n := min(displaywidth.String(m.Title), r.w-1)
		muted := lk
		muted.Base.Fg = lk.Theme.Muted
		write(g, m.Title, nil, muted, r.x, r.y, n, 1, -1)
		r.x, r.w = r.x+n+1, r.w-n-1
	}
	if dw > 0 {
		rules, _ := write(g, m.Doc, m.DocSpans, lk, lw+3, 0, dw, h, -1)
		b.rules, b.split = append(b.rules, rules...), lw+1
	}
	b.fill(g)
	return b
}

// write puts text, styled by spans in lk, in g from column x and row y. Each
// line takes a row, cut at w columns, and h rows at most. A row that a
// menu.selected span starts takes that span's ground, and a row that a
// markup.rule span starts is a rule. It returns the rules and where the
// byte at cursor is.
func write(g *compose.Grid, text string, spans []wire.Span, lk theme.Look, x, y, w, h, cursor int) ([]rule, cursorAt) {
	styles := compose.Styles(nil, spans, []wire.Range{{End: len(text) - 1}}, lk)
	selected, _ := lk.Name("menu.selected")
	var rules []rule
	var at cursorAt
	for row, start := 0, 0; row < h && start <= len(text); row++ {
		end := strings.IndexByte(text[start:], '\n')
		if end < 0 {
			end = len(text) - start
		}
		end += start
		ground := lk.Base.Bg
		for _, s := range spans {
			switch {
			case s.Name == "markup.rule" && s.Start == start:
				rules = append(rules, rule{x, y + row, w})
			case s.Name == "menu.selected" && s.Start <= start && start < s.End:
				ground = selected.Bg
			}
		}
		if ground != lk.Base.Bg {
			g.FillArea(x, y+row, w, 1, ' ', theme.Style{Fg: lk.Base.Fg, Bg: ground})
		}
		col := x
		for i := start; i <= end && col < x+w; {
			if i == cursor {
				at = cursorAt{x: col, y: y + row, shown: true}
			}
			if i == end {
				break
			}
			st := styles[i]
			st.Bg = ground
			rest, n := g.Put(col, y+row, text[i:end], st)
			i, col = end-len(rest), col+n
		}
		start = end + 1
	}
	return rules, at
}

// fill makes b's cells g's.
func (b *boxed) fill(g *compose.Grid) {
	w, h := g.Size()
	b.cells = make([][]cell, h)
	for y := range b.cells {
		b.cells[y] = make([]cell, w)
	}
	g.Paint(0, 0, func(x, y int, text string, width int, st theme.Style) {
		b.cells[y][x] = cell{text, width, st}
	})
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
		r.Size = size{float64(len(what.cells[0]))*cellW + 2*inset, float64(len(what.cells))*cellH + 2*inset}
		r.Origin = point{math.Round((bounds.W - r.Size.W) / 2), math.Round(bounds.H / 5)}
		switch {
		case what.middle:
			r.Origin.Y = math.Round((bounds.H - r.Size.H) / 2)
		case what.at != nil:
			r.Origin = point{float64(what.at[0])*cellW - inset, top + float64(what.at[1])*cellH - inset}
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
	drawCells(b.cells, b.cursor, inset)
	nsColor(b.look.Theme.Border).Send(sel("setFill"))
	for _, ru := range b.rules {
		r := at(ru.x, ru.y, ru.w, inset)
		rectFill(rect{point{r.Origin.X, r.Origin.Y + math.Floor(cellH/2)}, size{r.Size.W, 1}})
	}
	if b.split > 0 {
		r := at(b.split, 0, 1, inset)
		rectFill(rect{point{r.Origin.X + math.Floor(cellW/2), r.Origin.Y}, size{1, float64(len(b.cells)) * cellH}})
	}
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
