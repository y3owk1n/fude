package gui

import (
	"math"
	"strconv"

	"github.com/y3owk1n/fude/frontend/client"
)

// hit is where a tab of the strip is: a buffer's, numbered n, or with page
// set the tab page at index n. index is its place in its row.
type hit struct {
	from, to float64
	page     bool
	n, index int
}

// slotW is the width of a tab's slot for ● or ×, which ends it.
const slotW = 16

// show, shut and move are the keys that show a tab, close it, and move the
// current one after the tab at index n of its row, first with 0.
func (h hit) show() string {
	if h.page {
		return strconv.Itoa(h.n+1) + "gt"
	}
	return ":b " + strconv.Itoa(h.n) + "<CR>"
}

func (h hit) shut() string {
	if h.page {
		return h.show() + ":tabclose<CR>"
	}
	return ":bd " + strconv.Itoa(h.n) + "<CR>"
}

func (h hit) move(n int) string {
	if h.page {
		return ":tabmove " + strconv.Itoa(n) + "<CR>"
	}
	return ":bmove " + strconv.Itoa(n) + "<CR>"
}

// at returns the tab at x, if one is there.
func (w *window) at(x float64) (hit, bool) {
	for _, h := range w.hits {
		if x >= h.from && x < h.to {
			return h, true
		}
	}
	return hit{}, false
}

// dropIndex is the place of the tab after which a dragged one goes, as
// :bmove and :tabmove count it, 0 for first.
func (w *window) dropIndex() int {
	n := 0
	for _, h := range w.hits {
		if h.page == w.pressed.page && (h.from+h.to)/2 < w.dropX {
			n = h.index + 1
		}
	}
	return n
}

// dropAt is where the mark goes that shows where a dragged tab would go.
func (w *window) dropAt() (float64, bool) {
	if !w.dragging || w.pressed == nil {
		return 0, false
	}
	x, found := 0.0, false
	for _, h := range w.hits {
		if h.page != w.pressed.page {
			continue
		}
		switch {
		case (h.from+h.to)/2 < w.dropX:
			x, found = h.to, true
		case !found:
			x, found = h.from, true
		}
	}
	return x, found
}

// drawStrip draws the tab strip, wide points wide and height tall, in the
// system's colours. Its tabs go from x to end, the buffers' and right of
// them the tab pages', which take up to half of it. Each tab ends in a
// slot that shows a dot while its buffer has changes, and × to close it
// while the mouse is over it or it is current. With no tabs the title
// shows in its middle. It keeps where each tab is, for the mouse (B258,
// B545, B555, B559).
func (w *window) drawStrip(wide, x, end, height float64) {
	t := w.tabs
	w.hits = w.hits[:0]
	if len(t.buffers) == 0 && len(t.pages) == 0 {
		r := run{text: t.title, bold: true, fg: "secondaryLabelColor"}
		r.draw(math.Round((wide-r.size())/2), 0, height)
		return
	}
	tab := func(text string, modified, current bool) run {
		r := run{text: text, fg: "secondaryLabelColor", pill: true, slotted: true, slotFg: "secondaryLabelColor"}
		if current {
			r.bold, r.fg, r.ground = true, "labelColor", "quaternaryLabelColor"
		}
		if modified {
			r.slot = "circle.fill"
		}
		return r
	}
	// over marks the tab under the mouse, which shows × on a light ground,
	// as the current one shows × while it has no changes.
	over := func(rs []run, current int, from, to float64, i int) {
		hovered := w.hover >= from && w.hover < to
		if hovered && rs[i].ground == "" {
			rs[i].ground = "quaternaryLabelColor"
		}
		if hovered || i == current && rs[i].slot == "" {
			rs[i].slot = "xmark"
		}
	}
	if len(t.pages) > 0 {
		pages, at := make([]run, len(t.pages)), 0
		for i, p := range t.pages {
			_, name := client.SplitName(p.Name)
			pages[i] = tab(strconv.Itoa(i+1)+" "+name, p.Modified, p.Current)
			if p.Current {
				at = i
				pages[i].fg, pages[i].ground, pages[i].slotFg = "whiteColor", modeColour(t.mode), "whiteColor"
			}
		}
		start := end - min(width(pages), (end-x)/2)
		cut(pages, at, start, end, height, func(i int, from, to float64) {
			over(pages, at, from, to, i)
			w.hits = append(w.hits, hit{from, to, true, i, i})
		})
		end = start - pad
	}
	bufs, at := make([]run, len(t.buffers)), 0
	for i, tb := range t.buffers {
		bufs[i] = tab(strconv.Itoa(tb.Number)+" "+tb.Name, tb.Modified, tb.Current)
		if tb.Current {
			at = i
		}
	}
	cut(bufs, at, x, end, height, func(i int, from, to float64) {
		over(bufs, at, from, to, i)
		w.hits = append(w.hits, hit{from, to, false, t.buffers[i].Number, i})
	})
	// A tab being dragged shows where it would go.
	if h, ok := w.dropAt(); ok {
		fill("controlAccentColor", rect{point{h - 1, 6}, size{2, height - 12}})
	}
}

