package gui

import (
	"os"
	"path/filepath"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/theme"
)

// textSize is the text's size in points, which Command-= and Command-minus
// change (B558).
var textSize = 14.0

// makeFonts makes the text's fonts in textSize, and the cells they fill.
func makeFonts() {
	font = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), textSize, 0).Send(sel("retain"))
	boldFont = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), textSize, 0.4).Send(sel("retain"))
	fonts := cls("NSFontManager").Send(sel("sharedFontManager"))
	italicFont = fonts.Send(sel("convertFont:toHaveTrait:"), font, 1).Send(sel("retain"))
	boldItalicFont = fonts.Send(sel("convertFont:toHaveTrait:"), boldFont, 1).Send(sel("retain"))
	cellW = msgSizeOf(nsString("M"), sel("sizeWithAttributes:"), textAttrs(font, 0)).W
	cellH = float64(int(msgFloat(font, sel("ascender")) - msgFloat(font, sel("descender")) + msgFloat(font, sel("leading")) + 0.999))
}

// sizeText makes the text points tall, from 8 to 40, and every window as
// many cells as it then holds.
func sizeText(points float64) {
	for _, f := range []objc.ID{font, boldFont, italicFont, boldItalicFont} {
		f.Send(sel("release"))
	}
	for k, a := range attrsOf {
		a.Send(sel("release"))
		delete(attrsOf, k)
	}
	textSize = min(max(points, 8), 40)
	makeFonts()
	for v, w := range windows {
		if v != w.view {
			continue
		}
		w.cellPx = cellPixels(w.win)
		resizeRoot(w.view, 0, size{})
		for _, p := range w.panes {
			p.text.Send(sel("setNeedsDisplay:"), 1)
		}
	}
}

// cellPixels is how many of the screen's pixels a cell of win takes.
func cellPixels(win objc.ID) [2]int {
	scale := msgFloat(win, sel("backingScaleFactor"))
	return [2]int{int(cellW * scale), int(cellH * scale)}
}

// The View menu's actions, which size the text of every window.
var sizing = []objc.MethodDef{
	{Cmd: sel("bigger:"), Fn: func(objc.ID, objc.SEL, objc.ID) { sizeText(textSize + 1) }},
	{Cmd: sel("smaller:"), Fn: func(objc.ID, objc.SEL, objc.ID) { sizeText(textSize - 1) }},
	{Cmd: sel("actualSize:"), Fn: func(objc.ID, objc.SEL, objc.ID) { sizeText(14) }},
}

// dress shows on w what the frame says of the session: the theme's light
// or dark look on the title bar and the sheets, a dot in the close button
// while a buffer shown has changes, and the current file's icon in the
// title bar (B558).
func (w *window) dress(f frame) {
	look := "NSAppearanceNameAqua"
	if dark(f.ground) {
		look = "NSAppearanceNameDarkAqua"
	}
	if look != w.appearance {
		w.appearance = look
		w.win.Send(sel("setAppearance:"), cls("NSAppearance").Send(sel("appearanceNamed:"), nsString(look)))
	}
	w.win.Send(sel("setDocumentEdited:"), f.edited)
	if f.file != w.file {
		w.file = f.file
		w.win.Send(sel("setRepresentedFilename:"), nsString(f.file))
	}
}

// dark reports whether c is a dark ground.
func dark(c theme.RGB) bool {
	r, g, b := float64(c>>16&0xff), float64(c>>8&0xff), float64(c&0xff)
	return 0.299*r+0.587*g+0.114*b < 128
}

// file is the absolute path of a window's file, or "" when it names none
// on disk.
func file(name string) string {
	path, err := filepath.Abs(name)
	if err != nil {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}
