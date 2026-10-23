package gui

import "math"

// boxNative is a box's drawing area, laid over the panes on the theme's
// popover ground with rounded corners.
type boxNative struct {
	area          uintptr
	listBox       uintptr // the system's list its menu shows in
	scrolled      uintptr // the scrolled window of the list
	listed        string  // what the list shows, to know when it changes
	previewArea   uintptr // the finder's whole preview, or a menu's whole documentation
	previewScroll uintptr // its scrolled window
	previewed     string  // what it shows, to know when another shows
	wheeled       float64 // the part of a line the wheel moved a float, not yet sent
	said          spoken  // its text as assistive apps count it
	told          told
	labels        labels
}

// boxes are the boxes by their drawing areas, which the callbacks get.
var boxes = map[uintptr]*box{}

// newBox makes a hidden box over w's panes, above every box before it,
// whose area has the accessible role. It keeps a reference to its area,
// which the overlay drops and takes again as boxes go over it.
func newBox(w *window, role string) *box {
	b := &box{boxNative: boxNative{area: gObjectRefSink(newArea(role)), said: spoken{count: func(rune) int { return 1 }}}, w: w, seen: -2, hover: point{X: -1}}
	gtkWidgetSetHalign(b.area, 1)
	gtkWidgetSetValign(b.area, 1)
	listen(b.area)
	gtkWidgetSetVisible(b.area, 0)
	gtkDrawingAreaSetFn(b.area, callbacks.drawBox, b.area, 0)
	gtkOverlayAdd(w.overlay, b.area)
	boxes[b.area] = b
	b.newTable()
	return b
}

// raise puts b, its list and its preview over every other box.
func (b *box) raise() {
	for _, w := range []uintptr{b.area, b.scrolled, b.previewScroll} {
		gtkOverlayRemove(b.w.overlay, w)
		gtkOverlayAdd(b.w.overlay, w)
	}
}

// drop forgets b, whose float closed.
func (b *box) drop() {
	b.dropTable()
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
		b.scroll, b.seen, b.hover = 0, -2, point{X: -1}
		b.showTable(rect{})
		b.showPreview(rect{})
		return
	}
	area := size{float64(gtkWidgetGetWidth(b.w.overlay)), float64(gtkWidgetGetHeight(b.w.overlay))}
	r := placeBox(what, area, 0, &b.scroll, &b.seen)
	gtkWidgetMarginStart(b.area, int32(r.Origin.X))
	gtkWidgetMarginTop(b.area, int32(r.Origin.Y))
	gtkWidgetSizeRequest(b.area, int32(math.Ceil(r.Size.W)), int32(math.Ceil(r.Size.H)))
	gtkWidgetSetVisible(b.area, 1)
	b.showTable(r)
	b.showPreview(r)
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
	b.w.drawBoxed(b.boxed, b.hover, bounds)
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
