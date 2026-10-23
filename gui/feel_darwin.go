package gui

import (
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"
)

// selectorOf is the action a context item sends, its title in lower camel
// case, as goToDefinition: for Go to Definition.
func selectorOf(title string) string {
	words := strings.Fields(title)
	words[0] = strings.ToLower(words[0])
	return strings.Join(words, "") + ":"
}

// contextActions are the root view's methods for the language server's
// items, which type their keys in normal mode, after <Esc> in insert mode.
// Code actions take a selection too.
func contextActions() []objc.MethodDef {
	var defs []objc.MethodDef
	for _, it := range contextItems {
		if it.keys == "" {
			continue
		}
		keys := it.keys
		defs = append(defs, objc.MethodDef{Cmd: sel(selectorOf(it.title)), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
			if w := windows[self]; w != nil {
				w.contextAct(keys)
			}
		}})
	}
	return defs
}

// contextMenu is the menu a right click on text shows, made once.
var contextMenu objc.ID

// menuFor puts the cursor where a right click hits, unless a selection
// shows, and returns the menu it shows. It shows the language server's
// items the window's servers offer, and Cut, Copy and Paste only when
// there is a selection to take or text to paste (B561).
func menuFor(self objc.ID, _ objc.SEL, ev objc.ID) objc.ID {
	p := panes[self]
	if p == nil {
		return 0
	}
	visual := p.w.tabs.mode == "visual"
	if !visual {
		mouseAt(self, ev, 'c')
	}
	if contextMenu == 0 {
		contextMenu = cls("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsString(""))
		contextMenu.Send(sel("setAutoenablesItems:"), 0)
		for _, it := range contextItems {
			if it.title == "" {
				contextMenu.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
				continue
			}
			contextMenu.Send(sel("addItemWithTitle:action:keyEquivalent:"), nsString(it.title), sel(selectorOf(it.title)), nsString(""))
		}
	}
	pasted := goString(cls("NSPasteboard").Send(sel("generalPasteboard")).Send(sel("stringForType:"), nsString(pasteboardText))) != ""
	shown, enabled := contextState(p.offers, visual, pasted)
	for i := range contextItems {
		item := contextMenu.Send(sel("itemAtIndex:"), i)
		item.Send(sel("setHidden:"), !shown[i])
		item.Send(sel("setEnabled:"), enabled[i])
	}
	return contextMenu
}

// scrollerW is how wide a pane's scroll bar is, in points.
const scrollerW = 12

// newScroller makes a pane's scroll bar, the system's thin one that shows
// while the view moves.
func newScroller(p *pane) objc.ID {
	s := msgInitView(cls("NSScroller").Send(sel("alloc")), sel("initWithFrame:"), rect{Size: size{scrollerW, 100}})
	s.Send(sel("setScrollerStyle:"), 1)
	s.Send(sel("setEnabled:"), 1)
	s.Send(sel("setTarget:"), p.text)
	s.Send(sel("setAction:"), sel("scrolled:"))
	msgSetFloat(s, sel("setAlphaValue:"), 0)
	return s
}

// showBar puts where p's view is in its scroll bar, which shows for a
// second after the view moved, when the text is taller than it.
func (p *pane) showBar(moved bool) {
	if p.lines <= p.rows {
		msgSetFloat(p.scroller, sel("setAlphaValue:"), 0)
		return
	}
	msgSetFloat(p.scroller, sel("setKnobProportion:"), float64(p.rows)/float64(p.lines))
	msgSetFloat(p.scroller, sel("setDoubleValue:"), float64(p.top)/float64(p.lines-p.rows))
	if !moved {
		return
	}
	msgSetFloat(p.scroller, sel("setAlphaValue:"), 1)
	p.shownAt = time.Now()
	at := p.shownAt
	go func() {
		time.Sleep(time.Second)
		onMain(func() {
			// A pane its window dropped, as switching tab pages does, freed its bar.
			if p.shownAt == at && !p.gone {
				msgSetFloat(p.scroller.Send(sel("animator")), sel("setAlphaValue:"), 0)
			}
		}, false)
	}()
}

// scrolled scrolls a pane's window to where its scroll bar's knob went.
func scrolled(self objc.ID, _ objc.SEL, s objc.ID) {
	p := panes[self]
	if p == nil || p.lines <= p.rows {
		return
	}
	to := int(math.Round(msgFloat(s, sel("doubleValue")) * float64(p.lines-p.rows)))
	if n := to - p.want; n != 0 {
		p.w.mice <- mouse{'s', p.id, 0, n, 0}
		p.want = to
	}
	p.shownAt = time.Now()
}

// moved marks the cell of a pane's text the mouse is over, or for ev 0
// that it left. A move over a box, such as a hover's, changes nothing
// (B562).
func moved(view objc.ID, _ objc.SEL, ev objc.ID) {
	p := panes[view]
	if p == nil {
		return
	}
	if ev == 0 {
		p.pointAt([2]int{-1, -1}, 0)
		return
	}
	in := msgPoint(ev, sel("locationInWindow"))
	if p.w.overBox(msgConvertPoint(p.w.view, sel("convertPoint:fromView:"), in, 0)) {
		return
	}
	p.pointAt(cellAt(msgConvertPoint(view, sel("convertPoint:fromView:"), in, 0)), heldOf(objc.Send[uint](ev, sel("modifierFlags"))))
}

// flagsChanged shows or ends the link under the mouse as Command goes down
// or up (B563).
func flagsChanged(self objc.ID, _ objc.SEL, ev objc.ID) {
	if w := windows[self]; w != nil {
		for _, p := range w.panes {
			p.relink(heldOf(objc.Send[uint](ev, sel("modifierFlags"))))
		}
	}
}

// overBox reports whether at, in the root view, is over a box that shows.
func (w *window) overBox(at point) bool {
	for _, b := range append(slices.Collect(maps.Values(w.floats)), w.panel, w.toast) {
		if b != nil && !objc.Send[bool](b.frame, sel("isHidden")) {
			if r := msgRect(b.frame, sel("frame")); at.X >= r.Origin.X && at.X < r.Origin.X+r.Size.W && at.Y >= r.Origin.Y && at.Y < r.Origin.Y+r.Size.H {
				return true
			}
		}
	}
	return false
}

// blink turns the cursor of a window with the keys off and on, as the
// system's text fields do, and keeps it on for a while after each key or
// click (B561).
func (w *window) blink() {
	period := time.Duration(cls("NSUserDefaults").Send(sel("standardUserDefaults")).Send(sel("integerForKey:"), nsString("NSTextInsertionPointBlinkPeriodOn"))) * time.Millisecond
	if period <= 0 {
		period = 560 * time.Millisecond
	}
	for range time.Tick(period) {
		done := false
		onMain(func() {
			if _, open := windows[w.view]; !open {
				done = true
				return
			}
			on := !objc.Send[bool](w.win, sel("isKeyWindow")) || time.Since(w.typed) < period || !w.caretOn
			if on != w.caretOn {
				w.caretOn = on
				for _, p := range w.panes {
					if p.cursor.shown {
						p.text.Send(sel("setNeedsDisplay:"), 1)
					}
				}
			}
		}, true)
		if done {
			return
		}
	}
}
