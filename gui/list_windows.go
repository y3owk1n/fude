package gui

import (
	"math"
	"slices"
	"strconv"
	"unicode/utf16"
	"unsafe"
)

// natural is how wide l's rows would be to show every item whole.
func (l *list) natural() float64 {
	w := 0.0
	for _, it := range l.items {
		lw := measure(it.Label, uiFont).W
		dw := 0.0
		if it.Detail != "" {
			dw = measure(it.Detail, uiFont).W + 24
		}
		w = max(w, 8+lw+dw+12)
	}
	return math.Ceil(w)
}

// draw draws l in its box from x, with its rows w wide: each row's label
// with its marks bold, its line and its detail at its right, the selected
// row on the system's accent and the one under the mouse lit.
func (l *list) draw(x float64) {
	w := l.width
	if l.typing {
		l.drawInput(x, w)
	}
	y, h := l.top(), float64(l.shown())*l.rowH
	clip(rect{point{x, y}, size{w, h}})
	defer unclip()
	// A list taller than its rows shows a thin knob where its rows are.
	if all := float64(len(l.items)) * l.rowH; all > h {
		fillRounded("tertiaryLabelColor", rect{point{x + w - 4, y + l.scroll/all*h}, size{3, max(h*h/all, 16)}}, 1.5)
		w -= 8
	}
	first := int(l.scroll / l.rowH)
	for i := first; i < min(len(l.items), first+l.shown()+1); i++ {
		it := l.items[i]
		r := rect{point{x, y + float64(i)*l.rowH - l.scroll}, size{w, l.rowH}}
		label, detail := "labelColor", "secondaryLabelColor"
		if i == l.hovered && i != l.selected {
			fillRounded("quaternaryLabelColor", r, 4)
		}
		if i == l.selected {
			fillRounded("selectedContentBackgroundColor", r, 4)
			label, detail = "alternateSelectedControlTextColor", "alternateSelectedControlTextColor"
		}
		if it.Disabled {
			label = "tertiaryLabelColor"
		}
		f := uiFont
		if it.Header {
			f, label = uiBold, "secondaryLabelColor"
		}
		at := r.Origin.X + 8
		mid := func(sz size) float64 { return r.Origin.Y + math.Floor((l.rowH-sz.H)/2) }
		if it.Line > 0 {
			n := strconv.Itoa(it.Line)
			setColour(detail)
			text(n, uiFont, point{at, mid(measure(n, uiFont))})
			at += 36
		}
		end := r.Origin.X + w - 10
		if it.Detail != "" {
			sz := measure(it.Detail, uiFont)
			dx := max(end-sz.W, at+80)
			setColour(detail)
			fitted(it.Detail, nil, uiFont, point{dx, mid(sz)}, end-dx)
			end = dx - 12
		}
		setColour(label)
		fitted(it.Label, it.Marks, f, point{at, mid(measure(it.Label, f))}, max(end-at, 0))
	}
}

// drawInput draws the row typed into: what is typed in a larger font with
// the cursor in it, and the note at its right.
func (l *list) drawInput(x, w float64) {
	sz := measure(l.input+" ", inputFont)
	before := measure(l.input[:min(l.at, len(l.input))], inputFont).W
	end := x + w - 8
	if l.note != "" {
		ns := measure(l.note, uiFont)
		end -= ns.W
		setColour("tertiaryLabelColor")
		text(l.note, uiFont, point{end, (inputH - ns.H) / 2})
		end -= 12
	}
	at := x + 32
	// A line longer than the row shows its end, where the cursor usually is.
	shift := max(before-(end-at), 0)
	y := (inputH - sz.H) / 2
	clip(rect{point{at, 0}, size{max(end-at, 0), inputH}})
	setColour("labelColor")
	text(l.input, inputFont, point{at - shift, y})
	unclip()
	l.caret = point{at - shift + before, y}
	fill("controlAccentColor", rect{l.caret, size{2, sz.H}})
	fill("separatorColor", rect{point{x, inputH}, size{w, 1}})
}

// fitted draws s from at in font and the colour set last, cut with an
// ellipsis at w points, and the characters at its bytes bold in bold.
func fitted(s string, bold []int, font unsafe.Pointer, at point, w float64) {
	l := layoutOf(s, font, w, 1e6)
	defer release(l)
	trim(l, font)
	u := 0
	for i, r := range s {
		// A range of text is two uint32s, its start and its length in
		// UTF-16, which go in one register.
		if slices.Contains(bold, i) {
			com(l, 32, 700, uintptr(u)|uintptr(utf16.RuneLen(r))<<32)
		}
		u += utf16.RuneLen(r)
	}
	draw(l, at)
}
