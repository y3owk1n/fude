package gui

import "github.com/ebitengine/purego"

// The tab strip's callbacks, made once with the others.
var stripCallbacks struct {
	draw, pressed, released, motion, leave uintptr
}

// makeStripCallbacks makes the tab strip's callbacks.
func makeStripCallbacks() {
	stripCallbacks.draw = purego.NewCallback(func(area, cairo uintptr, width, height int32, win uintptr) uintptr {
		if w := windows[win]; w != nil {
			cr = cairo
			w.drawStrip(float64(width), pad, float64(width)-pad, float64(height))
			cr = 0
		}
		return 0
	})
	stripCallbacks.pressed = purego.NewCallback(func(gesture uintptr, _ int32, x, _ float64, win uintptr) uintptr {
		w := windows[win]
		if w == nil {
			return 0
		}
		switch gtkGestureButton(gesture) {
		case 1:
			// A press off the tabs is the title bar's, which drags or
			// maximizes the window.
			if !w.stripPress(x) {
				gtkGestureSetState(gesture, 2)
				return 0
			}
			gtkGestureSetState(gesture, 1)
		case 2:
			w.stripShut(x)
		}
		gtkWidgetQueueDraw(w.bar)
		return 0
	})
	stripCallbacks.released = purego.NewCallback(func(_ uintptr, _ int32, _, _ float64, win uintptr) uintptr {
		if w := windows[win]; w != nil && w.stripRelease() {
			gtkWidgetQueueDraw(w.bar)
		}
		return 0
	})
	stripCallbacks.motion = purego.NewCallback(func(_ uintptr, x, _ float64, win uintptr) uintptr {
		if w := windows[win]; w != nil {
			w.stripDrag(x)
			w.hover = x
			gtkWidgetQueueDraw(w.bar)
		}
		return 0
	})
	stripCallbacks.leave = purego.NewCallback(func(_, win uintptr) uintptr {
		if w := windows[win]; w != nil {
			w.hover = -1
			gtkWidgetQueueDraw(w.bar)
		}
		return 0
	})
}

// stripH is how tall the tab strip is, in points.
const stripH = 34

// newStrip puts w's tab strip in a header bar as its title, and makes the
// header bar the window's title bar. Where no tab is, the header bar drags
// and maximizes the window (B568).
func (w *window) newStrip() {
	header := gtkHeaderBarNew()
	w.bar = gtkDrawingAreaNew()
	gtkWidgetSetHexpand(w.bar, 1)
	gtkDrawingAreaHeight(w.bar, stripH)
	gtkDrawingAreaSetFn(w.bar, stripCallbacks.draw, w.win, 0)
	click := gtkGestureClickNew()
	gtkGestureSetButton(click, 0)
	gSignalConnectData(click, "pressed", stripCallbacks.pressed, w.win, 0, 0)
	gSignalConnectData(click, "released", stripCallbacks.released, w.win, 0, 0)
	gtkWidgetAddControl(w.bar, click)
	move := gtkMotionNew()
	gSignalConnectData(move, "motion", stripCallbacks.motion, w.win, 0, 0)
	gSignalConnectData(move, "leave", stripCallbacks.leave, w.win, 0, 0)
	gtkWidgetAddControl(w.bar, move)
	gtkHeaderBarTitle(header, w.bar)
	gtkWindowSetTitlebar(w.win, header)
}
