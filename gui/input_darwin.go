package gui

import (
	"math"
	"slices"
	"time"
	"unicode/utf16"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/vim"
)

// nsRange is an NSRange, in UTF-16 units.
type nsRange struct{ Loc, Len uint }

// notFound is NSNotFound, a range's location for none.
const notFound = math.MaxInt64

// textInput are the NSTextInputClient methods an input method calls on the
// root view. The text it composes shows at the cursor, and the text it
// inserts goes to the session as keys (B551).
var textInput = []objc.MethodDef{
	{Cmd: sel("insertText:replacementRange:"), Fn: insertText},
	{Cmd: sel("setMarkedText:selectedRange:replacementRange:"), Fn: setMarkedText},
	{Cmd: sel("unmarkText"), Fn: func(self objc.ID, _ objc.SEL) { mark(self, "") }},
	{Cmd: sel("hasMarkedText"), Fn: func(self objc.ID, _ objc.SEL) bool { w := windows[self]; return w != nil && w.marked != "" }},
	{Cmd: sel("markedRange"), Fn: markedRange},
	{Cmd: sel("selectedRange"), Fn: func(objc.ID, objc.SEL) nsRange { return nsRange{Loc: notFound} }},
	{Cmd: sel("validAttributesForMarkedText"), Fn: func(objc.ID, objc.SEL) objc.ID { return cls("NSArray").Send(sel("array")) }},
	{Cmd: sel("attributedSubstringForProposedRange:actualRange:"), Fn: func(objc.ID, objc.SEL, nsRange, uintptr) objc.ID { return 0 }},
	{Cmd: sel("characterIndexForPoint:"), Fn: func(objc.ID, objc.SEL, point) uint { return notFound }},
	{Cmd: sel("firstRectForCharacterRange:actualRange:"), Fn: firstRect},
	{Cmd: sel("doCommandBySelector:"), Fn: doCommand},
}

// keyDown types a key down. A key with Control, Option or Command, and one
// that types no character, goes straight to the session as keys while no
// text is being composed. Any other goes through the input method first.
func keyDown(self objc.ID, _ objc.SEL, ev objc.ID) {
	w := windows[self]
	if w == nil {
		return
	}
	w.typed, w.caretOn = time.Now(), true
	// A key settles text a trackpad left between lines.
	for _, p := range w.panes {
		if p.offset != 0 || p.want != p.top {
			p.offset, p.want = 0, p.top
			p.text.Send(sel("setNeedsDisplay:"), 1)
		}
	}
	code := objc.Send[uint16](ev, sel("keyCode"))
	mods := objc.Send[uint](ev, sel("modifierFlags"))
	if _, ok := named[code]; w.marked == "" && (ok || mods&(modControl|modOption|modCommand) != 0) {
		w.typeEvent(ev)
		return
	}
	w.event = ev
	self.Send(sel("interpretKeyEvents:"), cls("NSArray").Send(sel("arrayWithObject:"), ev))
	w.event = 0
}

// typeEvent types ev as the keys it is in Vim's notation.
func (w *window) typeEvent(ev objc.ID) {
	code := objc.Send[uint16](ev, sel("keyCode"))
	mods := objc.Send[uint](ev, sel("modifierFlags"))
	for _, k := range keys(code, mods, goString(ev.Send(sel("characters"))), goString(ev.Send(sel("charactersIgnoringModifiers")))) {
		w.keys <- k
	}
}

func insertText(self objc.ID, _ objc.SEL, text objc.ID, _ nsRange) {
	w := windows[self]
	if w == nil {
		return
	}
	mark(self, "")
	for _, r := range plain(text) {
		w.keys <- vim.Key(string(r))
	}
}

func setMarkedText(self objc.ID, _ objc.SEL, text objc.ID, _, _ nsRange) {
	mark(self, plain(text))
}

