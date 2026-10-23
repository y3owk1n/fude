package gui

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/frontend/theme"
)

// The fonts, as Pango describes them, which makeFonts makes as the
// application starts. The text is monospace, and the chrome takes the
// desktop's font from GTK.
var (
	font, boldFont, italicFont, boldItalicFont uintptr
	uiFont, uiBold, inputFont                  uintptr
	measurer                                   uintptr // a layout for measuring text outside drawing
)

// cr is the cairo context of the drawing being made, on the main thread.
var cr uintptr

// textSize is the text's size in points, which Control with =, minus and
// 0 change (B568).
var textSize = systemSize

// systemSize is the text's size in points unless the session asks for
// another (B584).
const systemSize = 11.0

// makeTextFonts makes the text's fonts in textSize, and sizes a cell in
// them.
func makeTextFonts() {
	font = pangoFontFromString(fmt.Sprintf("Monospace %g", textSize))
	// Pango takes the first of the families it has, and else the
	// monospace.
	if len(wanted.Families) > 0 {
		pangoFontSetFamily(font, strings.Join(append(slices.Clone(wanted.Families), "Monospace"), ","))
	}
	makeChromeFonts()
	boldFont, italicFont, boldItalicFont = pangoFontCopy(font), pangoFontCopy(font), pangoFontCopy(font)
	pangoFontSetWeight(boldFont, 700)
	pangoFontSetStyle(italicFont, 2)
	pangoFontSetWeight(boldItalicFont, 700)
	pangoFontSetStyle(boldItalicFont, 2)
	m := measure("M", font)
	cellW, cellH = m.W, math.Ceil(m.H)
}

// makeFonts makes the fonts, the chrome's from the desktop's font.
func makeFonts() {
	var v gValue
	gValueInit(&v, gTypeString)
	gObjectGetProperty(gtkSettingsDefault(), "gtk-font-name", &v)
	name := gValueGetString(&v)
	gValueUnset(&v)
	if name == "" {
		name = "Sans 10"
	}
	desktopFont = name
	measurer = pangoLayoutNew(pangoFontMapContext(pangoCairoFontMap()))
	makeTextFonts()
}

// desktopFont is the desktop's font, as GTK names it, such as "Cantarell 11".
var desktopFont string

// makeChromeFonts makes the fonts of the text the window composes itself,
// at their own sizes: in the session's families first when it asked for
// any, then in the desktop's font (B559, B584).
func makeChromeFonts() {
	for _, f := range []uintptr{uiFont, uiBold, inputFont} {
		if f != 0 {
			pangoFontFree(f)
		}
	}
	family := strings.TrimRightFunc(desktopFont, func(r rune) bool { return r == ' ' || r >= '0' && r <= '9' || r == '.' })
	uiFont = pangoFontFromString(desktopFont)
	inputFont = pangoFontFromString(family + " 14")
	if len(wanted.Families) > 0 {
		families := strings.Join(append(slices.Clone(wanted.Families), family), ",")
		pangoFontSetFamily(uiFont, families)
		pangoFontSetFamily(inputFont, families)
	}
	uiBold = pangoFontCopy(uiFont)
	pangoFontSetWeight(uiBold, 700)
}

// sizeText makes the text points tall, from 6 to 40, and every window as
// many cells as it then holds.
func sizeText(points float64) {
	for _, f := range []uintptr{font, boldFont, italicFont, boldItalicFont} {
		pangoFontFree(f)
	}
	textSize = min(max(points, 6), 40)
	makeTextFonts()
	for _, w := range windows {
		scale := float64(gtkWidgetScale(w.win))
		w.cellPx = [2]int{int(cellW * scale), int(cellH * scale)}
		w.placeAll()
		for _, p := range w.panes {
			p.redraw()
		}
	}
}

// measure is how big text draws in font.
func measure(text string, font uintptr) size {
	pangoLayoutSetFont(measurer, font)
	pangoLayoutSetText(measurer, text, -1)
	var w, h int32
	pangoLayoutSize(measurer, &w, &h)
	return size{float64(w) / pangoScale, float64(h) / pangoScale}
}

