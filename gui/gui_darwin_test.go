package gui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	pngenc "image/png"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/frontend/theme"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/modules/bufferline"
	"github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/completion"
	filesmod "github.com/y3owk1n/fude/modules/files"
	"github.com/y3owk1n/fude/modules/hints"
	lspmod "github.com/y3owk1n/fude/modules/lsp"
	termmod "github.com/y3owk1n/fude/modules/terminal"
	winmod "github.com/y3owk1n/fude/modules/window"
	"github.com/y3owk1n/fude/vim"
)

// TestMain runs AppKit on the main thread, in the background, and the tests
// beside it.
func TestMain(m *testing.M) {
	// As a language server it needs no window.
	if os.Getenv("FUDE_GUI_FAKE_LSP") != "" {
		os.Exit(m.Run())
	}
	load()
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	dragInfo, err = objc.RegisterClass("FudeTestDrag", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		{Cmd: sel("draggingPasteboard"), Fn: func(objc.ID, objc.SEL) objc.ID { return dragged }},
	})
	if err != nil {
		panic(err)
	}
	for _, f := range []any{&msgMarked, &msgInserted, &msgMouseEvent, &msgFirstRect, &msgCache, &msgColorAt, &msgComponent, &msgRangeOf, &msgInitSize} {
		purego.RegisterLibFunc(f, lib, "objc_msgSend")
	}
	app := startApp(true)
	code := 0
	go func() {
		code = editortest.Main(m)
		onMain(func() { stop(app) }, false)
	}()
	app.Send(sel("run"))
	os.Exit(code)
}

