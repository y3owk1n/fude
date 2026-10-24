package gui

import (
	"math"
	"slices"
)

// boxNative is where a box draws over its window's panes, on the system's
// popover ground with rounded corners.
type boxNative struct {
	frame         rect
	listView      uintptr // the system's list its menu shows in
	listed        string  // what the list shows, to know when it changes
	selectedRow   int     // the item the list last selected
	rowPx         int32   // how tall the list's rows are, in pixels
	themed        string  // the system's look the list has
	preview       uintptr // the window the finder's whole preview scrolls in
	previewed     string  // what it shows, to know when another shows
	previewThemed string  // the system's look its scroll bar has
	wheeled       float64 // the lines a float's wheel moved short of one
	boxAccess
}

// newBox makes a hidden box over w's panes, above every box before it.
func newBox(w *window) *box {
	b := &box{w: w, seen: -2, hover: point{X: -1}}
	w.order = append(w.order, b)
	// Direct2D's target for the window shows nothing once a child comes
	// after it, so each box makes its list view before the window paints.
	b.newTable()
	return b
}

// raise puts b over every other box.
func (b *box) raise() {
	b.drop()
	b.w.order = append(b.w.order, b)
}

// drop forgets b, whose float closed.
func (b *box) drop() {
	b.w.order = slices.DeleteFunc(b.w.order, func(o *box) bool { return o == b })
}

// show makes b show what, where placeBox puts it over the panes, under the
// tab strip. A nil what hides b.
func (b *box) show(what *boxed) {
	b.boxed = what
	if what == nil {
		b.scroll, b.seen, b.hover = 0, -2, point{X: -1}
		b.showTable()
		b.showPreview()
		return
	}
	r := placeBox(what, b.w.area(), 0, &b.scroll, &b.seen)
	b.frame = rect{point{math.Round(r.Origin.X), b.w.top() + math.Round(r.Origin.Y)}, size{math.Ceil(r.Size.W), math.Ceil(r.Size.H)}}
	b.showTable()
	b.showPreview()
}

// drawBox draws b bounds big on the system's popover ground, inside a
// hairline edge, and what it shows on that.
func drawBox(b *box, bounds size) {
	fillRounded("separatorColor", rect{Size: bounds}, 8)
	fillRounded("popoverColor", rect{point{1, 1}, size{bounds.W - 2, bounds.H - 2}}, 7)
	b.w.drawBoxed(b.boxed, b.hover, bounds)
}

// drawToast draws a toast's text in r, in the system's font, its lines
// wrapped and the last cut with an ellipsis.
func drawToast(s string, r rect) {
	l := wrapped(s, uiFont, r.Size.W, r.Size.H)
	defer release(l)
	setColour("labelColor")
	drawText(l, r.Origin)
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
