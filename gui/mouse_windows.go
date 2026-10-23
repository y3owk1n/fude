package gui

import (
	"math"
	"time"
	"unsafe"

	"github.com/y3owk1n/fude/frontend/clipboard"
)

// linkKey is the modifier that makes the word under the mouse a link,
// Control (B563).
const linkKey = controlKey

// The mouse's messages.
const (
	wmSetCursor   = 0x0020
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
	wmMouseWheel  = 0x020A
	wmMouseLeave  = 0x02A3
)

// The system's pointers, by their ids.
const (
	idcArrow  = 32512
	idcIBeam  = 32513
	idcSizeWE = 32644
	idcSizeNS = 32645
	idcHand   = 32649
)

var (
	setCapture       = user32.NewProc("SetCapture")
	releaseCapture   = user32.NewProc("ReleaseCapture")
	trackMouseEvent  = user32.NewProc("TrackMouseEvent")
	screenToClient   = user32.NewProc("ScreenToClient")
	clientToScreen   = user32.NewProc("ClientToScreen")
	getCursorPos     = user32.NewProc("GetCursorPos")
	setCursor        = user32.NewProc("SetCursor")
	doubleClickTime  = user32.NewProc("GetDoubleClickTime")
	createPopupMenu  = user32.NewProc("CreatePopupMenu")
	appendMenu       = user32.NewProc("AppendMenuW")
	trackPopupMenu   = user32.NewProc("TrackPopupMenu")
	destroyMenu      = user32.NewProc("DestroyMenu")
	clipboardHasText = user32.NewProc("IsClipboardFormatAvailable")
	systemParameters = user32.NewProc("SystemParametersInfoW")
)

// trackEvent is a TRACKMOUSEEVENT.
type trackEvent struct {
	size, flags uint32
	hwnd        uintptr
	hover       uint32
}

// mouseNative is what a window keeps of the mouse.
type mouseNative struct {
	pressing  *pane     // the pane the left button went down on, while it is down
	barring   *pane     // the pane whose scroll bar's knob is being dragged
	grab      float64   // where along the knob the mouse took it
	splitting int       // the divider being dragged, -1 for none
	over      *pane     // the pane the mouse was last over
	tracking  bool      // Windows tells the window when the mouse leaves it
	downAt    time.Time // when and where a button last went down, to count clicks
	downPt    point
	clicks    int
}

// mouse handles a message of the mouse, and reports whether it did. Its
// point is in the window's pixels, or the screen's for the wheel.
func (w *window) mouse(m uint32, wp, lp uintptr) bool {
	at := point{float64(int16(lp)) / w.scale, float64(int16(lp>>16)) / w.scale}
	switch m {
	case wmLButtonDown:
		call(setCapture, w.hwnd)
		w.mouseDown(1, at)
	case wmMButtonDown:
		w.mouseDown(2, at)
	case wmRButtonDown:
		w.mouseDown(3, at)
	case wmLButtonUp:
		call(releaseCapture)
		w.mouseUp(at)
	case wmMouseMove:
		if !w.tracking {
			w.tracking = true
			t := trackEvent{flags: 2, hwnd: w.hwnd}
			t.size = uint32(unsafe.Sizeof(t))
			call(trackMouseEvent, uintptr(unsafe.Pointer(&t)))
		}
		w.mouseMove(at)
	case wmMouseLeave:
		w.tracking = false
		w.mouseLeft()
	case wmMouseWheel:
		w.mouseRoll(w.client(int32(int16(lp)), int32(int16(lp>>16))), -float64(int16(wp>>16))/120)
	case wmSetCursor:
		if lp&0xffff != 1 {
			return false
		}
		var pt [2]int32
		call(getCursorPos, uintptr(unsafe.Pointer(&pt)))
		w.pointer(w.client(pt[0], pt[1]))
	default:
		return false
	}
	return true
}

// client converts the screen pixel x, y to a point in the window.
func (w *window) client(x, y int32) point {
	pt := [2]int32{x, y}
	call(screenToClient, w.hwnd, uintptr(unsafe.Pointer(&pt)))
	return point{float64(pt[0]) / w.scale, float64(pt[1]) / w.scale}
}

