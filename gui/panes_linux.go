package gui

import (
	"math"
	"slices"

	"github.com/y3owk1n/fude/frontend/wire"
)

// paneNative is a pane's drawing area, its text over its status bar.
type paneNative struct{ area uintptr }

// panes are the panes by their drawing areas, which the callbacks get.
var panes = map[uintptr]*pane{}

// pane returns the pane of the window with id, made the first time. It
// keeps a reference to its area, so the area outlives the splits it moves
// between.
func (w *window) pane(id int) *pane {
	if p := w.panes[id]; p != nil {
		return p
	}
	p := newPane(w, id)
	p.area = gObjectRefSink(gtkDrawingAreaNew())
	gtkWidgetSetCursor(p.area, "text")
	gtkWidgetSetHexpand(p.area, 1)
	gtkWidgetSetVexpand(p.area, 1)
	gtkDrawingAreaSetFn(p.area, callbacks.drawPane, p.area, 0)
	gSignalConnectData(p.area, "resize", callbacks.resized, p.area, 0, 0)
	listen(p.area)
	w.panes[id], panes[p.area] = p, p
	return p
}

// drop forgets p, whose window closed.
func (p *pane) drop() {
	p.gone = true
	delete(panes, p.area)
	gObjectUnref(p.area)
}

// build lays the panes out as l splits them, each split a GtkPaned, and
// drops the panes of windows that closed.
func (w *window) build(l wire.Split) {
	ids := leaves(l)
	gtkOverlaySetChild(w.overlay, 0)
	for _, sp := range w.splits {
		gtkPanedSetStart(sp, 0)
		gtkPanedSetEnd(sp, 0)
	}
	w.splits = w.splits[:0]
	for id, p := range w.panes {
		if !slices.Contains(ids, id) {
			p.drop()
			delete(w.panes, id)
		}
	}
	area := size{float64(gtkWidgetGetWidth(w.overlay)), float64(gtkWidgetGetHeight(w.overlay))}
	gtkOverlaySetChild(w.overlay, w.node(l, area))
	w.placeAll()
}

// node returns the widget that shows l in an area of that size: a pane, or
// splits of its kids. A kid that asks for rows or columns gets them, and the
// others share the rest.
func (w *window) node(l wire.Split, area size) uintptr {
	if len(l.Kids) == 0 {
		return w.pane(l.Window).area
	}
	total, cell, room := area.H, cellH, float64(2*pad+statusH)
	if l.Across {
		total, cell, room = area.W, cellW, 2*pad
	}
	sizes, left, shared := make([]float64, len(l.Kids)), total, 0
	for i, k := range l.Kids {
		if k.Size > 0 {
			sizes[i] = min(float64(k.Size)*cell+room, left)
			left -= sizes[i]
		} else {
			shared++
		}
	}
	for i, k := range l.Kids {
		if k.Size == 0 {
			sizes[i] = math.Floor(left / float64(shared))
		}
	}
	// Each split holds a kid and the splits of the kids after it.
	var chain func(i int) uintptr
	chain = func(i int) uintptr {
		kid := func(i int) uintptr {
			if l.Across {
				return w.node(l.Kids[i], size{sizes[i], area.H})
			}
			return w.node(l.Kids[i], size{area.W, sizes[i]})
		}
		if i == len(l.Kids)-1 {
			return kid(i)
		}
		orientation := int32(1)
		if l.Across {
			orientation = 0
		}
		sp := gtkPanedNew(orientation)
		w.splits = append(w.splits, sp)
		gtkPanedSetStart(sp, kid(i))
		gtkPanedSetEnd(sp, chain(i+1))
		gtkPanedSetPosition(sp, int32(sizes[i]))
		return sp
	}
	return chain(0)
}

// placeAll gives the loop where each pane puts its window's text, in cells
// of the area, when that changed.
func (w *window) placeAll() {
	area := size{float64(gtkWidgetGetWidth(w.overlay)), float64(gtkWidgetGetHeight(w.overlay))}
	pl := places{cols: max(int(area.W/cellW), 1), rows: max(int(area.H/cellH), 1), cell: w.cellPx}
	for id, p := range w.panes {
		var r grect
		if gtkWidgetParent(p.area) == 0 || gtkWidgetBounds(p.area, w.overlay, &r) == 0 {
			continue
		}
		x, y := float64(r.X)+pad, float64(r.Y)+pad
		pl.wins = append(pl.wins, [5]int{id, int(x / cellW), int(y / cellH), max(int((float64(r.W)-2*pad)/cellW), 1), max(int((float64(r.H)-statusH-2*pad)/cellH), 1)})
	}
	slices.SortFunc(pl.wins, func(a, b [5]int) int { return a[0] - b[0] })
	if pl.cols == w.sent.cols && pl.rows == w.sent.rows && pl.cell == w.sent.cell && slices.Equal(pl.wins, w.sent.wins) {
		return
	}
	w.sent = pl
	w.mu.Lock()
	w.places = pl
	w.mu.Unlock()
	select {
	case w.placed <- struct{}{}:
	default:
	}
}

// drawPane draws a pane's text on the theme's ground, and its status bar
// under it on GTK's window colour (B147).
func drawPane(area, cairo uintptr, width, height int32, _ uintptr) {
	p := panes[area]
	if p == nil {
		return
	}
	cr = cairo
	defer func() { cr = 0 }()
	b := size{float64(width), float64(height)}
	fillRGB(p.w.look.Base.Bg, rect{Size: b})
	drawCells(p.cells, p.cursor, p.origin(), nil, nil)
	bar := rect{point{0, b.H - statusH}, size{b.W, statusH}}
	fill("barColor", bar)
	drawParts(p.left, p.right, p.current, bar)
}

// redraw draws p again.
func (p *pane) redraw() { gtkWidgetQueueDraw(p.area) }

// pointer makes the mouse's pointer over p's text the hand over a link,
// and the text's otherwise.
func (p *pane) pointer(link bool) {
	if link {
		gtkWidgetSetCursor(p.area, "pointer")
	} else {
		gtkWidgetSetCursor(p.area, "text")
	}
}

// showBar shows no scroll bar, which Linux does not have yet.
func (p *pane) showBar(bool) {}
