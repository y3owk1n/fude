package gui

import (
	"math"
	"slices"
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

// draw draws the row typed into of l in its box from x. The system's list
// view under it shows the items (D68).
func (l *list) draw(x float64) {
	if l.typing {
		l.drawInput(x, l.width)
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
	drawText(l, at)
}
