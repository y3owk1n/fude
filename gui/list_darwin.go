package gui

import (
	"math"
	"path/filepath"
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"

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

// draw draws l in its box from x, with its rows w wide.
func (l *list) draw(x float64) {
	w := l.width
	if l.typing {
		l.drawInput(x, w)
	}
	y, h := l.top(), float64(l.shown())*l.rowH
	ctx := cls("NSGraphicsContext").Send(sel("currentContext"))
	ctx.Send(sel("saveGraphicsState"))
	defer ctx.Send(sel("restoreGraphicsState"))
	msgSetRect(cls("NSBezierPath"), sel("clipRect:"), rect{point{x, y}, size{w, h}})
	// A list taller than its rows shows a thin knob where its rows are.
	if all := float64(len(l.items)) * l.rowH; all > h {
		system("tertiaryLabelColor").Send(sel("setFill"))
		msgRounded(cls("NSBezierPath"), sel("bezierPathWithRoundedRect:xRadius:yRadius:"), rect{point{x + w - 4, y + l.scroll/all*h}, size{3, max(h*h/all, 16)}}, 1.5, 1.5).Send(sel("fill"))
		w -= 8
	}
	first := int(l.scroll / l.rowH)
	for i := first; i < min(len(l.items), first+l.shown()+1); i++ {
		it := l.items[i]
		r := rect{point{x, y + float64(i)*l.rowH - l.scroll}, size{w, l.rowH}}
		label, detail := system("labelColor"), system("secondaryLabelColor")
		if i == l.hovered && i != l.selected {
			system("quaternaryLabelColor").Send(sel("setFill"))
			msgRounded(cls("NSBezierPath"), sel("bezierPathWithRoundedRect:xRadius:yRadius:"), r, 6, 6).Send(sel("fill"))
		}
		if i == l.selected {
			system("selectedContentBackgroundColor").Send(sel("setFill"))
			msgRounded(cls("NSBezierPath"), sel("bezierPathWithRoundedRect:xRadius:yRadius:"), r, 6, 6).Send(sel("fill"))
			label, detail = system("alternateSelectedControlTextColor"), system("alternateSelectedControlTextColor")
		}
		if it.Disabled {
			label = system("tertiaryLabelColor")
		}
		font := listFont
		if it.Header {
			font, label = listBold, system("secondaryLabelColor")
		}
		at := r.Origin.X + 8
		if icon := iconOf(it, label); icon != 0 {
			drawSymbol(icon, rect{point{at, r.Origin.Y + (l.rowH-16)/2}, size{16, 16}})
			at += 24
		}
		if it.Line > 0 {
			n := nsString(strconv.Itoa(it.Line))
			a := uiAttrs(listFont, detail)
			msgDrawAt(n, sel("drawAtPoint:withAttributes:"), point{at, r.Origin.Y + (l.rowH-msgSizeOf(n, sel("sizeWithAttributes:"), a).H)/2}, a)
			at += 36
		}
		end := r.Origin.X + w - 10
		if it.Detail != "" {
			d := nsString(it.Detail)
			a := uiAttrs(listFont, detail)
			sz := msgSizeOf(d, sel("sizeWithAttributes:"), a)
			dx := max(end-sz.W, at+80)
			msgDrawIn(d, sel("drawWithRect:options:attributes:context:"), rect{point{dx, r.Origin.Y + (l.rowH-sz.H)/2}, size{end - dx, sz.H}}, lineFragments, a, 0)
			end = dx - 12
		}
		s := marked(it, font, label)
		sz := msgSize(s, sel("size"))
		msgDrawAttrIn(s, sel("drawWithRect:options:context:"), rect{point{at, r.Origin.Y + (l.rowH-sz.H)/2}, size{max(end-at, 0), sz.H}}, lineFragments, 0)
		s.Send(sel("release"))
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
