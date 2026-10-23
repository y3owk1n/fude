package gui

import (
	"cmp"
	"math"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/clipboard"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// native is a window's Win32 window, the Direct2D render target it draws
// everything on, and what the main thread keeps of them.
type native struct {
	hwnd          uintptr
	target, brush unsafe.Pointer     // nil until it paints, and again once the device goes
	scale         float64            // the window's pixels to a point
	title         string             // the title the session last gave
	layout        wire.Split         // the layout the panes show
	key           string             // says what layout is
	dividers      []divider          // between the panes
	moved         map[string]float64 // the sizes of the kids dividers were dragged after, by divider
	sent          places             // the places the loop was last given
	look          theme.Look
	asked         int    // the question the dialog is for, as frame.asked counts
	asking        bool   // the dialog shows
	order         []*box // the boxes, from the bottom
	surrogate     uint16 // the first half of a character two WM_CHARs bring

	mouseNative
}

// opened are the windows by their HWNDs.
var opened = map[uintptr]*window{}

// className is the class of fude's windows, and of the message window that
// runs the main thread's queue, which startApp registers.
var className *uint16

// Run shows s in a window until s ends (B573).
func Run(s client.Session) error {
	if err := load(); err != nil {
		return err
	}
	startApp()
	open(s, func() { call(postQuit, 0) })
	pump()
	return nil
}

// startApp makes the windows' class, the message window and the fonts.
// Each window takes its monitor's DPI.
func startApp() {
	call(setDpiAwareness, ^uintptr(3))
	className = cwide("fude")
	instance := call(getModuleHandle, 0)
	wc := wndClass{proc: syscall.NewCallback(wndProc), instance: instance, name: className, cursor: call(loadCursor, 0, 32512)}
	wc.size = uint32(unsafe.Sizeof(wc))
	call(registerClass, uintptr(unsafe.Pointer(&wc)))
	mainHwnd = call(createWindow, 0, uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, ^uintptr(2), 0, instance, 0)
	makeFonts()
}

// wndProc gives a window's messages to it, and the message window's to the
// main thread's queue.
func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	if hwnd == mainHwnd && m == wmApp {
		drain()
		return 0
	}
	if w := opened[hwnd]; w != nil {
		if r, ok := w.handle(uint32(m), wp, lp); ok {
			return r
		}
	}
	return call(defWindowProc, hwnd, m, wp, lp)
}

// handle handles a message of w's window, and reports whether it did.
func (w *window) handle(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case wmPaint:
		w.paint()
		call(validateRect, w.hwnd, 0)
	case wmEraseBkgnd:
		return 1, true
	case wmSize:
		if w.target != nil {
			s := sizeU{uint32(lp & 0xffff), uint32(lp >> 16 & 0xffff)}
			com(w.target, 58, uintptr(unsafe.Pointer(&s)))
		}
		w.arrange()
	case wmDpiChanged:
		w.scale = float64(wp&0xffff) / 96
		w.cellPx = [2]int{int(cellW * w.scale), int(cellH * w.scale)}
		w.dropTarget()
		r := (*rectI)(*(*unsafe.Pointer)(unsafe.Pointer(&lp)))
		call(setWindowPos, w.hwnd, 0, uintptr(r.L), uintptr(r.T), uintptr(r.R-r.L), uintptr(r.B-r.T), 0x14)
	case wmClose:
		// It quits as :qa does, which asks about unsaved changes, so the
		// window closes once the session ends.
		for _, k := range vim.Keys("<Esc>:qa<CR>") {
			w.keys <- k
		}
	case wmKeyDown, wmSysKeyDown:
		return 0, w.keyDown(wp)
	case wmKeyUp:
		// Control going up ends the link under the mouse.
		if wp == 0x11 {
			w.relink()
		}
		return 0, false
	case wmChar:
		w.char(wp)
	case wmSysChar:
		return 0, w.sysChar(wp)
	default:
		return 0, w.mouse(m, wp, lp)
	}
	return 0, true
}

// relink shows or ends the link under the mouse as Control goes down or up
// (B563).
func (w *window) relink() {
	for _, p := range w.panes {
		p.relink(held())
	}
}

// open makes a window for s and starts its loop. ended runs on the main
// thread once s ends.
func open(s client.Session, ended func()) *window {
	cols, rows := 100, 32
	w := &window{s: s, keys: make(chan vim.Key, 1024), do: make(chan func()), pastes: make(chan string, 8), mice: make(chan mouse, 64), ended: ended, placed: make(chan struct{}, 1), panes: map[int]*pane{}, floats: map[int]*box{}, caretOn: true, hover: -1, native: native{mouseNative: mouseNative{splitting: -1}}}
	const style = 0x00CF0000 // WS_OVERLAPPEDWINDOW
	w.hwnd = call(createWindow, 0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(cwide("fude"))), style, 1<<31, 1<<31, 1<<31, 1<<31, 0, 0, call(getModuleHandle, 0), 0)
	dpi := call(getDpiForWindow, w.hwnd)
	w.scale = float64(dpi) / 96
	r := rectI{0, 0, int32(math.Ceil((cellW*float64(cols) + 2*pad) * w.scale)), int32(math.Ceil((cellH*float64(rows) + 2*pad + statusH) * w.scale))}
	call(adjustWindowRect, uintptr(unsafe.Pointer(&r)), style, 0, 0, dpi)
	call(setWindowPos, w.hwnd, 0, 0, 0, uintptr(r.R-r.L), uintptr(r.B-r.T), 0x16)
	w.cellPx = [2]int{int(cellW * w.scale), int(cellH * w.scale)}
	w.panel, w.toast = newBox(w), newBox(w)
	opened[w.hwnd] = w
	call(showWindow, w.hwnd, 1)
	go w.loop(cols, rows)
	return w
}

