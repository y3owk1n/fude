package gui

import (
	"fmt"
	"maps"
	"slices"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"
)

// unitsOf counts a character as NSString does, in UTF-16 units.
func unitsOf(r rune) int { return max(utf16.RuneLen(r), 1) }

// readingAccess are the NSAccessibility methods of a view that shows text:
// a pane's, or a box's holding a float or what the palette has typed. A
// float and a pane are text areas, the palette a text field (B581).
var readingAccess = []objc.MethodDef{
	{Cmd: sel("accessibilityValue"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if _, s, ok := readerOf(self); ok {
			return nsString(s.text)
		}
		return boxValue(self)
	}},
	{Cmd: sel("accessibilityNumberOfCharacters"), Fn: func(self objc.ID, _ objc.SEL) int {
		if _, s, ok := readerOf(self); ok {
			return s.total
		}
		return 0
	}},
	{Cmd: sel("accessibilitySelectedTextRange"), Fn: func(self objc.ID, _ objc.SEL) nsRange {
		r, s, ok := readerOf(self)
		if !ok {
			return nsRange{}
		}
		from, to := s.unit(r.chosen[0]), s.unit(r.chosen[1])
		return nsRange{uint(from), uint(to - from)}
	}},
	{Cmd: sel("accessibilitySelectedText"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		r, s, ok := readerOf(self)
		if !ok {
			return 0
		}
		return nsString(s.slice(s.unit(r.chosen[0]), s.unit(r.chosen[1])))
	}},
	{Cmd: sel("accessibilityVisibleCharacterRange"), Fn: func(self objc.ID, _ objc.SEL) nsRange {
		r, s, ok := readerOf(self)
		if !ok {
			return nsRange{}
		}
		from, to := s.unit(r.view[0]), s.unit(r.view[1])
		return nsRange{uint(from), uint(to - from)}
	}},
	{Cmd: sel("accessibilityInsertionPointLineNumber"), Fn: func(self objc.ID, _ objc.SEL) int {
		if r, s, ok := readerOf(self); ok {
			return s.line(s.unit(r.cursor))
		}
		return 0
	}},
	{Cmd: sel("accessibilityLineForIndex:"), Fn: func(self objc.ID, _ objc.SEL, i int) int {
		if _, s, ok := readerOf(self); ok {
			return s.line(i)
		}
		return 0
	}},
	{Cmd: sel("accessibilityRangeForLine:"), Fn: func(self objc.ID, _ objc.SEL, n int) nsRange {
		if _, s, ok := readerOf(self); ok {
			from, to := s.lineUnits(n)
			return nsRange{uint(from), uint(to - from)}
		}
		return nsRange{}
	}},
	{Cmd: sel("accessibilityRangeForIndex:"), Fn: func(self objc.ID, _ objc.SEL, i int) nsRange {
		if _, s, ok := readerOf(self); ok {
			b := s.byteAt(i)
			n := 0
			if c, size := utf8.DecodeRuneInString(s.text[b:]); size > 0 {
				n = unitsOf(c)
			}
			return nsRange{uint(s.unit(b)), uint(n)}
		}
		return nsRange{}
	}},
	{Cmd: sel("accessibilityStringForRange:"), Fn: func(self objc.ID, _ objc.SEL, r nsRange) objc.ID {
		if _, s, ok := readerOf(self); ok {
			return nsString(s.slice(int(r.Loc), int(r.Loc+r.Len)))
		}
		return 0
	}},
	{Cmd: sel("accessibilityAttributedStringForRange:"), Fn: func(self objc.ID, _ objc.SEL, r nsRange) objc.ID {
		if _, s, ok := readerOf(self); ok {
			text := nsString(s.slice(int(r.Loc), int(r.Loc+r.Len)))
			return cls("NSAttributedString").Send(sel("alloc")).Send(sel("initWithString:"), text).Send(sel("autorelease"))
		}
		return 0
	}},
	{Cmd: sel("accessibilityFrameForRange:"), Fn: rangeFrame},
}

// readerOf is the reading of a view that shows text, in UTF-16 units.
func readerOf(self objc.ID) (reading, *spoken, bool) {
	if p := panes[self]; p != nil && self == p.text {
		return p.read, p.said.of(p.read), true
	}
	if b := boxes[self]; b != nil && b.boxed != nil && b.read != nil {
		return *b.read, b.said.of(*b.read), true
	}
	if b := fields[self]; b != nil && b.boxed != nil && b.list != nil && b.list.typing {
		r, _ := b.reading()
		return r, b.said.of(r), true
	}
	return reading{}, nil, false
}

