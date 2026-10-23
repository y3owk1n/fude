package gui

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/compose"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// pad is the room around the text, in points.
const pad = 6

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
	mainRun = objc.ID(class("FudeMain", "NSObject", objc.MethodDef{Cmd: sel("drain:"), Fn: drain}, objc.MethodDef{Cmd: sel("quit:"), Fn: quit})).Send(sel("alloc")).Send(sel("init"))
	viewClass = class("FudeView", "NSView", append(append([]objc.MethodDef{
		flipped,
		{Cmd: sel("acceptsFirstResponder"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: sel("keyDown:"), Fn: keyDown},
		{Cmd: sel("resizeSubviewsWithOldSize:"), Fn: resizeRoot},
		{Cmd: sel("splitResized:"), Fn: splitResized},
	}, textInput...), append(editing, dropFiles...)...)...)
	viewClass.AddProtocol(objc.GetProtocol("NSTextInputClient"))
	markedClass = class("FudeMarked", "NSView", flipped, objc.MethodDef{Cmd: sel("drawRect:"), Fn: drawMarked})
	paneClass = class("FudePane", "NSView", flipped, objc.MethodDef{Cmd: sel("resizeSubviewsWithOldSize:"), Fn: resizePane})
	textClass = class("FudeText", "NSView", append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawText}}, textMouse...)...)
	statusClass = class("FudeStatus", "NSView", append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawStatus}}, statusMouse...)...)
	splitClass = class("FudeSplit", "NSSplitView", objc.MethodDef{Cmd: sel("drawDividerInRect:"), Fn: drawDivider})
	barClass = class("FudeBar", "NSView", append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawBar}}, barMouse...)...)
	boxClass = class("FudeBox", "NSView", append([]objc.MethodDef{flipped, {Cmd: sel("drawRect:"), Fn: drawBox}}, textMouse...)...)
	windowDelegate = objc.ID(class("FudeWindowDelegate", "NSObject", objc.MethodDef{Cmd: sel("windowShouldClose:"), Fn: windowShouldClose})).Send(sel("alloc")).Send(sel("init"))
	font = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), 14, 0).Send(sel("retain"))
	boldFont = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), 14, 0.4).Send(sel("retain"))
	fonts := cls("NSFontManager").Send(sel("sharedFontManager"))
	italicFont = fonts.Send(sel("convertFont:toHaveTrait:"), font, 1).Send(sel("retain"))
	boldItalicFont = fonts.Send(sel("convertFont:toHaveTrait:"), boldFont, 1).Send(sel("retain"))
	toastFont = msgFont(cls("NSFont"), sel("systemFontOfSize:weight:"), 13, 0).Send(sel("retain"))
	uiFont = msgFont(cls("NSFont"), sel("systemFontOfSize:weight:"), 12, 0).Send(sel("retain"))
	uiBold = msgFont(cls("NSFont"), sel("systemFontOfSize:weight:"), 12, 0.4).Send(sel("retain"))
	menu(app)
	cellW = msgSizeOf(nsString("M"), sel("sizeWithAttributes:"), textAttrs(font, 0)).W
	cellH = float64(int(msgFloat(font, sel("ascender")) - msgFloat(font, sel("descender")) + msgFloat(font, sel("leading")) + 0.999))
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
	barClass, boxClass, markedClass objc.Class
	windowDelegate                  objc.ID
	font, boldFont, toastFont       objc.ID
	italicFont, boldItalicFont      objc.ID
	uiFont, uiBold                  objc.ID // the status bars' and the tab strip's
	cellW, cellH                    float64
	attrsOf                         = map[[2]int64]objc.ID{} // by font and colour
	windows                         = map[objc.ID]*window{}  // by view, and by NSWindow
	panes                           = map[objc.ID]*pane{}    // by text view, and by status view
	boxes                           = map[objc.ID]*box{}     // by view
	inBackground                    bool                     // the application takes no focus
)

