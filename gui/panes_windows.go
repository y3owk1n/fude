package gui

import (
	"math"
	"slices"
	"strconv"

	"github.com/y3owk1n/fude/frontend/wire"
)

// paneNative is where a pane draws in its window, its text over its status
// bar.
type paneNative struct{ frame rect }

// divider is a line a point wide between two kids of a split, which the
// mouse drags. key names it in moved, and from and to are where the kids
// on either side start and end, along the split.
type divider struct {
	r        rect
	across   bool
	key      string
	from, to float64
}

// pane returns the pane of the window with id, made the first time.
func (w *window) pane(id int) *pane {
	if p := w.panes[id]; p != nil {
		return p
	}
	p := newPane(w, id)
	w.panes[id] = p
	return p
}

// build lays the panes out as l splits them, and drops the panes of
// windows that closed.
func (w *window) build(l wire.Split) {
	ids := leaves(l)
	for id, p := range w.panes {
		if !slices.Contains(ids, id) {
			p.gone = true
			delete(w.panes, id)
		}
	}
	for _, id := range ids {
		w.pane(id)
	}
	w.layout, w.moved = l, map[string]float64{}
	w.arrange()
}

// arrange gives each pane its frame in the window, as the layout splits
// it, and the loop where the panes put the windows' text.
func (w *window) arrange() {
	w.dividers = w.dividers[:0]
	w.node(w.layout, rect{Size: w.area()}, "")
	w.placeAll()
	w.redraw()
}

// node gives the panes of l, at path in the layout, their frames in r,
// with a divider between each two kids. A kid a divider was dragged after
// keeps its size, one that asks for rows or columns gets them, the others
// share the rest, and the last takes what is left.
func (w *window) node(l wire.Split, r rect, path string) {
	if len(l.Kids) == 0 {
		if p := w.panes[l.Window]; p != nil {
			p.frame = r
		}
		return
	}
	total, cell, room := r.Size.H, cellH, float64(2*pad+statusH)
	if l.Across {
		total, cell, room = r.Size.W, cellW, 2*pad
	}
	total -= float64(len(l.Kids) - 1)
	sizes, left, shared := make([]float64, len(l.Kids)), total, 0
	key := func(i int) string { return path + "/" + strconv.Itoa(i) }
	for i, k := range l.Kids {
		if moved, ok := w.moved[key(i)]; ok {
			sizes[i] = min(moved, left)
			left -= sizes[i]
		} else if k.Size > 0 {
			sizes[i] = min(float64(k.Size)*cell+room, left)
			left -= sizes[i]
		} else {
			shared++
		}
	}
	for i, k := range l.Kids {
		if _, ok := w.moved[key(i)]; !ok && k.Size == 0 {
			sizes[i] = math.Floor(left / float64(shared))
		}
	}
	at := 0.0
	for i, k := range l.Kids {
		n := sizes[i]
		if i == len(l.Kids)-1 {
			n = total + float64(i) - at
		}
		kid, line := r, r
		start := r.Origin.Y
		if l.Across {
			start = r.Origin.X
			kid.Origin.X, kid.Size.W = start+at, n
			line.Origin.X, line.Size.W = start+at+n, 1
		} else {
			kid.Origin.Y, kid.Size.H = start+at, n
			line.Origin.Y, line.Size.H = start+at+n, 1
		}
		w.node(k, kid, key(i))
		if i < len(l.Kids)-1 {
			next := sizes[i+1]
			if i+1 == len(l.Kids)-1 {
				next = total + float64(i+1) - at - n - 1
			}
			w.dividers = append(w.dividers, divider{line, l.Across, key(i), start + at, start + at + n + 1 + next})
		}
		at += n + 1
	}
}

// placeAll gives the loop where each pane puts its window's text, in cells
// of the area, when that changed.
func (w *window) placeAll() {
	area := w.area()
	pl := places{cols: max(int(area.W/cellW), 1), rows: max(int(area.H/cellH), 1), cell: w.cellPx}
	for id, p := range w.panes {
		r := p.frame
		x, y := r.Origin.X+pad, r.Origin.Y+pad
		pl.wins = append(pl.wins, [5]int{id, int(x / cellW), int(y / cellH), max(int((r.Size.W-2*pad)/cellW), 1), max(int((r.Size.H-statusH-2*pad)/cellH), 1)})
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

// drawPane draws a pane bounds big: its text on the theme's ground, and
// its status bar under it on the system's bar colour (B147).
func drawPane(p *pane, bounds size) {
	fillRGB(p.w.look.Base.Bg, rect{Size: bounds})
	drawCells(p.cells, p.cursor, p.origin(), nil, nil)
	p.w.drawPics(p.pics, point{pad, pad})
	bar := rect{point{0, bounds.H - statusH}, size{bounds.W, statusH}}
	fill("barColor", bar)
	drawParts(p.left, p.right, p.current, bar)
}

// redraw draws p again, with its window.
func (p *pane) redraw() { p.w.redraw() }

// showBar and drawPics do nothing until the scroll bars and images come.
func (p *pane) showBar(bool)            {}
func (w *window) drawPics([]pic, point) {}
