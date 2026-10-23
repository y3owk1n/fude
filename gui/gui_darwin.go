package gui

import (
	"fmt"
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
	main, err := objc.RegisterClass("FudeMain", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("drain:"), Fn: drain},
	})
	if err != nil {
		panic(err)
	}
	mainRun = objc.ID(main).Send(sel("alloc")).Send(sel("init"))
	view, err := objc.RegisterClass("FudeView", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
		{Cmd: sel("drawRect:"), Fn: drawRect},
		{Cmd: sel("isFlipped"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: sel("acceptsFirstResponder"), Fn: func(objc.ID, objc.SEL) bool { return true }},
		{Cmd: sel("keyDown:"), Fn: keyDown},
		{Cmd: sel("frameChanged:"), Fn: frameChanged},
	})
	if err != nil {
		panic(err)
	}
	viewClass = view
	box, err := objc.RegisterClass("FudeBox", objc.GetClass("NSView"), nil, nil, []objc.MethodDef{
		{Cmd: sel("drawRect:"), Fn: drawBox},
		{Cmd: sel("isFlipped"), Fn: func(objc.ID, objc.SEL) bool { return true }},
	})
	if err != nil {
		panic(err)
	}
	boxClass = box
	delegate, err := objc.RegisterClass("FudeWindowDelegate", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("windowShouldClose:"), Fn: windowShouldClose},
	})
	if err != nil {
		panic(err)
	}
	windowDelegate = objc.ID(delegate).Send(sel("alloc")).Send(sel("init"))
	font = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), 14, 0).Send(sel("retain"))
	boldFont = msgFont(cls("NSFont"), sel("monospacedSystemFontOfSize:weight:"), 14, 0.4).Send(sel("retain"))
	toastFont = msgFont(cls("NSFont"), sel("systemFontOfSize:weight:"), 13, 0).Send(sel("retain"))
	attrs := cls("NSMutableDictionary").Send(sel("dictionary"))
	attrs.Send(sel("setObject:forKey:"), font, attrFont)
	cellW = msgSizeOf(nsString("M"), sel("sizeWithAttributes:"), attrs).W
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
	viewClass      objc.Class
	boxClass       objc.Class
	windowDelegate objc.ID
	font, boldFont objc.ID
	toastFont      objc.ID
	cellW, cellH   float64
	attrsOf        = map[[2]int64]objc.ID{} // by colour, and 0 plain, 1 bold or 2 a toast's
	windows        = map[objc.ID]*window{}  // by view, and by NSWindow
	boxes          = map[objc.ID]*box{}     // by view
	inBackground   bool                     // the application takes no focus
)

// window is a session in a native window.
type window struct {
	s         client.Session
	win, view objc.ID
	keys      chan vim.Key
	sizes     chan [2]int
	do        chan func() // run on the loop, between keys, for tests
	ended     func()      // runs on the main thread once the session ends

	mu      sync.Mutex
	pending []frame

	// The main thread's: the cells shown, where the cursor is, the boxes
	// over them and the question's sheet.
	cells        [][]cell
	bg           theme.RGB
	cursor       cursorAt
	cols         int
	rows         int
	panel, toast *box
	asked        int     // the question the sheet is for, as frame.asked counts
	sheet        objc.ID // the alert showing, 0 for none
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
	cols, rows int
	bg         theme.RGB
	put        []placed
	cursor     cursorAt
	title      string
	panel      *boxed // the command line or the finder, nil for neither
	toast      *boxed // the message, nil for none
	confirm    string // the question, "" for none
	asked      int    // counts each change of the question, to tell a new one
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
	w := &window{s: s, keys: make(chan vim.Key, 1024), sizes: make(chan [2]int, 8), do: make(chan func()), ended: ended}
	w.win = msgInitWindow(cls("NSWindow").Send(sel("alloc")), sel("initWithContentRect:styleMask:backing:defer:"), r, 15, 2, false)
	w.win.Send(sel("setReleasedWhenClosed:"), 0)
	w.win.Send(sel("setTitle:"), nsString("fude"))
	w.win.Send(sel("setDelegate:"), windowDelegate)
	w.view = msgInitView(objc.ID(viewClass).Send(sel("alloc")), sel("initWithFrame:"), r)
	w.view.Send(sel("setPostsFrameChangedNotifications:"), 1)
	cls("NSNotificationCenter").Send(sel("defaultCenter")).Send(sel("addObserver:selector:name:object:"), w.view, sel("frameChanged:"), nsString("NSViewFrameDidChangeNotification"), w.view)
	w.win.Send(sel("setContentView:"), w.view)
	w.panel, w.toast = newBox(w.view), newBox(w.view)
	w.win.Send(sel("makeFirstResponder:"), w.view)
	w.win.Send(sel("center"))
	w.win.Send(sel("makeKeyAndOrderFront:"), 0)
	if !inBackground {
		cls("NSApplication").Send(sel("sharedApplication")).Send(sel("activateIgnoringOtherApps:"), 1)
	}
	windows[w.view], windows[w.win] = w, w
	go w.loop(cols, rows)
	return w
}

