package gui

import (
	"math"

	"github.com/y3owk1n/fude/frontend/wire"
)

// list is a menu the window lays out itself, as a Mac application's lists
// are: rows in the system's font, each with its icon, and over them the
// line typed into, for the palette and the finder. It shows view rows of
// its items at a time, scrolled by scroll points, which the wheel moves
// and the selection keeps in view (B560).
type list struct {
	items    []wire.Item
	selected int // the index of the selected item, -1 for none
	hovered  int // the index of the item under the mouse, -1 for none
	view     int
	scroll   float64
	typing   bool // it has a row typed into
	input    string
	at       int    // the cursor's byte in input
	note     string // dim at the right of the input row: the finder's count
	width    float64
	rowH     float64
	caret    point // where the cursor drew, for an input method
}

// The heights of a list's rows, in points.
const (
	inputH   = 40
	listRowH = 26
	menuRowH = 22
)

// shown is how many rows l shows.
func (l *list) shown() int {
	if l.view == 0 {
		return len(l.items)
	}
	return min(len(l.items), l.view)
}

// follow scrolls l the least it must to show its selected item, and keeps
// its scroll within its rows.
func (l *list) follow(moved bool) {
	h := float64(l.shown()) * l.rowH
	if moved && l.selected >= 0 {
		top := float64(l.selected) * l.rowH
		l.scroll = min(l.scroll, top)
		l.scroll = max(l.scroll, top+l.rowH-h)
	}
	l.scroll = min(max(l.scroll, 0), max(float64(len(l.items))*l.rowH-h, 0))
}

// height is how tall l draws.
func (l *list) height() float64 {
	h := float64(l.shown())*l.rowH + 8
	if l.typing {
		h += inputH
		if len(l.items) == 0 {
			h = inputH
		}
	}
	return h
}

// top is where l's first row is, from its box's top.
func (l *list) top() float64 {
	if l.typing {
		return inputH + 4
	}
	return 4
}

// itemAt returns the item a point in l's box hits, if one is there that a
// click picks.
func (l *list) itemAt(p point) (int, bool) {
	i := int(math.Floor((p.Y - l.top() + l.scroll) / l.rowH))
	if p.Y < l.top() || p.Y >= l.top()+float64(l.shown())*l.rowH || i < 0 || i >= len(l.items) || p.X < 8 || p.X > 8+l.width {
		return 0, false
	}
	return i, !l.items[i].Header && !l.items[i].Disabled
}
