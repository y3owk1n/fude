package gui

import (
	"maps"
	"slices"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/vim"
)

// Run shows s in a native window until the session ends (B547). It runs
// on the main thread, which the main goroutine keeps on macOS.
func Run(s client.Session) error {
	load()
	app := startApp(false)
	pooled(func() { open(s, func() { stop(app) }) })
	app.Send(sel("run"))
	return nil
}

// startApp makes the application, once, with the objects AppKit calls back.
// In the background it takes no focus and shows in no Dock, for tests.
func startApp(background bool) objc.ID {
	app := cls("NSApplication").Send(sel("sharedApplication"))
	if mainRun != 0 {
		return app
	}
	policy := 0
	if inBackground = background; background {
		policy = 1
	}
	app.Send(sel("setActivationPolicy:"), policy)
	flipped := objc.MethodDef{Cmd: sel("isFlipped"), Fn: func(objc.ID, objc.SEL) bool { return true }}
	class := func(name, super string, methods ...objc.MethodDef) objc.Class {
		c, err := objc.RegisterClass(name, objc.GetClass(super), nil, nil, methods)
		if err != nil {
			panic(err)
		}
		return c
	}
	mainRun = objc.ID(class("FudeMain", "NSObject", append([]objc.MethodDef{{Cmd: sel("drain:"), Fn: drain}, {Cmd: sel("quit:"), Fn: quit}}, sizing...)...)).Send(sel("alloc")).Send(sel("init"))
	viewClass = class("FudeView", "NSView", append(append([]objc.MethodDef{
		flipped,
		{Cmd: sel("acceptsFirstResponder"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: sel("keyDown:"), Fn: keyDown},
		{Cmd: sel("flagsChanged:"), Fn: flagsChanged},
		{Cmd: sel("resizeSubviewsWithOldSize:"), Fn: resizeRoot},
		{Cmd: sel("splitResized:"), Fn: splitResized},
	}, textInput...), append(append(append(editing, dropFiles...), contextActions()...), rootAccess...)...)...)
	viewClass.AddProtocol(objc.GetProtocol("NSTextInputClient"))
	markedClass = class("FudeMarked", "NSView", flipped, objc.MethodDef{Cmd: sel("drawRect:"), Fn: drawMarked})
	paneClass = class("FudePane", "NSView", flipped, objc.MethodDef{Cmd: sel("resizeSubviewsWithOldSize:"), Fn: resizePane})
	textClass = class("FudeText", "NSView", append([]objc.MethodDef{
		flipped, {Cmd: sel("drawRect:"), Fn: drawText}, {Cmd: sel("menuForEvent:"), Fn: menuFor}, {Cmd: sel("scrolled:"), Fn: scrolled},
	}, append(append(textMouse, restMouse...), textAccess...)...)...)
	statusClass = class("FudeStatus", "NSView", append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawStatus}}, statusMouse...)...)
	// A label takes no clicks, which go to the status bar under it.
	labelClass = class("FudeLabel", "NSTextField", objc.MethodDef{Cmd: sel("hitTest:"), Fn: func(objc.ID, objc.SEL, point) objc.ID { return 0 }})
	splitClass = class("FudeSplit", "NSSplitView", objc.MethodDef{Cmd: sel("drawDividerInRect:"), Fn: drawDivider})
	barClass = class("FudeBar", "NSView", append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawBar}}, append(barMouse, barAccess...)...)...)
	boxClass = class("FudeBox", "NSView", append(append(append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawBox}}, textMouse...), boxMouse...), boxAccess...)...)
	actionClass = class("FudeAction", "NSAccessibilityElement", actionAccess...)
	fieldClass = class("FudeField", "NSAccessibilityElement", fieldAccess...)
	windowDelegate = objc.ID(class("FudeWindowDelegate", "NSObject", objc.MethodDef{Cmd: sel("windowShouldClose:"), Fn: windowShouldClose})).Send(sel("alloc")).Send(sel("init"))
	makeFonts()
	menu(app)
	return app
}

// stop ends the application's run loop, which takes an event to notice.
func stop(app objc.ID) {
	app.Send(sel("stop:"), 0)
	ev := msgOtherEvent(cls("NSEvent"), sel("otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:"), 15, point{}, 0, 0, 0, 0, 0, 0, 0)
	app.Send(sel("postEvent:atStart:"), ev, 1)
}

// The application's own, which the main thread sets once.
var (
	viewClass, paneClass, textClass objc.Class
	statusClass, splitClass         objc.Class
	labelClass                      objc.Class // the status bars' labels
	barClass, boxClass, markedClass objc.Class
	actionClass, fieldClass         objc.Class // the elements of actions and fields
	windowDelegate                  objc.ID
	font, boldFont, toastFont       objc.ID
	italicFont, boldItalicFont      objc.ID
	uiFont, uiBold                  objc.ID                  // the status bars' and the tab strip's
	listFont, listBold, inputFont   objc.ID                  // lists' rows and the row typed into
	attrsOf                         = map[[2]int64]objc.ID{} // by font and colour
	windows                         = map[objc.ID]*window{}  // by view, and by NSWindow
	panes                           = map[objc.ID]*pane{}    // by text view, and by status view
	boxes                           = map[objc.ID]*box{}     // by view
	inBackground                    bool                     // the application takes no focus
)