// window is a session in a native window.
type window struct {
	s      client.Session
	win    objc.ID
	view   objc.ID // the root, which takes the keys
	keys   chan vim.Key
	do     chan func() // run on the loop, between keys, for tests
	pastes chan string
	mice   chan mouse
	ended  func() // runs on the main thread once the session ends

	mu      sync.Mutex
	pending []frame
	places  places        // the latest, for the loop
	placed  chan struct{} // tells the loop places changed

	// The main thread's.
	bar, area    objc.ID // the tab strip, and the view the splits fill
	panes        map[int]*pane
	key          string // the layout the splits show
	sent         places // the places the loop was last given
	look         theme.Look
	tabs         strip
	floats       map[int]*box
	panel, toast *box
	asked        int             // the question the sheet is for, as frame.asked counts
	sheet        objc.ID         // the alert showing, 0 for none
	images       map[int]objc.ID // by id, the NSImages of the images shown
	cellPx       [2]int          // a cell's size in the screen's pixels
	dragged      mouse           // the last click or drag, to send a drag once a cell
	hits         []hit           // the tabs the strip shows
	hover        float64         // where the mouse is along the strip, -1 off it
	pressed      *hit            // the tab the mouse went down on
	pressX       float64         // where along the strip
	dragging     bool            // the tab pressed moves with the mouse
	dropX        float64         // where it is
	menuWheel    float64         // the lines a scroll over a menu moved short of one
	moving       bool            // the strip drags the window
	moveFrom     point           // where on the screen the drag began
	origin       point           // where the window was then
	marked       string          // the text an input method composes
	preedit      objc.ID         // the view that shows it
	event        objc.ID         // the key down the input method has
}

type cell struct {
	text  string
	width int
	style theme.Style
}

type cursorAt struct {
	x, y  int
	shape int // 0 a block, 1 a bar, 2 an underline
	shown bool
}

// frame is what the loop hands the main thread after each change.
type frame struct {
	title   string
	layout  wire.Split // the zoomed window alone while one is
	key     string     // says what layout is, to tell when it changes
	wins    []shown
	floats  map[int]*boxed // by window id, and -1 and -2 for a menu and its documentation
	tabs    strip
	look    theme.Look     // the windows', dim under a modal box
	panel   *boxed         // the command line or the finder, nil for neither
	toast   *boxed         // the message, nil for none
	confirm string         // the question, "" for none
	asked   int            // counts each change of the question, to tell a new one
	copied  *string        // to put on the system's clipboard
	busy    bool           // a status line turns a spinner, so frames go on
	pngs    map[int][]byte // by id, the images the main thread lacks
	freed   []int          // the images the session dropped
}

// shown is a window of the layout as a frame brings it, with the cells
// that changed, the cursor and its status line's parts.
type shown struct {
	id, w, h    int
	put         []placed
	cursor      cursorAt
	left, right []client.Part
	current     bool
	pics        []pic
}

type placed struct {
	x, y int
	cell
}

