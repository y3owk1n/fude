package gui

import (
	"math"
	"strings"

	"github.com/y3owk1n/fude/frontend/wire"
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

// draw draws l's input row in its box from x. The system's list under it
// shows the items (D68).
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

// fitted draws s, Pango markup when markup is set, from at in font and the
// colour set last, cut with an ellipsis at w points.
func fitted(s string, markup bool, font uintptr, at point, w float64) {
	l := pangoCairoLayout(cr)
	defer gObjectUnref(l)
	pangoLayoutSetFont(l, font)
	pangoLayoutSetWidth(l, int32(w*pangoScale))
	pangoLayoutEllipsize(l, 3)
	if markup {
		pangoLayoutMarkup(l, s, -1)
	} else {
		pangoLayoutSetText(l, s, -1)
	}
	cairoMoveTo(cr, at.X, at.Y)
	pangoCairoShow(cr, l)
}

// escape escapes text for Pango markup.
var escape = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// marked is an item's label as Pango markup, its marks bold.
func marked(it wire.Item) string {
	var b strings.Builder
	for i, r := range it.Label {
		s := escape.Replace(string(r))
		for _, m := range it.Marks {
			if m == i {
				s = "<b>" + s + "</b>"
				break
			}
		}
		b.WriteString(s)
	}
	return b.String()
}
