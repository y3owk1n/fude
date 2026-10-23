package gui

import (
	"math"
	"slices"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
)

// statusH is a status bar's height, in points.
const statusH = 22

// pane shows a window of the layout, its text over its status bar.
type pane struct {
	w                  *window
	view, text, status objc.ID
	cells              [][]cell
	cursor             cursorAt
	left, right        []client.Part
	current            bool
}

// places are where the panes put the windows' text, for the loop to give
// the session (B542).
type places struct {
	cols, rows int      // the area's, in cells
	wins       [][5]int // each window's id, x, y, w and h, in cells of the area
}

// pane returns the pane of the window with id, made the first time.
func (w *window) pane(id int) *pane {
	if p := w.panes[id]; p != nil {
		return p
	}
	p := &pane{w: w}
	p.view = msgInitView(objc.ID(paneClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	p.text = msgInitView(objc.ID(textClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	p.status = msgInitView(objc.ID(statusClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	p.view.Send(sel("addSubview:"), p.text)
	p.view.Send(sel("addSubview:"), p.status)
	w.panes[id], panes[p.text], panes[p.status] = p, p, p
	return p
}

// drop forgets p, whose window closed.
func (p *pane) drop() {
	delete(panes, p.text)
	delete(panes, p.status)
	p.view.Send(sel("removeFromSuperview"))
	for _, v := range []objc.ID{p.text, p.status, p.view} {
		v.Send(sel("release"))
	}
}

// build shows l in the area, with a split view for each split and a pane
// for each window. A window is as big as it asks, or takes an equal share
// (B541). build drops the panes of windows l no longer has.
func (w *window) build(l wire.Split) {
	ids := leaves(l)
	for id, p := range w.panes {
		if !slices.Contains(ids, id) {
			p.drop()
			delete(w.panes, id)
		}
	}
	for _, sub := range subviews(w.area) {
		sub.Send(sel("removeFromSuperview"))
	}
	for _, p := range w.panes {
		p.view.Send(sel("removeFromSuperview"))
	}
	b := msgRect(w.area, sel("bounds"))
	root := w.node(l, b)
	root.Send(sel("setAutoresizingMask:"), 18)
	w.area.Send(sel("addSubview:"), root)
	w.placeAll()
}

// node returns the view that shows l in r, a pane or a split view of its
// kids.
func (w *window) node(l wire.Split, r rect) objc.ID {
	if len(l.Kids) == 0 {
		p := w.pane(l.Window)
		msgSetRect(p.view, sel("setFrame:"), r)
		return p.view
	}
	sv := msgInitView(objc.ID(splitClass).Send(sel("alloc")), sel("initWithFrame:"), r)
	sv.Send(sel("setVertical:"), l.Across)
	sv.Send(sel("setDividerStyle:"), 2)
	thick := msgFloat(sv, sel("dividerThickness"))
	total, cell, room := r.Size.H, cellH, float64(2*pad+statusH)
	if l.Across {
		total, cell, room = r.Size.W, cellW, 2*pad
	}
	// A kid that asks for rows or columns gets them, and the others share
	// the rest.
	sizes, left, shared := make([]float64, len(l.Kids)), total-thick*float64(len(l.Kids)-1), 0
	for i, k := range l.Kids {
		if k.Size > 0 {
			sizes[i] = min(float64(k.Size)*cell+room, left)
			left -= sizes[i]
		} else {
			shared++
		}
	}
	at := 0.0
	for i, k := range l.Kids {
		if k.Size == 0 {
			sizes[i] = math.Floor(left / float64(shared))
		}
		kr := rect{point{0, at}, size{r.Size.W, sizes[i]}}
		if l.Across {
			kr = rect{point{at, 0}, size{sizes[i], r.Size.H}}
		}
		sv.Send(sel("addSubview:"), w.node(k, kr))
		at += sizes[i] + thick
	}
	at = 0
	for i := range len(l.Kids) - 1 {
		at += sizes[i]
		msgSetPosition(sv, sel("setPosition:ofDividerAtIndex:"), at, i)
		at += thick
	}
	return sv.Send(sel("autorelease"))
}

// leaves returns the ids of l's windows.
func leaves(l wire.Split) []int {
	if len(l.Kids) == 0 {
		return []int{l.Window}
	}
	var ids []int
	for _, k := range l.Kids {
		ids = append(ids, leaves(k)...)
	}
	return ids
}

// placeAll gives the loop where each pane puts its window's text, in cells
// of the area, when that changed. Each edge rounds down, so a window right
// of another starts past its end, as <C-w>l needs (B542).
func (w *window) placeAll() {
	area := msgRect(w.area, sel("frame"))
	pl := places{cols: max(int(area.Size.W/cellW), 1), rows: max(int(area.Size.H/cellH), 1)}
	for id, p := range w.panes {
		if objc.Send[objc.ID](p.view, sel("superview")) == 0 {
			continue
		}
		r := msgConvertRect(p.text, sel("convertRect:toView:"), msgRect(p.text, sel("bounds")), w.view)
		x, y := r.Origin.X-area.Origin.X+pad, r.Origin.Y-area.Origin.Y+pad
		pl.wins = append(pl.wins, [5]int{id, int(x / cellW), int(y / cellH), max(int((r.Size.W-2*pad)/cellW), 1), max(int((r.Size.H-2*pad)/cellH), 1)})
	}
	slices.SortFunc(pl.wins, func(a, b [5]int) int { return a[0] - b[0] })
	if pl.cols == w.sent.cols && pl.rows == w.sent.rows && slices.Equal(pl.wins, w.sent.wins) {
		return
	}
	w.sent = pl
	w.mu.Lock()
	w.places = pl
	w.mu.Unlock()
	select {
	case w.placed <- struct{}{}:
	default:
	}
}

// subviews returns v's subviews.
func subviews(v objc.ID) []objc.ID {
	list := v.Send(sel("subviews"))
	out := make([]objc.ID, objc.Send[int](list, sel("count")))
	for i := range out {
		out[i] = list.Send(sel("objectAtIndex:"), i)
	}
	return out
}

// barHeight is how much of win's content the title bar covers.
func barHeight(win objc.ID) float64 {
	return msgRect(win, sel("frame")).Size.H - msgRect(win, sel("contentLayoutRect")).Size.H
}

// resizeRoot puts the tab strip in the title bar and the area under it.
func resizeRoot(self objc.ID, _ objc.SEL, _ size) {
	w := windows[self]
	if w == nil {
		return
	}
	b := msgRect(self, sel("bounds"))
	top := barHeight(w.win)
	msgSetRect(w.bar, sel("setFrame:"), rect{Size: size{b.Size.W, top}})
	msgSetRect(w.area, sel("setFrame:"), rect{point{0, top}, size{b.Size.W, max(b.Size.H-top, 1)}})
	w.placeAll()
}

// resizePane puts a pane's text over its status bar.
func resizePane(self objc.ID, _ objc.SEL, _ size) {
	b := msgRect(self, sel("bounds")).Size
	subs := subviews(self)
	msgSetRect(subs[0], sel("setFrame:"), rect{Size: size{b.W, max(b.H-statusH, 1)}})
	msgSetRect(subs[1], sel("setFrame:"), rect{point{0, max(b.H-statusH, 0)}, size{b.W, statusH}})
}

func splitResized(self objc.ID, _ objc.SEL, note objc.ID) {
	if w := windows[self]; w != nil && objc.Send[objc.ID](note.Send(sel("object")), sel("window")) == w.win {
		w.placeAll()
	}
}

// update shows in p what the loop sent for its window.
func (p *pane) update(sh shown, ground theme.RGB) {
	if len(p.cells) != sh.h || len(p.cells) > 0 && len(p.cells[0]) != sh.w {
		p.cells = make([][]cell, sh.h)
		for y := range p.cells {
			p.cells[y] = make([]cell, sh.w)
			for x := range p.cells[y] {
				p.cells[y][x] = cell{" ", 1, theme.Style{Bg: ground}}
			}
		}
	}
	for _, c := range sh.put {
		if c.y >= len(p.cells) || c.x >= len(p.cells[c.y]) {
			continue
		}
		p.cells[c.y][c.x] = c.cell
		// A wide cell covers the next.
		for i := 1; i < c.width && c.x+i < len(p.cells[c.y]); i++ {
			p.cells[c.y][c.x+i] = cell{style: c.style}
		}
	}
	p.cursor, p.left, p.right, p.current = sh.cursor, sh.left, sh.right, sh.current
	p.text.Send(sel("setNeedsDisplay:"), 1)
	p.status.Send(sel("setNeedsDisplay:"), 1)
}

func drawText(self objc.ID, _ objc.SEL, _ rect) {
	p := panes[self]
	if p == nil {
		return
	}
	nsColor(p.w.look.Base.Bg).Send(sel("setFill"))
	rectFill(msgRect(self, sel("bounds")))
	drawCells(p.cells, p.cursor, point{pad, pad})
}

func drawDivider(self objc.ID, _ objc.SEL, r rect) {
	if w := windows[self.Send(sel("window"))]; w != nil {
		nsColor(w.look.Theme.Border).Send(sel("setFill"))
		rectFill(r)
	}
}

// drawStatus draws a window's status bar (B147). The left has the mode for
// the current window, the file's name after its folder, a dot while it is
// modified, the segments and the diagnostics. The right has the others'
// names, the keys being typed and the ruler, and goes when both sides do
// not fit. Another window's bar has muted text.
func drawStatus(self objc.ID, _ objc.SEL, _ rect) {
	p := panes[self]
	if p == nil {
		return
	}
	b := msgRect(self, sel("bounds"))
	nsColor(p.w.look.Theme.Surface).Send(sel("setFill"))
	rectFill(b)
	drawParts(p.left, p.right, p.w.look, p.current, b)
}

// drawParts draws a status line's parts in r, under a rule.
func drawParts(left, right []client.Part, lk theme.Look, current bool, r rect) {
	nsColor(lk.Theme.Border).Send(sel("setFill"))
	rectFill(rect{r.Origin, size{r.Size.W, 1}})
	ls, rs := runs(left, lk, current), runs(right, lk, current)
	if width(ls)+width(rs)+3*pad > r.Size.W {
		rs = nil
	}
	x := r.Origin.X + pad
	for _, p := range ls {
		x = p.draw(x, r.Origin.Y, r.Size.H)
	}
	x = r.Origin.X + r.Size.W - pad - width(rs)
	for _, p := range rs {
		x = p.draw(x, r.Origin.Y, r.Size.H)
	}
}

// run is a part of a status bar or of the tab strip as it draws, its text
// in a font and colour. A pill run has a rounded ground, and a tight run
// has no room after it.
type run struct {
	text        string
	font        objc.ID
	fg, ground  theme.RGB
	pill, tight bool
}

// size is how wide r draws, its room included.
func (r run) size() float64 {
	w := math.Ceil(msgSizeOf(nsString(r.text), sel("sizeWithAttributes:"), textAttrs(r.font, r.fg)).W)
	if r.pill {
		w += 2 * pad
	}
	if r.tight {
		return w
	}
	return w + pad
}

// draw draws r from x in a bar h tall from top, and returns where the next
// goes.
func (r run) draw(x, top, h float64) float64 {
	a := textAttrs(r.font, r.fg)
	sz := msgSizeOf(nsString(r.text), sel("sizeWithAttributes:"), a)
	at := x
	if r.pill {
		nsColor(r.ground).Send(sel("setFill"))
		msgRounded(cls("NSBezierPath"), sel("bezierPathWithRoundedRect:xRadius:yRadius:"), rect{point{x, top + 3}, size{math.Ceil(sz.W) + 2*pad, h - 6}}, 5, 5).Send(sel("fill"))
		at += pad
	}
	msgDrawAt(nsString(r.text), sel("drawAtPoint:withAttributes:"), point{at, top + math.Floor((h-sz.H)/2)}, a)
	return x + r.size()
}

// width is how wide rs draw.
func width(rs []run) float64 {
	n := 0.0
	for _, r := range rs {
		n += r.size()
	}
	return n
}

// runs are a status line's parts as the bar draws them, by their roles
// (B147).
func runs(parts []client.Part, lk theme.Look, current bool) []run {
	th := lk.Theme
	text := th.Text
	if !current {
		text = th.Muted
	}
	var out []run
	for _, p := range parts {
		r := run{text: p.Text, font: uiFont, fg: th.Muted}
		if kind, ok := strings.CutPrefix(p.Role, "mode."); ok {
			r = run{text: p.Text, font: uiBold, fg: th.Bg, ground: modeColour(th, kind), pill: true}
		}
		switch p.Role {
		case "recording":
			r.text, r.font, r.fg = "● "+p.Text, uiBold, th.Error
		case "dir":
			r.fg, r.tight = th.Faint, true
		case "name":
			r.fg = text
			if current {
				r.font = uiBold
			}
		case "modified":
			r.text, r.fg = "●", th.Cmd
		case "errors":
			r.fg = th.Error
		case "warnings":
			r.fg = th.Warn
		case "sharing", "zoomed":
			r.fg = th.Cmd
		case "busy":
			r.text, r.fg = "⠋", th.Normal
		}
		if r.text != "" {
			out = append(out, r)
		}
	}
	return out
}

// modeColour is the colour of a mode of the kind client.ModeName gives.
func modeColour(th theme.Theme, kind string) theme.RGB {
	switch kind {
	case "insert", "terminal":
		return th.Insert
	case "replace":
		return th.Error
	case "command":
		return th.Cmd
	case "visual":
		return th.Visual
	}
	return th.Normal
}
