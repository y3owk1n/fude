package gui

import (
	"math"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/y3owk1n/fude/vim"
)

// The input method's callbacks, made once with the others.
var inputCallbacks struct {
	commit, preedit, drawMarked uintptr
}

// makeInputCallbacks makes the input method's callbacks.
func makeInputCallbacks() {
	inputCallbacks.commit = purego.NewCallback(func(_, text, win uintptr) uintptr {
		if w := windows[win]; w != nil {
			p := gStrdup(text)
			w.typeText(cString(p))
			gFree(unsafe.Pointer(p))
		}
		return 0
	})
	inputCallbacks.preedit = purego.NewCallback(func(im, win uintptr) uintptr {
		if w := windows[win]; w != nil {
			var text *byte
			var attrs uintptr
			var cursor int32
			gtkIMPreedit(im, &text, &attrs, &cursor)
			w.mark(cString(text))
			gFree(unsafe.Pointer(text))
			pangoAttrListUnref(attrs)
		}
		return 0
	})
	inputCallbacks.drawMarked = purego.NewCallback(func(_, cairo uintptr, width, height int32, win uintptr) uintptr {
		if w := windows[win]; w != nil {
			cr = cairo
			fillRGB(w.look.Base.Bg, rect{Size: size{float64(width), float64(height)}})
			drawString(w.marked, w.look.Base, "", point{})
			fillRGB(w.look.Base.Fg, rect{point{0, float64(height) - 2}, size{float64(width), 1}})
			cr = 0
		}
		return 0
	})
}

// newInput gives w an input method, which takes each key first, and the
// view that shows what it composes. A key it does not take goes to
// keyPressed (B551, B571).
func (w *window) newInput(ctl uintptr) {
	w.im = gtkIMNew()
	gtkIMSetClient(w.im, w.overlay)
	gSignalConnectData(w.im, "commit", inputCallbacks.commit, w.win, 0, 0)
	gSignalConnectData(w.im, "preedit-changed", inputCallbacks.preedit, w.win, 0, 0)
	gtkKeySetIM(ctl, w.im)
	gtkIMFocusIn(w.im)
	w.preedit = gtkDrawingAreaNew()
	gtkWidgetSetHalign(w.preedit, 1)
	gtkWidgetSetValign(w.preedit, 1)
	gtkWidgetSetTarget(w.preedit, 0)
	gtkWidgetSetVisible(w.preedit, 0)
	gtkDrawingAreaSetFn(w.preedit, inputCallbacks.drawMarked, w.win, 0)
	gtkOverlayAdd(w.overlay, w.preedit)
}

// typeText types the text an input method inserts, each character a key.
func (w *window) typeText(text string) {
	for _, r := range text {
		w.keys <- vim.Key(string(r))
	}
}

// mark shows text, what an input method composes, underlined at the
// cursor, or hides it for "".
func (w *window) mark(text string) {
	if text == w.marked {
		return
	}
	w.marked = text
	at, ok := w.caret()
	if text == "" || !ok {
		gtkWidgetSetVisible(w.preedit, 0)
		return
	}
	gtkWidgetMarginStart(w.preedit, int32(at.X))
	gtkWidgetMarginTop(w.preedit, int32(at.Y))
	gtkWidgetSizeRequest(w.preedit, int32(math.Ceil(measure(text, font).W))+2, int32(cellH))
	gtkWidgetSetVisible(w.preedit, 1)
	gtkWidgetQueueDraw(w.preedit)
}

// caret is where the cursor is over the panes: in the palette's or the
// finder's line, in a float with the focus, or in a pane.
func (w *window) caret() (point, bool) {
	// in is where cell c of area's text is, its first cell at o.
	in := func(area uintptr, c cursorAt, o point) (point, bool) {
		var r grect
		if gtkWidgetBounds(area, w.overlay, &r) == 0 {
			return point{}, false
		}
		return point{float64(r.X) + o.X + float64(c.x)*cellW, float64(r.Y) + o.Y + float64(c.y)*cellH}, true
	}
	if b := w.panel; b.boxed != nil && b.list != nil {
		var r grect
		if gtkWidgetBounds(b.area, w.overlay, &r) == 0 {
			return point{}, false
		}
		return point{float64(r.X) + b.list.caret.X, float64(r.Y) + b.list.caret.Y}, true
	}
	for _, b := range w.floats {
		if b.boxed != nil && b.cursor.shown {
			return in(b.area, b.cursor, point{inset, inset + b.head()})
		}
	}
	for _, p := range w.panes {
		if p.cursor.shown {
			return in(p.area, p.cursor, p.origin())
		}
	}
	return point{}, false
}

// placeCaret tells the input method where the cursor is, for its
// candidates to show by it.
func (w *window) placeCaret() {
	if at, ok := w.caret(); ok {
		gtkIMCursorAt(w.im, &gdkRect{int32(at.X), int32(at.Y), 1, int32(cellH)})
	}
}