// fontOf is the text's font a style takes.
func fontOf(s theme.Style) uintptr {
	switch {
	case s.Bold && s.Italic:
		return boldItalicFont
	case s.Bold:
		return boldFont
	case s.Italic:
		return italicFont
	}
	return font
}

// systemColours are the theme's colours, by the names GTK's themes have
// kept since GTK 3, for the colours the shared code names as AppKit does,
// with how opaque each draws. The ones GTK does not name are fixed.
var systemColours = map[string]struct {
	name  string
	alpha float64
}{
	"labelColor":                        {"theme_fg_color", 1},
	"secondaryLabelColor":               {"theme_fg_color", 0.7},
	"tertiaryLabelColor":                {"theme_fg_color", 0.45},
	"quaternaryLabelColor":              {"theme_fg_color", 0.12},
	"separatorColor":                    {"borders", 1},
	"controlAccentColor":                {"theme_selected_bg_color", 1},
	"selectedContentBackgroundColor":    {"theme_selected_bg_color", 1},
	"alternateSelectedControlTextColor": {"theme_selected_fg_color", 1},
	"linkColor":                         {"theme_selected_bg_color", 1},
	"systemBlueColor":                   {"theme_selected_bg_color", 1},
	"systemRedColor":                    {"error_color", 1},
	"systemOrangeColor":                 {"warning_color", 1},
	"systemYellowColor":                 {"warning_color", 1},
	"systemGreenColor":                  {"success_color", 1},
	"popoverColor":                      {"theme_bg_color", 1},
	"barColor":                          {"theme_bg_color", 1},
}

// fixedColours are the colours GTK's theme does not name.
var fixedColours = map[string]gdkRGBA{
	"whiteColor":        {1, 1, 1, 1},
	"systemPurpleColor": {0.57, 0.25, 0.67, 1},
}

// styled is a widget whose style context answers for the theme's colours,
// and colours the answers, until the look changes.
var (
	styled  uintptr
	colours = map[string]gdkRGBA{}
)

// system returns the colour of the name, as the theme has it.
func system(name string) gdkRGBA {
	if c, ok := colours[name]; ok {
		return c
	}
	c, ok := fixedColours[name]
	if s, named := systemColours[name]; !ok && named {
		c = gdkRGBA{0.5, 0.5, 0.5, 1}
		if styled != 0 {
			gtkStyleLookupColor(gtkWidgetStyle(styled), s.name, &c)
		}
		c.A *= float32(s.alpha)
	}
	colours[name] = c
	return c
}

func setColour(name string) {
	c := system(name)
	cairoSetRGBA(cr, float64(c.R), float64(c.G), float64(c.B), float64(c.A))
}

func setRGB(c theme.RGB) {
	cairoSetRGBA(cr, float64(c>>16&0xff)/255, float64(c>>8&0xff)/255, float64(c&0xff)/255, 1)
}

// fill fills r with the system's colour of the name.
func fill(colour string, r rect) {
	setColour(colour)
	cairoRectangle(cr, r.Origin.X, r.Origin.Y, r.Size.W, r.Size.H)
	cairoFill(cr)
}

// fillRGB fills r with c.
func fillRGB(c theme.RGB, r rect) {
	setRGB(c)
	cairoRectangle(cr, r.Origin.X, r.Origin.Y, r.Size.W, r.Size.H)
	cairoFill(cr)
}

// fillRounded fills r with corners of the radius, in the system's colour
// of the name.
func fillRounded(colour string, r rect, radius float64) {
	setColour(colour)
	rounded(r, radius)
	cairoFill(cr)
}