// under returns the box or the pane at a point of the window, the box
// over the pane, and the point in it.
func (w *window) under(at point) (*pane, *box, point) {
	in := func(r rect) bool {
		return at.X >= r.Origin.X && at.Y >= r.Origin.Y && at.X < r.Origin.X+r.Size.W && at.Y < r.Origin.Y+r.Size.H
	}
	local := func(r rect) point { return point{at.X - r.Origin.X, at.Y - r.Origin.Y} }
	for i := len(w.order) - 1; i >= 0; i-- {
		if b := w.order[i]; b.boxed != nil && in(b.frame) {
			return nil, b, local(b.frame)
		}
	}
	for _, p := range w.panes {
		if in(p.frame) {
			return p, nil, local(p.frame)
		}
	}
	return nil, nil, at
}

// dividerAt is the divider within a few points of at, -1 for none.
func (w *window) dividerAt(at point) int {
	for i, d := range w.dividers {
		r := d.r
		if at.X >= r.Origin.X-3 && at.Y >= r.Origin.Y-3 && at.X < r.Origin.X+r.Size.W+3 && at.Y < r.Origin.Y+r.Size.H+3 {
			return i
		}
	}
	return -1
}

// count counts a button going down at a point as the next of a double or
// triple click, when it comes soon after the last near it.
func (w *window) count(at point) int {
	now := time.Now()
	soon := now.Sub(w.downAt) <= time.Duration(call(doubleClickTime))*time.Millisecond
	if soon && math.Abs(at.X-w.downPt.X) <= 4 && math.Abs(at.Y-w.downPt.Y) <= 4 {
		w.clicks = min(w.clicks+1, 3)
	} else {
		w.clicks = 1
	}
	w.downAt, w.downPt = now, at
	return w.clicks
}

// mouseDown gives the session a click of a button on a pane's text or a
// box, shows the right click's menu, or starts dragging a divider. A click
// on a status bar only gives its window the focus, and one on a tab goes
// to the strip (B552, B561).
func (w *window) mouseDown(button int, at point) {
	clicks, mods := w.count(at), held()
	if at.Y < stripH {
		w.stripDown(button, at.X)
		return
	}
	if i := w.dividerAt(at); i >= 0 && button == 1 {
		w.splitting = i
		return
	}
	p, b, local := w.under(at)
	switch {
	case p != nil && local.Y >= p.frame.Size.H-statusH:
		w.mice <- mouse{'c', p.id, 0, math.MaxInt32, 1}
	case p != nil && button == 1 && local.X >= p.frame.Size.W-barW && w.takeBar(p, local.Y):
	case p != nil && button == 3:
		p.contextMenu(local)
	case p != nil && button == 1:
		w.pressing = p
		w.press(p, nil, local, 'c', clicks, mods)
	case b != nil && button == 1:
		w.press(nil, b, local, 'c', clicks, mods)
	}
}

// takeBar starts dragging p's scroll bar's knob from y, when it shows,
// and reports whether it does. Off the knob, the knob's middle jumps to y
// (B577).
func (w *window) takeBar(p *pane, y float64) bool {
	k, shown := p.knob()
	if !shown {
		return false
	}
	w.barring, w.grab = p, y-k.Origin.Y
	if y < k.Origin.Y || y >= k.Origin.Y+k.Size.H {
		w.grab = k.Size.H / 2
		p.dragBar(y - w.grab)
	}
	return true
}

// mouseUp ends a click on a pane's text where it let go, or a divider's or
// a tab's drag (B555, B557).
func (w *window) mouseUp(at point) {
	w.splitting, w.barring = -1, nil
	if w.stripRelease() {
		w.redraw()
	}
	if p := w.pressing; p != nil {
		w.pressing = nil
		w.press(p, nil, point{at.X - p.frame.Origin.X, at.Y - p.frame.Origin.Y}, 'u', 1, held())
	}
}

// mouseMove drags a divider or a tab, or drags over the pane the button
// went down on. Otherwise it marks where the mouse is over the strip, a
// pane's text or a box's list. Over a box, the pane's hover stays (B552,
// B560, B562).
func (w *window) mouseMove(at point) {
	w.hoverCaption(0)
	if w.splitting >= 0 {
		w.split(at)
		return
	}
	if w.stripDrag(at.X) {
		w.redraw()
		return
	}
	if p := w.barring; p != nil {
		p.dragBar(at.Y - p.frame.Origin.Y - w.grab)
		return
	}
	hover := -1.0
	if at.Y < stripH {
		hover = at.X
	}
	if hover != w.hover {
		w.hover = hover
		w.redraw()
	}
	if p := w.pressing; p != nil {
		w.press(p, nil, point{at.X - p.frame.Origin.X, at.Y - p.frame.Origin.Y}, 'd', 1, held())
		return
	}
	p, b, local := w.under(at)
	for _, o := range w.order {
		hover := point{X: -1}
		if o == b {
			hover = local
		}
		if hover != o.hover {
			o.hover = hover
			w.redraw()
		}
	}
	if b != nil {
		return
	}
	if w.over != nil && w.over != p {
		w.over.pointAt([2]int{-1, -1}, 0)
	}
	if w.over = p; p != nil {
		p.pointAt(cellAt(local), held())
	}
}

