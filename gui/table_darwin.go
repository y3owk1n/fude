package gui

import (
	"fmt"
	"strconv"

	"github.com/ebitengine/purego/objc"
)

// The system's list a box shows its menu's items in: an NSTableView in
// an NSScrollView, which fude feeds and which never takes the keyboard's
// focus, so keys still go to the session (B560, D68).
var (
	tableClass, rowClass objc.Class
	previewClass         objc.Class           // the finder's whole preview
	tableSource          objc.ID              // the tables' data source, delegate and click's target
	tables               = map[objc.ID]*box{} // by table
)

// tableMethods are the tables' data source's and delegate's methods.
var tableMethods = []objc.MethodDef{
	{Cmd: sel("numberOfRowsInTableView:"), Fn: func(_ objc.ID, _ objc.SEL, t objc.ID) int {
		if b := tables[t]; b != nil && b.boxed != nil && b.list != nil {
			return len(b.list.items)
		}
		return 0
	}},
	{Cmd: sel("tableView:viewForTableColumn:row:"), Fn: func(_ objc.ID, _ objc.SEL, t, _ objc.ID, row int) objc.ID {
		if b := tables[t]; b != nil && b.boxed != nil && b.list != nil && row < len(b.list.items) {
			return b.rowView(row, msgRect(t, sel("bounds")).Size.W)
		}
		return 0
	}},
	{Cmd: sel("tableView:rowViewForRow:"), Fn: func(objc.ID, objc.SEL, objc.ID, int) objc.ID {
		return objc.ID(rowClass).Send(sel("alloc")).Send(sel("init")).Send(sel("autorelease"))
	}},
	{Cmd: sel("tableView:isGroupRow:"), Fn: func(_ objc.ID, _ objc.SEL, t objc.ID, row int) bool {
		b := tables[t]
		return b != nil && b.boxed != nil && b.list != nil && row < len(b.list.items) && b.list.items[row].Header
	}},
	{Cmd: sel("tableView:shouldSelectRow:"), Fn: func(_ objc.ID, _ objc.SEL, t objc.ID, row int) bool {
		b := tables[t]
		return b != nil && b.boxed != nil && b.list != nil && row < len(b.list.items) && !b.list.items[row].Header && !b.list.items[row].Disabled
	}},
}

// tableHover lights the row under the mouse as ev moves it, or none for
// 0, as the mouse leaves the table.
func tableHover(self, ev objc.ID) {
	b := tables[self]
	if b == nil || b.boxed == nil || b.list == nil {
		return
	}
	row := -1
	if ev != 0 {
		row = msgRowAt(self, sel("rowAtPoint:"), msgConvertPoint(self, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0))
	}
	if row >= 0 && row < len(b.list.items) && (b.list.items[row].Header || b.list.items[row].Disabled) {
		row = -1
	}
	if row == b.hovered {
		return
	}
	for _, r := range []int{b.hovered, row} {
		if r >= 0 {
			if v := self.Send(sel("rowViewAtRow:makeIfNecessary:"), r, false); v != 0 {
				v.Send(sel("setNeedsDisplay:"), true)
			}
		}
	}
	b.hovered = row
}

// drawRowBack draws a row's ground: a light one while the mouse is over
// it, under the selection, which the row draws over it.
func drawRowBack(self objc.ID, _ objc.SEL, _ rect) {
	t := self.Send(sel("superview"))
	if b := tables[t]; b != nil && b.hovered >= 0 && objc.Send[int](t, sel("rowForView:"), self) == b.hovered {
		fillRounded("quaternaryLabelColor", rowPill(self), 6)
	}
}

// drawRowSelection draws a selected row's ground, rounded within the
// row's edges as the system's lists are.
func drawRowSelection(self objc.ID, _ objc.SEL, _ rect) {
	fillRounded("selectedContentBackgroundColor", rowPill(self), 6)
}

// rowPill is where a row's selection and hover draw, inside its edges.
func rowPill(row objc.ID) rect {
	b := msgRect(row, sel("bounds"))
	return rect{point{b.Origin.X + 4, b.Origin.Y}, size{b.Size.W - 8, b.Size.H}}
}

// tableHit is the table for a point in it, so a click on a row's views
// reaches the table and picks the row.
func tableHit(self objc.ID, _ objc.SEL, at point) objc.ID {
	p := msgConvertPoint(self, sel("convertPoint:fromView:"), at, self.Send(sel("superview")))
	if b := msgRect(self, sel("bounds")); p.X >= b.Origin.X && p.Y >= b.Origin.Y && p.X < b.Origin.X+b.Size.W && p.Y < b.Origin.Y+b.Size.H {
		return self
	}
	return 0
}