// rounded makes r with corners of the radius the path.
func rounded(r rect, radius float64) {
	x, y, w, h := r.Origin.X, r.Origin.Y, r.Size.W, r.Size.H
	radius = min(radius, w/2, h/2)
	cairoNewSubPath(cr)
	cairoArc(cr, x+w-radius, y+radius, radius, -math.Pi/2, 0)
	cairoArc(cr, x+w-radius, y+h-radius, radius, 0, math.Pi/2)
	cairoArc(cr, x+radius, y+h-radius, radius, math.Pi/2, math.Pi)
	cairoArc(cr, x+radius, y+radius, radius, math.Pi, 3*math.Pi/2)
	cairoClosePath(cr)
}

// clip limits drawing to r until the matching unclip.
func clip(r rect) {
	cairoSave(cr)
	cairoRectangle(cr, r.Origin.X, r.Origin.Y, r.Size.W, r.Size.H)
	cairoClip(cr)
}

func unclip() { cairoRestore(cr) }

// text draws text from at in font, in the colour set last, and returns how
// big it drew.
func text(s string, font uintptr, at point) size {
	l := pangoCairoLayout(cr)
	defer gObjectUnref(l)
	pangoLayoutSetFont(l, font)
	pangoLayoutSetText(l, s, -1)
	cairoMoveTo(cr, at.X, at.Y)
	pangoCairoShow(cr, l)
	var w, h int32
	pangoLayoutSize(l, &w, &h)
	return size{float64(w) / pangoScale, float64(h) / pangoScale}
}

// drawString draws text from at in st's font and colour, or in the
// system's colour of the name when colour is not "".
func drawString(s string, st theme.Style, colour string, at point) {
	if colour != "" {
		setColour(colour)
	} else {
		setRGB(st.Fg)
	}
	text(s, fontOf(st), at)
}

// wavy draws a wavy line in c from x0 to x1 along y, as a diagnostic's
// underline.
func wavy(c theme.RGB, x0, x1, y float64) {
	setRGB(c)
	cairoLineWidth(cr, 1)
	cairoMoveTo(cr, x0, y)
	for x, up := x0+2, true; x <= x1; x, up = x+2, !up {
		dy := 0.0
		if up {
			dy = -2
		}
		cairoLineTo(cr, x, y+dy)
	}
	cairoStroke(cr)
}

// slotGlyphs draw a tab's slot, by the system's symbol the shared code
// names it by.
var slotGlyphs = map[string]string{"circle.fill": "●", "xmark": "×"}

// size is how wide r draws, its room included.
func (r run) size() float64 {
	w := math.Ceil(measure(r.text, r.font()).W)
	if r.pill {
		w += 2 * pad
	}
	if r.slotted {
		w += slotW
	}
	if r.tight {
		return w
	}
	return w + pad
}

func (r run) font() uintptr {
	if r.bold {
		return uiBold
	}
	return uiFont
}

// draw draws r from x in a bar h tall from top, and returns where the next
// goes.
func (r run) draw(x, top, h float64) float64 {
	sz := measure(r.text, r.font())
	at := x
	if r.pill {
		if r.ground != "" {
			fillRounded(r.ground, rect{point{x, top + 4}, size{r.size() - pad, h - 8}}, 6)
		}
		at += pad
	}
	setColour(r.fg)
	text(r.text, r.font(), point{at, top + math.Floor((h-sz.H)/2)})
	if r.slotGround != "" {
		fillRounded(r.slotGround, slotAt(at+math.Ceil(sz.W), top, h), 4)
	}
	if g := slotGlyphs[r.slot]; g != "" {
		setColour(r.slotFg)
		gs := measure(g, uiFont)
		text(g, uiFont, point{at + math.Ceil(sz.W) + 4, top + math.Floor((h-gs.H)/2)})
	}
	return x + r.size()
}

// toastSize is how big a toast's text draws in lines w points wide.
func toastSize(s string, w float64) size {
	pangoLayoutSetWidth(measurer, int32(w*pangoScale))
	pangoLayoutSetWrap(measurer, 2)
	defer pangoLayoutSetWidth(measurer, -1)
	return measure(s, uiFont)
}
