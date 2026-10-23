package gui

import (
	"fmt"
	"math"
	"strconv"

	"github.com/ebitengine/purego"

	"github.com/y3owk1n/fude/frontend/wire"
)

// A box shows its menu's items in a GtkListBox inside a GtkScrolledWindow
// over the box. The list never takes the keyboard's focus, so keys still go
// to the session (B560, D68).
var (
	tables, previews = map[uintptr]*box{}, map[uintptr]*box{} // by list box, and by preview area
	tableCallbacks   struct{ activated, drawPreview uintptr }
)

// makeTableCallbacks makes the lists' and the previews' callbacks.
func makeTableCallbacks() {
	// A click on a row picks its item, as a click on a menu's item does
	// (B556).
	tableCallbacks.activated = purego.NewCallback(func(list, row, _ uintptr) uintptr {
		if b := tables[list]; b != nil && b.boxed != nil && b.list != nil {
			b.w.mice <- mouse{kind: 'p', y: int(gtkRowIndex(row))}
		}
		return 0
	})
	tableCallbacks.drawPreview = purego.NewCallback(func(area, cairo uintptr, _, _ int32, _ uintptr) uintptr {
		if b := previews[area]; b != nil && b.boxed != nil && b.whole != nil {
			cr = cairo
			defer func() { cr = 0 }()
			drawPreview(b.whole.cells, cursorAt{}, b.whole.chosen, b.whole.rules, point{}, b.look.Base.Bg)
		}
		return 0
	})
}

// newTable makes b's list and its preview's scrolled area, hidden until b
// shows a menu or a long preview.
func (b *box) newTable() {
	b.listBox = gtkListBoxNew()
	// GTK_SELECTION_SINGLE.
	gtkListBoxMode(b.listBox, 1)
	gtkWidgetAddClass(b.listBox, "navigation-sidebar")
	gtkWidgetAddClass(b.listBox, "fude-list")
	gtkSetFocusable(b.listBox, 0)
	gSignalConnectData(b.listBox, "row-activated", tableCallbacks.activated, 0, 0, 0)
	b.scrolled = scrolledOver(b.w, b.listBox)
	tables[b.listBox] = b
	b.previewArea = gtkDrawingAreaNew()
	gtkDrawingAreaSetFn(b.previewArea, tableCallbacks.drawPreview, 0, 0)
	b.previewScroll = scrolledOver(b.w, b.previewArea)
	previews[b.previewArea] = b
}

// scrolledOver returns a hidden scrolled window of child over w's panes,
// which scrolls only up and down.
func scrolledOver(w *window, child uintptr) uintptr {
	sw := gObjectRefSink(gtkScrolledNew())
	// GTK_POLICY_NEVER across and GTK_POLICY_AUTOMATIC down.
	gtkScrolledPolicy(sw, 2, 1)
	gtkScrolledSetChild(sw, child)
	gtkWidgetSetHalign(sw, 1)
	gtkWidgetSetValign(sw, 1)
	gtkWidgetSetVisible(sw, 0)
	gtkOverlayAdd(w.overlay, sw)
	return sw
}

// place puts a widget over the panes at r.
func place(widget uintptr, r rect) {
	gtkWidgetMarginStart(widget, int32(r.Origin.X))
	gtkWidgetMarginTop(widget, int32(r.Origin.Y))
	gtkWidgetSizeRequest(widget, int32(math.Ceil(r.Size.W)), int32(math.Ceil(r.Size.H)))
	gtkWidgetSetVisible(widget, 1)
}