// rangeFrame is where a range of a view's text is on the screen: the
// cursor's cell for a range at the cursor, else the whole view, as cells
// do not say where other text is.
func rangeFrame(self objc.ID, _ objc.SEL, want nsRange) rect {
	r := msgRect(self, sel("bounds"))
	if rd, s, ok := readerOf(self); ok && int(want.Loc) == s.unit(rd.cursor) {
		if p := panes[self]; p != nil && p.cursor.shown {
			r = at(p.cursor.x, p.cursor.y, 1, p.origin())
		}
	}
	return screenRect(self, r)
}

// screenRect is r of view on the screen.
func screenRect(view objc.ID, r rect) rect {
	win := view.Send(sel("window"))
	if win == 0 {
		return rect{}
	}
	return msgMapRect(win, sel("convertRectToScreen:"), msgConvertRect(view, sel("convertRect:toView:"), r, 0))
}

// role returns an accessibilityRole method that answers name.
func role(name string) objc.MethodDef {
	return objc.MethodDef{Cmd: sel("accessibilityRole"), Fn: func(objc.ID, objc.SEL) objc.ID { return nsString(name) }}
}

// element marks a view as one assistive apps see.
var element = objc.MethodDef{Cmd: sel("isAccessibilityElement"), Fn: func(objc.ID, objc.SEL) bool { return true }}

// hasFocus says whether a view has the focus assistive apps see.
var hasFocus = objc.MethodDef{Cmd: sel("isAccessibilityFocused"), Fn: func(self objc.ID, _ objc.SEL) bool {
	if p := panes[self]; p != nil {
		return p.w.focus() == self
	}
	if b := boxes[self]; b != nil {
		return b.w.focus() == self
	}
	if b := fields[self]; b != nil {
		return b.w.focus() == self
	}
	return false
}}

// textAccess are a pane's text's, a text area named by its buffer.
var textAccess = append([]objc.MethodDef{
	element, role("AXTextArea"), hasFocus,
	{Cmd: sel("accessibilityLabel"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if p := panes[self]; p != nil {
			return nsString(p.read.label())
		}
		return 0
	}},
}, readingAccess...)

// statusAccess are a status bar's, its parts as text.
var statusAccess = []objc.MethodDef{
	element, role("AXStaticText"),
	{Cmd: sel("accessibilityValue"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if p := panes[self]; p != nil {
			return nsString(statusText(p.left, p.right))
		}
		return 0
	}},
}

// boxAccess are a box's: a text area for a float, a group of a text field
// and a list for the palette and the finder, a list for a menu and text
// for the rest.
var boxAccess = append([]objc.MethodDef{
	element, hasFocus,
	{Cmd: sel("accessibilityRole"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		b := boxes[self]
		switch {
		case b == nil || b.boxed == nil:
			return nsString("AXGroup")
		case b.read != nil:
			return nsString("AXTextArea")
		case b.list != nil && b.list.typing:
			return nsString("AXGroup")
		case b.list != nil:
			return nsString("AXList")
		}
		return nsString("AXStaticText")
	}},
	{Cmd: sel("accessibilityLabel"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if b := boxes[self]; b != nil && b.boxed != nil {
			if r, ok := b.reading(); ok {
				return nsString(r.label())
			}
			return nsString(b.title)
		}
		return 0
	}},
	{Cmd: sel("accessibilityChildren"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		b := boxes[self]
		switch {
		case b == nil || b.boxed == nil || b.list == nil:
			return cls("NSArray").Send(sel("array"))
		case b.list.typing:
			return b.parts()
		}
		return b.rows()
	}},
}, readingAccess...)

// boxValue is what a box that holds no text says: a toast's message, or
// its cells' text.
func boxValue(self objc.ID) objc.ID {
	b := boxes[self]
	switch {
	case b == nil || b.boxed == nil || b.list != nil:
		return 0
	case b.text != "":
		return nsString(b.text)
	}
	return nsString(cellsText(b.cells))
}

// barAccess are the tab strip's, a tab group of its tabs.
var barAccess = []objc.MethodDef{
	element, role("AXTabGroup"),
	{Cmd: sel("accessibilityLabel"), Fn: func(objc.ID, objc.SEL) objc.ID { return nsString("tabs") }},
	{Cmd: sel("accessibilityChildren"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if w := windows[self]; w != nil {
			return w.tabElements()
		}
		return cls("NSArray").Send(sel("array"))
	}},
}

// rootAccess are the root view's, which hands assistive apps the element
// with the focus.
var rootAccess = []objc.MethodDef{
	{Cmd: sel("accessibilityFocusedUIElement"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if w := windows[self]; w != nil && w.focus() != 0 {
			return w.focus()
		}
		return self
	}},
}

