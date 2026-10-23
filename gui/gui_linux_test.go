package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/bufferline"
	"github.com/y3owk1n/fude/modules/buffers"
	filesmod "github.com/y3owk1n/fude/modules/files"
	"github.com/y3owk1n/fude/vim"
)

// TestMain runs GTK on the main thread and the tests beside it, which reach
// it through onMain.
func TestMain(m *testing.M) {
	// A GTK call on a freed or wrong object fails the tests.
	os.Setenv("G_DEBUG", "fatal-criticals")
	if err := load(); err != nil {
		panic(err)
	}
	cairo, err := purego.Dlopen("libcairo.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&cairoCreate, cairo, "cairo_create")
	purego.RegisterLibFunc(&cairoDestroy, cairo, "cairo_destroy")
	purego.RegisterLibFunc(&cairoWritePNG, cairo, "cairo_surface_write_to_png")
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
	purego.RegisterLibFunc(&gActionActivate, gtk, "g_action_activate")
	purego.RegisterLibFunc(&gtkWindowTitle, gtk, "gtk_window_get_title")
	purego.RegisterLibFunc(&gtkWidgetVisible, gtk, "gtk_widget_get_visible")
	purego.RegisterLibFunc(&gtkWindowFocus, gtk, "gtk_window_get_focus")
	purego.RegisterLibFunc(&gtkAccessibleRole, gtk, "gtk_accessible_get_accessible_role")
	purego.RegisterLibFunc(&gTypeIfacePeek, gtk, "g_type_interface_peek")
	purego.RegisterLibFunc(&gBytesData, gtk, "g_bytes_get_data")
	purego.RegisterLibFunc(&gBytesUnref, gtk, "g_bytes_unref")
	purego.RegisterLibFunc(&pangoFontFamily, gtk, "pango_font_description_get_family")
	purego.RegisterLibFunc(&gtkHeaderTitle, gtk, "gtk_header_bar_get_title_widget")
	purego.RegisterLibFunc(&gtkLabelText, gtk, "gtk_label_get_text")
	purego.RegisterLibFunc(&gtkWidgetActivate, gtk, "gtk_widget_activate")
	ready := make(chan struct{})
	startApp(func() { close(ready) })
	// The application stays up between the tests' windows.
	gApplicationHold(app)
	code := 0
	go func() {
		<-ready
		code = editortest.Main(m)
		onMain(func() { gApplicationQuit(app) }, false)
	}()
	gApplicationRun(app, 0, 0)
	os.Exit(code)
}

// B566
func TestB566GTKWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	// Keys typed in the window reach the session as Vim keys.
	for _, k := range []struct{ keyval, state uintptr }{{'A', gdkShift}, {'!', gdkShift}, {0xff1b, 0}} {
		onMain(func() { keyPressed(0, k.keyval, 0, k.state, w.win) }, true)
	}
	until(t, "A!<Esc> to type", func() bool {
		var text string
		w.onLoop(func() { text = editortest.Text(e) })
		return text == "one!"
	})
	// The pane draws the text: a cell with text differs from an empty one.
	until(t, "the text to show", func() bool {
		drawn := false
		onMain(func() {
			for _, p := range w.panes {
				img := paint(t, p.area, drawPane)
				drawn = img != nil && inked(img, 0, 0) && !inked(img, 40, 10)
			}
		}, true)
		return drawn
	})
	// Without tabs the header bar shows the title itself, and each status
	// bar is GTK's labels.
	until(t, "the status bar's labels", func() bool {
		said, titled := "", true
		onMain(func() {
			titled = gtkHeaderTitle(w.header) != 0
			for _, p := range w.panes {
				said = gtkLabelText(p.leftLabel)
			}
		}, true)
		return !titled && strings.Contains(said, "a.txt")
	})
	// A window made bigger makes the session bigger.
	onMain(func() { gtkWidgetSizeRequest(w.overlay, int32(cellW*120+2*pad), int32(cellH*40+2*pad+statusH)) }, true)
	until(t, "the session to take 120 columns", func() bool {
		cols := 0
		w.onLoop(func() { cols = e.Windows()[0].W })
		return cols == 120
	})
	// Closing the window quits as :qa does.
	for _, k := range ":w\r" {
		keyval := uintptr(k)
		if k == '\r' {
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	until(t, "the file to be written", func() bool { b, _ := os.ReadFile(path); return string(b) == "one!\n" })
	onMain(func() { gtkWindowClose(w.win) }, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B567, B560
func TestB567GTKMouse(t *testing.T) {
	lines := []string{"one two", "three"}
	for i := range 100 {
		lines = append(lines, fmt.Sprint("line ", i))
	}
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, strings.Join(lines, "\n")+"\n")
	t.Chdir(filepath.Dir(path))
	e := editortest.Open(t, path)
	e.Use(hovering{more: 100})
	e.Use(filesmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var p *pane
	until(t, "the pane", func() bool {
		onMain(func() {
			for _, q := range w.panes {
				if len(q.cells) > 0 && gtkWidgetGetHeight(q.area) > 0 {
					p = q
				}
			}
		}, true)
		return p != nil
	})
	cell := func(x, y int) point { return point{pad + (float64(x)+0.5)*cellW, pad + (float64(y)+0.5)*cellH} }
	typed := func(keys string) {
		for _, k := range keys {
			keyval := map[rune]uintptr{'\x1b': 0xff1b, '\t': 0xff09, '\r': 0xff0d}[k]
			if keyval == 0 {
				keyval = uintptr(k)
			}
			onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
		}
	}
	click := func(clicks int, from, to point) {
		onMain(func() {
			mousePress(p.area, 1, clicks, from, 0)
			mouseMove(p.area, to, 0)
			mouseRelease(p.area, to, 0)
		}, true)
	}
	// selected reads the selection once the session has every click.
	selected := func(start, end int) bool {
		var s vim.Selection
		var ok bool
		until(t, "the clicks to be given", func() bool { return len(w.mice) == 0 })
		w.onLoop(func() { s, ok = e.Selection() })
		return ok && s.Start == start && s.End == end
	}
	// A click puts the cursor on the character it hits, and a drag selects
	// from there.
	click(1, cell(4, 0), cell(4, 0))
	until(t, "the cursor on two", func() bool {
		c := -1
		w.onLoop(func() { c = e.Cursor() })
		return c == 4
	})
	click(1, cell(0, 1), cell(2, 1))
	until(t, "thr to be selected", func() bool { return selected(8, 11) })
	// A double click selects the word it hits, and a move within its
	// cell does not drag.
	click(1, cell(5, 0), cell(5, 0))
	click(2, cell(5, 0), cell(5, 0))
	until(t, "two to be selected", func() bool { return selected(4, 7) })
	onMain(func() { keyPressed(0, 0xff1b, 0, 0, w.win) }, true)
	until(t, "normal mode", func() bool {
		var m vim.Mode
		w.onLoop(func() { m = e.Mode() })
		return m == vim.Normal
	})
	// A rest on a word shows its hover, once the mouse moved since the
	// last click.
	onMain(func() { mouseMove(p.area, cell(6, 0), 0) }, true)
	until(t, "the hover of two", func() bool {
		text := ""
		w.onLoop(func() {
			for _, v := range e.Windows() {
				if v.Float == "cursor" {
					text = v.Text.Slice(0, v.Text.Len())
				}
			}
		})
		return strings.HasPrefix(text, "at 6")
	})
	// The wheel over the hover scrolls its window.
	onMain(func() {
		for _, b := range w.floats {
			if b.boxed != nil && b.read != nil {
				mouseRoll(b.area, true, 1)
			}
		}
	}, true)
	until(t, "the hover to scroll", func() bool {
		top := 0
		w.onLoop(func() {
			for _, v := range e.Windows() {
				if v.Float == "cursor" {
					top = v.Text.LineOf(v.Top)
				}
			}
		})
		return top > 0
	})
	// The wheel scrolls the window three lines a notch.
	onMain(func() { mouseRoll(p.area, true, 2) }, true)
	until(t, "the window to scroll 6 lines", func() bool {
		top := -1
		w.onLoop(func() { top = e.Windows()[0].Text.LineOf(e.Windows()[0].Top) })
		return top == 6
	})
	// The right click's Select All selects the text.
	onMain(func() { mousePress(p.area, 3, 1, cell(0, 0), 0) }, true)
	all := slices.IndexFunc(contextItems, func(it struct{ title, keys, needs string }) bool { return it.title == "Select All" })
	onMain(func() { gActionActivate(w.actions[all], 0) }, true)
	until(t, "the text to be selected", func() bool {
		var s vim.Selection
		var ok bool
		w.onLoop(func() { s, ok = e.Selection() })
		return ok && s.Kind == "V" && s.Start == 0
	})
	// A click on a row of the palette's list puts its item in the line.
	typed("\x1b:\t")
	var second string
	until(t, "the palette's list", func() bool {
		onMain(func() {
			if w.panel.boxed != nil && w.panel.list != nil && len(w.panel.list.items) > 2 {
				second = w.panel.list.items[1].Label
			}
		}, true)
		return second != ""
	})
	onMain(func() { gtkWidgetActivate(gtkListBoxRowAt(w.panel.listBox, 1)) }, true)
	until(t, "the second item in the line", func() bool {
		line := ""
		w.onLoop(func() { line = e.Line() })
		return line == ":"+second
	})
	// The finder's preview of a file longer than its box scrolls.
	typed("\x1b:pick files\r")
	until(t, "the whole preview", func() bool {
		ok := false
		onMain(func() {
			var shown, whole grect
			ok = gtkWidgetVisible(w.panel.previewScroll) != 0 &&
				gtkWidgetBounds(w.panel.previewScroll, w.overlay, &shown) != 0 && gtkWidgetBounds(w.panel.previewArea, w.overlay, &whole) != 0 &&
				whole.H > shown.H
		}, true)
		return ok
	})
	typed("\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B568
func TestB568GTKStripAndLook(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "two\n")
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(bufferline.New())
	e.Use(buffers.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	for _, k := range ":e " + filepath.Join(dir, "b.txt") + "\r" {
		keyval := uintptr(k)
		if k == '\r' {
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	// The strip shows a tab for each buffer, and a click on one shows it.
	until(t, "two tabs", func() bool {
		n := 0
		onMain(func() {
			surface := cairoImageNew(0, 800, stripH)
			defer cairoSurfaceFree(surface)
			cr = cairoCreate(surface)
			w.drawStrip(800, pad, 800-pad, stripH)
			cairoDestroy(cr)
			cr = 0
			n = len(w.hits)
		}, true)
		return n == 2
	})
	// With tabs the strip is the header bar's title.
	onMain(func() {
		if gtkHeaderTitle(w.header) != w.bar {
			t.Error("the header bar does not show the tabs")
		}
	}, true)
	onMain(func() { w.stripPress((w.hits[0].from+w.hits[0].to)/2 - slotW) }, true)
	until(t, "a.txt to show", func() bool {
		name := ""
		w.onLoop(func() { name = e.Windows()[0].Name })
		return strings.HasSuffix(name, "a.txt")
	})
	// A change to a buffer puts a dot before the title.
	for _, k := range "ix" {
		onMain(func() { keyPressed(0, uintptr(k), 0, 0, w.win) }, true)
	}
	onMain(func() { keyPressed(0, 0xff1b, 0, 0, w.win) }, true)
	until(t, "the title to show the change", func() bool {
		title := ""
		onMain(func() { title = gtkWindowTitle(w.win) }, true)
		return strings.HasPrefix(title, "• ")
	})
	// Control with = makes the text bigger, so the window holds fewer
	// columns.
	cols := 0
	w.onLoop(func() { cols = e.Windows()[0].W })
	onMain(func() { keyPressed(0, '=', 0, gdkControl, w.win) }, true)
	until(t, "fewer columns", func() bool {
		now := 0
		w.onLoop(func() { now = e.Windows()[0].W })
		return now < cols
	})
	onMain(func() { keyPressed(0, '0', 0, gdkControl, w.win) }, true)
	for _, k := range "\x1b:qa!\r" {
		keyval := uintptr(k)
		switch k {
		case '\x1b':
			keyval = 0xff1b
		case '\r':
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B569
func TestB569GTKImages(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	editortest.Change(t, filepath.Join(dir, "pic.png"), data.String())
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\n")
	t.Chdir(dir)
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(filesmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	for _, k := range ":pick files\rpic.png" {
		keyval := uintptr(k)
		if k == '\r' {
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	// The finder's preview shows the image from its pixels.
	until(t, "the image in the finder", func() bool {
		red := false
		onMain(func() {
			b := w.panel
			if b.boxed == nil || len(b.pics) == 0 || w.images[b.pics[0].id] == 0 {
				return
			}
			shot := paint(t, b.area, drawBox)
			if shot == nil {
				return
			}
			p, o := b.pics[0], b.cellsOrigin()
			r, g, _, _ := shot.At(int(o.X+(float64(p.x)+0.5)*cellW), int(o.Y+(float64(p.y)+0.5)*cellH)).RGBA()
			red = r>>8 > 200 && g>>8 < 50
		}, true)
		return red
	})
	for _, k := range "\x1b\x1b:qa!\r" {
		keyval := uintptr(k)
		switch k {
		case '\x1b':
			keyval = 0xff1b
		case '\r':
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B570
func TestB570GTKScrollBar(t *testing.T) {
	var lines []string
	for i := range 200 {
		lines = append(lines, fmt.Sprint("line ", i))
	}
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, strings.Join(lines, "\n")+"\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var p *pane
	until(t, "the pane", func() bool {
		onMain(func() {
			for _, q := range w.panes {
				if gtkWidgetGetHeight(q.area) > 0 {
					p = q
				}
			}
		}, true)
		return p != nil
	})
	// The bar shows where the view is once it moves.
	onMain(func() { mouseRoll(p.area, true, 10) }, true)
	until(t, "the bar at line 30", func() bool {
		shown, at := false, 0.0
		onMain(func() { shown, at = gtkWidgetVisible(p.bar) != 0, gtkAdjustmentValue(p.adjustment) }, true)
		return shown && at == 30
	})
	// Dragging its knob scrolls the window there.
	onMain(func() { gtkAdjustmentSetValue(p.adjustment, 100) }, true)
	until(t, "the window at line 100", func() bool {
		top := -1
		w.onLoop(func() { top = e.Windows()[0].Text.LineOf(e.Windows()[0].Top) })
		return top == 100
	})
	for _, k := range ":qa!\r" {
		keyval := uintptr(k)
		if k == '\r' {
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B571
func TestB571GTKInputMethods(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	until(t, "the cursor", func() bool {
		shown := false
		onMain(func() { _, shown = w.caret() }, true)
		return shown
	})
	// What the input method composes shows at the cursor, and goes once
	// it ends.
	onMain(func() { w.mark("か") }, true)
	shown, at := false, point{}
	onMain(func() { shown = gtkWidgetVisible(w.preedit) != 0; at, _ = w.caret() }, true)
	if !shown || at.X < 0 {
		t.Errorf("composed text shows %v at %v", shown, at)
	}
	onMain(func() { w.mark("") }, true)
	onMain(func() { shown = gtkWidgetVisible(w.preedit) != 0 }, true)
	if shown {
		t.Error("composed text stayed once it ended")
	}
	// What it inserts goes to the session as keys.
	onMain(func() { keyPressed(0, 'A', 0, gdkShift, w.win) }, true)
	onMain(func() { w.typeText("かé") }, true)
	onMain(func() { keyPressed(0, 0xff1b, 0, 0, w.win) }, true)
	until(t, "the text it inserted", func() bool {
		text := ""
		w.onLoop(func() { text = editortest.Text(e) })
		return text == "oneかé"
	})
	for _, k := range ":qa!\r" {
		keyval := uintptr(k)
		if k == '\r' {
			keyval = 0xff0d
		}
		onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B582
func TestB582GTKAccessibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\ntwo\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, k := range keys {
			keyval := uintptr(k)
			switch k {
			case '\r':
				keyval = 0xff0d
			case '\x1b':
				keyval = 0xff1b
			}
			onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
		}
	}
	// said reads the area with the focus as an assistive app does, through
	// its GtkAccessibleText: its role, its text and its caret.
	type said struct {
		role  string
		text  string
		caret uintptr
	}
	focus := func() said {
		var s said
		onMain(func() {
			area := gtkWindowFocus(w.win)
			if area == 0 {
				return
			}
			s.role = map[int32]string{roleOf("text-box"): "text-box", roleOf("label"): "label"}[gtkAccessibleRole(area)]
			iface := gTypeIfacePeek(**(**uintptr)(unsafe.Pointer(&area)), gtkTextIfaceType())
			slot := func(at uintptr) uintptr {
				return *(*uintptr)(unsafe.Add(*(*unsafe.Pointer)(unsafe.Pointer(&iface)), at))
			}
			bytes, _, _ := purego.SyscallN(slot(16), area, 0, math.MaxUint32)
			var n uintptr
			data := gBytesData(bytes, &n)
			s.text = string(unsafe.Slice(data, n))
			gBytesUnref(bytes)
			s.caret, _, _ = purego.SyscallN(slot(32), area)
		}, true)
		return s
	}
	// The current window's text has the focus, with the cursor as its caret.
	until(t, "the text to have the focus", func() bool { return focus() == said{"text-box", "one\ntwo", 0} })
	typed("j")
	until(t, "the caret to follow", func() bool { return focus().caret == 4 })
	// The palette takes the focus while it shows.
	typed(":ec")
	until(t, "the palette to have the focus", func() bool { return focus() == said{"text-box", "ec", 2} })
	typed("\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// B584
func TestB584GTKFont(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, k := range keys {
			keyval := uintptr(k)
			if k == '\r' {
				keyval = 0xff0d
			}
			onMain(func() { keyPressed(0, keyval, 0, 0, w.win) }, true)
		}
	}
	// now is the families the text asks Pango for, of which Pango takes
	// the first it has, and its size.
	now := func() (family string, points float64) {
		onMain(func() { family, points = pangoFontFamily(font), textSize }, true)
		return family, points
	}
	// :set changes the font's size and family at once.
	typed(":set gui.font_size 20\r:set gui.font NoSuchFont,DejaVu Sans Mono\r")
	until(t, "the families at 20 points", func() bool {
		f, p := now()
		var chrome string
		onMain(func() { chrome = pangoFontFamily(uiFont) }, true)
		return f == "NoSuchFont,DejaVu Sans Mono,Monospace" && p == 20 && strings.HasPrefix(chrome, "NoSuchFont,DejaVu Sans Mono,")
	})
	// No family and no size are the system's.
	typed(":set gui.font_size 0\r")
	until(t, "the system's size", func() bool { _, p := now(); return p == systemSize })
	typed(":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.win] }, true)
		return !open
	})
}

// roleOf is the value of the accessible role named nick.
func roleOf(nick string) int32 { return gEnumByNick(roleClass, nick).value }

var (
	gtkWindowFocus    func(w uintptr) uintptr
	gtkAccessibleRole func(a uintptr) int32
	gTypeIfacePeek    func(class uintptr, iface uint64) uintptr
	gBytesData        func(b uintptr, n *uintptr) *byte
	gBytesUnref       func(b uintptr)
	pangoFontFamily   func(desc uintptr) string
	gtkHeaderTitle    func(bar uintptr) uintptr
	gtkLabelText      func(l uintptr) string
	gtkWidgetActivate func(w uintptr) int32
	cairoCreate       func(surface uintptr) uintptr
	cairoDestroy      func(cr uintptr)
	cairoWritePNG     func(surface uintptr, path string) int32
	gtkWindowClose    func(w uintptr)
	gActionActivate   func(action, param uintptr)
	gtkWindowTitle    func(w uintptr) string
	gtkWidgetVisible  func(w uintptr) int32
)

// paint draws a pane or a box as GTK would, with its draw function, into
// an image, or returns nil before it has a size.
func paint(t *testing.T, area uintptr, draw func(area, cairo uintptr, width, height int32, data uintptr)) image.Image {
	width, height := gtkWidgetGetWidth(area), gtkWidgetGetHeight(area)
	if width == 0 || height == 0 {
		return nil
	}
	surface := cairoImageNew(0, width, height)
	defer cairoSurfaceFree(surface)
	c := cairoCreate(surface)
	draw(area, c, width, height, 0)
	cairoDestroy(c)
	path := filepath.Join(t.TempDir(), "area.png")
	cairoWritePNG(surface, path)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// inked reports whether the cell at column x and row y of a pane's image
// shows anything but the ground.
func inked(img image.Image, x, y int) bool {
	ground := img.At(1, 1)
	for py := int(pad + float64(y)*cellH); py < int(pad+float64(y+1)*cellH); py++ {
		for px := int(pad + float64(x)*cellW); px < int(pad+float64(x+1)*cellW); px++ {
			if img.At(px, py) != ground {
				return true
			}
		}
	}
	return false
}
