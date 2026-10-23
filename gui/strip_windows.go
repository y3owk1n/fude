package gui

import (
	"math"
	"unsafe"
)

// stripH is how tall the tab strip is, in points, and captionW how wide
// each of the window's buttons at its right.
const (
	stripH   = 34
	captionW = 46
)

// The messages of the window's frame, which the strip replaces as its
// title bar.
const (
	wmNcCalcSize     = 0x0083
	wmNcHitTest      = 0x0084
	wmNcMouseMove    = 0x00A0
	wmNcLButtonDown  = 0x00A1
	wmNcLButtonUp    = 0x00A2
	wmNcMouseLeave   = 0x02A2
	htClient         = 1
	htCaption        = 2
	htMinButton      = 8
	htMaxButton      = 9
	htTop            = 12
	htTopLeft        = 13
	htTopRight       = 14
	htClose          = 20
	smCyFrame        = 33
	smCxPaddedBorder = 92
)

var (
	isZoomed      = user32.NewProc("IsZoomed")
	systemMetrics = user32.NewProc("GetSystemMetricsForDpi")
)

// captionFont draws the window's buttons, in the system's icon font.
var captionFont unsafe.Pointer

// captions are the window's buttons, from the left, by what a point over
// each hits, and the glyphs they show.
var captions = []struct {
	hit   uintptr
	glyph string
}{{htMinButton, ""}, {htMaxButton, ""}, {htClose, ""}}

// stripNative is what a window keeps of its strip's buttons.
type stripNative struct {
	caption  uintptr // the button the mouse is over, 0 for none
	down     uintptr // the button the mouse went down on, 0 for none
	tracking bool    // Windows tells the window when the mouse leaves its frame
}

// zoomed reports whether w is maximized.
func (w *window) zoomed() bool { return call(isZoomed, w.hwnd) != 0 }

// border is how thick the window's resizing border is, in pixels.
func (w *window) border() int32 {
	dpi := uintptr(96 * w.scale)
	return int32(call(systemMetrics, smCyFrame, dpi) + call(systemMetrics, smCxPaddedBorder, dpi))
}

// frame handles a message of the window's frame. The frame has no title
// bar, because the strip is the title bar. Off the tabs, the strip drags the window,
// and a double click there maximizes it. Its buttons minimize, maximize
// and close the window (B575).
func (w *window) frame(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmNcCalcSize:
		if wp == 0 {
			return 0, false
		}
		r := (*rectI)(*(*unsafe.Pointer)(unsafe.Pointer(&lp)))
		top := r.T
		call(defWindowProc, w.hwnd, uintptr(m), wp, lp)
		r.T = top
		// A maximized window reaches past the screen by its border.
		if w.zoomed() {
			r.T += w.border()
		}
		return 0, true
	case wmNcHitTest:
		if hit := call(defWindowProc, w.hwnd, uintptr(m), wp, lp); hit != htClient {
			return hit, true
		}
		return w.hit(w.client(int32(int16(lp)), int32(int16(lp>>16)))), true
	case wmNcMouseMove:
		if !w.stripNative.tracking {
			w.stripNative.tracking = true
			t := trackEvent{flags: 0x12, hwnd: w.hwnd}
			t.size = uint32(unsafe.Sizeof(t))
			call(trackMouseEvent, uintptr(unsafe.Pointer(&t)))
		}
		w.hoverCaption(wp)
		w.hover = -1
	case wmNcMouseLeave:
		w.stripNative.tracking = false
		w.hoverCaption(0)
	case wmNcLButtonDown:
		if !w.isCaption(wp) {
			return 0, false
		}
		w.down = wp
	case wmNcLButtonUp:
		if !w.isCaption(wp) {
			return 0, false
		}
		if wp == w.down {
			switch wp {
			case htMinButton:
				call(showWindow, w.hwnd, 6)
			case htMaxButton:
				if w.zoomed() {
					call(showWindow, w.hwnd, 9)
				} else {
					call(showWindow, w.hwnd, 3)
				}
			case htClose:
				call(postMessage, w.hwnd, wmClose, 0, 0)
			}
		}
		w.down = 0
	default:
		return 0, false
	}
	return 0, true
}

// isCaption reports whether hit is one of the window's buttons.
func (w *window) isCaption(hit uintptr) bool {
	return hit == htMinButton || hit == htMaxButton || hit == htClose
}

// hoverCaption lights the button the mouse is over, 0 for none.
func (w *window) hoverCaption(hit uintptr) {
	if !w.isCaption(hit) {
		hit = 0
	}
	if hit != w.caption {
		w.caption = hit
		w.redraw()
	}
}

// hit is what a point of the window's client area is to Windows: its top
// border while it is not maximized, a button, a tab, the rest of the strip
// as the title bar, or the panes.
func (w *window) hit(at point) uintptr {
	width := w.area().W
	edge := float64(w.border()) / w.scale
	switch {
	case at.Y >= stripH:
		return htClient
	case at.Y < edge && !w.zoomed():
		switch {
		case at.X < edge:
			return htTopLeft
		case at.X >= width-edge:
			return htTopRight
		}
		return htTop
	case at.X >= width-3*captionW:
		return captions[min(int((at.X-(width-3*captionW))/captionW), 2)].hit
	}
	if _, ok := w.at(at.X); ok {
		return htClient
	}
	return htCaption
}

// drawStripBar draws the tab strip across the top of the window, width
// points wide, on the system's bar colour, with the window's buttons at
// its right.
func (w *window) drawStripBar(width float64) {
	bar := rect{Size: size{width, stripH}}
	fill("barColor", bar)
	fill("separatorColor", rect{point{0, stripH - 1}, size{width, 1}})
	w.drawStrip(width-3*captionW, pad, width-3*captionW-pad, stripH-1)
	for i, c := range captions {
		r := rect{point{width - float64(3-i)*captionW, 0}, size{captionW, stripH - 1}}
		glyph, colour := c.glyph, "labelColor"
		if c.hit == htMaxButton && w.zoomed() {
			glyph = ""
		}
		switch {
		case c.hit == w.caption && c.hit == htClose:
			fillRGB(0xc42b1c, r)
			colour = "whiteColor"
		case c.hit == w.caption:
			fill("quaternaryLabelColor", r)
		}
		sz := measure(glyph, captionFont)
		setColour(colour)
		text(glyph, captionFont, point{r.Origin.X + math.Round((captionW-sz.W)/2), math.Round((r.Size.H - sz.H) / 2)})
	}
}

// stripDown takes a press on the tab strip: the left button shows the tab
// at x or starts dragging it, and the middle one closes it (B555).
func (w *window) stripDown(button int, x float64) {
	switch button {
	case 1:
		w.stripPress(x)
	case 2:
		w.stripShut(x)
	}
	w.redraw()
}
