package gui

import (
	"cmp"
	"maps"
	"runtime"
	"slices"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/vim"
)

// native is a window's GTK widgets and what the main thread keeps of them.
type native struct {
	win, overlay uintptr
	bar          uintptr   // the tab strip, in the header bar
	title        string    // the title the session last gave
	splits       []uintptr // the GtkPaneds the layout made, which build takes apart
	key          string    // the layout the splits show
	sent         places    // the places the loop was last given
	look         theme.Look
	asked        int             // the question the dialog is for, as frame.asked counts
	asking       uintptr         // the GCancellable of the dialog showing, 0 for none
	dark         bool            // the window has GTK's dark look
	pressing     bool            // the mouse's button went down on a pane and is still down
	actions      map[int]uintptr // the right click's actions, by their item of contextItems
	images       map[int]uintptr // by id, the cairo surfaces of the images shown
}

// windows are the windows by their GtkWindows.
var windows = map[uintptr]*window{}

// app is the GtkApplication, and activated what runs as it starts.
var (
	app       uintptr
	activated func()
)

// callbacks are the C functions GTK calls back, made once, as purego
// cannot free one.
var callbacks struct {
	activate, drawPane, drawBox, resized, key, modifiers, closing, answered, barMoved uintptr
}

// Run shows s in a GTK window until s ends (B566).
func Run(s client.Session) error {
	if err := load(); err != nil {
		return err
	}
	startApp(func() { open(s, func() { gApplicationQuit(app) }) })
	gApplicationRun(app, 0, 0)
	return nil
}

// startApp makes the application and the callbacks, once, and makes ready
// run as it starts. G_APPLICATION_NON_UNIQUE lets each fude have its own.
func startApp(ready func()) {
	activated = ready
	if app != 0 {
		return
	}
	drainFn = purego.NewCallback(drain)
	callbacks.activate = purego.NewCallback(func(_, _ uintptr) uintptr {
		makeFonts()
		activated()
		return 0
	})
	callbacks.drawPane = purego.NewCallback(func(area, cairo uintptr, w, h int32, data uintptr) uintptr {
		drawPane(area, cairo, w, h, data)
		return 0
	})
	callbacks.drawBox = purego.NewCallback(func(area, cairo uintptr, w, h int32, data uintptr) uintptr {
		drawBox(area, cairo, w, h, data)
		return 0
	})
	callbacks.resized = purego.NewCallback(func(area uintptr, _, _ int32, _ uintptr) uintptr {
		if p := panes[area]; p != nil {
			p.w.placeAll()
		}
		return 0
	})
	callbacks.key = purego.NewCallback(keyPressed)
	callbacks.modifiers = purego.NewCallback(modifiers)
	makeMouseCallbacks()
	makeStripCallbacks()
	callbacks.closing = purego.NewCallback(func(win, _ uintptr) uintptr {
		// It quits as :qa does, which asks about unsaved changes, so the
		// window closes once the session ends.
		if w := windows[win]; w != nil {
			for _, k := range vim.Keys("<Esc>:qa<CR>") {
				w.keys <- k
			}
		}
		return 1
	})
	callbacks.answered = purego.NewCallback(answered)
	callbacks.barMoved = purego.NewCallback(func(adjustment, area uintptr) uintptr {
		barMoved(adjustment, area)
		return 0
	})
	app = gtkApplicationNew("dev.fude.fude", 32)
	gSignalConnectData(app, "activate", callbacks.activate, 0, 0, 0)
}

// open makes a window for s and starts its loop. ended runs on the main
// thread once s ends.
func open(s client.Session, ended func()) *window {
	cols, rows := 100, 32
	w := &window{s: s, keys: make(chan vim.Key, 1024), do: make(chan func()), pastes: make(chan string, 8), mice: make(chan mouse, 64), ended: ended, placed: make(chan struct{}, 1), panes: map[int]*pane{}, floats: map[int]*box{}, caretOn: true, hover: -1, native: native{images: map[int]uintptr{}}}
	w.win = gtkAppWindowNew(app)
	gtkWindowSetTitle(w.win, "fude")
	gtkWindowSetSize(w.win, int32(cellW*float64(cols)+2*pad), int32(cellH*float64(rows)+2*pad+statusH))
	w.overlay = gtkOverlayNew()
	gtkWindowSetChild(w.win, w.overlay)
	// The window takes every key before its widgets, so Tab does not move
	// the focus.
	ctl := gtkKeyControllerNew()
	gtkControllerPhase(ctl, 1)
	gSignalConnectData(ctl, "key-pressed", callbacks.key, w.win, 0, 0)
	gSignalConnectData(ctl, "modifiers", callbacks.modifiers, w.win, 0, 0)
	gtkWidgetAddControl(w.win, ctl)
	gSignalConnectData(w.win, "close-request", callbacks.closing, 0, 0, 0)
	w.newStrip()
	if styled == 0 {
		styled = w.win
	}
	scale := float64(gtkWidgetScale(w.win))
	w.cellPx = [2]int{int(cellW * scale), int(cellH * scale)}
	w.panel, w.toast = newBox(w), newBox(w)
	w.contextActions()
	windows[w.win] = w
	gtkWindowPresent(w.win)
	go w.loop(cols, rows)
	return w
}