// close closes the window of a session that ended, and forgets its
// render target.
func (w *window) close() {
	delete(opened, w.hwnd)
	for _, p := range w.panes {
		p.gone = true
	}
	w.dropTarget()
	call(destroyWindow, w.hwnd)
	if w.ended != nil {
		w.ended()
	}
}

// makeTarget makes the render target w draws on, at its DPI, and its
// brush.
func (w *window) makeTarget() bool {
	var r rectI
	call(getClientRect, w.hwnd, uintptr(unsafe.Pointer(&r)))
	props := rtProps{dpiX: float32(96 * w.scale), dpiY: float32(96 * w.scale)}
	hp := hwndProps{w.hwnd, sizeU{uint32(r.R), uint32(r.B)}, 0}
	if com(d2d, 14, uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&hp)), uintptr(unsafe.Pointer(&w.target))) != 0 {
		w.target = nil
		return false
	}
	var c colorF
	com(w.target, 8, uintptr(unsafe.Pointer(&c)), 0, uintptr(unsafe.Pointer(&w.brush)))
	return true
}

// dropTarget lets go of w's render target and its brush, which paint
// makes again.
func (w *window) dropTarget() {
	release(w.brush)
	release(w.target)
	w.brush, w.target = nil, nil
}

// paint draws the panes, the dividers between them and the boxes over
// them, from the bottom.
func (w *window) paint() {
	if w.target == nil && !w.makeTarget() {
		return
	}
	rt, brush = w.target, w.brush
	defer func() { rt, brush = nil, nil }()
	com(rt, 48)
	ground := rgba(w.look.Base.Bg, 1)
	com(rt, 47, uintptr(unsafe.Pointer(&ground)))
	for _, p := range w.panes {
		within(p.frame, func(sz size) { drawPane(p, sz) })
	}
	for _, d := range w.dividers {
		fill("separatorColor", d.r)
	}
	for _, b := range w.order {
		if b.boxed != nil {
			within(b.frame, func(sz size) { drawBox(b, sz) })
		}
	}
	// Direct2D lost its device, as when a driver updates.
	if com(rt, 49, 0, 0) == 0x8899000C {
		w.dropTarget()
		w.redraw()
	}
}

// within calls draw with r's top left as the origin, clipped to r.
func within(r rect, draw func(size)) {
	m := matrixF{1, 0, 0, 1, float32(r.Origin.X), float32(r.Origin.Y)}
	com(rt, 30, uintptr(unsafe.Pointer(&m)))
	clip(rect{Size: r.Size})
	draw(r.Size)
	unclip()
	m = matrixF{1, 0, 0, 1, 0, 0}
	com(rt, 30, uintptr(unsafe.Pointer(&m)))
}

// redraw asks Windows to paint the window again.
func (w *window) redraw() { call(invalidateRect, w.hwnd, 0, 0) }

// area is the size of the window's client area, in points.
func (w *window) area() size {
	var r rectI
	call(getClientRect, w.hwnd, uintptr(unsafe.Pointer(&r)))
	return size{float64(r.R) / w.scale, float64(r.B) / w.scale}
}

// apply shows the frames the loop sent, on the main thread.
func (w *window) apply() {
	w.mu.Lock()
	fs := w.pending
	w.pending = nil
	w.mu.Unlock()
	if len(fs) == 0 {
		return
	}
	for _, f := range fs {
		w.look = f.look
		if f.key != w.key {
			w.key = f.key
			w.build(f.layout)
		}
		for _, sh := range f.wins {
			if p := w.panes[sh.id]; p != nil {
				p.update(sh, f.look.Base.Bg)
			}
		}
		// The clipboard refuses a copy while another program holds it.
		// The copy is then lost, as on the other systems.
		if f.copied != nil {
			_ = clipboard.Write(*f.copied)
		}
	}
	f := fs[len(fs)-1]
	if f.title != "" {
		w.title = f.title
	}
	// A title starts with a dot while a buffer the window shows has
	// changes.
	title := cmp.Or(w.title, "fude")
	if f.edited {
		title = "• " + title
	}
	call(setWindowText, w.hwnd, uintptr(unsafe.Pointer(cwide(title))))
	f.tabs.title = cmp.Or(w.title, "fude")
	w.tabs = f.tabs
	w.dress(f)
	for id, b := range w.floats {
		if f.floats[id] == nil {
			b.drop()
			delete(w.floats, id)
		}
	}
	for id, what := range f.floats {
		b := w.floats[id]
		if b == nil {
			// It goes under the palette, the finder and toasts.
			b = newBox(w)
			b.id = id
			w.floats[id] = b
			w.panel.raise()
			w.toast.raise()
		}
		b.show(what)
	}
	w.panel.show(f.panel)
	w.toast.show(f.toast)
	w.redraw()
	if f.asked != w.asked {
		w.asked = f.asked
		if w.asking {
			if box := call(getWindow, w.hwnd, 6); box != 0 {
				call(endDialog, box, 0)
			}
		}
		if f.confirm != "" {
			w.ask(f.confirm)
		}
	}
}