// cut draws rs from x to end in a bar h tall. It tells at where each goes,
// by its index, before it draws, so at may change it. When they do not all
// fit, it draws the ones around the one at current, with ‹ or › where it
// cuts.
func cut(rs []run, current int, x, end, h float64, at func(i int, from, to float64)) {
	mark := func(text string) run { return run{text: text, fg: "tertiaryLabelColor"} }
	if width(rs) <= end-x {
		for i := range rs {
			at(i, x, x+rs[i].size())
			x = rs[i].draw(x, 0, h)
		}
		return
	}
	marks := mark("‹").size() + mark("›").size()
	first := 0
	for first < current && width(rs[first:current+1])+marks > end-x {
		first++
	}
	if first > 0 {
		x = mark("‹").draw(x, 0, h)
	}
	for i := first; i < len(rs); i++ {
		if more := i < len(rs)-1; x+rs[i].size() > end-mark("›").size() && more || x+rs[i].size() > end {
			mark("›").draw(x, 0, h)
			return
		}
		at(i, x, x+rs[i].size())
		x = rs[i].draw(x, 0, h)
	}
}

// typeKeys types keys in the mode the window is in, after <Esc> in insert
// and visual mode. Other modes take none.
func (w *window) typeKeys(keys string) {
	w.act(keys, "<Esc>"+keys, "<Esc>"+keys)
}

// stripPress shows the tab a click at x along the strip hits, as :b and gt
// do, or closes it when the click hits its ×, and reports whether a tab
// took the click.
func (w *window) stripPress(x float64) bool {
	w.pressed, w.dragging = nil, false
	h, ok := w.at(x)
	switch {
	case !ok:
		return false
	case x >= h.to-pad-slotW:
		w.typeKeys(h.shut())
		return true
	}
	w.typeKeys(h.show())
	w.pressed, w.pressX = &h, x
	return true
}

// stripDrag drags the tab pressed to x, once it moves past a few points,
// and reports whether a tab moves.
func (w *window) stripDrag(x float64) bool {
	if w.pressed == nil {
		return false
	}
	w.dragging = w.dragging || math.Abs(x-w.pressX) > 4
	w.dropX = x
	return true
}

// stripRelease moves a dragged tab where it was dropped, as :bmove and
// :tabmove do, and reports whether a tab was pressed (B555).
func (w *window) stripRelease() bool {
	if w.pressed == nil {
		return false
	}
	if w.dragging {
		w.typeKeys(w.pressed.move(w.dropIndex()))
	}
	w.pressed, w.dragging = nil, false
	return true
}

// stripShut closes the tab at x, as a middle click does.
func (w *window) stripShut(x float64) {
	if h, ok := w.at(x); ok {
		w.typeKeys(h.shut())
	}
}
