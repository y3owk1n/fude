package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/modules/completion"
	"github.com/y3owk1n/fude/modules/hints"
	winmod "github.com/y3owk1n/fude/modules/window"
)

// TestMain runs AppKit on the main thread, in the background, and the tests
// beside it.
func TestMain(m *testing.M) {
	load()
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
	// shows reports whether b shows with something drawn in its first cell.
	shows := func(b *box) bool {
		var hidden, drawn bool
		onMain(func() { hidden, drawn = objc.Send[bool](b.view, sel("isHidden")), inked(b.view, inset, 0, 0) }, true)
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
				r, ok = msgRect(b.view, sel("frame")), true
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
	typed("\x1b:window float split b.txt\r")
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
	var marked func(objc.ID, objc.SEL, objc.ID, nsRange, nsRange)
	var inserted func(objc.ID, objc.SEL, objc.ID, nsRange)
	lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	purego.RegisterLibFunc(&marked, lib, "objc_msgSend")
	purego.RegisterLibFunc(&inserted, lib, "objc_msgSend")
	until(t, "insert mode", func() bool {
		var mode string
		onMain(func() { mode = w.tabs.mode }, true)
		return mode == "insert"
	})
	onMain(func() {
		marked(w.view, sel("setMarkedText:selectedRange:replacementRange:"), nsString("にほん"), nsRange{3, 0}, nsRange{Loc: notFound})
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
		inserted(w.view, sel("insertText:replacementRange:"), nsString("日本"), nsRange{Loc: notFound})
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

// onLoop runs fn on w's loop, where the session may be read.
func (w *window) onLoop(fn func()) {
	done := make(chan struct{})
	w.do <- func() { fn(); close(done) }
	<-done
}

// until waits for ok, failing t once it waited too long for what.
func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(editortest.Patience); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("still waiting for %s", what)
		}
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
	msgFirstRect func(objc.ID, objc.SEL, nsRange, uintptr) rect
	msgCache     func(objc.ID, objc.SEL, rect, objc.ID)
	msgColorAt   func(objc.ID, objc.SEL, int, int) objc.ID
	msgComponent func(objc.ID, objc.SEL) float64
)

// inked reports whether the cell at column x and row y, its cells edge
// points in from the view's edges, shows anything but the ground, read from
// what the view draws.
func inked(view objc.ID, edge float64, x, y int) bool {
	if msgCache == nil {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		for _, f := range []any{&msgFirstRect, &msgCache, &msgColorAt, &msgComponent} {
			purego.RegisterLibFunc(f, lib, "objc_msgSend")
		}
	}
	b := msgRect(view, sel("bounds"))
	if b.Size.W < 1 {
		return false
	}
	rep := msgInitView(view, sel("bitmapImageRepForCachingDisplayInRect:"), b)
	msgCache(view, sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
	scale := float64(objc.Send[int](rep, sel("pixelsWide"))) / b.Size.W
	rgb := func(px, py int) [3]float64 {
		c := msgColorAt(rep, sel("colorAtX:y:"), px, py)
		return [3]float64{msgComponent(c, sel("redComponent")), msgComponent(c, sel("greenComponent")), msgComponent(c, sel("blueComponent"))}
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
