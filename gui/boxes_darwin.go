package gui

import (
	"math"

	"github.com/ebitengine/purego/objc"
)

// box is a panel over a window's text, which shows the palette, the
// finder, a menu, a float or a toast: the system's translucent material
// with rounded corners and a shadow, and view on it, which draws what it
// shows.
type box struct {
	frame, view objc.ID
	*boxed
	scroll float64 // how far its list scrolled, in points
	seen   int     // the item its list selected last, to tell when that moves
	hover  point   // where the mouse is over it, X -1 when it is not
	id     int     // a float's window, which the mouse goes to, or 0
	w      *window
}

// newBox makes a hidden box over w's views (B559).
func newBox(w *window) *box {
	frame := msgInitView(cls("NSView").Send(sel("alloc")), sel("initWithFrame:"), rect{})
	frame.Send(sel("setWantsLayer:"), 1)
	shadow := cls("NSShadow").Send(sel("alloc")).Send(sel("init"))
	msgSetFloat(shadow, sel("setShadowBlurRadius:"), 18)
	msgSetSize(shadow, sel("setShadowOffset:"), size{0, -6})
	shadow.Send(sel("setShadowColor:"), msgColor(cls("NSColor"), sel("colorWithSRGBRed:green:blue:alpha:"), 0, 0, 0, 0.35))
	frame.Send(sel("setShadow:"), shadow)
	shadow.Send(sel("release"))
	back := material(popover, withinWindow)
	back.Send(sel("setWantsLayer:"), 1)
	msgSetFloat(back.Send(sel("layer")), sel("setCornerRadius:"), 10)
	back.Send(sel("layer")).Send(sel("setMasksToBounds:"), 1)
	back.Send(sel("setAutoresizingMask:"), 18)
	v := msgInitView(objc.ID(boxClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	v.Send(sel("setAutoresizingMask:"), 18)
	frame.Send(sel("addSubview:"), back)
	frame.Send(sel("addSubview:"), v)
	// It hears the mouse move over it, for its list's rows to light.
	track := msgInitTracking(cls("NSTrackingArea").Send(sel("alloc")), sel("initWithRect:options:owner:userInfo:"), rect{}, 0x283, v, 0)
	v.Send(sel("addTrackingArea:"), track)
	track.Send(sel("release"))
	back.Send(sel("release"))
	frame.Send(sel("setHidden:"), 1)
	v.Send(sel("setHidden:"), 1)
	w.view.Send(sel("addSubview:"), frame)
	b := &box{frame: frame, view: v, w: w, seen: -2, hover: point{X: -1}}
	boxes[v] = b
	return b
}

// show makes b show what, sized and placed in view, whose area under the
// title bar starts top points down. A palette goes in the upper middle,
// the finder in the middle and a float at its place. A toast goes in the
// area's top right corner, where it fades in (B149). A nil what hides b.
func (b *box) show(what *boxed, view objc.ID, top float64) {
	shown := b.boxed != nil
	b.boxed = what
	if what == nil {
		b.frame.Send(sel("setHidden:"), 1)
		b.view.Send(sel("setHidden:"), 1)
		b.scroll, b.seen, b.hover = 0, -2, point{X: -1}
		return
	}
	bounds := msgRect(view, sel("bounds")).Size
	var r rect
	if what.text != "" {
		// It wraps at half the window's width, and a third of its height
		// ends in an ellipsis.
		limit := size{max(bounds.W/2, 240) - 2*inset, max(bounds.H/3-2*inset, cellH)}
		need := msgBounding(nsString(what.text), sel("boundingRectWithSize:options:attributes:context:"), size{limit.W, math.MaxFloat32}, lineFragments, uiAttrs(toastFont, system("labelColor")), 0).Size
		r.Size = size{math.Ceil(min(need.W, limit.W)) + 2*inset, math.Ceil(min(need.H, limit.H)) + 2*inset}
		r.Origin = point{bounds.W - r.Size.W - 2*pad, top + 2*pad}
	} else if l := what.list; l != nil {
		switch {
		case what.middle:
			// The finder is as tall as its preview, and shows as many rows
			// as fit under its query.
			h := float64(max(len(what.cells), 12))*cellH + inputH + inset
			l.view = max(int((h-inputH-12)/l.rowH), 1)
			r.Size = size{l.width + 16, h}
			if len(what.cells) > 0 {
				r.Size.W += 12 + float64(len(what.cells[0]))*cellW + inset
			}
		case what.at == nil:
			l.width = min(620, bounds.W-80) - 16
			l.view = 10
			r.Size = size{l.width + 16, l.height() + 4}
		default:
			l.width = min(max(l.natural(), 160), 520)
			r.Size = size{l.width + 16, l.height()}
		}
		// The list keeps where it scrolled, and follows its selection.
		l.scroll = b.scroll
		l.follow(l.selected != b.seen)
		b.scroll, b.seen = l.scroll, l.selected
		r.Origin = point{math.Round((bounds.W - r.Size.W) / 2), math.Round(bounds.H / 5)}
		switch {
		case what.middle:
			r.Origin.Y = math.Round((bounds.H - r.Size.H) / 2)
		case what.at != nil:
			r.Origin.X = max(min(float64(what.at[0])*cellW, bounds.W-r.Size.W), 0)
			r.Origin.Y = max(min(top+float64(what.at[1])*cellH, bounds.H-r.Size.H), top)
		}
	} else {
		r.Size = size{float64(len(what.cells[0]))*cellW + 2*inset, float64(len(what.cells))*cellH + 2*inset + what.head() + what.foot()}
		r.Origin = point{math.Round((bounds.W - r.Size.W) / 2), math.Round(bounds.H / 5)}
		switch {
		case what.middle:
			r.Origin.Y = math.Round((bounds.H - r.Size.H) / 2)
		case what.at != nil:
			// It stays in the area.
			r.Origin.X = max(min(float64(what.at[0])*cellW-inset, bounds.W-r.Size.W), 0)
			r.Origin.Y = max(min(top+float64(what.at[1])*cellH-inset-what.head(), bounds.H-r.Size.H), top)
		}
	}
	msgSetRect(b.frame, sel("setFrame:"), r)
	for _, v := range subviews(b.frame) {
		msgSetRect(v, sel("setFrame:"), rect{Size: r.Size})
	}
	if !shown {
		if what.text != "" {
			msgSetFloat(b.frame, sel("setAlphaValue:"), 0)
			msgSetFloat(b.frame.Send(sel("animator")), sel("setAlphaValue:"), 1)
		}
		b.frame.Send(sel("setHidden:"), 0)
		b.view.Send(sel("setHidden:"), 0)
	}
	b.view.Send(sel("setNeedsDisplay:"), 1)
}

// promptSymbols are the system's symbols for a command line's prompts.
var promptSymbols = map[string]string{"command": "chevron.right", "search": "magnifyingglass"}

// The options that lay a toast's text out in lines, the last one cut with
// an ellipsis.
const lineFragments = 1 | 1<<5

// drawBox draws what a box shows on its material: a toast's text in the
// system's font and colour, or cells with no ground of their own, the
// rows a menu selected in the system's selection, and its rules and
// symbols in the system's colours.
func drawBox(self objc.ID, _ objc.SEL, _ rect) {
	b := boxes[self]
	if b == nil || b.boxed == nil {
		return
	}
	if b.text != "" {
		r := msgRect(self, sel("bounds"))
		r = rect{point{inset, inset}, size{r.Size.W - 2*inset, r.Size.H - 2*inset}}
		msgDrawIn(nsString(b.text), sel("drawWithRect:options:attributes:context:"), r, lineFragments, uiAttrs(toastFont, system("labelColor")), 0)
		return
	}
	bounds := msgRect(self, sel("bounds")).Size
	if l := b.list; l != nil {
		l.hovered = -1
		if i, ok := l.itemAt(b.hover); ok && b.hover.X >= 0 {
			l.hovered = i
		}
		l.draw(8)
		if b.prompt != "" {
			drawSymbol(symbol(promptSymbols[b.prompt], system("secondaryLabelColor")), rect{point{14, (inputH - 16) / 2}, size{16, 16}})
		}
		if len(b.cells) == 0 {
			return
		}
		// The finder's preview goes right of its list, past a hairline.
		system("separatorColor").Send(sel("setFill"))
		rectFill(rect{point{b.cellsOrigin().X - 6, inputH}, size{1, bounds.H - inputH}})
	} else if b.title != "" {
		run{text: b.title, font: uiFont, fg: system("secondaryLabelColor")}.draw(inset, inset/2, head)
	}
	origin := b.cellsOrigin()
	system("selectedContentBackgroundColor").Send(sel("setFill"))
	for _, c := range b.chosen {
		r := at(c.x, c.y, c.w, origin)
		msgRounded(cls("NSBezierPath"), sel("bezierPathWithRoundedRect:xRadius:yRadius:"), rect{point{r.Origin.X - 4, r.Origin.Y}, size{r.Size.W + 8, r.Size.H}}, 5, 5).Send(sel("fill"))
	}
	ground := b.look.Base.Bg
	drawCells(b.cells, b.cursor, origin, &ground, b.chosen)
	b.w.drawPics(b.pics, origin)
	system("separatorColor").Send(sel("setFill"))
	for _, ru := range b.rules {
		r := at(ru.x, ru.y, ru.w, origin)
		rectFill(rect{point{r.Origin.X, r.Origin.Y + math.Floor(cellH/2)}, size{r.Size.W, 1}})
	}
	if f := b.foot(); f > 0 {
		drawParts(b.left, b.right, true, rect{point{0, bounds.H - f}, size{bounds.W, f}})
	}
}

// ask shows q in a sheet on the window. Its buttons and the keys y and n
// answer it as y and n do in the window (B362). apply closes the sheet
// when the question goes away unanswered.
func (w *window) ask(q string) {
	alert := cls("NSAlert").Send(sel("alloc")).Send(sel("init"))
	alert.Send(sel("setMessageText:"), nsString(q))
	for _, b := range [][2]string{{"Yes", "y"}, {"No", "n"}} {
		alert.Send(sel("addButtonWithTitle:"), nsString(b[0])).Send(sel("setKeyEquivalent:"), nsString(b[1]))
	}
	var done objc.Block
	done = objc.NewBlock(func(_ objc.Block, answer int) {
		switch answer {
		case 1000:
			w.keys <- "y"
		case 1001:
			w.keys <- "n"
		}
		if w.sheet == alert {
			w.sheet = 0
		}
		alert.Send(sel("release"))
		done.Release()
	})
	w.sheet = alert
	alert.Send(sel("beginSheetModalForWindow:completionHandler:"), w.win, done)
}
