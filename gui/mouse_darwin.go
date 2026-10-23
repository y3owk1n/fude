package gui

import (
	"math"
	"strconv"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/client"
)

// mouse is what the mouse did, for the loop to give the session: a click
// or a drag at cell x, y of the window with the id, or a scroll of y lines.
type mouse struct {
	kind     byte // c a click, d a drag and s a scroll
	id, x, y int
}

// give gives the session what the mouse did, on the loop (B552).
func (m mouse) give(s client.Session) {
	switch m.kind {
	case 'c':
		s.Click(m.id, m.x, m.y)
	case 'd':
		s.Drag(m.id, m.x, m.y)
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
		{Cmd: sel("scrollWheel:"), Fn: wheel},
	}
	statusMouse = []objc.MethodDef{
		{Cmd: sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: sel("mouseDown:"), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
			// A row past the window's text gives it the focus alone.
			if p := panes[self]; p != nil {
				p.w.mice <- mouse{'c', p.id, 0, math.MaxInt32}
			}
		}},
	}
	barMouse = []objc.MethodDef{
		{Cmd: sel("acceptsFirstMouse:"), Fn: func(objc.ID, objc.SEL, objc.ID) bool { return true }},
		{Cmd: sel("mouseDownCanMoveWindow"), Fn: func(objc.ID, objc.SEL) bool { return false }},
		{Cmd: sel("mouseDown:"), Fn: barDown},
	}
)

// mouseAt gives the session a click or a drag at the cell under ev, in the
// text of a pane or of a float's box. A drag goes once per cell.
func mouseAt(view objc.ID, ev objc.ID, kind byte) {
	var w *window
	var id int
	o := point{pad, pad}
	if p := panes[view]; p != nil {
		w, id = p.w, p.id
	} else if b := boxes[view]; b != nil && b.id > 0 && b.boxed != nil {
		w, id, o = b.w, b.id, point{inset, inset + b.head()}
	}
	if w == nil {
		return
	}
	at := msgConvertPoint(view, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
	m := mouse{kind, id, int(math.Floor((at.X - o.X) / cellW)), int(math.Floor((at.Y - o.Y) / cellH))}
	if kind == 'd' && m == w.dragged {
		return
	}
	w.dragged = m
	w.mice <- m
}

// wheel scrolls the window under the mouse: a line for each row's height
// a trackpad moves, or each line a wheel turns.
func wheel(self objc.ID, _ objc.SEL, ev objc.ID) {
	p := panes[self]
	if p == nil {
		return
	}
	unit := 1.0
	if objc.Send[bool](ev, sel("hasPreciseScrollingDeltas")) {
		unit = cellH
	}
	p.wheel -= msgFloat(ev, sel("scrollingDeltaY")) / unit
	if n := int(p.wheel); n != 0 {
		p.wheel -= float64(n)
		p.w.mice <- mouse{'s', p.id, 0, n}
	}
}

// barDown shows the buffer or the tab page a click on the tab strip hits,
// as :b and gt do, and else moves the window as its title bar does. A
// double click there zooms it.
func barDown(self objc.ID, _ objc.SEL, ev objc.ID) {
	w := windows[self]
	if w == nil {
		return
	}
	x := msgConvertPoint(self, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0).X
	for _, h := range w.hits {
		if x >= h.from && x < h.to {
			act(w.view, h.keys, "<Esc>"+h.keys, "<Esc>"+h.keys)
			return
		}
	}
	if objc.Send[int](ev, sel("clickCount")) == 2 {
		w.win.Send(sel("performZoom:"), 0)
		return
	}
	w.win.Send(sel("performWindowDragWithEvent:"), ev)
}

// hit is where a tab of the strip is, and the keys that show it.
type hit struct {
	from, to float64
	keys     string
}

// bufferKeys and pageKeys show the buffer numbered n and the tab page at
// index i.
func bufferKeys(n int) string { return ":b " + strconv.Itoa(n) + "<CR>" }
func pageKeys(i int) string   { return strconv.Itoa(i+1) + "gt" }