// B547
func TestB547NativeWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	// Keys typed in the window reach the session as Vim keys.
	for _, k := range []struct {
		code        uint16
		mods        uint
		chars, bare string
	}{{0, modShift, "A", "a"}, {18, modShift, "!", "1"}, {53, 0, "\x1b", "\x1b"}} {
		onMain(func() { press(w, k.code, k.mods, k.chars, k.bare) }, true)
	}
	until(t, "A!<Esc> to type", func() bool {
		var text string
		w.onLoop(func() { text = editortest.Text(e) })
		return text == "one!"
	})
	// The window shows the text: a cell with text differs from an empty one.
	until(t, "the text to show", func() bool {
		var drawn, empty bool
		onMain(func() {
			for _, p := range w.panes {
				drawn, empty = inked(p.text, pad, 0, 0), inked(p.text, pad, 40, 10)
			}
		}, true)
		return drawn && !empty
	})
	// A resized window resizes the session.
	onMain(func() { msgSetSize(w.win, sel("setContentSize:"), size{cellW*60 + 2*pad, cellH*20 + 2*pad}) }, true)
	until(t, "the session to take 60 columns", func() bool {
		cols := 0
		w.onLoop(func() { cols = e.Windows()[0].W })
		return cols == 60
	})
	for _, c := range ":qa!" {
		onMain(func() { press(w, 0, 0, string(c), string(c)) }, true)
	}
	onMain(func() { press(w, 36, 0, "\r", "\r") }, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B548
func TestB548NativeBoxes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, c := range keys {
			code := uint16(0)
			switch c {
			case '\r':
				code = 36
			case '\x1b':
				code = 53
			}
			onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
		}
	}
	// shows reports whether b shows with something drawn in its first cells.
	shows := func(b *box) bool {
		var hidden, drawn bool
		onMain(func() {
			hidden = objc.Send[bool](b.view, sel("isHidden"))
			drawn = inked(b.view, inset, 0, 0) || inked(b.view, inset, 1, 0) || inked(b.view, inset, 2, 0)
		}, true)
		return !hidden && drawn
	}
	// The command line shows in the palette, and goes with it.
	typed(":")
	until(t, "the palette", func() bool { return shows(w.panel) })
	typed("\x1b")
	until(t, "the palette to go", func() bool { return !shows(w.panel) })
	// A message shows as a toast.
	typed(":nosuch\r")
	until(t, "the toast", func() bool { return shows(w.toast) })
	// A question shows in a sheet, which n answers.
	editortest.Change(t, path, "two\n")
	typed(":w\r")
	var sheet objc.ID
	until(t, "the sheet", func() bool {
		onMain(func() { sheet = w.win.Send(sel("attachedSheet")) }, true)
		return sheet != 0
	})
	onMain(func() {
		n := objc.Send[int](sheet, sel("windowNumber"))
		ev := msgKeyEvent(cls("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
			10, point{}, 0, 0, n, 0, nsString("n"), nsString("n"), false, 45)
		sheet.Send(sel("performKeyEquivalent:"), ev)
	}, true)
	until(t, "n to answer", func() bool {
		var q string
		w.onLoop(func() { q = e.Confirm() })
		onMain(func() { sheet = w.win.Send(sel("attachedSheet")) }, true)
		return q == "" && sheet == 0
	})
	if got, _ := os.ReadFile(path); string(got) != "two\n" {
		t.Errorf("answering no wrote %q", got)
	}
	// The finder shows in a box.
	typed(":pick\r")
	until(t, "the finder", func() bool { return shows(w.panel) })
	typed("\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B549
func TestB549NativeSplits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	onMain(func() { press(w, 13, modControl, "\x17", "w") }, true)
	onMain(func() { press(w, 9, 0, "v", "v") }, true)
	// sides returns the two windows from the left, once there are two.
	sides := func() (l, r wire.Window, ok bool) {
		w.onLoop(func() {
			if ws := e.Windows(); len(ws) == 2 {
				l, r, ok = ws[0], ws[1], true
				if l.X > r.X {
					l, r = r, l
				}
			}
		})
		return l, r, ok
	}
	// The panes place the windows side by side across the area.
	until(t, "two windows side by side", func() bool {
		l, r, ok := sides()
		var cols int
		onMain(func() { cols = w.sent.cols }, true)
		return ok && r.X >= l.X+l.W && l.W+r.W >= cols-3
	})
	// Dragging the divider resizes the windows beside it.
	onMain(func() { msgSetPosition(subviews(w.area)[0], sel("setPosition:ofDividerAtIndex:"), 200, 0) }, true)
	until(t, "the left window to follow the divider", func() bool {
		l, r, _ := sides()
		return l.W == int((200-2*pad)/cellW) && r.X >= l.X+l.W
	})
	// The window with the focus has its mode in its status bar.
	until(t, "the status bar", func() bool {
		drawn := false
		onMain(func() {
			for _, p := range w.panes {
				drawn = drawn || p.current && inked(p.status, pad, 0, 0)
			}
		}, true)
		return drawn
	})
	for _, c := range ":qa!" {
		onMain(func() { press(w, 0, 0, string(c), string(c)) }, true)
	}
	onMain(func() { press(w, 36, 0, "\r", "\r") }, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B550
func TestB550BoxesByAnchor(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "b.txt"), "two\n")
	path := filepath.Join(dir, "a.txt")
	editortest.Change(t, path, "hello helium\n")
	e := editortest.Open(t, path)
	e.Use(completion.New())
	e.Use(hints.New())
	e.Use(winmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, c := range keys {
			code, mods, ch := uint16(0), uint(0), string(c)
			switch c {
			case '\r':
				code = 36
			case '\x1b':
				code = 53
			case '\x0e':
				code, mods, ch = 45, modControl, "n"
			}
			onMain(func() { press(w, code, mods, ch, ch) }, true)
		}
	}
	// frame returns where the box at key is in the window's content, once
	// it shows something.
	frame := func(key int) (rect, bool) {
		var r rect
		ok := false
		onMain(func() {
			if b := w.floats[key]; b != nil && !objc.Send[bool](b.view, sel("isHidden")) && inked(b.view, inset, 0, 0) {
				r, ok = msgRect(b.frame, sel("frame")), true
			}
		}, true)
		return r, ok
	}
	var top float64
	onMain(func() { top = barHeight(w.win) }, true)
	// The completion menu shows under the cursor, on the second line.
	typed("Ohe\x0e")
	until(t, "the menu under the cursor", func() bool {
		r, ok := frame(-1)
		return ok && r.Origin.Y > top+pad+cellH
	})
	// The key hints show across the bottom.
	typed("\x1bu")
	typed("g")
	until(t, "the key hints at the bottom", func() bool {
		r, ok := frame(-1)
		var bounds rect
		onMain(func() { bounds = msgRect(w.view, sel("bounds")) }, true)
		return ok && r.Origin.Y+r.Size.H > bounds.Size.H*3/4 && r.Size.W > bounds.Size.W/2
	})
	// A float shows in a box at its place, with its status line.
	typed("\x1b:window float split " + filepath.Join(dir, "b.txt") + "\r")
	until(t, "the float", func() bool {
		shown := false
		onMain(func() {
			for id, b := range w.floats {
				shown = shown || id > 0 && b.boxed != nil && b.foot() > 0 && inked(b.view, inset, 0, 0)
			}
		}, true)
		return shown
	})
	typed("\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B551
func TestB551InputMethodsAndMenu(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	text := func() string {
		var got string
		w.onLoop(func() { got = editortest.Text(e) })
		return got
	}
	onMain(func() { press(w, 34, 0, "i", "i") }, true)
	// Text being composed shows at the cursor and types nothing, until the
	// input method inserts it.
	until(t, "insert mode", func() bool {
		var mode string
		onMain(func() { mode = w.tabs.mode }, true)
		return mode == "insert"
	})
	onMain(func() {
		msgMarked(w.view, sel("setMarkedText:selectedRange:replacementRange:"), nsString("にほん"), nsRange{3, 0}, nsRange{Loc: notFound})
	}, true)
	var composing, shown bool
	var at, frame rect
	onMain(func() {
		composing = objc.Send[bool](w.view, sel("hasMarkedText"))
		shown = !objc.Send[bool](w.preedit, sel("isHidden")) && inked(w.preedit, 0, 0, 0)
		at = msgFirstRect(w.view, sel("firstRectForCharacterRange:actualRange:"), nsRange{}, 0)
		frame = msgRect(w.win, sel("frame"))
	}, true)
	if !composing || !shown || text() != "one" {
		t.Errorf("composing showed %v and typed %q", shown, text())
	}
	// The candidates go by the cursor, in the window.
	if at.Origin.X < frame.Origin.X || at.Origin.X > frame.Origin.X+frame.Size.W || at.Origin.Y < frame.Origin.Y || at.Origin.Y > frame.Origin.Y+frame.Size.H {
		t.Errorf("the cursor is at %v, outside the window at %v", at, frame)
	}
	onMain(func() {
		msgInserted(w.view, sel("insertText:replacementRange:"), nsString("日本"), nsRange{Loc: notFound})
	}, true)
	until(t, "the inserted text", func() bool { return text() == "日本one" })
	// Command-Z undoes, and Command-Q quits.
	onMain(func() { press(w, 53, 0, "\x1b", "\x1b") }, true)
	// command sends the action of the menu item Command and key choose to
	// the window, as AppKit does with the window that has the keys, which a
	// test's window in the background does not.
	command := func(key string) {
		var find func(m objc.ID) objc.ID
		find = func(m objc.ID) objc.ID {
			for i := range objc.Send[int](m, sel("numberOfItems")) {
				it := m.Send(sel("itemAtIndex:"), i)
				if sub := it.Send(sel("submenu")); sub != 0 {
					if found := find(sub); found != 0 {
						return found
					}
				}
				if goString(it.Send(sel("keyEquivalent"))) == key && objc.Send[uint](it, sel("keyEquivalentModifierMask")) == modCommand {
					return it
				}
			}
			return 0
		}
		onMain(func() {
			app := cls("NSApplication").Send(sel("sharedApplication"))
			it := find(app.Send(sel("mainMenu")))
			to := it.Send(sel("target"))
			if to == 0 {
				to = w.win.Send(sel("firstResponder"))
			}
			app.Send(sel("sendAction:to:from:"), objc.Send[objc.SEL](it, sel("action")), to, it)
		}, true)
	}
	until(t, "normal mode", func() bool {
		var mode string
		onMain(func() { mode = w.tabs.mode }, true)
		return mode == "normal"
	})
	command("z")
	until(t, "Command-Z to undo", func() bool { return text() == "one" })
	command("q")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B552
func TestB552Mouse(t *testing.T) {
	dir := t.TempDir()
	var lines []string
	for i := range 60 {
		lines = append(lines, fmt.Sprint("line ", i))
	}
	editortest.Change(t, filepath.Join(dir, "b.txt"), "two\n")
	path := filepath.Join(dir, "a.txt")
	editortest.Change(t, path, strings.Join(lines, "\n"))
	e := editortest.Open(t, path)
	e.Use(bufferline.New())
	e.Use(buffers.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	for _, c := range ":e b.txt" {
		onMain(func() { press(w, 0, 0, string(c), string(c)) }, true)
	}
	onMain(func() { press(w, 36, 0, "\r", "\r") }, true)
	// shown is the name of the buffer the window shows, and its top.
	shown := func() (string, int) {
		var name string
		var top int
		w.onLoop(func() { name, top = filepath.Base(e.Windows()[0].Name), e.Windows()[0].Top })
		return name, top
	}
	until(t, "b.txt", func() bool { name, _ := shown(); return name == "b.txt" })
	// click sends a click at p in view, as the mouse does.
	click := func(view objc.ID, p point) {
		onMain(func() {
			at := msgConvertPoint(view, sel("convertPoint:toView:"), p, 0)
			ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				1, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 1, 1)
			w.win.Send(sel("sendEvent:"), ev)
		}, true)
	}
	// A click on the first buffer's tab shows it.
	until(t, "the tabs", func() bool {
		n := 0
		onMain(func() { n = len(w.hits) }, true)
		return n == 2
	})
	var tab hit
	onMain(func() { tab = w.hits[0] }, true)
	click(w.bar, point{(tab.from + tab.to) / 2, 10})
	until(t, "a.txt", func() bool { name, _ := shown(); return name == "a.txt" })
	// A click in the text puts the cursor on the cell.
	var text objc.ID
	onMain(func() {
		for _, p := range w.panes {
			text = p.text
		}
	}, true)
	click(text, point{pad + 2.5*cellW, pad + 3.5*cellH})
	until(t, "the cursor on the cell clicked", func() bool {
		var at int
		w.onLoop(func() { at = e.Cursor() })
		return at == strings.Index(strings.Join(lines, "\n"), "line 3")+2
	})
	// A turn of the wheel scrolls the window.
	onMain(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL)
		if err != nil {
			t.Fatal(err)
		}
		var scroll func(uintptr, uint32, uint32, int32, int32, int32) uintptr
		purego.RegisterLibFunc(&scroll, cg, "CGEventCreateScrollWheelEvent2")
		ev := cls("NSEvent").Send(sel("eventWithCGEvent:"), scroll(0, 1, 1, -3, 0, 0))
		text.Send(sel("scrollWheel:"), ev)
	}, true)
	until(t, "the view to scroll", func() bool { _, top := shown(); return top > 0 })
	for _, c := range ":qa!" {
		onMain(func() { press(w, 0, 0, string(c), string(c)) }, true)
	}
	onMain(func() { press(w, 36, 0, "\r", "\r") }, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B553
func TestB553TerminalsAndImages(t *testing.T) {
	t.Setenv("SHELL", "sh")
	dir := t.TempDir()
	t.Chdir(dir)
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{255, 0, 0, 255}}, image.Point{}, draw.Src)
	var png bytes.Buffer
	if err := pngenc.Encode(&png, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pic.png"), png.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\n")
	e := editortest.Open(t, "a.txt")
	e.Use(termmod.New())
	e.Use(filesmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, c := range keys {
			code := uint16(0)
			switch c {
			case '\r':
				code = 36
			case '\x1b':
				code = 53
			}
			onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
		}
	}
	// A terminal window shows its program's screen.
	typed(":terminal cat\rhi\r")
	term := 0
	until(t, "the terminal", func() bool {
		w.onLoop(func() {
			for _, v := range e.Windows() {
				if v.Cells != nil {
					term = v.ID
				}
			}
		})
		return term != 0
	})
	// cat shows what it reads under the line typed.
	until(t, "the terminal's screen", func() bool {
		drawn := false
		onMain(func() {
			p := w.panes[term]
			drawn = p != nil && inked(p.text, pad, 0, 0) && inked(p.text, pad, 0, 1)
		}, true)
		return drawn
	})
	// The finder shows an image from its pixels.
	typed("\x1b\x1b:pick files\rpic.png")
	until(t, "the image in the finder", func() bool {
		shown := false
		onMain(func() {
			b := w.panel
			if b.boxed == nil || len(b.pics) == 0 || w.images[b.pics[0].id] == 0 {
				return
			}
			// The middle of its first cell is the image's red.
			p, o := b.pics[0], b.cellsOrigin()
			rgb, scale, ok := pixels(b.view)
			if ok {
				c := rgb(int(scale*(o.X+(float64(p.x)+0.5)*cellW)), int(scale*(o.Y+(float64(p.y)+0.5)*cellH)))
				shown = c[0] > 0.8 && c[1] < 0.2 && c[2] < 0.2
			}
		}, true)
		return shown
	})
	typed("\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B555
func TestB555MouseOnTabsFilesAndWords(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, f := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		editortest.Change(t, filepath.Join(dir, f), "one two\n")
	}
	e := editortest.Open(t, "a.txt")
	e.Use(bufferline.New())
	e.Use(buffers.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	for _, c := range ":e b.txt\r:e c.txt\r" {
		code := uint16(0)
		if c == '\r' {
			code = 36
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	row := func() string {
		var names []string
		w.onLoop(func() {
			for _, tb := range e.Buffers() {
				names = append(names, strings.TrimSuffix(tb.Name, ".txt"))
			}
		})
		return strings.Join(names, " ")
	}
	until(t, "three tabs", func() bool {
		n := 0
		onMain(func() { n = len(w.hits) }, true)
		return row() == "a b c" && n == 3
	})
	// mouse sends a mouse event of the type at x along the strip.
	mouse := func(kind uint, x float64, clicks int) {
		onMain(func() {
			at := msgConvertPoint(w.bar, sel("convertPoint:toView:"), point{x, 10}, 0)
			ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				kind, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, clicks, 1)
			w.win.Send(sel("sendEvent:"), ev)
		}, true)
	}
	var tabs []hit
	onMain(func() { tabs = slices.Clone(w.hits) }, true)
	// Dragging the first tab past the last moves it there.
	mouse(1, (tabs[0].from+tabs[0].to)/2, 1)
	mouse(6, tabs[2].to-2, 1)
	mouse(2, tabs[2].to-2, 1)
	until(t, "a last", func() bool { return row() == "b c a" })
	// A click on a tab's × closes its buffer.
	until(t, "the tabs to move", func() bool {
		onMain(func() { tabs = slices.Clone(w.hits) }, true)
		return len(tabs) == 3 && tabs[0].n == 2
	})
	// The × under the mouse draws on a ground of its own. The strip draws
	// over the material, so the ground shows in the alpha.
	lit := func(x float64) float64 {
		var alpha float64
		onMain(func() {
			hover(w.bar, x)
			b := msgRect(w.bar, sel("bounds"))
			rep := msgInitView(w.bar, sel("bitmapImageRepForCachingDisplayInRect:"), b)
			msgCache(w.bar, sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
			scale := float64(objc.Send[int](rep, sel("pixelsWide"))) / b.Size.W
			alpha = msgComponent(msgColorAt(rep, sel("colorAtX:y:"), int(scale*(shutFrom(tabs[0].to)+3)), int(scale*b.Size.H/2)), sel("alphaComponent"))
		}, true)
		return alpha
	}
	if lit(tabs[0].to-pad-slotW/2) <= lit(tabs[0].from+pad) {
		t.Error("the × under the mouse has no ground of its own")
	}
	mouse(1, tabs[0].to-pad-slotW/2, 1)
	until(t, "b to close", func() bool { return row() == "c a" })
	// A file dropped on the window opens.
	onMain(func() {
		pb := cls("NSPasteboard").Send(sel("pasteboardWithUniqueName"))
		pb.Send(sel("clearContents"))
		url := cls("NSURL").Send(sel("fileURLWithPath:"), nsString(filepath.Join(dir, "d.txt")))
		pb.Send(sel("writeObjects:"), cls("NSArray").Send(sel("arrayWithObject:"), url))
		info := objc.ID(dragInfo).Send(sel("alloc")).Send(sel("init"))
		dragged = pb
		w.view.Send(sel("performDragOperation:"), info)
	}, true)
	until(t, "d to open", func() bool { return row() == "c a d" })
	// A double click on a word selects it, and a triple click its line.
	var text objc.ID
	onMain(func() {
		for _, p := range w.panes {
			text = p.text
		}
	}, true)
	click := func(clicks int) {
		onMain(func() {
			at := msgConvertPoint(text, sel("convertPoint:toView:"), point{pad + 5.5*cellW, pad + cellH/2}, 0)
			for _, kind := range []uint{1, 2} {
				ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
					kind, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, clicks, 1)
				w.win.Send(sel("sendEvent:"), ev)
			}
		}, true)
	}
	selected := func() (s vim.Selection, ok bool) {
		until(t, "the clicks to be given", func() bool { return len(w.mice) == 0 })
		w.onLoop(func() { s, ok = e.Selection() })
		return s, ok
	}
	click(1)
	click(2)
	if s, ok := selected(); !ok || s.Start != 4 || s.End != 7 {
		t.Errorf("a double click selected %+v, %v", s, ok)
	}
	click(3)
	if s, ok := selected(); !ok || s.Kind != "V" {
		t.Errorf("a triple click selected %+v, %v", s, ok)
	}
	for _, c := range "\x1b:qa!\r" {
		code := uint16(0)
		switch c {
		case '\r':
			code = 36
		case '\x1b':
			code = 53
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B556
func TestB556MenusByMouse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	for _, c := range ":\t" {
		code := uint16(0)
		if c == '\t' {
			code = 48
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	// The menu of every command is longer than the palette shows.
	var items []string
	until(t, "the menu", func() bool {
		w.onLoop(func() {
			m, _ := e.Menu()
			items = nil
			for _, it := range m.Items {
				items = append(items, it.Label)
			}
		})
		shown := false
		onMain(func() { shown = w.panel.boxed != nil && w.panel.list != nil && len(w.panel.list.items) > 20 }, true)
		return len(items) > 20 && shown
	})
	line := func() string {
		var l string
		w.onLoop(func() { l = e.Line() })
		return l
	}
	// A click on an item puts it in the line.
	onMain(func() {
		l := w.panel.list
		at := msgConvertPoint(w.panel.view, sel("convertPoint:toView:"), point{40, l.top() + 1.5*l.rowH}, 0)
		ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			1, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 1, 1)
		w.win.Send(sel("sendEvent:"), ev)
	}, true)
	until(t, "the second item in the line", func() bool { return line() == ":"+items[1] })
	// A turn of the wheel scrolls the menu, and leaves its selection.
	onMain(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL)
		if err != nil {
			t.Fatal(err)
		}
		var scroll func(uintptr, uint32, uint32, int32, int32, int32) uintptr
		purego.RegisterLibFunc(&scroll, cg, "CGEventCreateScrollWheelEvent2")
		w.panel.view.Send(sel("scrollWheel:"), cls("NSEvent").Send(sel("eventWithCGEvent:"), scroll(0, 1, 1, -1, 0, 0)))
	}, true)
	until(t, "the menu to scroll", func() bool {
		var scrolled float64
		onMain(func() { scrolled = w.panel.scroll }, true)
		return scrolled > 0 && line() == ":"+items[1]
	})
	// A click off the palette closes it.
	onMain(func() {
		for _, p := range w.panes {
			at := msgConvertPoint(p.text, sel("convertPoint:toView:"), point{pad + cellW/2, pad + cellH/2}, 0)
			ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				1, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 1, 1)
			w.win.Send(sel("sendEvent:"), ev)
		}
	}, true)
	until(t, "the command line to close", func() bool {
		var mode vim.Mode
		w.onLoop(func() { mode = e.Mode() })
		return mode == vim.Normal
	})
	for _, c := range ":qa!\r" {
		code := uint16(0)
		if c == '\r' {
			code = 36
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B557
func TestB557MovingTextByMouse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one two three\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var text objc.ID
	until(t, "the pane", func() bool {
		onMain(func() {
			for _, p := range w.panes {
				text = p.text
			}
		}, true)
		return text != 0
	})
	// mouse sends a mouse event of the kind at column x of the first row.
	mouse := func(kind uint, x float64) {
		onMain(func() {
			at := msgConvertPoint(text, sel("convertPoint:toView:"), point{pad + (x+0.5)*cellW, pad + cellH/2}, 0)
			ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				kind, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 1, 1)
			w.win.Send(sel("sendEvent:"), ev)
		}, true)
	}
	// Select "one " by dragging across it, then drag it past the end.
	mouse(1, 0)
	mouse(6, 3)
	mouse(2, 3)
	mouse(1, 1)
	mouse(6, 20)
	mouse(2, 20)
	until(t, "the selection to move", func() bool {
		var got string
		w.onLoop(func() { got = editortest.Text(e) })
		return got == "two threeone "
	})
	onMain(func() { press(w, 0, 0, ":", ":") }, true)
	for _, c := range "qa!\r" {
		code := uint16(0)
		if c == '\r' {
			code = 36
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B558
func TestB558WindowFollowsTheSession(t *testing.T) {
	editortest.Config(t, "theme = \"light\"\n")
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Start(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	// The window takes the theme's look, shows the file's icon, and a dot in
	// its close button once the file has changes.
	until(t, "the light look and the file", func() bool {
		var look, file string
		onMain(func() {
			look = goString(w.win.Send(sel("effectiveAppearance")).Send(sel("name")))
			file = goString(w.win.Send(sel("representedFilename")))
		}, true)
		return look == "NSAppearanceNameAqua" && file == path
	})
	onMain(func() { press(w, 31, 0, "o", "o") }, true)
	onMain(func() { press(w, 7, 0, "x", "x") }, true)
	onMain(func() { press(w, 53, 0, "\x1b", "\x1b") }, true)
	until(t, "the dot", func() bool {
		edited := false
		onMain(func() { edited = objc.Send[bool](w.win, sel("isDocumentEdited")) }, true)
		return edited
	})
	// Command-= makes the text bigger, so the window holds fewer columns,
	// and Command-0 makes it its first size again.
	cols := func() int {
		n := 0
		w.onLoop(func() { n = e.Windows()[0].W })
		return n
	}
	before := cols()
	onMain(func() { mainRun.Send(sel("bigger:"), 0) }, true)
	until(t, "fewer columns", func() bool { return cols() < before })
	onMain(func() { mainRun.Send(sel("actualSize:"), 0) }, true)
	until(t, "the columns again", func() bool { return cols() == before })
	for _, c := range "\x1b:qa!\r" {
		code := uint16(0)
		switch c {
		case '\r':
			code = 36
		case '\x1b':
			code = 53
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B559
func TestB559SystemChrome(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	effect := cls("NSVisualEffectView")
	kind := func(v objc.ID) bool { return v != 0 && objc.Send[bool](v, sel("isKindOfClass:"), effect) }
	var text objc.ID
	until(t, "the pane", func() bool {
		onMain(func() {
			for _, p := range w.panes {
				text = p.text
			}
		}, true)
		return text != 0
	})
	onMain(func() {
		// The tab strip and each status bar draw on the system's material.
		if !kind(w.barBack) {
			t.Error("the tab strip has no material under it")
		}
		for _, p := range w.panes {
			if !slices.ContainsFunc(subviews(p.view), kind) {
				t.Error("a status bar has no material under it")
			}
		}
		// The rows past the text show nothing.
		if inked(text, pad, 0, 4) {
			t.Error("a row past the text shows something")
		}
	}, true)
	// The palette is a rounded panel of the system's material, its selected
	// item in the system's selection.
	for _, c := range ":se\t" {
		code := uint16(0)
		if c == '\t' {
			code = 48
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	until(t, "the selection", func() bool {
		ok := false
		onMain(func() {
			ok = w.panel.boxed != nil && w.panel.list != nil && len(w.panel.list.items) > 0 && w.panel.list.selected >= 0
		}, true)
		return ok
	})
	onMain(func() {
		back := subviews(w.panel.frame)[0]
		if !kind(back) || msgFloat(back.Send(sel("layer")), sel("cornerRadius")) == 0 {
			t.Error("the palette is not a rounded panel of the system's material")
		}
		cls("NSAppearance").Send(sel("setCurrentAppearance:"), w.win.Send(sel("effectiveAppearance")))
		srgb := cls("NSColorSpace").Send(sel("sRGBColorSpace"))
		want := cls("NSColor").Send(sel("selectedContentBackgroundColor")).Send(sel("colorUsingColorSpace:"), srgb)
		rgb, scale, _ := pixels(w.panel.view)
		l := w.panel.list
		got := rgb(int(scale*12), int(scale*(l.top()+(float64(l.selected)+0.5)*l.rowH)))
		for i, part := range []string{"redComponent", "greenComponent", "blueComponent"} {
			if d := got[i] - msgComponent(want, sel(part)); d > 0.05 || d < -0.05 {
				t.Errorf("the selected item is %v, not the system's selection", got)
				break
			}
		}
	}, true)
	for _, c := range "\x1b:qa!\r" {
		code := uint16(0)
		switch c {
		case '\r':
			code = 36
		case '\x1b':
			code = 53
		}
		onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
	}
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B560
func TestB560NativeLists(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\n")
	e := editortest.Open(t, "a.txt")
	e.Use(filesmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, c := range keys {
			code := uint16(0)
			switch c {
			case '\r':
				code = 36
			case '\t':
				code = 48
			case '\x1b':
				code = 53
			}
			onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
		}
	}
	// The command line's menu is a list of rows the window lays out, with
	// no cells of the editor's.
	typed(":se\t")
	until(t, "the list", func() bool {
		ok := false
		onMain(func() {
			if w.panel == nil || w.panel.boxed == nil {
				return
			}
			l := w.panel.list
			ok = l != nil && len(l.items) > 2 && w.panel.cells == nil &&
				inked(w.panel.view, 0, int((8+l.width/2)/cellW), int((l.top()+0.5*l.rowH)/cellH))
		}, true)
		return ok
	})
	// The row the mouse is over lights.
	onMain(func() {
		l := w.panel.list
		at := msgConvertPoint(w.panel.view, sel("convertPoint:toView:"), point{40, l.top() + 2.5*l.rowH}, 0)
		ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			5, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 0, 0)
		w.panel.view.Send(sel("mouseMoved:"), ev)
		w.panel.view.Send(sel("display"))
		if l.hovered != 2 {
			t.Errorf("the mouse over the third row lights row %d", l.hovered)
		}
	}, true)
	// The finder's files show the Finder's icon for them.
	typed("\x1b:pick files\r")
	until(t, "the file's icon", func() bool {
		ok := false
		onMain(func() {
			if l := w.panel.list; w.panel.boxed != nil && l != nil {
				for _, it := range l.items {
					ok = ok || it.Label == "a.txt" && icons[it.Path] != 0
				}
			}
		}, true)
		return ok
	})
	typed("\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B561
func TestB561Feel(t *testing.T) {
	var lines []string
	for i := range 200 {
		lines = append(lines, fmt.Sprint("line ", i))
	}
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, strings.Join(lines, "\n"))
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var p *pane
	until(t, "the pane", func() bool {
		onMain(func() {
			for _, q := range w.panes {
				p = q
			}
		}, true)
		return p != nil
	})
	top := func() int {
		n := 0
		w.onLoop(func() { n = e.Windows()[0].Text.LineOf(e.Windows()[0].Top) })
		return n
	}
	// A right click with no language server shows only the clipboard's
	// items, Copy greyed with nothing selected, and Select All there selects
	// the text.
	onMain(func() {
		at := msgConvertPoint(p.text, sel("convertPoint:toView:"), point{pad + cellW/2, pad + cellH/2}, 0)
		ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
			3, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 1, 1)
		menu := p.text.Send(sel("menuForEvent:"), ev)
		if !objc.Send[bool](menu.Send(sel("itemWithTitle:"), nsString("Go to Definition")), sel("isHidden")) {
			t.Error("the menu offers the language server's items with no server")
		}
		if objc.Send[bool](menu.Send(sel("itemWithTitle:"), nsString("Copy")), sel("isEnabled")) {
			t.Error("the menu offers Copy with nothing selected")
		}
		item := menu.Send(sel("itemWithTitle:"), nsString("Select All"))
		cls("NSApplication").Send(sel("sharedApplication")).Send(sel("sendAction:to:from:"), objc.Send[objc.SEL](item, sel("action")), w.view, item)
	}, true)
	until(t, "the text to be selected", func() bool {
		var s vim.Selection
		var ok bool
		w.onLoop(func() { s, ok = e.Selection() })
		return ok && s.Start == 0 && s.Kind == "V"
	})
	onMain(func() { press(w, 53, 0, "\x1b", "\x1b") }, true)
	for range 2 {
		onMain(func() { press(w, 5, 0, "g", "g") }, true)
	}
	until(t, "the top", func() bool { return top() == 0 })
	// Dragging the scroll bar's knob to its end scrolls to the end.
	until(t, "the scroll bar", func() bool {
		ok := false
		onMain(func() { ok = p.lines == 200 }, true)
		return ok
	})
	onMain(func() {
		msgSetFloat(p.scroller, sel("setDoubleValue:"), 1)
		p.text.Send(sel("scrolled:"), p.scroller)
	}, true)
	until(t, "the end", func() bool { return top() > 150 })
	// A trackpad moves the text by points: two rows and a half up show
	// three lines more, with the text moved up by the half left over.
	before := top()
	onMain(func() {
		cg, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics", purego.RTLD_GLOBAL)
		if err != nil {
			t.Fatal(err)
		}
		var scroll func(uintptr, uint32, uint32, int32, int32, int32) uintptr
		purego.RegisterLibFunc(&scroll, cg, "CGEventCreateScrollWheelEvent2")
		p.text.Send(sel("scrollWheel:"), cls("NSEvent").Send(sel("eventWithCGEvent:"), scroll(0, 0, 1, int32(2.5*cellH), 0, 0)))
	}, true)
	until(t, "the text two rows and a half up", func() bool {
		var offset float64
		onMain(func() { offset = p.offset }, true)
		return top() == before-3 && offset > cellH/4 && offset < 3*cellH/4
	})
	for _, c := range ":qa!" {
		onMain(func() { press(w, 0, 0, string(c), string(c)) }, true)
	}
	onMain(func() { press(w, 36, 0, "\r", "\r") }, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// dragInfo is a class whose objects tell of a drag of the pasteboard
// dragged, as AppKit tells a view of a drop.
var (
	dragInfo objc.Class
	dragged  objc.ID
)

// B562
func TestB562HoverAtTheMouse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one two\n")
	e := editortest.Open(t, path)
	e.Use(hovering{})
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var p *pane
	until(t, "the text", func() bool {
		onMain(func() {
			for _, q := range w.panes {
				if len(q.cells) > 0 {
					p = q
				}
			}
		}, true)
		return p != nil
	})
	move := func(col float64) {
		onMain(func() {
			at := msgConvertPoint(p.text, sel("convertPoint:toView:"), point{pad + col*cellW, pad + cellH/2}, 0)
			ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				5, at, 0, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 0, 0)
			p.text.Send(sel("mouseMoved:"), ev)
		}, true)
	}
	float := func() string {
		text := ""
		w.onLoop(func() {
			for _, v := range e.Windows() {
				if v.Float == "cursor" {
					text = v.Text.Slice(0, v.Text.Len())
				}
			}
		})
		return text
	}
	// A rest on a word shows its hover, and a move off it closes it.
	move(5.5)
	until(t, "the hover of two", func() bool { return float() == "at 5" })
	move(3.5)
	until(t, "the hover to close", func() bool { return float() == "" })
	onMain(func() {
		for _, c := range ":qa!\r" {
			code := uint16(0)
			if c == '\r' {
				code = 36
			}
			press(w, code, 0, string(c), string(c))
		}
	}, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B563
func TestB563CommandClickGoesToTheDefinition(t *testing.T) {
	t.Setenv("FUDE_GUI_FAKE_LSP", "1")
	editortest.Config(t, fmt.Sprintf("lsp.fake.command = [%q, \"-test.run=^TestFakeServer$\"]\n[filetype.go]\nlsp = [\"fake\"]\n", os.Args[0]))
	path := filepath.Join(t.TempDir(), "a.go")
	editortest.Change(t, path, "one two\n")
	e := editortest.Start(t, path)
	e.Use(lspmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var p *pane
	until(t, "the server", func() bool {
		onMain(func() {
			for _, q := range w.panes {
				if slices.Contains(q.offers, "definition") && len(q.cells) > 0 {
					p = q
				}
			}
		}, true)
		return p != nil
	})
	mouse := func(kind uint, col float64, mods uint) {
		onMain(func() {
			at := msgConvertPoint(p.text, sel("convertPoint:toView:"), point{pad + col*cellW, pad + cellH/2}, 0)
			ev := msgMouseEvent(cls("NSEvent"), sel("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:"),
				kind, at, mods, 0, objc.Send[int](w.win, sel("windowNumber")), 0, 0, 1, 1)
			switch kind {
			case 5:
				p.text.Send(sel("mouseMoved:"), ev)
			default:
				w.win.Send(sel("sendEvent:"), ev)
			}
		}, true)
	}
	// The text starts after the gutter, at the column of o.
	one := 0
	onMain(func() {
		for one < len(p.cells[0]) && p.cells[0][one].text != "o" {
			one++
		}
	}, true)
	two := float64(one) + 5.5
	// underlined reports whether the bottom of column x of the first row
	// shows anything but the cell's ground.
	underlined := func(x int) bool {
		ok := false
		onMain(func() {
			rgb, scale, drawn := pixels(p.text)
			if !drawn {
				return
			}
			top, bottom := int(scale*(pad+1)), int(scale*(pad+cellH-1.5))
			for px := int(scale * (pad + float64(x)*cellW)); px < int(scale*(pad+float64(x+1)*cellW)); px++ {
				ok = ok || rgb(px, bottom) != rgb(px, top)
			}
		}, true)
		return ok
	}
	// With Command held the word under the mouse is a link.
	mouse(5, two, 0)
	if underlined(one + 5) {
		t.Error("a word is a link with Command up")
	}
	mouse(5, two, modCommand)
	if !underlined(one+5) || underlined(one+1) {
		t.Errorf("with Command held two is a link %v and one %v", underlined(one+5), underlined(one+1))
	}
	// A click on it goes where the server says its definition is.
	mouse(1, two, modCommand)
	mouse(2, two, modCommand)
	until(t, "the definition", func() bool {
		c := -1
		w.onLoop(func() { c = e.Cursor() })
		return c == 1
	})
	// A plain click stays a click.
	mouse(1, two, 0)
	mouse(2, two, 0)
	until(t, "the click", func() bool {
		c := -1
		w.onLoop(func() { c = e.Cursor() })
		return c == 5
	})
	onMain(func() {
		for _, c := range ":qa!\r" {
			code := uint16(0)
			if c == '\r' {
				code = 36
			}
			press(w, code, 0, string(c), string(c))
		}
	}, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// TestFakeServer runs as a language server while FUDE_GUI_FAKE_LSP is set.
// It says every definition is at the second character of the text.
func TestFakeServer(t *testing.T) {
	if os.Getenv("FUDE_GUI_FAKE_LSP") == "" {
		return
	}
	r := bufio.NewReader(os.Stdin)
	for {
		n := 0
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				os.Exit(0)
			}
			if line = strings.TrimSpace(line); line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				n, _ = strconv.Atoi(v)
			}
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			os.Exit(0)
		}
		var m struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
			Params struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			} `json:"params"`
		}
		if err := json.Unmarshal(body, &m); err != nil {
			os.Exit(2)
		}
		var result any
		switch m.Method {
		case "initialize":
			result = map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-8", "textDocumentSync": 1, "definitionProvider": true}}
		case "textDocument/definition":
			at := map[string]int{"line": 0, "character": 1}
			result = map[string]any{"uri": m.Params.TextDocument.URI, "range": map[string]any{"start": at, "end": at}}
		case "exit":
			os.Exit(0)
		}
		if m.ID != nil {
			reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
			fmt.Printf("Content-Length: %d\r\n\r\n%s", len(reply), reply)
		}
	}
}

// B581
func TestB581Accessibility(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "three\n")
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(bufferline.New())
	e.Use(buffers.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, c := range keys {
			code := uint16(0)
			switch c {
			case '\r':
				code = 36
			case '\x1b':
				code = 53
			}
			onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
		}
	}
	// focus reads the element with the focus, as an assistive app does.
	type said struct {
		role, label, value string
		chosen             nsRange
	}
	focus := func() said {
		var s said
		onMain(func() {
			f := w.win.Send(sel("accessibilityFocusedUIElement"))
			s = said{goString(f.Send(sel("accessibilityRole"))), goString(f.Send(sel("accessibilityLabel"))), goString(f.Send(sel("accessibilityValue"))), msgRangeOf(f, sel("accessibilitySelectedTextRange"))}
		}, true)
		return s
	}
	// The current window's text has the focus, with the cursor as its
	// selection, which the visual selection takes.
	until(t, "the text to have the focus", func() bool {
		return focus() == said{"AXTextArea", "a.txt", "one\ntwo", nsRange{0, 0}}
	})
	typed("jvl")
	until(t, "the selection to follow", func() bool { return focus().chosen == nsRange{4, 2} })
	// The palette takes the focus while it shows.
	typed("\x1b:ec")
	until(t, "the palette to have the focus", func() bool {
		return focus() == said{"AXTextField", "command line", "ec", nsRange{2, 0}}
	})
	// The finder's field has its list of items beside it.
	typed("\x1b:pick\r")
	until(t, "the finder's items", func() bool {
		rows := 0
		onMain(func() {
			if parts := w.panel.view.Send(sel("accessibilityChildren")); objc.Send[int](parts, sel("count")) == 2 {
				rows = objc.Send[int](parts.Send(sel("objectAtIndex:"), 1).Send(sel("accessibilityChildren")), sel("count"))
			}
		}, true)
		return focus().role == "AXTextField" && rows > 0
	})
	typed("\x1b:e " + filepath.Join(dir, "b.txt") + "\r")
	until(t, "b.txt to have the focus", func() bool { return focus().label == "b.txt" })
	// The strip's tabs show their buffers when pressed, and their close
	// buttons close them.
	tabs := func() (out []objc.ID) {
		onMain(func() {
			kids := w.bar.Send(sel("accessibilityChildren"))
			for i := range objc.Send[int](kids, sel("count")) {
				out = append(out, kids.Send(sel("objectAtIndex:"), i))
			}
		}, true)
		return out
	}
	until(t, "two tabs", func() bool { return len(tabs()) == 2 })
	first := tabs()[0]
	onMain(func() { first.Send(sel("accessibilityPerformPress")) }, true)
	until(t, "a.txt to show", func() bool { return focus().label == "a.txt" })
	second := tabs()[1]
	onMain(func() {
		shut := second.Send(sel("accessibilityChildren")).Send(sel("objectAtIndex:"), 0)
		if got := goString(shut.Send(sel("accessibilityLabel"))); got != "close" {
			t.Errorf("a tab's button says %q", got)
		}
		shut.Send(sel("accessibilityPerformPress"))
	}, true)
	until(t, "b.txt to close", func() bool { return len(tabs()) == 1 })
	typed(":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B584
func TestB584Font(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typed := func(keys string) {
		for _, c := range keys {
			code := uint16(0)
			if c == '\r' {
				code = 36
			}
			onMain(func() { press(w, code, 0, string(c), string(c)) }, true)
		}
	}
	// now is the text's family and size, and the family of the status
	// bars' text.
	var chrome string
	now := func() (family string, points float64) {
		onMain(func() {
			family, points = goString(font.Send(sel("familyName"))), textSize
			chrome = goString(uiFont.Send(sel("familyName")))
		}, true)
		return family, points
	}
	system, size := now()
	systemUI := chrome
	// :set changes the font's size and family at once, the first family
	// the system has.
	typed(":set gui.font_size 20\r:set gui.font NoSuchFont,Courier\r")
	until(t, "Courier at 20 points", func() bool { f, p := now(); return f == "Courier" && p == 20 && chrome == "Courier" })
	// Actual size goes back to gui.font_size.
	onMain(func() {
		mainRun.Send(sel("bigger:"), 0)
		mainRun.Send(sel("actualSize:"), 0)
	}, true)
	if _, p := now(); p != 20 {
		t.Errorf("actual size is %g points", p)
	}
	// A family the system does not have, and no size, are the system's.
	typed(":set gui.font NoSuchFont\r:set gui.font_size 0\r")
	until(t, "the system's font", func() bool { f, p := now(); return f == system && p == size && chrome == systemUI })
	typed(":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// B547
func TestB547WideGlyphShowsWhole(t *testing.T) {
	// A glyph wider than its cell, as a nerd font's icon is, draws over
	// the blank cell after it, as a terminal draws it.
	ink, ground := theme.Style{Fg: 0xffffff, Bg: 0}, theme.Style{Bg: 0x0000ff}
	cells := [][]cell{{{"MMMM", 1, ink}, {" ", 1, ground}, {" ", 1, ground}}}
	var drawn bool
	onMain(func() {
		img := msgInitSize(cls("NSImage").Send(sel("alloc")), sel("initWithSize:"), size{4 * cellW, cellH})
		defer img.Send(sel("release"))
		img.Send(sel("lockFocusFlipped:"), true)
		drawCells(cells, cursorAt{}, point{}, nil, nil)
		img.Send(sel("unlockFocus"))
		rep := cls("NSBitmapImageRep").Send(sel("imageRepWithData:"), img.Send(sel("TIFFRepresentation")))
		scale := float64(objc.Send[int](rep, sel("pixelsWide"))) / (4 * cellW)
		for px := int(scale * cellW); px < int(scale*2*cellW); px++ {
			for py := 0; py < int(scale*cellH); py++ {
				if c := msgColorAt(rep, sel("colorAtX:y:"), px, py); msgComponent(c, sel("redComponent")) > 0.5 {
					drawn = true
				}
			}
		}
	}, true)
	if !drawn {
		t.Error("the blank cell after a wide glyph hides it")
	}
}

// press sends w a key down, as the keyboard does.
func press(w *window, code uint16, mods uint, chars, bare string) {
	n := objc.Send[int](w.win, sel("windowNumber"))
	ev := msgKeyEvent(cls("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
		10, point{}, mods, 0, n, 0, nsString(chars), nsString(bare), false, code)
	w.win.Send(sel("sendEvent:"), ev)
}

var (
	msgMarked     func(objc.ID, objc.SEL, objc.ID, nsRange, nsRange)
	msgInserted   func(objc.ID, objc.SEL, objc.ID, nsRange)
	msgMouseEvent func(objc.ID, objc.SEL, uint, point, uint, float64, int, objc.ID, int, int, float32) objc.ID
	msgFirstRect  func(objc.ID, objc.SEL, nsRange, uintptr) rect
	msgCache      func(objc.ID, objc.SEL, rect, objc.ID)
	msgColorAt    func(objc.ID, objc.SEL, int, int) objc.ID
	msgComponent  func(objc.ID, objc.SEL) float64
	msgRangeOf    func(objc.ID, objc.SEL) nsRange
	msgInitSize   func(objc.ID, objc.SEL, size) objc.ID
)

// pixels returns the colour of each pixel of what view draws, and how many
// pixels a point takes.
func pixels(view objc.ID) (func(px, py int) [3]float64, float64, bool) {
	b := msgRect(view, sel("bounds"))
	if b.Size.W < 1 {
		return nil, 0, false
	}
	rep := msgInitView(view, sel("bitmapImageRepForCachingDisplayInRect:"), b)
	msgCache(view, sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
	scale := float64(objc.Send[int](rep, sel("pixelsWide"))) / b.Size.W
	return func(px, py int) [3]float64 {
		c := msgColorAt(rep, sel("colorAtX:y:"), px, py)
		return [3]float64{msgComponent(c, sel("redComponent")), msgComponent(c, sel("greenComponent")), msgComponent(c, sel("blueComponent"))}
	}, scale, true
}

// inked reports whether the cell at column x and row y, its cells edge
// points in from the view's edges, shows anything but the ground, read from
// what the view draws.
func inked(view objc.ID, edge float64, x, y int) bool {
	rgb, scale, ok := pixels(view)
	if !ok {
		return false
	}
	ground := rgb(int(scale*2), int(scale*2))
	for py := int(scale * (edge + float64(y)*cellH)); py < int(scale*(edge+float64(y+1)*cellH)); py++ {
		for px := int(scale * (edge + float64(x)*cellW)); px < int(scale*(edge+float64(x+1)*cellW)); px++ {
			if rgb(px, py) != ground {
				return true
			}
		}
	}
	return false
}