// native is a window's AppKit objects and what the main thread keeps of
// them.
type native struct {
	win  objc.ID
	view objc.ID // the root, which takes the keys

	bar, area  objc.ID   // the tab strip, and the view the splits fill
	ground     theme.RGB // the window's background, the theme's ground
	key        string    // the layout the splits show
	sent       places    // the places the loop was last given
	look       theme.Look
	asked      int             // the question the sheet is for, as frame.asked counts
	sheet      objc.ID         // the alert showing, 0 for none
	images     map[int]objc.ID // by id, the NSImages of the images shown
	appearance string          // the window's look, as dress last set it
	file       string          // the file the title bar shows
	moving     bool            // the strip drags the window
	moveFrom   point           // where on the screen the drag began
	origin     point           // where the window was then
	marked     string          // the text an input method composes
	preedit    objc.ID         // the view that shows it
	event      objc.ID         // the key down the input method has
	focused    objc.ID         // the view assistive apps were last told has the focus
	tabIDs     elements        // the strip's tabs for assistive apps
}

// open makes a window for s and starts its loop. ended runs on the main
// thread once s ends.
func open(s client.Session, ended func()) *window {
	cols, rows := 100, 32
	r := rect{Size: size{cellW*float64(cols) + 2*pad, cellH*float64(rows) + 2*pad}}
	w := &window{s: s, keys: make(chan vim.Key, 1024), do: make(chan func()), pastes: make(chan string, 8), mice: make(chan mouse, 64), ended: ended, placed: make(chan struct{}, 1), panes: map[int]*pane{}, floats: map[int]*box{}, caretOn: true, hover: -1, native: native{images: map[int]objc.ID{}}}
	// The content reaches under the title bar, which the tab strip takes.
	w.win = msgInitWindow(cls("NSWindow").Send(sel("alloc")), sel("initWithContentRect:styleMask:backing:defer:"), r, 15|1<<15, 2, false)
	w.win.Send(sel("setReleasedWhenClosed:"), 0)
	w.win.Send(sel("setTitlebarAppearsTransparent:"), 1)
	// The strip moves the window itself, so dragging a tab moves only it.
	w.win.Send(sel("setMovable:"), 0)
	w.win.Send(sel("setTitleVisibility:"), 1)
	w.win.Send(sel("setTitle:"), nsString("fude"))
	w.win.Send(sel("setDelegate:"), windowDelegate)
	w.cellPx = cellPixels(w.win)
	w.view = msgInitView(objc.ID(viewClass).Send(sel("alloc")), sel("initWithFrame:"), r)
	w.bar = msgInitView(objc.ID(barClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	w.area = msgInitView(cls("NSView").Send(sel("alloc")), sel("initWithFrame:"), rect{})
	w.view.Send(sel("addSubview:"), w.area)
	w.view.Send(sel("addSubview:"), w.bar)
	// The strip hears the mouse move over it, for its tabs' ×.
	track := msgInitTracking(cls("NSTrackingArea").Send(sel("alloc")), sel("initWithRect:options:owner:userInfo:"), rect{}, 0x283, w.bar, 0)
	w.bar.Send(sel("addTrackingArea:"), track)
	track.Send(sel("release"))
	w.view.Send(sel("registerForDraggedTypes:"), cls("NSArray").Send(sel("arrayWithObject:"), nsString("public.file-url")))
	cls("NSNotificationCenter").Send(sel("defaultCenter")).Send(sel("addObserver:selector:name:object:"), w.view, sel("splitResized:"), nsString("NSSplitViewDidResizeSubviewsNotification"), 0)
	w.win.Send(sel("setContentView:"), w.view)
	w.panel, w.toast = newBox(w), newBox(w)
	w.preedit = msgInitView(objc.ID(markedClass).Send(sel("alloc")), sel("initWithFrame:"), rect{})
	w.preedit.Send(sel("setHidden:"), 1)
	w.view.Send(sel("addSubview:"), w.preedit)
	w.win.Send(sel("makeFirstResponder:"), w.view)
	w.win.Send(sel("center"))
	// The window opens where it last closed.
	if !inBackground {
		w.win.Send(sel("setFrameAutosaveName:"), nsString("fude"))
	}
	windows[w.view], windows[w.win], windows[w.bar], windows[w.preedit] = w, w, w, w
	resizeRoot(w.view, 0, size{})
	w.win.Send(sel("makeKeyAndOrderFront:"), 0)
	if !inBackground {
		cls("NSApplication").Send(sel("sharedApplication")).Send(sel("activateIgnoringOtherApps:"), 1)
	}
	go w.loop(cols, rows)
	go w.blink()
	return w
}

// windowShouldClose quits as :qa does, which asks about unsaved changes,
// so the window closes once the session ends.
func windowShouldClose(_ objc.ID, _ objc.SEL, win objc.ID) bool {
	if w := windows[win]; w != nil {
		for _, k := range vim.Keys("<Esc>:qa<CR>") {
			w.keys <- k
		}
	}
	return false
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
	askFont(fs[len(fs)-1].font)
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
	}
	for _, f := range fs {
		if f.copied != nil {
			copyOut(*f.copied)
		}
		if f.alert != "" {
			w.notify(f.alert)
		}
	}
	f := fs[len(fs)-1]
	defer w.tell()
	if f.title != "" {
		w.win.Send(sel("setTitle:"), nsString(f.title))
		f.tabs.title = f.title
	} else {
		f.tabs.title = w.tabs.title
	}
	w.tabs = f.tabs
	// Without tabs, the title bar shows the title and the file's icon
	// itself. NSWindowDocumentIconButton is 4.
	tabbed := len(f.tabs.buffers)+len(f.tabs.pages) > 0
	hidden := 0
	if tabbed {
		hidden = 1
	}
	w.win.Send(sel("setTitleVisibility:"), hidden)
	w.win.Send(sel("standardWindowButton:"), 4).Send(sel("setHidden:"), tabbed)
	w.bar.Send(sel("setNeedsDisplay:"), 1)
	w.dress(f)
	for id, b := range w.floats {
		if f.floats[id] == nil {
			b.show(nil, w.view, 0)
			b.frame.Send(sel("removeFromSuperview"))
			b.frame.Send(sel("release"))
			b.view.Send(sel("release"))
			b.drop()
			delete(w.floats, id)
		}
	}
	top := barHeight(w.win)
	for id, what := range f.floats {
		b := w.floats[id]
		if b == nil {
			// It goes under the palette, the finder and toasts.
			b = newBox(w)
			b.id = id
			w.view.Send(sel("addSubview:positioned:relativeTo:"), b.frame, -1, w.panel.frame)
			w.floats[id] = b
		}
		b.show(what, w.view, top)
	}
	w.panel.show(f.panel, w.view, top)
	w.toast.show(f.toast, w.view, top)
	if f.asked != w.asked {
		w.asked = f.asked
		if w.sheet != 0 {
			w.win.Send(sel("endSheet:"), w.sheet.Send(sel("window")))
		}
		if f.confirm != "" {
			w.ask(f.confirm)
		}
	}
}

// attrs returns the text attributes of a style in the text's font.
func attrs(s theme.Style) objc.ID { return textAttrs(fontOf(s), s.Fg) }

// fontOf is the text's font a style takes.
func fontOf(s theme.Style) objc.ID {
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

// textAttrs returns the text attributes of f in fg, made once each.
func textAttrs(f objc.ID, fg theme.RGB) objc.ID {
	k := [2]int64{int64(f), int64(fg)}
	if a, ok := attrsOf[k]; ok {
		return a
	}
	a := cls("NSMutableDictionary").Send(sel("alloc")).Send(sel("init"))
	a.Send(sel("setObject:forKey:"), f, attrFont)
	a.Send(sel("setObject:forKey:"), nsColor(fg), attrColor)
	attrsOf[k] = a
	return a
}

// The modifier flags of a key event.
const (
	modShift   = 1 << 17
	modControl = 1 << 18
	modOption  = 1 << 19
	modCommand = 1 << 20
)

// named are the keys that type no character, by key code.
var named = map[uint16]vim.Key{
	48: "\t", 53: "<Esc>", 36: "<CR>", 76: "<CR>", 51: "<BS>", 117: "<Del>",
	123: "<Left>", 124: "<Right>", 125: "<Down>", 126: "<Up>",
	115: "<Home>", 119: "<End>", 116: "<PageUp>", 121: "<PageDown>", 114: "<Insert>",
}

// keys returns a key down in Vim notation: code is its key code, mods its
// modifier flags, chars what it types and plain what it types without
// modifiers.
func keys(code uint16, mods uint, chars, plain string) []vim.Key {
	return chord(named[code], heldOf(mods), chars, plain)
}

// heldOf is the modifiers mods hold, as chord counts them.
func heldOf(mods uint) int {
	held := 0
	for _, m := range [][2]uint{{modShift, shiftKey}, {modControl, controlKey}, {modOption, altKey}, {modCommand, superKey}} {
		if mods&m[0] != 0 {
			held |= int(m[1])
		}
	}
	return held
}

// linkKey is the modifier that makes the word under the mouse a link,
// Command (B563).
const linkKey = superKey

// close closes the window of a session that ended, and forgets its views.
func (w *window) close() {
	for _, v := range []objc.ID{w.view, w.win, w.bar, w.preedit} {
		delete(windows, v)
	}
	w.tabIDs.drop()
	for _, p := range w.panes {
		p.drop()
	}
	for _, img := range w.images {
		img.Send(sel("release"))
	}
	for _, b := range append([]*box{w.panel, w.toast}, slices.Collect(maps.Values(w.floats))...) {
		b.drop()
	}
	w.win.Send(sel("close"))
	if w.ended != nil {
		w.ended()
	}
}
