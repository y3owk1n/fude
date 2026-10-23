package gui

import (
	"math"
	"slices"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/wire"
)

// paneNative is a pane's views: its text and status bar on a view of
// their own, and its scroll bar, which shows from shownAt for a second.
type paneNative struct {
	view, text, status objc.ID
	scroller           objc.ID
	shownAt            time.Time
	said               spoken // its text as assistive apps count it
	told               told
}

// pane returns the pane of the window with id, made the first time.
func (w *window) pane(id int) *pane {
	if p := w.panes[id]; p != nil {
		return p
	}
	p := newPane(w, id)
	p.said.count = unitsOf
	p.view = msgInitView(objc.ID(paneClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	p.text = msgInitView(objc.ID(textClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	track := msgInitTracking(cls("NSTrackingArea").Send(sel("alloc")), sel("initWithRect:options:owner:userInfo:"), rect{}, 0x223, p.text, 0)
	p.text.Send(sel("addTrackingArea:"), track)
	track.Send(sel("release"))
	p.status = msgInitView(objc.ID(statusClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	// The status bar draws on the title bar's material.
	back := material(titlebar, behindWindow)
	p.view.Send(sel("addSubview:"), p.text)
	p.view.Send(sel("addSubview:"), back)
	p.view.Send(sel("addSubview:"), p.status)
	back.Send(sel("release"))
	p.scroller = newScroller(p)
	p.view.Send(sel("addSubview:"), p.scroller)
	w.panes[id], panes[p.text], panes[p.status], panes[p.view] = p, p, p, p
	return p
}

// drop forgets p, whose window closed.
func (p *pane) drop() {
	p.gone = true
	delete(panes, p.text)
	delete(panes, p.status)
	delete(panes, p.view)
	p.view.Send(sel("removeFromSuperview"))
	for _, v := range []objc.ID{p.text, p.status, p.scroller, p.view} {
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

// placeAll gives the loop where each pane puts its window's text, in cells
// of the area, when that changed. Each edge rounds down, so a window right
// of another starts past its end, as <C-w>l needs (B542).
func (w *window) placeAll() {
	area := msgRect(w.area, sel("frame"))
	pl := places{cols: max(int(area.Size.W/cellW), 1), rows: max(int(area.Size.H/cellH), 1), cell: w.cellPx}
	for id, p := range w.panes {
		if objc.Send[objc.ID](p.view, sel("superview")) == 0 {
			continue
		}
		r := msgConvertRect(p.text, sel("convertRect:toView:"), msgRect(p.text, sel("bounds")), w.view)
		x, y := r.Origin.X-area.Origin.X+pad, r.Origin.Y-area.Origin.Y+pad
		pl.wins = append(pl.wins, [5]int{id, int(x / cellW), int(y / cellH), max(int((r.Size.W-2*pad)/cellW), 1), max(int((r.Size.H-2*pad)/cellH), 1)})
	}
	slices.SortFunc(pl.wins, func(a, b [5]int) int { return a[0] - b[0] })
	if pl.cols == w.sent.cols && pl.rows == w.sent.rows && pl.cell == w.sent.cell && slices.Equal(pl.wins, w.sent.wins) {
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
	msgSetRect(w.barBack, sel("setFrame:"), rect{Size: size{b.Size.W, top}})
	msgSetRect(w.area, sel("setFrame:"), rect{point{0, top}, size{b.Size.W, max(b.Size.H-top, 1)}})
	w.placeAll()
}

// resizePane puts a pane's text over its status bar and the material
// behind it, and its scroll bar at the text's right edge.
func resizePane(self objc.ID, _ objc.SEL, _ size) {
	p := panes[self]
	if p == nil {
		return
	}
	b := msgRect(self, sel("bounds")).Size
	h := max(b.H-statusH, 1)
	msgSetRect(p.text, sel("setFrame:"), rect{Size: size{b.W, h}})
	for _, v := range subviews(self)[1:3] {
		msgSetRect(v, sel("setFrame:"), rect{point{0, h}, size{b.W, statusH}})
	}
	msgSetRect(p.scroller, sel("setFrame:"), rect{point{b.W - scrollerW, 0}, size{scrollerW, h}})
}

func splitResized(self objc.ID, _ objc.SEL, note objc.ID) {
	if w := windows[self]; w != nil && objc.Send[objc.ID](note.Send(sel("object")), sel("window")) == w.win {
		w.placeAll()
	}
}

func drawText(self objc.ID, _ objc.SEL, _ rect) {
	p := panes[self]
	if p == nil {
		return
	}
	nsColor(p.w.look.Base.Bg).Send(sel("setFill"))
	rectFill(msgRect(self, sel("bounds")))
	cursor := p.cursor
	cursor.shown = cursor.shown && p.w.caretOn
	o := p.origin()
	drawCells(p.cells, cursor, o, nil, nil)
	if l := p.link; l.w > 0 {
		r := at(l.x, l.y, l.w, o)
		r.Origin.Y, r.Size.H = r.Origin.Y+cellH-2, 1
		system("linkColor").Send(sel("setFill"))
		rectFill(r)
	}
	p.w.drawPics(p.pics, point{pad, pad})
}

func drawDivider(_ objc.ID, _ objc.SEL, r rect) {
	system("separatorColor").Send(sel("setFill"))
	rectFill(r)
}

// drawStatus draws a window's status bar (B147), on the title bar's
// material. The left has the mode for the current window, the file's name
// after its folder, a dot while it is modified, the segments and the
// diagnostics. The right has the others' names, the keys being typed and
// the ruler, and goes when both sides do not fit. Another window's bar has
// dimmer text.
func drawStatus(self objc.ID, _ objc.SEL, _ rect) {
	if p := panes[self]; p != nil {
		drawParts(p.left, p.right, p.current, msgRect(self, sel("bounds")))
	}
}

// attrs are r's text attributes, in the system's font.
func (r run) attrs() objc.ID {
	if r.bold {
		return uiAttrs(uiBold, system(r.fg))
	}
	return uiAttrs(uiFont, system(r.fg))
}

// size is how wide r draws, its room included.
func (r run) size() float64 {
	w := math.Ceil(msgSizeOf(nsString(r.text), sel("sizeWithAttributes:"), r.attrs()).W)
	if r.pill {
		w += 2 * pad
	}
	if r.slotted {
		w += slotW
	}
	if r.tight {
		return w
	}
	return w + pad
}

// draw draws r from x in a bar h tall from top, and returns where the next
// goes.
func (r run) draw(x, top, h float64) float64 {
	a := r.attrs()
	sz := msgSizeOf(nsString(r.text), sel("sizeWithAttributes:"), a)
	at := x
	if r.pill {
		if r.ground != "" {
			fillRounded(r.ground, rect{point{x, top + 4}, size{r.size() - pad, h - 8}}, 6)
		}
		at += pad
	}
	msgDrawAt(nsString(r.text), sel("drawAtPoint:withAttributes:"), point{at, top + math.Floor((h-sz.H)/2)}, a)
	if r.slotGround != "" {
		fillRounded(r.slotGround, slotAt(at+math.Ceil(sz.W), top, h), 4)
	}
	if r.slot != "" {
		drawSymbol(symbol(r.slot, system(r.slotFg)), rect{point{at + math.Ceil(sz.W) + 4, top + math.Floor(h/2) - 4}, size{slotW - 8, 8}})
	}
	return x + r.size()
}

// redraw draws p's text and status bar again.
func (p *pane) redraw() {
	p.text.Send(sel("setNeedsDisplay:"), 1)
	p.status.Send(sel("setNeedsDisplay:"), 1)
}

// pointer makes the mouse's pointer over p's text the pointing hand over a
// link, and the I-beam otherwise.
func (p *pane) pointer(link bool) {
	if link {
		cls("NSCursor").Send(sel("pointingHandCursor")).Send(sel("set"))
	} else if p.mouse[0] >= 0 {
		cls("NSCursor").Send(sel("IBeamCursor")).Send(sel("set"))
	}
}
