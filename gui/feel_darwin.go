package gui

import (
	"maps"
	"math"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"
)

// contextItems are what a right click on text offers, the language
// server's items for the place clicked, then the clipboard's. Each has its
// title, the action it sends, the keys that action types and the language
// server feature it needs (B561).
var contextItems = []struct{ title, action, keys, needs string }{
	{"Go to Definition", "goToDefinition:", "<C-]>", "definition"},
	{"Go to Type Definition", "goToTypeDefinition:", "grt", "typeDefinition"},
	{"Find References", "findReferences:", "grr", "references"},
	{"Find Implementations", "findImplementations:", "gri", "implementation"},
	{"Rename Symbol", "renameSymbol:", "grn", "rename"},
	{"Code Actions", "codeActions:", "gra", "codeAction"},
	{"Show Hover", "showHover:", "K", "hover"},
	{},
	{"Cut", "cut:", "", ""},
	{"Copy", "copy:", "", ""},
	{"Paste", "paste:", "", ""},
	{},
	{"Select All", "selectAll:", "", ""},
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
			contextMenu.Send(sel("addItemWithTitle:action:keyEquivalent:"), nsString(it.title), sel(it.action), nsString(""))
		}
	}
	pasted := goString(cls("NSPasteboard").Send(sel("generalPasteboard")).Send(sel("stringForType:"), nsString(pasteboardText))) != ""
	offered := false
	for i, it := range contextItems {
		item := contextMenu.Send(sel("itemAtIndex:"), i)
		switch {
		case it.needs != "":
			on := slices.Contains(p.offers, it.needs)
			offered = offered || on
			item.Send(sel("setHidden:"), !on)
		case it.title == "Cut" || it.title == "Copy":
			item.Send(sel("setEnabled:"), visual)
		case it.title == "Paste":
			item.Send(sel("setEnabled:"), pasted)
		case it.title == "":
			// The separator after the language server's items hides with them.
			if contextItems[i-1].needs != "" {
				item.Send(sel("setHidden:"), !offered)
			}
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

// restFor is how long the mouse rests on a word before its hover shows.
const restFor = 500 * time.Millisecond

// moved marks the cell of a pane's text the mouse is over, or for ev 0
// that it left. A move off the word whose hover it asked for closes that
// hover, and a rest on another word asks for its hover. A move over a box,
// such as that hover's, changes nothing (B562).
func moved(view objc.ID, _ objc.SEL, ev objc.ID) {
	p := panes[view]
	if p == nil {
		return
	}
	cell, mods := [2]int{-1, -1}, uint(0)
	if ev != 0 {
		mods = objc.Send[uint](ev, sel("modifierFlags"))
		in := msgPoint(ev, sel("locationInWindow"))
		if p.w.overBox(msgConvertPoint(p.w.view, sel("convertPoint:fromView:"), in, 0)) {
			return
		}
		at := msgConvertPoint(view, sel("convertPoint:fromView:"), in, 0)
		cell = [2]int{int(math.Floor((at.X - pad) / cellW)), int(math.Floor((at.Y - pad) / cellH))}
	}
	// Command may go down or up between moves on one cell.
	same := cell == p.mouse
	p.mouse = cell
	if p.relink(mods); same {
		return
	}
	p.movedAt = time.Now()
	if h := p.hovered; h[0] >= 0 && (cell[1] != h[0] || cell[0] < h[1] || cell[0] >= h[2]) {
		p.hovered = [3]int{-1}
		p.w.mice <- mouse{'h', p.id, -1, -1, 0}
	}
	from, to := p.wordAt(cell)
	if from == to || p.hovered[0] >= 0 {
		return
	}
	at := p.movedAt
	time.AfterFunc(restFor, func() {
		onMain(func() {
			if p.movedAt == at && panes[p.text] == p {
				p.hovered = [3]int{cell[1], from, to}
				p.w.mice <- mouse{'h', p.id, cell[0], cell[1], 0}
			}
		}, false)
	})
}

// relink makes the word under the mouse a link while mods hold Command
// and the window's servers offer definitions. A link is underlined, and
// the pointer over it is the pointing hand (B563).
func (p *pane) relink(mods uint) {
	var l rule
	if mods&modCommand != 0 && slices.Contains(p.offers, "definition") {
		if from, to := p.wordAt(p.mouse); from != to {
			l = rule{from, p.mouse[1], to - from}
		}
	}
	if l == p.link {
		return
	}
	p.link = l
	if l.w > 0 {
		cls("NSCursor").Send(sel("pointingHandCursor")).Send(sel("set"))
	} else if p.mouse[0] >= 0 {
		cls("NSCursor").Send(sel("IBeamCursor")).Send(sel("set"))
	}
	p.text.Send(sel("setNeedsDisplay:"), 1)
}

// flagsChanged shows or ends the link under the mouse as Command goes down
// or up (B563).
func flagsChanged(self objc.ID, _ objc.SEL, ev objc.ID) {
	if w := windows[self]; w != nil {
		for _, p := range w.panes {
			p.relink(objc.Send[uint](ev, sel("modifierFlags")))
		}
	}
}

// wordAt returns the columns of the word the pane shows at cell, from its
// first to past its last, the same twice for none.
func (p *pane) wordAt(cell [2]int) (int, int) {
	x, y := cell[0], cell[1]
	if y < 0 || y >= len(p.cells) || x < 0 || x >= len(p.cells[y]) {
		return 0, 0
	}
	row := p.cells[y]
	word := func(i int) bool {
		r, _ := utf8.DecodeRuneInString(row[i].text)
		return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
	}
	if !word(x) {
		return 0, 0
	}
	from, to := x, x+1
	for from > 0 && word(from-1) {
		from--
	}
	for to < len(row) && word(to) {
		to++
	}
	return from, to
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