// grid returns how many columns and rows of cells the view holds.
func grid(view objc.ID) (int, int) {
	b := msgRect(view, sel("bounds"))
	return max(int((b.Size.W-2*pad)/cellW), 1), max(int((b.Size.H-2*pad)/cellH), 1)
}

func frameChanged(self objc.ID, _ objc.SEL, _ objc.ID) {
	if w := windows[self]; w != nil {
		cols, rows := grid(self)
		w.sizes <- [2]int{cols, rows}
	}
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
	draw := func() {
		s.SetSize(cols, rows)
		f := p.paint(s, cols, rows)
		f.asked = asked
		w.mu.Lock()
		w.pending = append(w.pending, f)
		w.mu.Unlock()
		onMain(w.apply, false)
	}
	draw()
	s.Load()
	var timeout <-chan time.Time
	for !s.Done() {
		select {
		case k := <-w.keys:
			s.Feed(k)
		case f := <-s.Queue():
			f()
		case size := <-w.sizes:
			cols, rows = size[0], size[1]
			p.grids = nil
		case f := <-w.do:
			f()
		case <-timeout:
			timeout = nil
			s.Timeout()
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
		delete(windows, w.view)
		delete(windows, w.win)
		delete(boxes, w.panel.view)
		delete(boxes, w.toast.view)
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
	f := frame{cols: cols, rows: rows, bg: look.Base.Bg, title: s.Title(), confirm: question}
	put := func(x, y int, text string, width int, st theme.Style) {
		f.put = append(f.put, placed{x, y, cell{text, width, st}})
	}
	sel, visual := s.Selection()
	for _, v := range s.Windows() {
		if v.Cells != nil {
			continue
		}
		g := p.grids[v.ID]
		if gw, gh := g.Size(); g == nil || gw != v.W || gh != v.H {
			g = compose.NewGrid(v.W, v.H, look.Base)
			p.grids[v.ID] = g
		}
		var cursors []int
		if v.Current {
			cursors = s.Cursors()
		}
		out := p.comp.Compose(g, v, compose.Options{
			Look: look, Frame: p.frame, Insert: s.Mode() == vim.Insert, Cursors: cursors,
			Selection: sel, Visual: visual && v.Current, Tint: look.Theme.Normal,
		})
		g.Paint(v.X, v.Y, put)
		if out.Found && v.Current {
			f.cursor = cursorAt{x: v.X + out.X, y: v.Y + out.Y, shape: shape(s), shown: true}
		}
	}
	switch {
	case cmdline:
		// The completion menu is part of the command line's box.
		if !floats {
			menu = wire.Menu{}
		}
		f.panel = palette(s.Line(), s.LineCursor(), menu, boxLook, cols)
	case floats && menu.Anchor == "middle":
		f.panel = finder(menu, boxLook)
	}
	if msg := s.Message(); msg != "" {
		f.toast = &boxed{text: compose.Carets(msg), look: boxLook}
	}
	// The cursor is the box's, and no window's while a question shows.
	if f.panel != nil || question != "" {
		f.cursor.shown = false
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

// apply runs on the main thread: it puts the frames the loop sent in the
// cells shown and asks for a redraw.
func (w *window) apply() {
	w.mu.Lock()
	fs := w.pending
	w.pending = nil
	w.mu.Unlock()
	for _, f := range fs {
		if f.cols != w.cols || f.rows != w.rows || f.bg != w.bg {
			w.cols, w.rows, w.bg = f.cols, f.rows, f.bg
			w.cells = make([][]cell, f.rows)
			for y := range w.cells {
				w.cells[y] = make([]cell, f.cols)
				for x := range w.cells[y] {
					w.cells[y][x] = cell{" ", 1, theme.Style{Bg: f.bg}}
				}
			}
		}
		for _, p := range f.put {
			if p.y >= len(w.cells) || p.x >= len(w.cells[p.y]) {
				continue
			}
			w.cells[p.y][p.x] = p.cell
			// A wide cell covers the next.
			for i := 1; i < p.width && p.x+i < len(w.cells[p.y]); i++ {
				w.cells[p.y][p.x+i] = cell{style: p.style}
			}
		}
		w.cursor = f.cursor
		if f.title != "" {
			w.win.Send(sel("setTitle:"), nsString(f.title))
		}
	}
	w.view.Send(sel("setNeedsDisplay:"), 1)
	if len(fs) == 0 {
		return
	}
	f := fs[len(fs)-1]
	w.panel.show(f.panel, w.view)
	w.toast.show(f.toast, w.view)
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

// attrs returns the text attributes of a style, made once each.
func attrs(s theme.Style) objc.ID {
	k := [2]int64{int64(s.Fg), 0}
	if s.Bold {
		k[1] = 1
	}
	if a, ok := attrsOf[k]; ok {
		return a
	}
	f := font
	if s.Bold {
		f = boldFont
	}
	a := cls("NSMutableDictionary").Send(sel("alloc")).Send(sel("init"))
	a.Send(sel("setObject:forKey:"), f, attrFont)
	a.Send(sel("setObject:forKey:"), nsColor(s.Fg), attrColor)
	attrsOf[k] = a
	return a
}

func drawRect(self objc.ID, _ objc.SEL, _ rect) {
	w := windows[self]
	if w == nil {
		return
	}
	nsColor(w.bg).Send(sel("setFill"))
	rectFill(msgRect(self, sel("bounds")))
	drawCells(w.cells, w.cursor, pad)
}

// at is where the n cells from column x of row y go, the first edge points
// in from the view's.
func at(x, y, n int, edge float64) rect {
	return rect{point{edge + float64(x)*cellW, edge + float64(y)*cellH}, size{float64(n) * cellW, cellH}}
}

// drawCells draws cells and the cursor on them, edge points in from the
// view's edges. A run of one-column ASCII in one style draws as one string,
// and any other cell alone at its own column, so the font's fallbacks for
// other characters never shift the row.
func drawCells(cells [][]cell, cursor cursorAt, edge float64) {
	ascii := func(c cell) bool { return c.width == 1 && len(c.text) == 1 && c.text[0] >= ' ' && c.text[0] <= '~' }
	for y, row := range cells {
		for x := 0; x < len(row); {
			c := row[x]
			n := 1
			for ascii(c) && x+n < len(row) && ascii(row[x+n]) && row[x+n].style == c.style {
				n++
			}
			r := at(x, y, max(n, c.width), edge)
			nsColor(c.style.Bg).Send(sel("setFill"))
			rectFill(r)
			var text strings.Builder
			for _, o := range row[x : x+n] {
				text.WriteString(o.text)
			}
			if t := strings.TrimRight(text.String(), " "); t != "" {
				msgDrawAt(nsString(t), sel("drawAtPoint:withAttributes:"), r.Origin, attrs(c.style))
			}
			x += max(n, c.width)
		}
	}
	if c := cursor; c.shown && c.y < len(cells) && c.x < len(cells[c.y]) {
		r := at(c.x, c.y, 1, edge)
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

func keyDown(self objc.ID, _ objc.SEL, ev objc.ID) {
	w := windows[self]
	if w == nil {
		return
	}
	code := objc.Send[uint16](ev, sel("keyCode"))
	mods := objc.Send[uint](ev, sel("modifierFlags"))
	for _, k := range keys(code, mods, goString(ev.Send(sel("characters"))), goString(ev.Send(sel("charactersIgnoringModifiers")))) {
		w.keys <- k
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