// showTable shows b's menu in its list, under the row typed into, at r of
// the box, and keeps its selected item in view. Without a menu it hides
// the list.
func (b *box) showTable(r rect) {
	if b.boxed == nil || b.list == nil || len(b.list.items) == 0 {
		gtkWidgetSetVisible(b.scrolled, 0)
		b.listed = ""
		return
	}
	l := b.list
	width := r.Size.W
	if len(b.cells) > 0 {
		width = 8 + l.width + 4
	}
	place(b.scrolled, rect{point{r.Origin.X, r.Origin.Y + l.top()}, size{width, float64(l.shown()) * l.rowH}})
	if listed := fmt.Sprint(l.items, l.rowH); listed != b.listed {
		b.listed = listed
		for c := gtkWidgetFirstChild(b.listBox); c != 0; c = gtkWidgetFirstChild(b.listBox) {
			gtkListBoxRemove(b.listBox, c)
		}
		for i, it := range l.items {
			gtkListBoxAppend(b.listBox, rowOf(it, l.rowH))
			row := gtkListBoxRowAt(b.listBox, int32(i))
			gtkSetFocusable(row, 0)
			if it.Header || it.Disabled {
				gtkRowSelectable(row, 0)
				gtkRowActivatable(row, 0)
			}
		}
	}
	if l.selected < 0 || l.selected >= len(l.items) {
		gtkListBoxUnselect(b.listBox)
		return
	}
	gtkListBoxSelect(b.listBox, gtkListBoxRowAt(b.listBox, int32(l.selected)))
	// Each row is the list's row height, so this scrolls the selected row
	// into view before GTK lays the rows out.
	adj := gtkScrolledVAdjust(b.scrolled)
	page := float64(l.shown()) * l.rowH
	top, at := gtkAdjustmentValue(adj), float64(l.selected)*l.rowH
	top = min(top, at)
	top = max(top, at+l.rowH-page)
	gtkAdjustmentSet(adj, max(top, 0), 0, float64(len(l.items))*l.rowH, l.rowH, page, page)
}

// rowOf is an item's row: a grep match's line, its label with the typed
// text's matches bold, and its detail dim at the right, h tall.
func rowOf(it wire.Item, h float64) uintptr {
	row := gtkBoxNew(0, 8)
	gtkWidgetSizeRequest(row, -1, int32(h))
	if icon := iconName(it); icon != "" {
		gtkBoxAppend(row, gtkImageFromIcon(icon))
	}
	if it.Line > 0 {
		n := gtkLabelNew(strconv.Itoa(it.Line))
		gtkWidgetAddClass(n, "dim-label")
		gtkBoxAppend(row, n)
	}
	label := gtkLabelNew("")
	gtkLabelSetMarkup(label, marked(it))
	gtkLabelSetXalign(label, 0)
	gtkWidgetSetHexpand(label, 1)
	// PANGO_ELLIPSIZE_END.
	gtkLabelEllipsize(label, 3)
	if it.Header || it.Disabled {
		gtkWidgetAddClass(label, "dim-label")
	}
	gtkBoxAppend(row, label)
	if it.Detail != "" {
		d := gtkLabelNew(it.Detail)
		gtkWidgetAddClass(d, "dim-label")
		gtkLabelEllipsize(d, 3)
		gtkBoxAppend(row, d)
	}
	return row
}

// iconName is the desktop's icon for an item's kind, "" for none.
func iconName(it wire.Item) string {
	switch it.Kind {
	case "file", "buffer":
		return "text-x-generic-symbolic"
	case "folder", "path":
		return "folder-symbolic"
	}
	return ""
}

// showPreview shows the finder's whole preview, or a menu's whole
// documentation, in its scrolled area over the box at r. It scrolls back
// to the top when another file shows, and hides the area when there is no
// whole preview (B130, B560).
func (b *box) showPreview(r rect) {
	if b.boxed == nil || b.whole == nil || len(b.cells) == 0 {
		gtkWidgetSetVisible(b.previewScroll, 0)
		b.previewed = ""
		return
	}
	b.scrolls = true
	o := b.cellsOrigin()
	w := float64(len(b.cells[0])) * cellW
	place(b.previewScroll, rect{point{r.Origin.X + o.X, r.Origin.Y + o.Y}, size{w, float64(len(b.cells)) * cellH}})
	gtkWidgetSizeRequest(b.previewArea, int32(w), int32(float64(len(b.whole.cells))*cellH))
	if seen := cellsText(b.whole.cells); seen != b.previewed {
		b.previewed = seen
		gtkAdjustmentSetValue(gtkScrolledVAdjust(b.previewScroll), 0)
	}
	gtkWidgetQueueDraw(b.previewArea)
}

// dropTable forgets b's list and preview, as b goes.
func (b *box) dropTable() {
	for _, sw := range []uintptr{b.scrolled, b.previewScroll} {
		gtkOverlayRemove(b.w.overlay, sw)
		gObjectUnref(sw)
	}
	delete(tables, b.listBox)
	delete(previews, b.previewArea)
}
