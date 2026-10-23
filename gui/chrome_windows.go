package gui

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/y3owk1n/fude/frontend/theme"
)

// The fonts, as DirectWrite's text formats, which makeFonts makes as the
// application starts. The text is monospace, and the chrome takes the
// system's message font.
var (
	font, boldFont, italicFont, boldItalicFont unsafe.Pointer
	uiFont, uiBold, inputFont                  unsafe.Pointer
	textFamily                                 string
)

// rt is the render target that paint draws on, and brush its brush. Only
// the main thread uses them.
var rt, brush unsafe.Pointer

// textSize is the text's size in points, which Control with =, minus and
// 0 change.
var textSize = 11.0

// dips converts points to DirectWrite's units, 96 to an inch.
func dips(points float64) float64 { return points * 96 / 72 }

// newFormat makes a text format that does not wrap. style is 0, or 2 for
// italic, and size is in DirectWrite's units.
func newFormat(family string, weight, style uint32, size float64) unsafe.Pointer {
	create := bind[func(this unsafe.Pointer, family *uint16, collection uintptr, weight, style, stretch uint32, size float32, locale *uint16, out *unsafe.Pointer) uintptr](dw, 15)
	var f unsafe.Pointer
	create(dw, cwide(family), 0, weight, style, 5, float32(size), cwide(""), &f)
	com(f, 5, 1)
	return f
}

// makeTextFonts makes the text's fonts in textSize, and sizes a cell in
// them.
func makeTextFonts() {
	size := dips(textSize)
	font, boldFont = newFormat(textFamily, 400, 0, size), newFormat(textFamily, 700, 0, size)
	italicFont, boldItalicFont = newFormat(textFamily, 400, 2, size), newFormat(textFamily, 700, 2, size)
	m := measure("M", font)
	cellW, cellH = m.W, math.Ceil(m.H)
}

// logFont is a LOGFONTW, and ncMetrics a NONCLIENTMETRICSW.
type (
	logFont struct {
		height, width, escapement, orientation, weight int32
		italic, underline, strike, charset             byte
		out, clip, quality, pitch                      byte
		face                                           [32]uint16
	}
	ncMetrics struct {
		size                                         uint32
		border, scrollW, scrollH, captionW, captionH int32
		caption                                      logFont
		smCaptionW, smCaptionH                       int32
		smCaption                                    logFont
		menuW, menuH                                 int32
		menu, status, message                        logFont
		paddedBorder                                 int32
	}
)

// makeFonts makes the fonts: the text's in Cascadia Mono, or Consolas
// where it is missing, the chrome's in the system's message font, and the
// window's buttons' in its icon font, Windows 11's or else Windows 10's.
func makeFonts() {
	var collection unsafe.Pointer
	com(dw, 3, uintptr(unsafe.Pointer(&collection)), 0)
	defer release(collection)
	// first is the first of the families the system has.
	first := func(families ...string) string {
		for _, f := range families[:len(families)-1] {
			var index uint32
			var exists int32
			if com(collection, 5, uintptr(unsafe.Pointer(cwide(f))), uintptr(unsafe.Pointer(&index)), uintptr(unsafe.Pointer(&exists))) == 0 && exists != 0 {
				return f
			}
		}
		return families[len(families)-1]
	}
	textFamily = first("Cascadia Mono", "Consolas")
	captionFont = newFormat(first("Segoe Fluent Icons", "Segoe MDL2 Assets"), 400, 0, 10)
	ui, size := "Segoe UI", 12.0
	m := ncMetrics{}
	m.size = uint32(unsafe.Sizeof(m))
	if call(systemParams, 0x29, uintptr(m.size), uintptr(unsafe.Pointer(&m)), 0, 96) != 0 {
		ui, size = windows.UTF16ToString(m.message.face[:]), float64(-m.message.height)
	}
	uiFont, uiBold = newFormat(ui, 400, 0, size), newFormat(ui, 700, 0, size)
	inputFont = newFormat(ui, 400, 0, dips(14))
	makeTextFonts()
}

// sizeText makes the text points tall, from 6 to 40, and every window as
// many cells as it then holds.
func sizeText(points float64) {
	for _, f := range []unsafe.Pointer{font, boldFont, italicFont, boldItalicFont} {
		release(f)
	}
	textSize = min(max(points, 6), 40)
	makeTextFonts()
	for _, w := range opened {
		w.cellPx = [2]int{int(cellW * w.scale), int(cellH * w.scale)}
		w.arrange()
	}
}