// mark shows text being composed at the cursor, or hides it for "".
func mark(view objc.ID, text string) {
	w := windows[view]
	if w == nil || text == w.marked {
		return
	}
	w.marked = text
	if text == "" {
		w.preedit.Send(sel("setHidden:"), 1)
		return
	}
	at, ok := w.caret()
	if !ok {
		return
	}
	a := textAttrs(font, w.look.Base.Fg)
	sz := msgSizeOf(nsString(text), sel("sizeWithAttributes:"), a)
	msgSetRect(w.preedit, sel("setFrame:"), rect{at, size{math.Ceil(sz.W) + 2, cellH}})
	w.view.Send(sel("addSubview:positioned:relativeTo:"), w.preedit, 1, 0)
	w.preedit.Send(sel("setHidden:"), 0)
	w.preedit.Send(sel("setNeedsDisplay:"), 1)
}

// drawMarked draws the text being composed, underlined, on the ground.
func drawMarked(self objc.ID, _ objc.SEL, _ rect) {
	w := windows[self]
	if w == nil {
		return
	}
	b := msgRect(self, sel("bounds"))
	nsColor(w.look.Base.Bg).Send(sel("setFill"))
	rectFill(b)
	msgDrawAt(nsString(w.marked), sel("drawAtPoint:withAttributes:"), point{}, textAttrs(font, w.look.Base.Fg))
	nsColor(w.look.Base.Fg).Send(sel("setFill"))
	rectFill(rect{point{0, b.Size.H - 2}, size{b.Size.W, 1}})
}

func markedRange(self objc.ID, _ objc.SEL) nsRange {
	w := windows[self]
	if w == nil || w.marked == "" {
		return nsRange{Loc: notFound}
	}
	return nsRange{0, uint(len(utf16.Encode([]rune(w.marked))))}
}

// firstRect is where the cursor is on the screen, for the input method to
// put its candidates by.
func firstRect(self objc.ID, _ objc.SEL, _ nsRange, _ uintptr) rect {
	w := windows[self]
	if w == nil {
		return rect{}
	}
	at, _ := w.caret()
	r := msgConvertRect(self, sel("convertRect:toView:"), rect{at, size{cellW, cellH}}, 0)
	return msgMapRect(w.win, sel("convertRectToScreen:"), r)
}

// doCommand types the key the input method did not take, as keys.
func doCommand(self objc.ID, _ objc.SEL, _ objc.SEL) {
	if w := windows[self]; w != nil && w.event != 0 {
		w.typeEvent(w.event)
	}
}

// plain returns the text of an NSString or an NSAttributedString.
func plain(text objc.ID) string {
	if text.Send(sel("isKindOfClass:"), cls("NSAttributedString")) != 0 {
		text = text.Send(sel("string"))
	}
	return goString(text)
}

// caret is where the cursor shows in the root view: in the palette or the
// finder while one shows, else in a float or a pane.
func (w *window) caret() (point, bool) {
	in := func(view objc.ID, c cursorAt, o point) (point, bool) {
		r := msgConvertRect(view, sel("convertRect:toView:"), at(c.x, c.y, 1, o), w.view)
		return r.Origin, true
	}
	if b := w.panel; b.boxed != nil && b.list != nil {
		r := msgConvertRect(b.view, sel("convertRect:toView:"), rect{b.list.caret, size{1, 1}}, w.view)
		return r.Origin, true
	}
	for _, b := range w.floats {
		if b.boxed != nil && b.cursor.shown {
			return in(b.view, b.cursor, point{inset, inset + b.head()})
		}
	}
	for _, p := range w.panes {
		if p.cursor.shown {
			return in(p.text, p.cursor, point{pad, pad})
		}
	}
	return point{}, false
}