// close closes the window of a session that ended, and forgets its
// widgets.
func (w *window) close() {
	delete(windows, w.win)
	// The theme's colours come from another open window's style context
	// once this one closes.
	if styled == w.win {
		styled = 0
		for win := range windows {
			styled = win
			break
		}
		clear(colours)
	}
	for _, p := range w.panes {
		p.drop()
	}
	for _, s := range w.images {
		cairoSurfaceFree(s)
	}
	for _, b := range append([]*box{w.panel, w.toast}, slices.Collect(maps.Values(w.floats))...) {
		delete(boxes, b.area)
	}
	gtkWindowDestroy(w.win)
	if w.ended != nil {
		w.ended()
	}
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
		w.keep(f.pngs, f.freed)
		if f.key != w.key {
			w.key = f.key
			w.build(f.layout)
		}
		for _, sh := range f.wins {
			if p := w.panes[sh.id]; p != nil {
				p.update(sh, f.look.Base.Bg)
			}
		}
		if f.copied != nil {
			gdkClipboardSetText(gdkDisplayClipboard(gdkDisplayDefault()), *f.copied)
		}
	}
	f := fs[len(fs)-1]
	if f.title != "" {
		w.title = f.title
	}
	// A title starts with a dot while a buffer the window shows has
	// changes. The tab strip shows the title when there are no tabs.
	title := cmp.Or(w.title, "fude")
	if f.edited {
		title = "• " + title
	}
	gtkWindowSetTitle(w.win, title)
	f.tabs.title = cmp.Or(w.title, "fude")
	w.tabs = f.tabs
	gtkWidgetQueueDraw(w.bar)
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
	if f.asked != w.asked {
		w.asked = f.asked
		if w.asking != 0 {
			gCancellableCancel(w.asking)
			w.asking = 0
		}
		if f.confirm != "" {
			w.ask(f.confirm)
		}
	}
}

// dress gives the window GTK's dark look on a dark theme, and its light
// look on a light one.
func (w *window) dress(f frame) {
	if d := dark(f.ground); d != w.dark {
		w.dark = d
		var v gValue
		gValueInit(&v, gTypeBoolean)
		on := int32(0)
		if d {
			on = 1
		}
		gValueSetBoolean(&v, on)
		gObjectSetProperty(gtkSettingsDefault(), "gtk-application-prefer-dark-theme", &v)
		clear(colours)
	}
}

// ask shows q in a dialog over the window, whose buttons answer it as y
// and n do in the window (B362). apply cancels the dialog when the
// question goes away unanswered.
func (w *window) ask(q string) {
	d := gtkAlertDialogNew("")
	gtkAlertSetMessage(d, q)
	labels := [][]byte{[]byte("Yes\x00"), []byte("No\x00")}
	ptrs := []uintptr{uintptr(unsafe.Pointer(&labels[0][0])), uintptr(unsafe.Pointer(&labels[1][0])), 0}
	gtkAlertSetButtons(d, &ptrs[0])
	runtime.KeepAlive(labels)
	w.asking = gCancellableNew()
	asked[d] = w
	gtkAlertChoose(d, w.win, w.asking, callbacks.answered, 0)
}

// asked are the windows of the dialogs showing, by dialog.
var asked = map[uintptr]*window{}

// answered types the answer a dialog's button gave, y or n.
func answered(d, result, _ uintptr) uintptr {
	w := asked[d]
	delete(asked, d)
	var err uintptr
	i := gtkAlertChooseFinish(d, result, &err)
	gObjectUnref(d)
	if w == nil || err != 0 {
		return 0
	}
	w.asking = 0
	switch i {
	case 0:
		w.keys <- "y"
	case 1:
		w.keys <- "n"
	}
	return 0
}

// keyvals are the keys that type no character, by GDK's keyval.
var keyvals = map[uint32]vim.Key{
	0xff1b: "<Esc>", 0xff0d: "<CR>", 0xff8d: "<CR>", 0xff08: "<BS>", 0xffff: "<Del>",
	0xff09: "\t", 0xfe20: "\t",
	0xff51: "<Left>", 0xff53: "<Right>", 0xff54: "<Down>", 0xff52: "<Up>",
	0xff50: "<Home>", 0xff57: "<End>", 0xff55: "<PageUp>", 0xff56: "<PageDown>", 0xff63: "<Insert>",
}

// keyPressed types a key down as the keys it is in Vim's notation, and
// tells GTK whether it took the key. Control and Shift with C copy and with
// V paste, as Linux's terminals do.
func keyPressed(_, keyval, _, state, win uintptr) uintptr {
	w := windows[win]
	if w == nil {
		return 0
	}
	held := heldOf(state)
	// Control with =, minus and 0 make the text bigger, smaller and its
	// first size, as GTK's applications zoom (B568).
	if held&^shiftKey == controlKey {
		switch keyval {
		case '=', '+':
			sizeText(textSize + 1)
			return 1
		case '-', '_':
			sizeText(textSize - 1)
			return 1
		case '0':
			sizeText(11)
			return 1
		}
	}
	if held == controlKey|shiftKey && (keyval == 'C' || keyval == 'V') {
		if keyval == 'C' {
			w.edit("Copy")
		} else {
			w.paste()
		}
		return 1
	}
	chars := ""
	if r := gdkKeyvalToUnicode(uint32(keyval)); r != 0 {
		chars = string(rune(r))
	}
	ks := chord(keyvals[uint32(keyval)], held, chars, chars)
	for _, k := range ks {
		w.keys <- k
	}
	if len(ks) == 0 {
		return 0
	}
	return 1
}

// modifiers shows or ends the link under the mouse as Control goes down or
// up (B563).
func modifiers(_, state, win uintptr) uintptr {
	if w := windows[win]; w != nil {
		for _, p := range w.panes {
			p.relink(heldOf(state))
		}
	}
	return 0
}
