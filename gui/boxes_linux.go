package gui

import "math"

// box is a panel over a window's text, which shows the palette, the
// finder, a menu, a float or a toast: a drawing area over the panes, on
// the theme's popover ground with rounded corners.
type box struct {
	area uintptr
	*boxed
	scroll float64 // how far its list scrolled, in points
	seen   int     // the item its list selected last, to tell when that moves
	w      *window
}

// boxes are the boxes by their drawing areas, which the callbacks get.
var boxes = map[uintptr]*box{}

// newBox makes a hidden box over w's panes, above every box before it. It
// keeps a reference to its area, which the overlay drops and takes again
// as boxes go over it.
func newBox(w *window) *box {
	b := &box{area: gObjectRefSink(gtkDrawingAreaNew()), w: w, seen: -2}
	gtkWidgetSetHalign(b.area, 1)
	gtkWidgetSetValign(b.area, 1)
	gtkWidgetSetTarget(b.area, 0)
	gtkWidgetSetVisible(b.area, 0)
	gtkDrawingAreaSetFn(b.area, callbacks.drawBox, b.area, 0)
	gtkOverlayAdd(w.overlay, b.area)
	boxes[b.area] = b
	return b
}

// raise puts b over every other box.
func (b *box) raise() {
	gtkOverlayRemove(b.w.overlay, b.area)
	gtkOverlayAdd(b.w.overlay, b.area)
}

// drop forgets b, whose float closed.
func (b *box) drop() {
	gtkOverlayRemove(b.w.overlay, b.area)
	delete(boxes, b.area)
	gObjectUnref(b.area)
}

// show makes b show what, where placeBox puts it over the panes. A nil
// what hides b.
func (b *box) show(what *boxed) {
	b.boxed = what
	if what == nil {
		gtkWidgetSetVisible(b.area, 0)
		b.scroll, b.seen = 0, -2
		return
	}
	area := size{float64(gtkWidgetGetWidth(b.w.overlay)), float64(gtkWidgetGetHeight(b.w.overlay))}
	r := placeBox(what, area, 0, &b.scroll, &b.seen)
	gtkWidgetMarginStart(b.area, int32(r.Origin.X))
	gtkWidgetMarginTop(b.area, int32(r.Origin.Y))
	gtkWidgetSizeRequest(b.area, int32(math.Ceil(r.Size.W)), int32(math.Ceil(r.Size.H)))
	gtkWidgetSetVisible(b.area, 1)
	gtkWidgetQueueDraw(b.area)
}

// drawBox draws a box on the theme's popover ground, under a hairline
// edge, and what it shows on that.
func drawBox(area, cairo uintptr, width, height int32, _ uintptr) {
	b := boxes[area]
	if b == nil || b.boxed == nil {
		return
	}
	cr = cairo
	defer func() { cr = 0 }()
	bounds := size{float64(width), float64(height)}
	fillRounded("separatorColor", rect{Size: bounds}, 10)
	fillRounded("popoverColor", rect{point{1, 1}, size{bounds.W - 2, bounds.H - 2}}, 9)
	b.w.drawBoxed(b.boxed, point{X: -1}, bounds)
}

// drawToast draws a toast's text in r, in the desktop's font, its lines
// wrapped and the last cut with an ellipsis.
func drawToast(s string, r rect) {
	l := pangoCairoLayout(cr)
	defer gObjectUnref(l)
	pangoLayoutSetFont(l, uiFont)
	pangoLayoutSetWidth(l, int32(r.Size.W*pangoScale))
	pangoLayoutSetHeight(l, int32(r.Size.H*pangoScale))
	pangoLayoutSetWrap(l, 2)
	pangoLayoutEllipsize(l, 3)
	pangoLayoutSetText(l, s, -1)
	setColour("labelColor")
	cairoMoveTo(cr, r.Origin.X, r.Origin.Y)
	pangoCairoShow(cr, l)
}

// promptGlyphs show a command line's prompt, by what it is.
var promptGlyphs = map[string]string{"command": "›", "search": "/"}

// drawPrompt draws a command line's prompt in r.
func drawPrompt(prompt string, r rect) {
	g := promptGlyphs[prompt]
	sz := measure(g, inputFont)
	setColour("secondaryLabelColor")
	text(g, inputFont, point{r.Origin.X + (r.Size.W-sz.W)/2, r.Origin.Y + (r.Size.H-sz.H)/2})
}
