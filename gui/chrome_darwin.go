package gui

import (
	"github.com/ebitengine/purego/objc"
	"github.com/y3owk1n/fude/frontend/theme"
)

// The system's colours, symbols and text attributes, made once each by
// name. A colour is the system's own, which takes the look of the view
// that draws it, so it is drawn only in drawRect.
var (
	colours   = map[string]objc.ID{}
	symbols   = map[[2]objc.ID]objc.ID{} // by name and colour
	uiAttrsOf = map[[2]objc.ID]objc.ID{} // by font and colour
)

// system returns the system's colour of the name NSColor gives it, such as
// labelColor.
func system(name string) objc.ID {
	if c, ok := colours[name]; ok {
		return c
	}
	c := cls("NSColor").Send(sel(name)).Send(sel("retain"))
	colours[name] = c
	return c
}

// symbol returns the system's symbol of the name, in colour.
func symbol(name string, colour objc.ID) objc.ID {
	k := [2]objc.ID{objc.ID(sel(name)), colour}
	if s, ok := symbols[k]; ok {
		return s
	}
	img := cls("NSImage").Send(sel("imageWithSystemSymbolName:accessibilityDescription:"), nsString(name), 0)
	cfg := cls("NSImageSymbolConfiguration").Send(sel("configurationWithHierarchicalColor:"), colour)
	s := img.Send(sel("imageWithSymbolConfiguration:"), cfg).Send(sel("retain"))
	symbols[k] = s
	return s
}

// drawSymbol draws a symbol in r, fit to its height and centred.
func drawSymbol(s objc.ID, r rect) {
	sz := msgSize(s, sel("size"))
	if sz.H <= 0 {
		return
	}
	scale := min(r.Size.H/sz.H, r.Size.W/sz.W)
	w, h := sz.W*scale, sz.H*scale
	msgDrawImage(s, sel("drawInRect:fromRect:operation:fraction:respectFlipped:hints:"), rect{point{r.Origin.X + (r.Size.W-w)/2, r.Origin.Y + (r.Size.H-h)/2}, size{w, h}}, rect{}, 2, 1, true, 0)
}

// uiAttrs returns the text attributes of font in a colour of the system's.
func uiAttrs(font, colour objc.ID) objc.ID {
	k := [2]objc.ID{font, colour}
	if a, ok := uiAttrsOf[k]; ok {
		return a
	}
	a := cls("NSMutableDictionary").Send(sel("alloc")).Send(sel("init"))
	a.Send(sel("setObject:forKey:"), font, attrFont)
	a.Send(sel("setObject:forKey:"), colour, attrColor)
	uiAttrsOf[k] = a
	return a
}

// popover is NSVisualEffectView's material of popovers, and withinWindow
// its blending with what is under it in the window.
const (
	popover      = 6
	withinWindow = 1
)

// material makes a view of the system's translucent material, blended
// with what is behind the window or with what is under it in the window.
func material(kind, blend int) objc.ID {
	v := msgInitView(cls("NSVisualEffectView").Send(sel("alloc")), sel("initWithFrame:"), rect{})
	v.Send(sel("setMaterial:"), kind)
	v.Send(sel("setBlendingMode:"), blend)
	v.Send(sel("setState:"), 0)
	return v
}

// fill fills r with the system's colour of the name.
func fill(colour string, r rect) {
	system(colour).Send(sel("setFill"))
	rectFill(r)
}

// fillRounded fills r with corners of the radius, in the system's colour
// of the name.
func fillRounded(colour string, r rect, radius float64) {
	system(colour).Send(sel("setFill"))
	msgRounded(cls("NSBezierPath"), sel("bezierPathWithRoundedRect:xRadius:yRadius:"), r, radius, radius).Send(sel("fill"))
}

// fillRGB fills r with c.
func fillRGB(c theme.RGB, r rect) {
	nsColor(c).Send(sel("setFill"))
	rectFill(r)
}

// drawString draws text from at in st's font and colour, or in the
// system's colour of the name when colour is not "".
func drawString(text string, st theme.Style, colour string, at point) {
	a := attrs(st)
	if colour != "" {
		a = uiAttrs(fontOf(st), system(colour))
	}
	msgDrawAt(nsString(text), sel("drawAtPoint:withAttributes:"), at, a)
}

// wavy draws a wavy line in c from x0 to x1 along y, as a diagnostic's
// underline.
func wavy(c theme.RGB, x0, x1, y float64) {
	nsColor(c).Send(sel("setStroke"))
	path := cls("NSBezierPath").Send(sel("bezierPath"))
	msgPointArg(path, sel("moveToPoint:"), point{x0, y})
	for x, up := x0+2, true; x <= x1; x, up = x+2, !up {
		dy := 0.0
		if up {
			dy = -2
		}
		msgPointArg(path, sel("lineToPoint:"), point{x, y + dy})
	}
	path.Send(sel("stroke"))
}
