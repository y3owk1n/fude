package gui

import (
	"math"
	"strconv"
	"time"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/vim"
)

// The mouse's methods for a pane's text, its status bar, a float's box and
// the tab strip.
var (
	textMouse = []objc.MethodDef{
		{Cmd: sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: sel("mouseDown:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) { mouseAt(self, ev, 'c') }},
		{Cmd: sel("mouseDragged:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) { mouseAt(self, ev, 'd') }},
		{Cmd: sel("mouseUp:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) { mouseAt(self, ev, 'u') }},
		{Cmd: sel("scrollWheel:"), Fn: wheel},
		{Cmd: sel("resetCursorRects"), Fn: func(self objc.ID, _ objc.SEL) {
			msgRectID(self, sel("addCursorRect:cursor:"), msgRect(self, sel("bounds")), cls("NSCursor").Send(sel("IBeamCursor")))
		}},
	}
	statusMouse = []objc.MethodDef{
		{Cmd: sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: sel("mouseDown:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
			// A row past the window's text gives it the focus alone.
			if p := panes[self]; p != nil {
				p.w.mice <- mouse{'c', p.id, 0, math.MaxInt32, 1}
			}
		}},
	}
	barMouse = []objc.MethodDef{
		{Cmd: sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: sel("mouseDownCanMoveWindow"), Fn: func(objc.ID, objc.SEL) bool { return false }},
		{Cmd: sel("mouseDown:"), Fn: barDown},
		{Cmd: sel("mouseDragged:"), Fn: barDragged},
		{Cmd: sel("mouseUp:"), Fn: barUp},
		{Cmd: sel("otherMouseDown:"), Fn: barMiddle},
		{Cmd: sel("mouseMoved:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) { hover(self, barX(self, ev)) }},
		{Cmd: sel("mouseExited:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) { hover(self, -1) }},
	}
	// restMouse shows the hover of the word the mouse rests on (B562).
	restMouse = []objc.MethodDef{
		{Cmd: sel("mouseMoved:"), Fn: moved},
		{Cmd: sel("mouseExited:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) { moved(self, 0, 0) }},
	}
	// boxMouse lights the row of a list the mouse is over (B560).
	boxMouse = []objc.MethodDef{
		{Cmd: sel("mouseMoved:"), Fn: func(self objc.ID, _ objc.SEL, ev objc.ID) {
			hoverBox(self, msgConvertPoint(self, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0))
		}},
		{Cmd: sel("mouseExited:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) { hoverBox(self, point{X: -1}) }},
	}
	// dropFiles opens the files dropped on the window (B555).
	dropFiles = []objc.MethodDef{
		{Cmd: sel("draggingEntered:"), Fn: func(objc.ID, objc.SEL, objc.ID) uint { return 1 }},
		{Cmd: sel("performDragOperation:"), Fn: dropped},
	}
)

// mouseAt gives the session a click or a drag at the cell under ev, in the
// text of a pane or of a float's box, or picks the item a click on a menu
// hits. A drag goes once per cell. While the palette or the finder shows,
// a click off it closes it as <Esc> does (B556).
func mouseAt(view objc.ID, ev objc.ID, kind byte) {
	var w *window
	var id int
	o := point{pad, pad}
	b := boxes[view]
	if p := panes[view]; p != nil {
		w, id = p.w, p.id
	} else if b != nil && b.boxed != nil {
		w, id, o = b.w, b.id, point{inset, inset + b.head()}
	}
	if w == nil {
		return
	}
	w.typed, w.caretOn = time.Now(), true
	at := msgConvertPoint(view, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
	m := mouse{kind, id, int(math.Floor((at.X - o.X) / cellW)), int(math.Floor((at.Y - o.Y) / cellH)), 1}
	switch {
	case b != nil && b.list != nil:
		if i, ok := b.list.itemAt(at); kind == 'c' && ok {
			w.mice <- mouse{kind: 'p', y: i}
		}
		return
	case w.panel.boxed != nil:
		if kind == 'c' && b != w.panel {
			w.keys <- "<Esc>"
		}
		return
	case id <= 0:
		return
	}
	if kind == 'c' {
		m.clicks = objc.Send[int](ev, sel("clickCount"))
		// A click closes hover, and a rest after it waits for a move.
		if p := panes[view]; p != nil {
			p.movedAt, p.hovered = time.Now(), [3]int{-1}
			// A click on a link goes to its definition (B563).
			p.mouse = [2]int{m.x, m.y}
			if p.relink(objc.Send[uint](ev, sel("modifierFlags"))); p.link.w > 0 {
				m.kind = 'g'
			}
		}
	}
	if kind == 'd' && m == w.dragged {
		return
	}
	w.dragged = m
	w.mice <- m
}

// wheel scrolls the window or the list under the mouse: a window a line
// for each line a wheel turns, and a list by the points a trackpad moves
// or a row for each line a wheel turns, leaving its selection where it is.
// A trackpad moves a window's text by its points between lines, and when
// it lets go the text settles on the nearest line (B560, B561).
func wheel(self objc.ID, _ objc.SEL, ev objc.ID) {
	dy := msgFloat(ev, sel("scrollingDeltaY"))
	precise := objc.Send[bool](ev, sel("hasPreciseScrollingDeltas"))
	if b := boxes[self]; b != nil && b.boxed != nil && b.list != nil {
		if !precise {
			dy *= b.list.rowH
		}
		b.list.scroll -= dy
		b.list.follow(false)
		b.scroll = b.list.scroll
		self.Send(sel("setNeedsDisplay:"), 1)
		return
	}
	p := panes[self]
	switch {
	case p == nil:
		return
	case precise:
		p.smooth(dy, objc.Send[uint](ev, sel("phase"))|objc.Send[uint](ev, sel("momentumPhase")))
		return
	}
	p.wheel -= dy
	if n := int(p.wheel); n != 0 {
		p.wheel -= float64(n)
		p.w.mice <- mouse{'s', p.id, 0, n, 0}
	}
}

// hit is where a tab of the strip is: a buffer's, numbered n, or with page
// set the tab page at index n. index is its place in its row.
type hit struct {
	from, to float64
	page     bool
	n, index int
}

// slotW is the width of a tab's slot for ● or ×, which ends it.
const slotW = 16

// show, shut and move are the keys that show a tab, close it, and move the
// current one after the tab at index n of its row, first with 0.
func (h hit) show() string {
	if h.page {
		return strconv.Itoa(h.n+1) + "gt"
	}
	return ":b " + strconv.Itoa(h.n) + "<CR>"
}

func (h hit) shut() string {
	if h.page {
		return h.show() + ":tabclose<CR>"
	}
	return ":bd " + strconv.Itoa(h.n) + "<CR>"
}

func (h hit) move(n int) string {
	if h.page {
		return ":tabmove " + strconv.Itoa(n) + "<CR>"
	}
	return ":bmove " + strconv.Itoa(n) + "<CR>"
}

// barX is where ev is along the strip.
func barX(view, ev objc.ID) float64 {
	return msgConvertPoint(view, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0).X
}

// at returns the tab at x, if one is there.
func (w *window) at(x float64) (hit, bool) {
	for _, h := range w.hits {
		if x >= h.from && x < h.to {
			return h, true
		}
	}
	return hit{}, false
}

// barDown shows the tab a click hits, as :b and gt do, or closes it when
// the click hits its ×. Elsewhere a drag moves the window as its title bar
// does, and a double click zooms it.
func barDown(self objc.ID, _ objc.SEL, ev objc.ID) {
	w := windows[self]
	if w == nil {
		return
	}
	x := barX(self, ev)
	w.pressed, w.dragging, w.moving = nil, false, false
	if h, ok := w.at(x); ok {
		if x >= h.to-pad-slotW {
			typeKeys(w, h.shut())
			return
		}
		typeKeys(w, h.show())
		w.pressed, w.pressX = &h, x
		return
	}
	if objc.Send[int](ev, sel("clickCount")) == 2 {
		w.win.Send(sel("performZoom:"), 0)
		return
	}
	w.moving = true
	w.moveFrom = msgPoint(cls("NSEvent"), sel("mouseLocation"))
	w.origin = msgRect(w.win, sel("frame")).Origin
}

// barDragged drags the tab pressed, once it moves past a few points, or
// moves the window by the strip. The window cannot move by its title bar
// on its own, since that would move it along with a dragged tab (B555).
func barDragged(self objc.ID, _ objc.SEL, ev objc.ID) {
	w := windows[self]
	switch {
	case w == nil:
		return
	case w.moving:
		at := msgPoint(cls("NSEvent"), sel("mouseLocation"))
		msgPointArg(w.win, sel("setFrameOrigin:"), point{w.origin.X + at.X - w.moveFrom.X, w.origin.Y + at.Y - w.moveFrom.Y})
		return
	case w.pressed == nil:
		return
	}
	x := barX(self, ev)
	w.dragging = w.dragging || math.Abs(x-w.pressX) > 4
	w.dropX = x
	self.Send(sel("setNeedsDisplay:"), 1)
}

// barUp moves a dragged tab where it was dropped, as :bmove and :tabmove
// do (B555).
func barUp(self objc.ID, _ objc.SEL, _ objc.ID) {
	w := windows[self]
	if w == nil {
		return
	}
	w.moving = false
	if w.pressed == nil {
		return
	}
	if w.dragging {
		typeKeys(w, w.pressed.move(w.dropIndex()))
	}
	w.pressed, w.dragging = nil, false
	self.Send(sel("setNeedsDisplay:"), 1)
}

// dropIndex is the place of the tab after which a dragged one goes, as
// :bmove and :tabmove count it, 0 for first.
func (w *window) dropIndex() int {
	n := 0
	for _, h := range w.hits {
		if h.page == w.pressed.page && (h.from+h.to)/2 < w.dropX {
			n = h.index + 1
		}
	}
	return n
}

// dropAt is where the mark goes that shows where a dragged tab would go.
func (w *window) dropAt() (float64, bool) {
	if !w.dragging || w.pressed == nil {
		return 0, false
	}
	x, found := 0.0, false
	for _, h := range w.hits {
		if h.page != w.pressed.page {
			continue
		}
		switch {
		case (h.from+h.to)/2 < w.dropX:
			x, found = h.to, true
		case !found:
			x, found = h.from, true
		}
	}
	return x, found
}

// barMiddle closes the tab a middle click hits.
func barMiddle(self objc.ID, _ objc.SEL, ev objc.ID) {
	w := windows[self]
	if w == nil || objc.Send[int](ev, sel("buttonNumber")) != 2 {
		return
	}
	if h, ok := w.at(barX(self, ev)); ok {
		typeKeys(w, h.shut())
	}
}

// hoverBox marks where the mouse is over a box, and draws its list again
// when that lights another row.
func hoverBox(view objc.ID, at point) {
	b := boxes[view]
	if b == nil {
		return
	}
	b.hover = at
	if b.boxed != nil && b.list != nil {
		view.Send(sel("setNeedsDisplay:"), 1)
	}
}

// hover marks the tab the mouse is over, x along the strip or -1 for none.
func hover(self objc.ID, x float64) {
	if w := windows[self]; w != nil && w.hover != x {
		w.hover = x
		self.Send(sel("setNeedsDisplay:"), 1)
	}
}

// typeKeys types keys in the mode the window is in, after <Esc> in insert
// and visual mode. Other modes take none.
func typeKeys(w *window, keys string) {
	act(w.view, keys, "<Esc>"+keys, "<Esc>"+keys)
}

// dropped opens each file dropped on the window, as :e does.
func dropped(self objc.ID, _ objc.SEL, info objc.ID) bool {
	w := windows[self]
	if w == nil {
		return false
	}
	urls := info.Send(sel("draggingPasteboard")).Send(sel("readObjectsForClasses:options:"), cls("NSArray").Send(sel("arrayWithObject:"), cls("NSURL")), 0)
	if urls == 0 {
		return false
	}
	var keys []vim.Key
	if mode := w.tabs.mode; mode == "insert" || mode == "visual" {
		keys = append(keys, "<Esc>")
	} else if mode != "normal" {
		return false
	}
	for i := range objc.Send[int](urls, sel("count")) {
		path := goString(urls.Send(sel("objectAtIndex:"), i).Send(sel("path")))
		keys = append(keys, vim.Keys(":e ")...)
		for _, r := range path {
			keys = append(keys, vim.Key(string(r)))
		}
		keys = append(keys, "<CR>")
	}
	for _, k := range keys {
		w.keys <- k
	}
	return true
}
