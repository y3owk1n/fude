package gui

import (
	"math"
	"time"

	"github.com/ebitengine/purego/objc"
)

// contextItems are what a right click on text offers, by title, with the
// action each sends and the keys the action types: the language server's
// for the place clicked, then the clipboard's (B561).
var contextItems = []struct{ title, action, keys string }{
	{"Go to Definition", "goToDefinition:", "<C-]>"},
	{"Go to Type Definition", "goToTypeDefinition:", "grt"},
	{"Find References", "findReferences:", "grr"},
	{"Find Implementations", "findImplementations:", "gri"},
	{"Rename Symbol", "renameSymbol:", "grn"},
	{"Code Actions", "codeActions:", "gra"},
	{"Show Hover", "showHover:", "K"},
	{},
	{"Cut", "cut:", ""},
	{"Copy", "copy:", ""},
	{"Paste", "paste:", ""},
	{},
	{"Select All", "selectAll:", ""},
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
		keys, visual := it.keys, "<Esc>"+it.keys
		if it.keys == "gra" {
			visual = keys
		}
		defs = append(defs, objc.MethodDef{Cmd: sel(it.action), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
			act(self, keys, "<Esc>"+keys, visual)
		}})
	}
	return defs
}

// contextMenu is the menu a right click on text shows, made once.
var contextMenu objc.ID

// menuFor puts the cursor where a right click hits, unless a selection
// shows, and returns the menu it shows (B561).
func menuFor(self objc.ID, _ objc.SEL, ev objc.ID) objc.ID {
	p := panes[self]
	if p == nil {
		return 0
	}
	if p.w.tabs.mode != "visual" {
		mouseAt(self, ev, 'c')
	}
	if contextMenu == 0 {
		contextMenu = cls("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsString(""))
		for _, it := range contextItems {
			if it.title == "" {
				contextMenu.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
				continue
			}
			contextMenu.Send(sel("addItemWithTitle:action:keyEquivalent:"), nsString(it.title), sel(it.action), nsString(""))
		}
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

// showScroll puts where a pane's view is in its scroll bar. The bar shows
// for a second after the view moves, when the text is taller than it.
func (p *pane) showScroll(top, rows, lines int) {
	moved := top != p.top
	// With no scroll asked for on the way, the view asks for where it is.
	if p.want == p.top {
		p.want = top
	}
	p.top, p.rows, p.lines = top, rows, lines
	if lines <= rows {
		msgSetFloat(p.scroller, sel("setAlphaValue:"), 0)
		return
	}
	msgSetFloat(p.scroller, sel("setKnobProportion:"), float64(rows)/float64(lines))
	msgSetFloat(p.scroller, sel("setDoubleValue:"), float64(top)/float64(lines-rows))
	if !moved {
		return
	}
	msgSetFloat(p.scroller, sel("setAlphaValue:"), 1)
	p.shownAt = time.Now()
	at := p.shownAt
	go func() {
		time.Sleep(time.Second)
		onMain(func() {
			if p.shownAt == at {
				msgSetFloat(p.scroller.Send(sel("animator")), sel("setAlphaValue:"), 0)
			}
		}, false)
	}()
}

// smooth moves p's text up by dy points, a trackpad's move, asking its
// window to scroll a line for each row's height it passes. At the ends of
// the text it stops. When the phase says the fingers or the momentum let
// go, the text settles on the nearest line.
func (p *pane) smooth(dy float64, phase uint) {
	p.offset -= dy
	n := int(math.Floor(p.offset / cellH))
	n = min(max(n, -p.want), max(p.lines-1-p.want, 0))
	if n != 0 {
		p.w.mice <- mouse{'s', p.id, 0, n, 0}
		p.want += n
		p.offset -= float64(n) * cellH
	}
	if p.want == 0 && p.offset < 0 || p.want >= p.lines-1 && p.offset > 0 {
		p.offset = 0
	}
	// The fingers or the momentum ended.
	if phase&8 != 0 {
		if p.offset > cellH/2 && p.want < p.lines-1 {
			p.w.mice <- mouse{'s', p.id, 0, 1, 0}
			p.want++
		}
		p.offset = 0
	}
	p.offset = min(max(p.offset, 0), cellH)
	p.text.Send(sel("setNeedsDisplay:"), 1)
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