// layoutOf makes a text layout of s in font, at most w by h.
func layoutOf(s string, font unsafe.Pointer, w, h float64) unsafe.Pointer {
	create := bind[func(this unsafe.Pointer, text *uint16, n uint32, format unsafe.Pointer, w, h float32, out *unsafe.Pointer) uintptr](dw, 18)
	u := wide(s)
	n := len(u)
	if s == "" {
		n = 0
	}
	var l unsafe.Pointer
	create(dw, &u[0], uint32(n), font, float32(w), float32(h), &l)
	return l
}

// metrics is how big l lays its text out.
func metrics(l unsafe.Pointer) size {
	var m textMetrics
	com(l, 60, uintptr(unsafe.Pointer(&m)))
	return size{float64(m.widthTrailing), float64(m.height)}
}

// measure is how big text draws in font.
func measure(text string, font unsafe.Pointer) size {
	l := layoutOf(text, font, 1e6, 1e6)
	defer release(l)
	return metrics(l)
}

// fontOf is the text's font a style takes.
func fontOf(s theme.Style) unsafe.Pointer {
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

// tone is the colour the system shows for a name that the shared code
// gives as AppKit does. It has light and dark colours, or the accent, and
// how opaque it draws. Windows names only the accent, so the others are
// fixed.
type tone struct {
	light, dark theme.RGB
	alpha       float64
	accent      bool
}

var systemColours = map[string]tone{
	"labelColor":                        {light: 0x1b1b1b, dark: 0xffffff, alpha: 1},
	"secondaryLabelColor":               {light: 0x1b1b1b, dark: 0xffffff, alpha: 0.62},
	"tertiaryLabelColor":                {light: 0x1b1b1b, dark: 0xffffff, alpha: 0.4},
	"quaternaryLabelColor":              {light: 0x1b1b1b, dark: 0xffffff, alpha: 0.09},
	"separatorColor":                    {light: 0xe0e0e0, dark: 0x3d3d3d, alpha: 1},
	"controlAccentColor":                {alpha: 1, accent: true},
	"selectedContentBackgroundColor":    {alpha: 1, accent: true},
	"linkColor":                         {alpha: 1, accent: true},
	"systemBlueColor":                   {alpha: 1, accent: true},
	"alternateSelectedControlTextColor": {light: 0xffffff, dark: 0xffffff, alpha: 1},
	"whiteColor":                        {light: 0xffffff, dark: 0xffffff, alpha: 1},
	"systemRedColor":                    {light: 0xc42b1c, dark: 0xff6b5e, alpha: 1},
	"systemOrangeColor":                 {light: 0xca5010, dark: 0xf7894a, alpha: 1},
	"systemYellowColor":                 {light: 0x9d5d00, dark: 0xfce100, alpha: 1},
	"systemGreenColor":                  {light: 0x0f7b0f, dark: 0x6ccb5f, alpha: 1},
	"systemPurpleColor":                 {light: 0x8764b8, dark: 0xb4a0ff, alpha: 1},
	"popoverColor":                      {light: 0xf9f9f9, dark: 0x2c2c2c, alpha: 1},
	"barColor":                          {light: 0xf3f3f3, dark: 0x202020, alpha: 1},
}

// darkLook is whether the system's colours are the dark ones, as the
// theme's ground is.
var darkLook bool

// accent is the system's accent colour, from the window frame's.
func accent() theme.RGB {
	var argb uint32
	var opaque int32
	if call(dwmColorization, uintptr(unsafe.Pointer(&argb)), uintptr(unsafe.Pointer(&opaque))) != 0 {
		return 0x0067c0
	}
	return theme.RGB(argb & 0xffffff)
}

// system returns the colour of the name, in the look the windows have.
func system(name string) colorF {
	t := systemColours[name]
	c := t.light
	if darkLook {
		c = t.dark
	}
	if t.accent {
		c = accent()
	}
	return rgba(c, t.alpha)
}

// rgba is c as Direct2D's colour, alpha opaque.
func rgba(c theme.RGB, alpha float64) colorF {
	return colorF{float32(c>>16&0xff) / 255, float32(c>>8&0xff) / 255, float32(c&0xff) / 255, float32(alpha)}
}

func setColour(name string) {
	c := system(name)
	com(brush, 8, uintptr(unsafe.Pointer(&c)))
}

func setRGB(c theme.RGB) {
	f := rgba(c, 1)
	com(brush, 8, uintptr(unsafe.Pointer(&f)))
}

// fill fills r with the system's colour of the name.
func fill(colour string, r rect) {
	setColour(colour)
	com(rt, 17, uintptr(unsafe.Pointer(rf(r))), uintptr(brush))
}

// fillRGB fills r with c.
func fillRGB(c theme.RGB, r rect) {
	setRGB(c)
	com(rt, 17, uintptr(unsafe.Pointer(rf(r))), uintptr(brush))
}

// fillRounded fills r with corners of the radius, in the system's colour
// of the name.
func fillRounded(colour string, r rect, radius float64) {
	setColour(colour)
	radius = min(radius, r.Size.W/2, r.Size.H/2)
	rr := roundedF{*rf(r), float32(radius), float32(radius)}
	com(rt, 19, uintptr(unsafe.Pointer(&rr)), uintptr(brush))
}

// clip limits drawing to r until the matching unclip.
func clip(r rect) { com(rt, 45, uintptr(unsafe.Pointer(rf(r))), 1) }

func unclip() { com(rt, 46) }

// draw draws the layout l from at, in the colour set last.
func draw(l unsafe.Pointer, at point) {
	drawLayout(rt, float32(at.X), float32(at.Y), l, brush)
}

// text draws s from at in font, in the colour set last, and returns how
// big it drew.
func text(s string, font unsafe.Pointer, at point) size {
	l := layoutOf(s, font, 1e6, 1e6)
	defer release(l)
	draw(l, at)
	return metrics(l)
}

// drawString draws text from at in st's font and colour, or in the
// system's colour of the name when colour is not "". A colour font, such
// as an emoji's, keeps its colours.
func drawString(s string, st theme.Style, colour string, at point) {
	if colour != "" {
		setColour(colour)
	} else {
		setRGB(st.Fg)
	}
	u := wide(s)
	r := rectF{float32(at.X), float32(at.Y), float32(at.X) + 1e5, float32(at.Y) + 1e5}
	com(rt, 27, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)), uintptr(fontOf(st)), uintptr(unsafe.Pointer(&r)), uintptr(brush), 4, 0)
}

