package gui

import (
	"math"
	"strconv"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/vim"
)

// mouse is what the mouse did, for the loop to give the session: a click,
// a drag or a release at cell x, y of the window with the id, a scroll of
// y lines, or a pick of line y of a menu. A click's clicks count a double
// or triple click.
type mouse struct {
	kind             byte // c a click, d a drag, u a release, s a scroll and p a pick
	id, x, y, clicks int
}

// give gives the session what the mouse did, on the loop. A double click
// selects the word it hits and a triple click the line, as viw and V do
// (B552, B555).
func (m mouse) give(s client.Session) {
	switch m.kind {
	case 'c':
		if m.clicks > 1 && s.Mode() == vim.Insert {
			s.Feed("<Esc>")
		}
		s.Click(m.id, m.x, m.y)
		switch {
		case m.clicks == 2:
			for _, k := range vim.Keys("viw") {
				s.Feed(k)
			}
		case m.clicks > 2:
			s.Feed("V")
		}
	case 'd':
		s.Drag(m.id, m.x, m.y)
	case 'u':
		s.Release(m.id, m.x, m.y)
	case 'p':
		s.Pick(m.y)
	default:
		s.Scroll(m.id, m.y)
	}
}

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
	at := msgConvertPoint(view, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
	m := mouse{kind, id, int(math.Floor((at.X - o.X) / cellW)), int(math.Floor((at.Y - o.Y) / cellH)), 1}
	switch {
	case b != nil && b.list != nil:
		if i, ok := b.list.itemAt(at); kind == 'c' && ok {
			w.mice <- mouse{kind: 'p', y: b.list.first + i}
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
	}
	if kind == 'd' && m == w.dragged {
		return
	}
	w.dragged = m
	w.mice <- m
}

// wheel scrolls the window under the mouse, or moves the selection of a
// menu under it as <C-n> and <C-p> do: a line for each row's height a
// trackpad moves, or each line a wheel turns.
func wheel(self objc.ID, _ objc.SEL, ev objc.ID) {
	var w *window
	var turned *float64
	p, b := panes[self], boxes[self]
	switch {
	case p != nil:
		w, turned = p.w, &p.wheel
	case b != nil && b.list != nil:
		w, turned = b.w, &b.w.menuWheel
	default:
		return
	}
	unit := 1.0
	if objc.Send[bool](ev, sel("hasPreciseScrollingDeltas")) {
		unit = cellH
	}
	*turned -= msgFloat(ev, sel("scrollingDeltaY")) / unit
	n := int(*turned)
	if n == 0 {
		return
	}
	*turned -= float64(n)
	if p != nil {
		w.mice <- mouse{'s', p.id, 0, n, 0}
		return
	}
	k := vim.Key("<C-n>")
	if n < 0 {
		k, n = "<C-p>", -n
	}
	for range n {
		w.keys <- k
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
