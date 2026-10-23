package gui

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"unsafe"

	"github.com/ebitengine/purego"
)

// linkKey is the modifier that makes the word under the mouse a link,
// Control (B563).
const linkKey = controlKey

// The modifier bits of a GdkModifierType.
const (
	gdkShift   = 1
	gdkControl = 1 << 2
	gdkAlt     = 1 << 3
	gdkSuper   = 1 << 26
)

// heldOf is the modifiers a GdkModifierType holds, as chord counts them.
func heldOf(state uintptr) int {
	held := 0
	for _, m := range [][2]uintptr{{gdkShift, shiftKey}, {gdkControl, controlKey}, {gdkAlt, altKey}, {gdkSuper, superKey}} {
		if state&m[0] != 0 {
			held |= int(m[1])
		}
	}
	return held
}

// wheelLines is how many lines a wheel's notch scrolls a window, as GTK's
// own text views scroll.
const wheelLines = 3

// The callbacks of the mouse, made once with the others.
var mouseCallbacks struct {
	pressed, released, motion, leave, scroll, scrollEnd, context, closed, pasted uintptr
}

// makeMouseCallbacks makes the mouse's callbacks.
func makeMouseCallbacks() {
	mouseCallbacks.pressed = purego.NewCallback(pressed)
	mouseCallbacks.released = purego.NewCallback(released)
	mouseCallbacks.motion = purego.NewCallback(motion)
	mouseCallbacks.leave = purego.NewCallback(leave)
	mouseCallbacks.scroll = purego.NewCallback(scrolled)
	mouseCallbacks.scrollEnd = purego.NewCallback(func(ctl, area uintptr) uintptr {
		if p := panes[area]; p != nil && gtkScrollUnit(ctl) == 1 {
			p.smooth(0, true)
		}
		return 0
	})
	mouseCallbacks.context = purego.NewCallback(contextChosen)
	mouseCallbacks.closed = purego.NewCallback(func(popover, area uintptr) uintptr {
		// The menu goes once its item's action has run, unless its pane
		// went first and removed it.
		onMain(func() {
			if p := panes[area]; p != nil && p.menu == popover {
				p.menu = 0
				gtkWidgetUnparent(popover)
			}
		}, false)
		return 0
	})
	mouseCallbacks.pasted = purego.NewCallback(pasted)
}

// listen gives a pane's or a box's area to the mouse: its buttons, its
// moves and its wheel or touchpad.
func listen(area uintptr) {
	click := gtkGestureClickNew()
	gtkGestureSetButton(click, 0)
	gSignalConnectData(click, "pressed", mouseCallbacks.pressed, area, 0, 0)
	gSignalConnectData(click, "released", mouseCallbacks.released, area, 0, 0)
	gtkWidgetAddControl(area, click)
	move := gtkMotionNew()
	gSignalConnectData(move, "motion", mouseCallbacks.motion, area, 0, 0)
	gSignalConnectData(move, "leave", mouseCallbacks.leave, area, 0, 0)
	gtkWidgetAddControl(area, move)
	roll := gtkScrollNew(1)
	gSignalConnectData(roll, "scroll", mouseCallbacks.scroll, area, 0, 0)
	gSignalConnectData(roll, "scroll-end", mouseCallbacks.scrollEnd, area, 0, 0)
	gtkWidgetAddControl(area, roll)
}

// The mouse's callbacks take what GTK gives and pass it on, in points of
// the area and the modifiers chord counts.
func pressed(gesture uintptr, clicks int32, x, y float64, area uintptr) uintptr {
	mousePress(area, int(gtkGestureButton(gesture)), int(clicks), point{x, y}, heldOf(uintptr(gtkControllerState(gesture))))
	return 0
}

func released(gesture uintptr, _ int32, x, y float64, area uintptr) uintptr {
	mouseRelease(area, point{x, y}, heldOf(uintptr(gtkControllerState(gesture))))
	return 0
}

func motion(ctl uintptr, x, y float64, area uintptr) uintptr {
	mouseMove(area, point{x, y}, heldOf(uintptr(gtkControllerState(ctl))))
	return 0
}

func scrolled(ctl uintptr, _, dy float64, area uintptr) uintptr {
	return uintptr(boolC(mouseRoll(area, gtkScrollUnit(ctl) == 0, dy)))
}

// mousePress gives the session a click of a button on a pane's text or a
// box, or shows the right click's menu. A click on a status bar only gives
// its window the focus (B552, B561).
func mousePress(area uintptr, button, clicks int, at point, mods int) {
	p, b := panes[area], boxes[area]
	switch {
	case p != nil && at.Y >= float64(gtkWidgetGetHeight(area))-statusH:
		p.w.mice <- mouse{'c', p.id, 0, math.MaxInt32, 1}
	case p != nil && button == 3:
		p.contextMenu(at)
	case p != nil && button == 1:
		p.w.pressing = true
		p.w.press(p, nil, at, 'c', clicks, mods)
	case b != nil && button == 1:
		b.w.press(nil, b, at, 'c', clicks, mods)
	}
}

// mouseRelease ends a click on a pane's text where it let go (B557).
func mouseRelease(area uintptr, at point, mods int) {
	if p := panes[area]; p != nil && p.w.pressing {
		p.w.pressing = false
		p.w.press(p, nil, at, 'u', 1, mods)
	}
}