// mouseLeft marks that the mouse left the window.
func (w *window) mouseLeft() {
	w.hover = -1
	for _, b := range w.order {
		b.hover = point{X: -1}
	}
	if w.over != nil {
		w.over.pointAt([2]int{-1, -1}, 0)
		w.over = nil
	}
	w.redraw()
}

// split moves the dragged divider to at, which sizes the kid before it.
// Each kid keeps at least a cell.
func (w *window) split(at point) {
	d := w.dividers[w.splitting]
	pos, least := at.Y, cellH+2*pad+statusH
	if d.across {
		pos, least = at.X, cellW+2*pad
	}
	w.moved[d.key] = max(min(pos-d.from, d.to-d.from-1-least), least)
	w.arrange()
}

// mouseRoll scrolls the list of the box at a point by a wheel's notches,
// or the pane's window by the lines the system scrolls a notch (B552,
// B560).
func (w *window) mouseRoll(at point, notches float64) {
	p, b, _ := w.under(at)
	if b != nil && b.list != nil {
		b.roll(notches * 3 * b.list.rowH)
		w.redraw()
		return
	}
	if p != nil {
		lines := uint32(3)
		call(systemParameters, 0x68, 0, uintptr(unsafe.Pointer(&lines)), 0)
		p.roll(notches * float64(lines))
	}
}

// pointer shows the system's pointer for what is at a point: a divider's
// arrows, a link's hand, a text's beam, or the arrow.
func (w *window) pointer(at point) {
	id := uintptr(idcArrow)
	p, b, local := w.under(at)
	switch i := w.dividerAt(at); {
	case i >= 0 && w.dividers[i].across:
		id = idcSizeWE
	case i >= 0:
		id = idcSizeNS
	case b != nil || p == nil || local.Y >= p.frame.Size.H-statusH:
	case p.link.w > 0:
		id = idcHand
	default:
		id = idcIBeam
	}
	call(setCursor, call(loadCursor, 0, id))
}

// pointer shows the hand over a link while the mouse is over p, and the
// text's beam otherwise (B563).
func (p *pane) pointer(link bool) {
	if p.w.over != p {
		return
	}
	id := uintptr(idcIBeam)
	if link {
		id = idcHand
	}
	call(setCursor, call(loadCursor, 0, id))
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
	shown, enabled := contextState(p.offers, visual, call(clipboardHasText, 13) != 0)
	menu := call(createPopupMenu)
	defer call(destroyMenu, menu)
	for i, it := range contextItems {
		switch {
		case !shown[i]:
		case it.title == "":
			call(appendMenu, menu, 0x800, 0, 0)
		default:
			grey := uintptr(0)
			if !enabled[i] {
				grey = 1
			}
			call(appendMenu, menu, grey, uintptr(i+1), uintptr(unsafe.Pointer(cwide(it.title))))
		}
	}
	pt := [2]int32{int32((p.frame.Origin.X + at.X) * w.scale), int32((p.frame.Origin.Y + at.Y) * w.scale)}
	call(clientToScreen, w.hwnd, uintptr(unsafe.Pointer(&pt)))
	// TPM_RETURNCMD returns the item picked, 0 for none.
	if i := call(trackPopupMenu, menu, 0x102, uintptr(pt[0]), uintptr(pt[1]), 0, w.hwnd, 0); i > 0 {
		w.choose(int(i - 1))
	}
}

// choose does what item i of contextItems does: its keys, or the system's
// editing command of its title.
func (w *window) choose(i int) {
	switch it := contextItems[i]; {
	case it.keys != "":
		w.contextAct(it.keys)
	case it.title == "Paste":
		w.paste()
	default:
		w.edit(it.title)
	}
}

// paste pastes the text on the system's clipboard. A clipboard another
// program holds pastes nothing, as an empty one does.
func (w *window) paste() {
	if text, err := clipboard.Read(); err == nil && text != "" {
		w.pastes <- text
	}
}