// focus is the view with the focus: the palette or the finder while one
// shows, else a float with the cursor, else the current window's text.
func (w *window) focus() objc.ID {
	if b := w.panel; b.boxed != nil {
		if b.list != nil && b.list.typing {
			return b.field
		}
		return b.view
	}
	for _, b := range w.floats {
		if b.boxed != nil && b.read != nil && b.cursor.shown {
			return b.view
		}
	}
	for _, p := range w.panes {
		if p.current {
			return p.text
		}
	}
	return 0
}

// actions are the elements a view makes for what it draws, which
// assistive apps press: tabs, their close buttons and a list's rows.
var actions = map[objc.ID]action{}

// action is where an element is in its view, and what pressing it does.
type action struct {
	view  objc.ID
	frame rect
	press func()
}

// actionAccess are the methods of an element of actions.
var actionAccess = []objc.MethodDef{
	{Cmd: sel("accessibilityFrame"), Fn: func(self objc.ID, _ objc.SEL) rect {
		if a, ok := actions[self]; ok {
			return screenRect(a.view, a.frame)
		}
		return rect{}
	}},
	{Cmd: sel("accessibilityPerformPress"), Fn: func(self objc.ID, _ objc.SEL) bool {
		if a, ok := actions[self]; ok && a.press != nil {
			a.press()
			return true
		}
		return false
	}},
}

// elements are the elements a view keeps for what it draws, made again
// when what it draws changes.
type elements struct {
	key string
	ids []objc.ID
}

// of returns the elements for key, which build makes when key changed.
func (e *elements) of(key string, build func() []objc.ID) objc.ID {
	if key != e.key || e.ids == nil {
		e.drop()
		e.key, e.ids = key, build()
	}
	return nsArray(e.ids)
}

// nsArray returns an autoreleased NSMutableArray of ids.
func nsArray(ids []objc.ID) objc.ID {
	list := cls("NSMutableArray").Send(sel("array"))
	for _, id := range ids {
		list.Send(sel("addObject:"), id)
	}
	return list
}

// drop forgets e's elements and the elements in them.
func (e *elements) drop() {
	for _, id := range e.ids {
		kids := id.Send(sel("accessibilityChildren"))
		for i := range objc.Send[int](kids, sel("count")) {
			delete(actions, kids.Send(sel("objectAtIndex:"), i))
		}
		delete(actions, id)
		id.Send(sel("release"))
	}
	e.ids = nil
}

// newAction returns an element of role named label, at frame in view,
// whose parent is parent and which press runs when pressed.
func newAction(role, label string, parent, view objc.ID, frame rect, press func()) objc.ID {
	id := objc.ID(actionClass).Send(sel("alloc")).Send(sel("init"))
	id.Send(sel("setAccessibilityRole:"), nsString(role))
	id.Send(sel("setAccessibilityLabel:"), nsString(label))
	id.Send(sel("setAccessibilityParent:"), parent)
	actions[id] = action{view, frame, press}
	return id
}

// tabElements are the strip's tabs, each a radio button that shows its
// tab, with a close button that closes it (B555).
func (w *window) tabElements() objc.ID {
	return w.tabIDs.of(fmt.Sprint(w.hits, w.tabs), func() []objc.ID {
		h := msgRect(w.bar, sel("bounds")).Size.H
		var ids []objc.ID
		for _, t := range w.hits {
			name, current := w.tabName(t)
			tab := newAction("AXRadioButton", name, w.bar, w.bar, rect{point{t.from, 0}, size{t.to - t.from, h}}, func() { w.typeKeys(t.show()) })
			value := 0
			if current {
				value = 1
			}
			tab.Send(sel("setAccessibilityValue:"), cls("NSNumber").Send(sel("numberWithInt:"), value))
			// The tab's children retain its close button.
			shut := newAction("AXButton", "close", tab, w.bar, rect{point{shutFrom(t.to), 0}, size{slotW, h}}, func() { w.typeKeys(t.shut()) })
			tab.Send(sel("setAccessibilityChildren:"), cls("NSArray").Send(sel("arrayWithObject:"), shut))
			shut.Send(sel("release"))
			ids = append(ids, tab)
		}
		return ids
	})
}

// rows are a menu's rows, the list a menu at the cursor is.
func (b *box) rows() objc.ID {
	l := b.list
	return b.rowIDs.of(fmt.Sprint("rows", l.items, l.selected, l.scroll), func() []objc.ID { return b.rowsIn(b.view) })
}