// mouseMove drags over a pane's text while its button is down, and
// otherwise marks where the mouse is over it, or over a box's list (B552,
// B560, B562).
func mouseMove(area uintptr, at point, mods int) {
	if p := panes[area]; p != nil {
		if p.w.pressing {
			p.w.press(p, nil, at, 'd', 1, mods)
			return
		}
		p.pointAt(cellAt(at), mods)
	} else if b := boxes[area]; b != nil {
		b.hover = at
		gtkWidgetQueueDraw(area)
	}
}

// leave marks that the mouse left a pane's text or a box. Moving from a
// pane onto a box, such as a hover's, leaves the pane's hover showing.
func leave(_, area uintptr) uintptr {
	if b := boxes[area]; b != nil {
		b.hover = point{X: -1}
		gtkWidgetQueueDraw(area)
		return 0
	}
	p := panes[area]
	if p == nil {
		return 0
	}
	// GTK tells a box the mouse went onto after this.
	onMain(func() {
		for _, b := range append([]*box{p.w.panel, p.w.toast}, slices.Collect(maps.Values(p.w.floats))...) {
			if b.hover.X >= 0 {
				return
			}
		}
		if !p.gone {
			p.pointAt([2]int{-1, -1}, 0)
		}
	}, false)
	return 0
}

// mouseRoll scrolls a pane's window by dy, a wheel's notches of lines when
// wheel is set and a touchpad's points otherwise, or a box's list, and
// reports whether it scrolled anything (B552, B560, B561).
func mouseRoll(area uintptr, wheel bool, dy float64) bool {
	if b := boxes[area]; b != nil && b.boxed != nil && b.list != nil {
		if wheel {
			dy *= b.list.rowH
		}
		b.roll(dy)
		gtkWidgetQueueDraw(area)
		return true
	}
	p := panes[area]
	switch {
	case p == nil:
		return false
	case wheel:
		p.roll(dy * wheelLines)
	default:
		p.smooth(-dy, false)
	}
	return true
}

// contextMenu puts the cursor where a right click hits, unless a selection
// shows, and shows the menu of contextItems there, as contextState says
// (B561).
func (p *pane) contextMenu(at point) {
	w := p.w
	visual := w.tabs.mode == "visual"
	if !visual {
		w.press(p, nil, at, 'c', 1, 0)
	}
	cb := gdkDisplayClipboard(gdkDisplayDefault())
	shown, enabled := contextState(p.offers, visual, gdkFormatsHasType(gdkClipboardFormats(cb), gTypeString) != 0)
	menu, section := gMenuNew(), gMenuNew()
	for i, it := range contextItems {
		switch {
		case !shown[i]:
		case it.title == "":
			gMenuAppendSection(menu, 0, section)
			gObjectUnref(section)
			section = gMenuNew()
		default:
			gMenuAppend(section, it.title, fmt.Sprintf("win.context-%d", i))
			gSimpleActionEnable(w.actions[i], boolC(enabled[i]))
		}
	}
	gMenuAppendSection(menu, 0, section)
	gObjectUnref(section)
	pop := gtkPopoverMenuNew(menu)
	gObjectUnref(menu)
	if p.menu != 0 {
		gtkWidgetUnparent(p.menu)
	}
	p.menu = pop
	gtkWidgetSetParent(pop, p.area)
	gtkPopoverHasArrow(pop, 0)
	gtkPopoverPointTo(pop, &gdkRect{int32(at.X), int32(at.Y), 1, 1})
	gSignalConnectData(pop, "closed", mouseCallbacks.closed, p.area, 0, 0)
	gtkPopoverPopup(pop)
}

// contextActions makes w's actions for contextItems, which the right
// click's menu names, as context-0 and on.
func (w *window) contextActions() {
	w.actions = map[int]uintptr{}
	for i, it := range contextItems {
		if it.title == "" {
			continue
		}
		a := gSimpleActionNew(fmt.Sprintf("context-%d", i), 0)
		gSignalConnectData(a, "activate", mouseCallbacks.context, uintptr(i), 0, 0)
		gActionMapAdd(w.win, a)
		w.actions[i] = a
		contextOf[a] = w
	}
}

// contextOf are the windows of the right click's actions, by action.
var contextOf = map[uintptr]*window{}

// contextChosen does what item i of contextItems does: its keys, or the
// system's editing command of its title.
func contextChosen(action, _, i uintptr) uintptr {
	w := contextOf[action]
	if w == nil {
		return 0
	}
	switch it := contextItems[i]; {
	case it.keys != "":
		w.contextAct(it.keys)
	case it.title == "Paste":
		w.paste()
	default:
		w.edit(it.title)
	}
	return 0
}

// paste pastes the text on the system's clipboard, once it has read it.
func (w *window) paste() {
	gdkClipboardRead(gdkDisplayClipboard(gdkDisplayDefault()), 0, mouseCallbacks.pasted, w.win)
}

// pasted pastes the clipboard's text that paste read.
func pasted(clipboard, result, win uintptr) uintptr {
	var err uintptr
	text := gdkClipboardReadDone(clipboard, result, &err)
	if text == nil {
		return 0
	}
	defer gFree(unsafe.Pointer(text))
	if w := windows[win]; w != nil {
		w.pastes <- cString(text)
	}
	return 0
}

// boolC is b as a gboolean.
func boolC(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
