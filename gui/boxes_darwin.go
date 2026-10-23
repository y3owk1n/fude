package gui

import (
	"math"

	"github.com/ebitengine/purego/objc"
)

// boxNative is a box's views: the system's translucent material with
// rounded corners and a shadow, and view on it, which draws what the box
// shows.
type boxNative struct{ frame, view objc.ID }

// newBox makes a hidden box over w's views (B559).
func newBox(w *window) *box {
	frame := msgInitView(cls("NSView").Send(sel("alloc")), sel("initWithFrame:"), rect{})
	frame.Send(sel("setWantsLayer:"), 1)
	shadow := cls("NSShadow").Send(sel("alloc")).Send(sel("init"))
	msgSetFloat(shadow, sel("setShadowBlurRadius:"), 18)
	msgSetSize(shadow, sel("setShadowOffset:"), size{0, -6})
	shadow.Send(sel("setShadowColor:"), msgColor(cls("NSColor"), sel("colorWithSRGBRed:green:blue:alpha:"), 0, 0, 0, 0.35))
	frame.Send(sel("setShadow:"), shadow)
	shadow.Send(sel("release"))
	back := material(popover, withinWindow)
	back.Send(sel("setWantsLayer:"), 1)
	msgSetFloat(back.Send(sel("layer")), sel("setCornerRadius:"), 10)
	back.Send(sel("layer")).Send(sel("setMasksToBounds:"), 1)
	back.Send(sel("setAutoresizingMask:"), 18)
	v := msgInitView(objc.ID(boxClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	v.Send(sel("setAutoresizingMask:"), 18)
	frame.Send(sel("addSubview:"), back)
	frame.Send(sel("addSubview:"), v)
	// It hears the mouse move over it, for its list's rows to light.
	track := msgInitTracking(cls("NSTrackingArea").Send(sel("alloc")), sel("initWithRect:options:owner:userInfo:"), rect{}, 0x283, v, 0)
	v.Send(sel("addTrackingArea:"), track)
	track.Send(sel("release"))
	back.Send(sel("release"))
	frame.Send(sel("setHidden:"), 1)
	v.Send(sel("setHidden:"), 1)
	w.view.Send(sel("addSubview:"), frame)
	b := &box{boxNative: boxNative{frame, v}, w: w, seen: -2, hover: point{X: -1}}
	boxes[v] = b
	return b
}

// show makes b show what in view, where placeBox puts it, and a toast
// fades in. A nil what hides b.
func (b *box) show(what *boxed, view objc.ID, top float64) {
	shown := b.boxed != nil
	b.boxed = what
	if what == nil {
		b.frame.Send(sel("setHidden:"), 1)
		b.view.Send(sel("setHidden:"), 1)
		b.scroll, b.seen, b.hover = 0, -2, point{X: -1}
		return
	}
	r := placeBox(what, msgRect(view, sel("bounds")).Size, top, &b.scroll, &b.seen)
	msgSetRect(b.frame, sel("setFrame:"), r)
	for _, v := range subviews(b.frame) {
		msgSetRect(v, sel("setFrame:"), rect{Size: r.Size})
	}
	if !shown {
		if what.text != "" {
			msgSetFloat(b.frame, sel("setAlphaValue:"), 0)
			msgSetFloat(b.frame.Send(sel("animator")), sel("setAlphaValue:"), 1)
		}
		b.frame.Send(sel("setHidden:"), 0)
		b.view.Send(sel("setHidden:"), 0)
	}
	b.view.Send(sel("setNeedsDisplay:"), 1)
}

// promptSymbols are the system's symbols for a command line's prompts.
var promptSymbols = map[string]string{"command": "chevron.right", "search": "magnifyingglass"}

// toastSize is how big a toast's text draws in lines w points wide.
func toastSize(text string, w float64) size {
	return msgBounding(nsString(text), sel("boundingRectWithSize:options:attributes:context:"), size{w, math.MaxFloat32}, lineFragments, uiAttrs(toastFont, system("labelColor")), 0).Size
}

// The options that lay a toast's text out in lines, the last one cut with
// an ellipsis.
const lineFragments = 1 | 1<<5

// drawBox draws what a box shows on its material.
func drawBox(self objc.ID, _ objc.SEL, _ rect) {
	if b := boxes[self]; b != nil && b.boxed != nil {
		b.w.drawBoxed(b.boxed, b.hover, msgRect(self, sel("bounds")).Size)
	}
}

// drawToast draws a toast's text in r, in the system's font and colour.
func drawToast(text string, r rect) {
	msgDrawIn(nsString(text), sel("drawWithRect:options:attributes:context:"), r, lineFragments, uiAttrs(toastFont, system("labelColor")), 0)
}

// drawPrompt draws the system's symbol of a command line's prompt in r.
func drawPrompt(prompt string, r rect) {
	drawSymbol(symbol(promptSymbols[prompt], system("secondaryLabelColor")), r)
}

// ask shows q in a sheet on the window. Its buttons and the keys y and n
// answer it as y and n do in the window (B362). apply closes the sheet
// when the question goes away unanswered.
func (w *window) ask(q string) {
	alert := cls("NSAlert").Send(sel("alloc")).Send(sel("init"))
	alert.Send(sel("setMessageText:"), nsString(q))
	for _, b := range [][2]string{{"Yes", "y"}, {"No", "n"}} {
		alert.Send(sel("addButtonWithTitle:"), nsString(b[0])).Send(sel("setKeyEquivalent:"), nsString(b[1]))
	}
	var done objc.Block
	done = objc.NewBlock(func(_ objc.Block, answer int) {
		switch answer {
		case 1000:
			w.keys <- "y"
		case 1001:
			w.keys <- "n"
		}
		if w.sheet == alert {
			w.sheet = 0
		}
		alert.Send(sel("release"))
		done.Release()
	})
	w.sheet = alert
	alert.Send(sel("beginSheetModalForWindow:completionHandler:"), w.win, done)
}