// menu makes the menu bar: the application's menu with Hide and Quit, File
// with Close Window, Edit with undo and the clipboard as Vim's keys, and
// Window (B551).
func menu(app objc.ID) {
	bar := cls("NSMenu").Send(sel("alloc")).Send(sel("init"))
	add := func(title string, items ...[3]string) objc.ID {
		m := cls("NSMenu").Send(sel("alloc")).Send(sel("initWithTitle:"), nsString(title))
		for _, it := range items {
			if it[0] == "" {
				m.Send(sel("addItem:"), cls("NSMenuItem").Send(sel("separatorItem")))
				continue
			}
			item := m.Send(sel("addItemWithTitle:action:keyEquivalent:"), nsString(it[0]), sel(it[1]), nsString(it[2]))
			// The application's own actions go to it, not to a window.
			if it[1] == "quit:" || slices.ContainsFunc(sizing, func(m objc.MethodDef) bool { return m.Cmd == sel(it[1]) }) {
				item.Send(sel("setTarget:"), mainRun)
			}
		}
		top := bar.Send(sel("addItemWithTitle:action:keyEquivalent:"), nsString(title), 0, nsString(""))
		top.Send(sel("setSubmenu:"), m)
		m.Send(sel("release"))
		return m
	}
	add("fude", [3]string{"Hide fude", "hide:", "h"}, [3]string{"Hide Others", "hideOtherApplications:", "h"}, [3]string{}, [3]string{"Quit fude", "quit:", "q"})
	add("File", [3]string{"Close Window", "performClose:", "w"})
	add("Edit",
		[3]string{"Undo", "undo:", "z"}, [3]string{"Redo", "redo:", "Z"}, [3]string{},
		[3]string{"Cut", "cut:", "x"}, [3]string{"Copy", "copy:", "c"}, [3]string{"Paste", "paste:", "v"},
		[3]string{"Select All", "selectAll:", "a"},
	)
	add("View", [3]string{"Bigger", "bigger:", "="}, [3]string{"Smaller", "smaller:", "-"}, [3]string{"Actual Size", "actualSize:", "0"})
	window := add("Window", [3]string{"Minimize", "performMiniaturize:", "m"}, [3]string{"Zoom", "performZoom:", ""})
	// Hide Others is Option-Command-H.
	items := bar.Send(sel("itemAtIndex:"), 0).Send(sel("submenu"))
	items.Send(sel("itemAtIndex:"), 1).Send(sel("setKeyEquivalentModifierMask:"), modOption|modCommand)
	app.Send(sel("setMainMenu:"), bar)
	app.Send(sel("setWindowsMenu:"), window)
	bar.Send(sel("release"))
}

// quit closes each window as its close button does, for Command-Q.
func quit(objc.ID, objc.SEL, objc.ID) {
	for id, w := range windows {
		if id == w.win {
			windowShouldClose(0, 0, w.win)
		}
	}
}

// editing are the Edit menu's actions as Vim's keys for the mode: undo and
// redo as u and <C-r>, cut and copy of a selection through the + register,
// paste of the system's clipboard as a paste, and select all as ggVG.
var editing = []objc.MethodDef{
	editAction("Undo"), editAction("Redo"), editAction("Cut"), editAction("Copy"), editAction("Select All"),
	{Cmd: sel("paste:"), Fn: paste},
}

// editAction is the root view's method for the system's editing command of
// the title, which types what edits says.
func editAction(title string) objc.MethodDef {
	return objc.MethodDef{Cmd: sel(selectorOf(title)), Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
		if w := windows[self]; w != nil {
			w.edit(title)
		}
	}}
}

// act types keys on the window of view as w.act does.
func act(view objc.ID, normal, insert, visual string) {
	if w := windows[view]; w != nil {
		w.act(normal, insert, visual)
	}
}

// paste pastes the text on the system's clipboard.
func paste(self objc.ID, _ objc.SEL, _ objc.ID) {
	w := windows[self]
	if w == nil {
		return
	}
	text := goString(cls("NSPasteboard").Send(sel("generalPasteboard")).Send(sel("stringForType:"), nsString(pasteboardText)))
	if text != "" {
		w.pastes <- text
	}
}

// pasteboardText is NSPasteboardTypeString.
const pasteboardText = "public.utf8-plain-text"

// copyOut puts text on the system's clipboard, as an attached session's
// copy asks (B241).
func copyOut(text string) {
	pb := cls("NSPasteboard").Send(sel("generalPasteboard"))
	pb.Send(sel("clearContents"))
	pb.Send(sel("setString:forType:"), nsString(text), nsString(pasteboardText))
}
