package gui

import (
	"github.com/ebitengine/purego/objc"
)

// drawBar draws the tab strip right of the window's buttons (B258, B545),
// on the title bar's material in the system's colours. The pages take up
// to half of it. Each tab ends in a slot that shows a dot while its buffer
// has changes, and × to close it while the mouse is over it or it is
// current. It keeps where each tab is, for the mouse (B552, B555, B559).
func drawBar(self objc.ID, _ objc.SEL, _ rect) {
	w := windows[self]
	if w == nil {
		return
	}
	b := msgRect(self, sel("bounds")).Size
	system("separatorColor").Send(sel("setFill"))
	rectFill(rect{point{0, b.H - 1}, size{b.W, 1}})
	zoom := w.win.Send(sel("standardWindowButton:"), 2)
	r := msgConvertRect(zoom, sel("convertRect:toView:"), msgRect(zoom, sel("bounds")), self)
	x, end := r.Origin.X+r.Size.W+2*pad, b.W-pad
	w.drawStrip(b.W, x, end, b.H)
}