// wavy draws a wavy line in c from x0 to x1 along y, as a diagnostic's
// underline, a point at a time.
func wavy(c theme.RGB, x0, x1, y float64) {
	setRGB(c)
	for x := x0; x < x1; x++ {
		dy := math.Abs(math.Mod(x-x0, 4)-2) - 2
		com(rt, 17, uintptr(unsafe.Pointer(rf(rect{point{x, y + dy}, size{1, 1}}))), uintptr(brush))
	}
}

// slotGlyphs draw a tab's slot, by the system's symbol the shared code
// names it by.
var slotGlyphs = map[string]string{"circle.fill": "●", "xmark": "✕"}

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

func (r run) font() unsafe.Pointer {
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
			fillRounded(r.ground, rect{point{x, top + 4}, size{r.size() - pad, h - 8}}, 4)
		}
		at += pad
	}
	setColour(r.fg)
	text(r.text, r.font(), point{at, top + math.Floor((h-sz.H)/2)})
	if g := slotGlyphs[r.slot]; g != "" {
		setColour(r.slotFg)
		gs := measure(g, uiFont)
		text(g, uiFont, point{at + math.Ceil(sz.W) + 4, top + math.Floor((h-gs.H)/2)})
	}
	return x + r.size()
}

// wrapped makes a layout of s in font that wraps at w points, and cuts the
// line past h points with an ellipsis.
func wrapped(s string, font unsafe.Pointer, w, h float64) unsafe.Pointer {
	l := layoutOf(s, font, w, h)
	com(l, 5, 0)
	trim(l, font)
	return l
}

// toastSize is how big a toast's text draws in lines w points wide.
func toastSize(s string, w float64) size {
	l := layoutOf(s, uiFont, w, 1e6)
	defer release(l)
	com(l, 5, 0)
	return metrics(l)
}

// signs are the ellipses DirectWrite cuts text with, by font.
var signs = map[unsafe.Pointer]unsafe.Pointer{}

// trim cuts what of l's text does not fit with an ellipsis, in font.
func trim(l, font unsafe.Pointer) {
	sign := signs[font]
	if sign == nil {
		com(dw, 20, uintptr(font), uintptr(unsafe.Pointer(&sign)))
		signs[font] = sign
	}
	trimming := [3]uint32{1, 0, 0}
	com(l, 9, uintptr(unsafe.Pointer(&trimming)), uintptr(sign))
}