// open makes a window for s and starts its loop. ended runs on the main
// thread once s ends.
func open(s client.Session, ended func()) *window {
	cols, rows := 100, 32
	r := rect{Size: size{cellW*float64(cols) + 2*pad, cellH*float64(rows) + 2*pad}}
	w := &window{s: s, keys: make(chan vim.Key, 1024), do: make(chan func()), pastes: make(chan string, 8), mice: make(chan mouse, 64), ended: ended, placed: make(chan struct{}, 1), panes: map[int]*pane{}, floats: map[int]*box{}, images: map[int]objc.ID{}, hover: -1}
	// The content reaches under the title bar, which the tab strip takes.
	w.win = msgInitWindow(cls("NSWindow").Send(sel("alloc")), sel("initWithContentRect:styleMask:backing:defer:"), r, 15|1<<15, 2, false)
	w.win.Send(sel("setReleasedWhenClosed:"), 0)
	w.win.Send(sel("setTitlebarAppearsTransparent:"), 1)
	// The strip moves the window itself, so dragging a tab moves only it.
	w.win.Send(sel("setMovable:"), 0)
	w.win.Send(sel("setTitleVisibility:"), 1)
	w.win.Send(sel("setTitle:"), nsString("fude"))
	w.win.Send(sel("setDelegate:"), windowDelegate)
	scale := msgFloat(w.win, sel("backingScaleFactor"))
	w.cellPx = [2]int{int(cellW * scale), int(cellH * scale)}
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
	windows[w.view], windows[w.win], windows[w.bar], windows[w.preedit] = w, w, w, w
	resizeRoot(w.view, 0, size{})
	w.win.Send(sel("makeKeyAndOrderFront:"), 0)
	if !inBackground {
		cls("NSApplication").Send(sel("sharedApplication")).Send(sel("activateIgnoringOtherApps:"), 1)
	}
	go w.loop(cols, rows)
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

// loop runs the session, as tui.Run does: it feeds keys and runs the
// session's work, and after each draws what changed.
func (w *window) loop(cols, rows int) {
	s := w.s
	var p painter
	question, asked := "", 0
	var spin <-chan time.Time
	draw := func() {
		f := p.paint(s, cols, rows)
		f.asked = asked
		if f.busy && spin == nil {
			spin = time.After(spinnerFrame)
		}
		w.mu.Lock()
		w.pending = append(w.pending, f)
		w.mu.Unlock()
		onMain(w.apply, false)
	}
	s.SetSize(cols, rows)
	s.SetCell(w.cellPx[0], w.cellPx[1])
	draw()
	s.Load()
	var timeout <-chan time.Time
	for !s.Done() {
		select {
		case k := <-w.keys:
			s.Feed(k)
		case f := <-s.Queue():
			f()
		case text := <-w.pastes:
			s.Paste(text)
		case m := <-w.mice:
			m.give(s)
		case <-w.placed:
			w.mu.Lock()
			pl := w.places
			w.mu.Unlock()
			cols, rows = pl.cols, pl.rows
			s.SetSize(pl.cols, pl.rows)
			for _, at := range pl.wins {
				s.Place(at[0], at[1], at[2], at[3], at[4])
			}
		case f := <-w.do:
			f()
		case <-timeout:
			timeout = nil
			s.Timeout()
		case <-spin:
			spin = nil
		}
		if d, ok := s.Wait(); !ok {
			timeout = nil
		} else if timeout == nil {
			timeout = time.After(d)
		}
		// A question asked again after its answer, between frames, is new.
		if q := s.Confirm(); q != question {
			question = q
			asked++
		}
		if len(w.keys) > 0 {
			continue
		}
		draw()
	}
	onMain(func() {
		for _, v := range []objc.ID{w.view, w.win, w.bar, w.preedit} {
			delete(windows, v)
		}
		for _, p := range w.panes {
			p.drop()
		}
		for _, img := range w.images {
			img.Send(sel("release"))
		}
		for _, b := range append([]*box{w.panel, w.toast}, slices.Collect(maps.Values(w.floats))...) {
			delete(boxes, b.view)
		}
		w.win.Send(sel("close"))
		if w.ended != nil {
			w.ended()
		}
	}, false)
}

// painter turns a session into the cells that changed, keeping each
// window's grid between frames.
type painter struct {
	comp    compose.Composer
	grids   map[int]*compose.Grid
	had     map[int]bool // the windows of the last frame's layout
	menuTop int          // the first row of a menu at the cursor that shows (B64)
	sent    map[int]bool // the images the main thread has
	frame   int
	lookKey string
	modal   bool
	// lit is the theme's look, dim the windows' under a modal box, and
	// boxed lit on dim's ground for the box (B151).
	lit, dim, boxed theme.Look
}

func (p *painter) paint(s client.Session, cols, rows int) frame {
	p.frame++
	th := s.Theme()
	own, restyled := s.Styles()
	if key := themeKey(th, restyled); key != p.lookKey {
		t, ok := theme.Themes[th.Name]
		if !ok {
			t = theme.Themes["dark"]
		}
		if th.Base16 != nil {
			t = theme.FromBase16(*th.Base16)
		}
		if th.Colors != nil {
			t = t.With(*th.Colors)
		}
		p.lit, p.dim = theme.LookOf(t), theme.LookOf(t.Tint())
		ground := t
		ground.Bg = p.dim.Theme.Bg
		p.boxed = theme.LookOf(ground)
		same := func(c theme.RGB) theme.RGB { return c }
		p.lit.Restyle(own, same)
		p.boxed.Restyle(own, same)
		p.dim.Restyle(own, func(c theme.RGB) theme.RGB { return theme.Mix(c, p.dim.Theme.Bg, 0.55) })
		p.lookKey, p.grids = key, nil
	}
	menu, floats := s.Menu()
	question := s.Confirm()
	cmdline := s.Mode() == vim.Cmdline
	// A :s being typed shows its preview in the windows, so they stay lit.
	modal := cmdline && !s.Substituting() || floats && menu.Anchor == "middle" || question != ""
	if p.grids == nil || modal != p.modal {
		p.grids, p.modal = map[int]*compose.Grid{}, modal
	}
	look, boxLook := p.lit, p.lit
	if modal {
		look, boxLook = p.dim, p.boxed
	}
	wins := s.Windows()
	layout := s.Layout()
	for _, v := range wins {
		if v.Zoomed {
			layout = wire.Split{Window: v.ID}
		}
	}
	_, mode := client.ModeName(s)
	f := frame{
		title: s.Title(), layout: layout, key: fmt.Sprint(layout), floats: map[int]*boxed{}, pngs: map[int][]byte{},
		tabs: strip{buffers: s.Buffers(), pages: s.Pages(), mode: mode}, look: look, confirm: question,
	}
	in := map[int]bool{}
	for _, id := range leaves(layout) {
		in[id] = true
	}
	sel, visual := s.Selection()
	floated := false // a float has the focus
	cx, cy := 0, 0   // where the cursor is in the area
	for _, v := range wins {
		if v.Float == "" && !in[v.ID] {
			continue
		}
		var cursors []int
		if v.Current {
			cursors = s.Cursors()
		}
		o := compose.Options{
			Look: look, Frame: p.frame, Insert: s.Mode() == vim.Insert, Cursors: cursors,
			Selection: sel, Visual: visual && v.Current, Tint: look.Theme.Normal,
		}
		at := cursorAt{shape: shape(s)}
		// draw draws v in g: its text, or a terminal's screen.
		draw := func(g *compose.Grid, o compose.Options) compose.Composed {
			if v.Cells != nil {
				return termCells(g, v, o.Look)
			}
			return p.comp.Compose(g, v, o)
		}
		if v.Float != "" {
			// A float shows lit in a box of its own, composed whole.
			o.Look, o.Tint = boxLook, boxLook.Theme.Normal
			g := compose.NewGrid(v.W, v.H, boxLook.Base)
			out := draw(g, o)
			b := &boxed{look: boxLook, at: &[2]int{v.X, v.Y}, title: v.Name, cells: cellsOf(g), pics: pics(v, out)}
			// Its foot shows under a rule, and its status line under that
			// while it has the focus (D55, B159).
			if v.Foot != "" {
				foot, _ := compose.Wrap(v.Foot, v.FootSpans, v.W)
				fg := compose.NewGrid(v.W, len(foot)+1, boxLook.Base)
				write(fg, v.Foot, v.FootSpans, foot, boxLook, 0, 1, v.W, -1)
				b.rules = []rule{{0, v.H, v.W}}
				b.cells = append(b.cells, cellsOf(fg)...)
			}
			if v.Current {
				b.left, b.right = client.StatusParts(s, v, false)
				f.busy = f.busy || turning(b.right)
			}
			if out.Found && v.Current {
				at.x, at.y, at.shown = out.X, out.Y, true
				b.cursor, floated = at, true
				cx, cy = v.X+out.X, v.Y+out.Y
			}
			f.floats[v.ID] = b
			continue
		}
		// A window new to the layout has a new pane, which takes every cell.
		g := p.grids[v.ID]
		if gw, gh := g.Size(); g == nil || !p.had[v.ID] || gw != v.W || gh != v.H {
			g = compose.NewGrid(v.W, v.H, look.Base)
			p.grids[v.ID] = g
		}
		out := draw(g, o)
		sh := shown{id: v.ID, w: v.W, h: v.H, current: v.Current}
		g.Paint(0, 0, func(x, y int, text string, width int, st theme.Style) {
			sh.put = append(sh.put, placed{x, y, cell{text, width, st}})
		})
		if out.Found && v.Current {
			at.x, at.y, at.shown = out.X, out.Y, true
			sh.cursor = at
			if !floated {
				cx, cy = v.X+out.X, v.Y+out.Y
			}
		}
		sh.left, sh.right = client.StatusParts(s, v, true)
		f.busy = f.busy || turning(sh.right)
		sh.pics = pics(v, out)
		f.wins = append(f.wins, sh)
	}
	p.had = in
	switch {
	case cmdline:
		// The completion menu is part of the command line's box.
		if !floats {
			menu = wire.Menu{}
		}
		f.panel = palette(s.Line(), s.LineCursor(), menu, boxLook, cols)
	case floats && menu.Anchor == "middle":
		f.panel = finder(menu, boxLook)
	case floats && menu.Anchor == "cursor":
		for i, b := range p.atCursor(menu, boxLook, cx, cy, cols, rows) {
			f.floats[-1-i] = b
		}
	case floats && menu.Anchor == "bottom":
		f.floats[-1] = keyHints(menu, boxLook)
	}
	if msg := s.Message(); msg != "" {
		f.toast = &boxed{text: compose.Carets(msg), look: boxLook}
	}
	if text, ok := s.Clipboard(); ok {
		f.copied = &text
	}
	var all []pic
	for _, sh := range f.wins {
		all = append(all, sh.pics...)
	}
	for _, b := range f.floats {
		all = append(all, b.pics...)
	}
	if f.panel != nil {
		all = append(all, f.panel.pics...)
	}
	p.images(s, &f, all)
	// While the palette, the finder or a question shows, no window shows
	// the cursor. A float with the focus shows it, and the window it opened
	// from does not.
	if f.panel != nil || question != "" || floated {
		for i := range f.wins {
			f.wins[i].cursor.shown = false
		}
	}
	if f.panel != nil || question != "" {
		for _, b := range f.floats {
			b.cursor.shown = false
		}
	}
	return f
}

// themeKey says what the look is made from, to know when it changes.
func themeKey(th wire.Theme, restyled int) string {
	key := fmt.Sprint(th.Name, restyled)
	if th.Base16 != nil {
		key += fmt.Sprint(*th.Base16)
	}
	if th.Colors != nil {
		key += fmt.Sprintf("%+v", *th.Colors)
	}
	return key
}

// shape is the cursor's shape: a bar in insert mode, an underline in
// replace mode and while an operator waits, and a block otherwise (B402).
func shape(s client.Session) int {
	switch {
	case s.Mode() == vim.Insert:
		return 1
	case s.Mode() == vim.Replace, s.Pending():
		return 2
	}
	return 0
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
	}
	for _, f := range fs {
		if f.copied != nil {
			copyOut(*f.copied)
		}
	}
	f := fs[len(fs)-1]
	if f.title != "" {
		w.win.Send(sel("setTitle:"), nsString(f.title))
		f.tabs.title = f.title
	} else {
		f.tabs.title = w.tabs.title
	}
	w.tabs = f.tabs
	w.bar.Send(sel("setNeedsDisplay:"), 1)
	for id, b := range w.floats {
		if f.floats[id] == nil {
			b.show(nil, w.view, 0)
			b.view.Send(sel("removeFromSuperview"))
			b.view.Send(sel("release"))
			delete(boxes, b.view)
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
			w.view.Send(sel("addSubview:positioned:relativeTo:"), b.view, -1, w.panel.view)
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
func attrs(s theme.Style) objc.ID {
	switch {
	case s.Bold && s.Italic:
		return textAttrs(boldItalicFont, s.Fg)
	case s.Bold:
		return textAttrs(boldFont, s.Fg)
	case s.Italic:
		return textAttrs(italicFont, s.Fg)
	}
	return textAttrs(font, s.Fg)
}

// decorate draws the lines s asks for over the text in r: a strike through
// its middle and an underline under it. An underline with a colour of its
// own, as a diagnostic's has, is wavy.
func decorate(s theme.Style, r rect) {
	if s.Strike {
		nsColor(s.Fg).Send(sel("setFill"))
		rectFill(rect{point{r.Origin.X, r.Origin.Y + math.Floor(cellH/2)}, size{r.Size.W, 1}})
	}
	switch {
	case !s.Underline:
	case s.UnderSet:
		nsColor(s.Under).Send(sel("setStroke"))
		path := cls("NSBezierPath").Send(sel("bezierPath"))
		y := r.Origin.Y + cellH - 2
		msgPointArg(path, sel("moveToPoint:"), point{r.Origin.X, y})
		for x, up := r.Origin.X+2, true; x <= r.Origin.X+r.Size.W; x, up = x+2, !up {
			dy := 0.0
			if up {
				dy = -2
			}
			msgPointArg(path, sel("lineToPoint:"), point{x, y + dy})
		}
		path.Send(sel("stroke"))
	default:
		nsColor(s.Fg).Send(sel("setFill"))
		rectFill(rect{point{r.Origin.X, r.Origin.Y + cellH - 2}, size{r.Size.W, 1}})
	}
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

// at is where the n cells from column x of row y go, the first cell at o.
func at(x, y, n int, o point) rect {
	return rect{point{o.X + float64(x)*cellW, o.Y + float64(y)*cellH}, size{float64(n) * cellW, cellH}}
}

// drawCells draws cells and the cursor on them, the first cell at o. A run
// of one-column ASCII in one style draws as one string, and any other cell
// alone at its own column, so the font's fallbacks for other characters
// never shift the row.
func drawCells(cells [][]cell, cursor cursorAt, o point) {
	ascii := func(c cell) bool { return c.width == 1 && len(c.text) == 1 && c.text[0] >= ' ' && c.text[0] <= '~' }
	for y, row := range cells {
		for x := 0; x < len(row); {
			c := row[x]
			n := 1
			for ascii(c) && x+n < len(row) && ascii(row[x+n]) && row[x+n].style == c.style {
				n++
			}
			r := at(x, y, max(n, c.width), o)
			nsColor(c.style.Bg).Send(sel("setFill"))
			rectFill(r)
			var text strings.Builder
			for _, o := range row[x : x+n] {
				text.WriteString(o.text)
			}
			if t := strings.TrimRight(text.String(), " "); t != "" {
				msgDrawAt(nsString(t), sel("drawAtPoint:withAttributes:"), r.Origin, attrs(c.style))
			}
			decorate(c.style, r)
			x += max(n, c.width)
		}
	}
	if c := cursor; c.shown && c.y >= 0 && c.x >= 0 && c.y < len(cells) && c.x < len(cells[c.y]) {
		r := at(c.x, c.y, 1, o)
		switch c.shape {
		case 1:
			r.Size.W = 2
		case 2:
			r.Origin.Y, r.Size.H = r.Origin.Y+cellH-2, 2
		}
		nsColor(cells[c.y][c.x].style.Fg).Send(sel("setFill"))
		rectFill(r)
		if under := cells[c.y][c.x]; c.shape == 0 && strings.TrimSpace(under.text) != "" {
			msgDrawAt(nsString(under.text), sel("drawAtPoint:withAttributes:"), r.Origin, attrs(theme.Style{Fg: under.style.Bg}))
		}
	}
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
	53: "<Esc>", 36: "<CR>", 76: "<CR>", 51: "<BS>", 117: "<Del>",
	123: "<Left>", 124: "<Right>", 125: "<Down>", 126: "<Up>",
	115: "<Home>", 119: "<End>", 116: "<PageUp>", 121: "<PageDown>", 114: "<Insert>",
}

// keys returns a key down in Vim notation: code is its key code, mods its
// modifier flags, chars what it types and plain what it types without
// modifiers. An Option chord is <Esc> and the key, as the terminal client
// reads one (D26). A Command chord is the system's, so it gives none.
func keys(code uint16, mods uint, chars, plain string) []vim.Key {
	switch {
	case mods&modCommand != 0:
		return nil
	case code == 48 && mods&modShift != 0:
		return []vim.Key{"<S-Tab>"}
	case code == 48:
		return []vim.Key{"\t"}
	}
	if k, ok := named[code]; ok {
		if mods&modControl != 0 && strings.Contains("<Left><Right><Up><Down><Home><End>", string(k)) {
			return []vim.Key{"<C-" + k[1:]}
		}
		if mods&modShift != 0 && strings.Contains("<Left><Right><Up><Down><Home><End>", string(k)) {
			return []vim.Key{"<S-" + k[1:]}
		}
		return []vim.Key{k}
	}
	if mods&modControl != 0 {
		switch p := strings.ToLower(plain); {
		case p == "6" || p == "^":
			return []vim.Key{"<C-^>"}
		case p == " " || p == "@" || p == "2":
			return []vim.Key{"<C-@>"}
		case utf8.RuneCountInString(p) == 1:
			return []vim.Key{vim.Key("<C-" + p + ">")}
		}
		return nil
	}
	if mods&modOption != 0 && plain != "" {
		return []vim.Key{"<Esc>", vim.Key(plain)}
	}
	var out []vim.Key
	for _, r := range chars {
		out = append(out, vim.Key(string(r)))
	}
	return out
}