// parts are the palette's and the finder's: the text field, the list of
// items and the finder's preview.
func (b *box) parts() objc.ID {
	l, preview := b.list, cellsText(b.cells)
	parts := b.rowIDs.of(fmt.Sprint("parts", l.items, l.selected, l.scroll, preview), func() []objc.ID {
		bounds := msgRect(b.view, sel("bounds")).Size
		list := newAction("AXList", "", b.view, b.view, rect{point{0, l.top()}, size{bounds.W, float64(l.shown()) * l.rowH}}, nil)
		// The list's children retain its rows.
		rows := b.rowsIn(list)
		list.Send(sel("setAccessibilityChildren:"), nsArray(rows))
		for _, row := range rows {
			row.Send(sel("release"))
		}
		ids := []objc.ID{list}
		if preview != "" {
			p := newAction("AXStaticText", "preview", b.view, b.view, rect{Size: bounds}, nil)
			p.Send(sel("setAccessibilityValue:"), nsString(preview))
			ids = append(ids, p)
		}
		return ids
	})
	parts.Send(sel("insertObject:atIndex:"), b.field, 0)
	return parts
}

// rowsIn returns a list's rows in parent, each its item's text, which picks
// it when pressed (B556).
func (b *box) rowsIn(parent objc.ID) []objc.ID {
	l := b.list
	wide := msgRect(b.view, sel("bounds")).Size.W - 16
	var ids []objc.ID
	for i, it := range l.items {
		y := l.top() + float64(i)*l.rowH - l.scroll
		row := newAction("AXStaticText", itemText(it), parent, b.view, rect{point{8, y}, size{wide, l.rowH}}, func() { b.w.mice <- mouse{kind: 'p', y: i} })
		row.Send(sel("setAccessibilityValue:"), nsString(itemText(it)))
		row.Send(sel("setAccessibilitySelected:"), i == l.selected)
		ids = append(ids, row)
	}
	return ids
}

// fields are the palette's and the finder's text fields, by element, each
// of its box.
var fields = map[objc.ID]*box{}

// fieldAccess are the methods of a text field of fields, the row typed
// into over the box's list.
var fieldAccess = append([]objc.MethodDef{
	role("AXTextField"), hasFocus,
	{Cmd: sel("accessibilityLabel"), Fn: func(self objc.ID, _ objc.SEL) objc.ID {
		if r, _, ok := readerOf(self); ok {
			return nsString(r.label())
		}
		return 0
	}},
	{Cmd: sel("accessibilityFrame"), Fn: func(self objc.ID, _ objc.SEL) rect {
		if b := fields[self]; b != nil {
			return screenRect(b.view, rect{Size: size{msgRect(b.view, sel("bounds")).Size.W, inputH}})
		}
		return rect{}
	}},
}, readingAccess...)

// post tells assistive apps of a change to element.
func post(element objc.ID, name string) { postAccess(element, nsString(name)) }

// say has assistive apps speak text.
func say(text string) {
	info := cls("NSMutableDictionary").Send(sel("dictionary"))
	info.Send(sel("setObject:forKey:"), nsString(text), nsString("AXAnnouncementKey"))
	// NSAccessibilityPriorityHigh.
	info.Send(sel("setObject:forKey:"), cls("NSNumber").Send(sel("numberWithInt:"), 90), nsString("AXPriorityKey"))
	postAccessInfo(cls("NSApplication").Send(sel("sharedApplication")), nsString("AXAnnouncementRequested"), info)
}

// tell tells assistive apps what changed in the frame w shows: the focus,
// a text, a cursor, a list's selected item and a new message (B581).
func (w *window) tell() {
	if f := w.focus(); f != w.focused {
		w.focused = f
		if f != 0 {
			post(f, "AXFocusedUIElementChanged")
		}
	}
	text := func(view objc.ID, r reading, t *told) {
		if k := r.key(); k != t.key {
			t.key = k
			post(view, "AXValueChanged")
		}
		if r.chosen != t.chosen {
			t.chosen = r.chosen
			post(view, "AXSelectedTextChanged")
		}
	}
	for _, p := range w.panes {
		text(p.text, p.read, &p.told)
	}
	for _, b := range append([]*box{w.panel, w.toast}, slices.Collect(maps.Values(w.floats))...) {
		if b.boxed == nil {
			b.told = told{}
			continue
		}
		if b.read != nil {
			text(b.view, *b.read, &b.told)
		} else if r, ok := b.reading(); ok {
			text(b.field, r, &b.told)
		}
		// A list's selected item, or a message, is spoken once it changes.
		spoken := b.text
		if l := b.list; l != nil && l.selected >= 0 && l.selected < len(l.items) {
			spoken = itemText(l.items[l.selected])
		}
		if spoken != b.told.spoke {
			b.told.spoke = spoken
			if spoken != "" {
				say(spoken)
			}
		}
	}
}
