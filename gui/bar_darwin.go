package gui

import (
	"math"
	"strconv"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/client"
)

// drawBar draws the tab strip right of the window's buttons (B258, B545),
// on the title bar's material in the system's colours. The pages take up
// to half of it. Each tab ends in a slot that shows a dot while its buffer
// has changes, and × to close it while the mouse is over it or it is
// current. It keeps where each tab is, for the mouse (B552, B555, B559).
func drawBar(self objc.ID, _ objc.SEL, _ rect) {
	w := windows[self]
	if w == nil {
		return
	}
	b := msgRect(self, sel("bounds")).Size
	system("separatorColor").Send(sel("setFill"))
	rectFill(rect{point{0, b.H - 1}, size{b.W, 1}})
	zoom := w.win.Send(sel("standardWindowButton:"), 2)
	r := msgConvertRect(zoom, sel("convertRect:toView:"), msgRect(zoom, sel("bounds")), self)
	x, end := r.Origin.X+r.Size.W+2*pad, b.W-pad
	t := w.tabs
	w.hits = w.hits[:0]
	if len(t.buffers) == 0 && len(t.pages) == 0 {
		r := run{text: t.title, bold: true, fg: "secondaryLabelColor"}
		r.draw(math.Round((b.W-r.size())/2), 0, b.H)
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
		cut(pages, at, start, end, b.H, func(i int, from, to float64) {
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
	cut(bufs, at, x, end, b.H, func(i int, from, to float64) {
		over(bufs, at, from, to, i)
		w.hits = append(w.hits, hit{from, to, false, t.buffers[i].Number, i})
	})
	// A tab being dragged shows where it would go.
	if h, ok := w.dropAt(); ok {
		system("controlAccentColor").Send(sel("setFill"))
		rectFill(rect{point{h - 1, 6}, size{2, b.H - 12}})
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