// dress gives the window's frame and the system's colours the dark look
// on a dark theme, and the light look on a light one.
func (w *window) dress(f frame) {
	d := dark(f.ground)
	darkLook = d
	on := int32(0)
	if d {
		on = 1
	}
	call(dwmSetAttribute, w.hwnd, 20, uintptr(unsafe.Pointer(&on)), 4)
}

// ask shows q in a dialog over the window, whose buttons answer it as y
// and n do in the window (B548). apply closes the dialog when the question
// goes away unanswered. The dialog runs its own loop, so it opens once
// apply returns.
func (w *window) ask(q string) {
	asked := w.asked
	onMain(func() {
		if w.asked != asked || opened[w.hwnd] != w {
			return
		}
		w.asking = true
		answer := call(messageBox, w.hwnd, uintptr(unsafe.Pointer(cwide(q))), uintptr(unsafe.Pointer(cwide("fude"))), 0x24)
		w.asking = false
		if w.asked != asked || opened[w.hwnd] != w {
			return
		}
		switch answer {
		case 6:
			w.keys <- "y"
		case 7:
			w.keys <- "n"
		}
	}, false)
}

// virtualKeys are the keys that type no character, by their virtual-key
// codes.
var virtualKeys = map[uintptr]vim.Key{
	0x1b: "<Esc>", 0x0d: "<CR>", 0x08: "<BS>", 0x2e: "<Del>", 0x09: "\t",
	0x25: "<Left>", 0x27: "<Right>", 0x28: "<Down>", 0x26: "<Up>",
	0x24: "<Home>", 0x23: "<End>", 0x21: "<PageUp>", 0x22: "<PageDown>", 0x2d: "<Insert>",
}

// held is the modifiers held, as chord counts them. Control with Alt is
// AltGr, which types a character, so it counts as neither.
func held() int {
	down := func(vk uintptr) bool { return int16(call(getKeyState, vk)) < 0 }
	h := 0
	for _, m := range [][2]int{{0x10, shiftKey}, {0x11, controlKey}, {0x12, altKey}, {0x5b, superKey}, {0x5c, superKey}} {
		if down(uintptr(m[0])) {
			h |= m[1]
		}
	}
	if h&(controlKey|altKey) == controlKey|altKey {
		h &^= controlKey | altKey
	}
	return h
}

// keyDown types a key down that types no character, or one with Control,
// as its keys in Vim's notation, and reports whether it took the key. The
// characters the others type come as WM_CHAR and WM_SYSCHAR. Control and
// Shift with C copy and with V paste, as Windows Terminal does.
func (w *window) keyDown(vk uintptr) bool {
	mods := held()
	switch {
	case vk == 0x11:
		w.relink()
		return false
	case mods == controlKey|shiftKey && vk == 'C':
		w.edit("Copy")
		return true
	case mods == controlKey|shiftKey && vk == 'V':
		w.paste()
		return true
	}
	named, plain := virtualKeys[vk], ""
	if named == "" {
		if mods&controlKey == 0 {
			return false
		}
		// A dead key's character has the top bit set.
		c := call(mapVirtualKey, vk, 2)
		if c == 0 || c&(1<<31) != 0 {
			return false
		}
		plain = string(rune(c))
	}
	ks := chord(named, mods, "", plain)
	for _, k := range ks {
		w.keys <- k
	}
	return len(ks) > 0
}

// char types a character a key typed. Control characters are the keys
// keyDown typed, and a character past U+FFFF comes in two halves.
func (w *window) char(c uintptr) {
	if c < 0x20 || c == 0x7f || held()&controlKey != 0 {
		return
	}
	r := rune(c)
	if utf16.IsSurrogate(r) {
		if c < 0xdc00 {
			w.surrogate = uint16(c)
			return
		}
		r = utf16.DecodeRune(rune(w.surrogate), r)
	}
	w.keys <- vim.Key(string(r))
}

// sysChar types a character typed with Alt as <Esc> and the character, as
// the terminal client reads one (D26), and reports whether it took it.
func (w *window) sysChar(c uintptr) bool {
	if c < 0x20 {
		return false
	}
	for _, k := range chord("", altKey, string(rune(c)), string(rune(c))) {
		w.keys <- k
	}
	return true
}