// tableClicked picks the item of the row a click hits, as a click on a
// menu's item does (B556). The session selects it, and the next frame
// shows the selection.
func tableClicked(self objc.ID, _ objc.SEL, ev objc.ID) {
	b := tables[self]
	if b == nil || b.boxed == nil || b.list == nil {
		return
	}
	at := msgConvertPoint(self, sel("convertPoint:fromView:"), msgPoint(ev, sel("locationInWindow")), 0)
	row := msgRowAt(self, sel("rowAtPoint:"), at)
	if row >= 0 && row < len(b.list.items) && !b.list.items[row].Header && !b.list.items[row].Disabled {
		b.w.mice <- mouse{kind: 'p', y: row}
	}
}

// newTable makes b's table, hidden until b shows a menu.
func (b *box) newTable() {
	b.scrolled = msgInitView(cls("NSScrollView").Send(sel("alloc")), sel("initWithFrame:"), rect{})
	b.scrolled.Send(sel("setDrawsBackground:"), false)
	// The window's content reaches under the title bar, which would give
	// the list a title bar's room at its top.
	b.scrolled.Send(sel("setAutomaticallyAdjustsContentInsets:"), false)
	b.scrolled.Send(sel("setHasVerticalScroller:"), true)
	b.scrolled.Send(sel("setAutohidesScrollers:"), true)
	// NSScrollerStyleOverlay.
	b.scrolled.Send(sel("setScrollerStyle:"), 1)
	b.table = msgInitView(objc.ID(tableClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	column := cls("NSTableColumn").Send(sel("alloc")).Send(sel("initWithIdentifier:"), nsString("item"))
	b.table.Send(sel("addTableColumn:"), column)
	column.Send(sel("release"))
	b.table.Send(sel("setHeaderView:"), 0)
	b.table.Send(sel("setBackgroundColor:"), cls("NSColor").Send(sel("clearColor")))
	// NSTableViewStylePlain: the rows draw their rounded selection, as the
	// inset style's padding would not fit the box's rows.
	b.table.Send(sel("setStyle:"), 4)
	// NSTableViewColumnAutoresizingUniformColumnAutoresizingStyle.
	b.table.Send(sel("setColumnAutoresizingStyle:"), 1)
	msgSetSize(b.table, sel("setIntercellSpacing:"), size{})
	b.table.Send(sel("setDataSource:"), tableSource)
	b.table.Send(sel("setDelegate:"), tableSource)
	// The table hears the mouse move over it, for its rows to light.
	track := msgInitTracking(cls("NSTrackingArea").Send(sel("alloc")), sel("initWithRect:options:owner:userInfo:"), rect{}, 0x283, b.table, 0)
	b.table.Send(sel("addTrackingArea:"), track)
	track.Send(sel("release"))
	b.scrolled.Send(sel("setDocumentView:"), b.table)
	b.scrolled.Send(sel("setHidden:"), true)
	b.view.Send(sel("addSubview:"), b.scrolled)
	tables[b.table] = b
}

// showTable shows b's menu in its table, under the row typed into, and
// keeps its selected item in view. Without a menu it hides the table.
func (b *box) showTable() {
	if b.boxed == nil || b.list == nil || len(b.list.items) == 0 {
		b.scrolled.Send(sel("setHidden:"), true)
		b.listed, b.hovered = "", -1
		return
	}
	l := b.list
	bounds := msgRect(b.view, sel("bounds")).Size
	width := bounds.W
	if len(b.cells) > 0 {
		width = 8 + l.width + 4
	}
	msgSetRect(b.scrolled, sel("setFrame:"), rect{point{0, l.top()}, size{width, float64(l.shown()) * l.rowH}})
	msgSetFloat(b.table, sel("setRowHeight:"), l.rowH)
	b.scrolled.Send(sel("setHidden:"), false)
	if listed := fmt.Sprint(l.items, l.selected); listed != b.listed {
		b.listed = listed
		b.table.Send(sel("reloadData"))
	}
	if l.selected < 0 || l.selected >= len(l.items) {
		b.table.Send(sel("deselectAll:"), 0)
		return
	}
	at := cls("NSIndexSet").Send(sel("indexSetWithIndex:"), l.selected)
	b.table.Send(sel("selectRowIndexes:byExtendingSelection:"), at, false)
	b.table.Send(sel("scrollRowToVisible:"), l.selected)
}

// rowView is the view of b's item at row, width wide: its icon, a grep
// match's line, its label with the typed text's matches bold, and its
// detail at the right, in the system's selected text while selected.
func (b *box) rowView(row int, width float64) objc.ID {
	l, it := b.list, b.list.items[row]
	h := l.rowH
	v := msgInitView(cls("NSView").Send(sel("alloc")), sel("initWithFrame:"), rect{Size: size{width, h}}).Send(sel("autorelease"))
	label, detail := system("labelColor"), system("secondaryLabelColor")
	if row == l.selected {
		label, detail = system("alternateSelectedControlTextColor"), system("alternateSelectedControlTextColor")
	}
	font := listFont
	switch {
	case it.Header:
		font, label = listBold, system("secondaryLabelColor")
	case it.Disabled:
		label = system("tertiaryLabelColor")
	}
	// The table sizes the view to its cell, which its inset style makes
	// narrower than the table: the label then narrows, and the detail
	// keeps to the right edge.
	add := func(view objc.ID, r rect, resize uint) {
		msgSetRect(view, sel("setFrame:"), r)
		view.Send(sel("setAutoresizingMask:"), resize)
		v.Send(sel("addSubview:"), view)
	}
	text := func(s objc.ID) objc.ID {
		t := objc.ID(labelClass).Send(sel("labelWithAttributedString:"), s)
		// NSLineBreakByTruncatingTail.
		t.Send(sel("setLineBreakMode:"), 4)
		return t
	}
	mid := func(sz size) float64 { return (h - sz.H) / 2 }
	at := 12.0
	if icon := iconOf(it, label); icon != 0 {
		img := cls("NSImageView").Send(sel("imageViewWithImage:"), icon)
		add(img, rect{point{at, (h - 16) / 2}, size{16, 16}}, 0)
		at += 24
	}
	if it.Line > 0 {
		n := text(attributed(strconv.Itoa(it.Line), uiAttrs(listFont, detail)))
		n.Send(sel("sizeToFit"))
		add(n, rect{point{at, mid(msgRect(n, sel("frame")).Size)}, size{32, msgRect(n, sel("frame")).Size.H}}, 0)
		at += 36
	}
	end := width - 12
	if it.Detail != "" {
		d := text(attributed(it.Detail, uiAttrs(listFont, detail)))
		d.Send(sel("sizeToFit"))
		sz := msgRect(d, sel("frame")).Size
		dx := max(end-sz.W, at+80)
		// NSViewMinXMargin.
		add(d, rect{point{dx, mid(sz)}, size{end - dx, sz.H}}, 1)
		end = dx - 12
	}
	s := marked(it, font, label)
	t := text(s)
	s.Send(sel("release"))
	t.Send(sel("sizeToFit"))
	sz := msgRect(t, sel("frame")).Size
	// NSViewWidthSizable.
	add(t, rect{point{at, mid(sz)}, size{max(end-at, 0), sz.H}}, 2)
	return v
}

// attributed returns an autoreleased string of text with attrs.
func attributed(text string, attrs objc.ID) objc.ID {
	return cls("NSAttributedString").Send(sel("alloc")).Send(sel("initWithString:attributes:"), nsString(text), attrs).Send(sel("autorelease"))
}

// previews are the finder's preview views, by document view.
var previews = map[objc.ID]*box{}

// newPreview makes the scroll view b's finder shows its whole preview in,
// hidden until it has more rows than the box shows (B130).
func (b *box) newPreview() {
	b.previewScroll = msgInitView(cls("NSScrollView").Send(sel("alloc")), sel("initWithFrame:"), rect{})
	b.previewScroll.Send(sel("setDrawsBackground:"), false)
	b.previewScroll.Send(sel("setHasVerticalScroller:"), true)
	b.previewScroll.Send(sel("setAutohidesScrollers:"), true)
	b.previewScroll.Send(sel("setAutomaticallyAdjustsContentInsets:"), false)
	// NSScrollerStyleOverlay.
	b.previewScroll.Send(sel("setScrollerStyle:"), 1)
	doc := msgInitView(objc.ID(previewClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	b.previewScroll.Send(sel("setDocumentView:"), doc)
	doc.Send(sel("release"))
	b.previewScroll.Send(sel("setHidden:"), true)
	b.view.Send(sel("addSubview:"), b.previewScroll)
	previews[doc] = b
}

// showPreview shows the finder's whole preview in its scroll view, at its
// top whenever another file shows, or hides the scroll view.
func (b *box) showPreview() {
	if b.boxed == nil || b.whole == nil || len(b.cells) == 0 {
		b.previewScroll.Send(sel("setHidden:"), true)
		b.previewed = ""
		return
	}
	b.scrolls = true
	doc := b.previewScroll.Send(sel("documentView"))
	w := float64(len(b.cells[0])) * cellW
	msgSetRect(b.previewScroll, sel("setFrame:"), rect{b.cellsOrigin(), size{w, float64(len(b.cells)) * cellH}})
	msgSetRect(doc, sel("setFrame:"), rect{Size: size{w, float64(len(b.whole.cells)) * cellH}})
	b.previewScroll.Send(sel("setHidden:"), false)
	if seen := cellsText(b.whole.cells); seen != b.previewed {
		b.previewed = seen
		msgPointArg(doc, sel("scrollPoint:"), point{})
	}
	doc.Send(sel("setNeedsDisplay:"), true)
}

func drawWholePreview(self objc.ID, _ objc.SEL, _ rect) {
	if b := previews[self]; b != nil && b.boxed != nil && b.whole != nil {
		drawPreview(b.whole.cells, cursorAt{}, b.whole.chosen, b.whole.rules, point{}, b.look.Base.Bg)
	}
}
