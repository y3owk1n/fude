package gui

import (
	"math"
	"path/filepath"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/wire"
)

// natural is how wide l's rows would be to show every item whole.
func (l *list) natural() float64 {
	w := 0.0
	for _, it := range l.items {
		lw := msgSizeOf(nsString(it.Label), sel("sizeWithAttributes:"), uiAttrs(listFont, system("labelColor"))).W
		dw := 0.0
		if it.Detail != "" {
			dw = msgSizeOf(nsString(it.Detail), sel("sizeWithAttributes:"), uiAttrs(listFont, system("labelColor"))).W + 24
		}
		w = max(w, 8+16+8+lw+dw+12)
	}
	return math.Ceil(w)
}

// draw draws the row typed into of l in its box from x. The system's table
// under it shows the items (D68).
func (l *list) draw(x float64) {
	if l.typing {
		l.drawInput(x, l.width)
	}
}

// drawInput draws the row typed into: the prompt's symbol, what is typed
// in the system's font with the cursor in it, and the note at its right.
func (l *list) drawInput(x, w float64) {
	a := uiAttrs(inputFont, system("labelColor"))
	text := nsString(l.input)
	sz := msgSizeOf(nsString(l.input+" "), sel("sizeWithAttributes:"), a)
	before := msgSizeOf(nsString(l.input[:min(l.at, len(l.input))]), sel("sizeWithAttributes:"), a).W
	end := x + w - 8
	if l.note != "" {
		n := nsString(l.note)
		na := uiAttrs(listFont, system("tertiaryLabelColor"))
		ns := msgSizeOf(n, sel("sizeWithAttributes:"), na)
		end -= ns.W
		msgDrawAt(n, sel("drawAtPoint:withAttributes:"), point{end, (inputH - ns.H) / 2}, na)
		end -= 12
	}
	at := x + 32
	// A line longer than the row shows its end, where the cursor usually is.
	shift := max(before-(end-at), 0)
	y := (inputH - sz.H) / 2
	ctx := cls("NSGraphicsContext").Send(sel("currentContext"))
	ctx.Send(sel("saveGraphicsState"))
	msgSetRect(cls("NSBezierPath"), sel("clipRect:"), rect{point{at, 0}, size{max(end-at, 0), inputH}})
	msgDrawAt(text, sel("drawAtPoint:withAttributes:"), point{at - shift, y}, a)
	ctx.Send(sel("restoreGraphicsState"))
	l.caret = point{at - shift + before, y}
	system("controlAccentColor").Send(sel("setFill"))
	rectFill(rect{l.caret, size{2, sz.H}})
	system("separatorColor").Send(sel("setFill"))
	rectFill(rect{point{x, inputH}, size{w, 1}})
}

// marked is an item's label in font and colour, its marks bold.
func marked(it wire.Item, font, colour objc.ID) objc.ID {
	s := cls("NSMutableAttributedString").Send(sel("alloc")).Send(sel("initWithString:attributes:"), nsString(it.Label), uiAttrs(font, colour))
	bold := uiAttrs(listBold, colour)
	for _, at := range it.Marks {
		if at < 0 || at >= len(it.Label) {
			continue
		}
		from := len(utf16.Encode([]rune(it.Label[:at])))
		_, n := utf8.DecodeRuneInString(it.Label[at:])
		msgAddAttrs(s, sel("addAttributes:range:"), bold, nsRange{uint(from), uint(len(utf16.Encode([]rune(it.Label[at : at+n]))))})
	}
	return s
}

// iconOf is an item's icon: a file's own, as the Finder shows it, or the
// system's symbol for its kind, or 0 for none.
func iconOf(it wire.Item, colour objc.ID) objc.ID {
	if it.Path != "" && (it.Kind == "file" || it.Kind == "folder") {
		return fileIcon(it.Path)
	}
	if name, ok := kindSymbols[it.Kind]; ok {
		return symbol(name, colour)
	}
	return 0
}

// kindSymbols are the system's symbols for the kinds of item, by kind.
var kindSymbols = map[string]string{
	"file": "doc", "folder": "folder", "function": "function", "method": "function",
	"variable": "x.squareroot", "field": "square.stack.3d.down.right", "type": "t.square",
	"module": "shippingbox", "keyword": "textformat", "snippet": "curlybraces",
	"text": "text.alignleft", "constant": "number", "buffer": "doc.text", "path": "folder",
}

// icons are files' icons, by path, which the Finder's look up reads the
// disk for.
var icons = map[string]objc.ID{}

// fileIcon returns the icon the Finder shows for the file at path.
func fileIcon(path string) objc.ID {
	if img, ok := icons[path]; ok {
		return img
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	img := cls("NSWorkspace").Send(sel("sharedWorkspace")).Send(sel("iconForFile:"), nsString(abs)).Send(sel("retain"))
	icons[path] = img
	return img
}
