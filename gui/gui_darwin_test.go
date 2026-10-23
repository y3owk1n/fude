package gui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/y3owk1n/fude/editor/editortest"
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
	// onLoop reads the session on its loop, which no other goroutine may.
	onLoop := func(fn func()) {
		done := make(chan struct{})
		w.do <- func() { fn(); close(done) }
		<-done
	}
	until := func(what string, ok func() bool) {
		t.Helper()
		for deadline := time.Now().Add(editortest.Patience); !ok(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("still waiting for %s", what)
			}
		}
	}
	// Keys typed in the window reach the session as Vim keys.
	for _, k := range []struct {
		code        uint16
		mods        uint
		chars, bare string
	}{{0, modShift, "A", "a"}, {18, modShift, "!", "1"}, {53, 0, "\x1b", "\x1b"}} {
		onMain(func() { press(w, k.code, k.mods, k.chars, k.bare) }, true)
	}
	until("A!<Esc> to type", func() bool {
		var text string
		onLoop(func() { text = editortest.Text(e) })
		return text == "one!"
	})
	// The window shows the text: a cell with text differs from an empty one.
	until("the text to show", func() bool {
		var drawn, empty bool
		onMain(func() { drawn, empty = inked(w, 0, 0), inked(w, 40, 10) }, true)
		return drawn && !empty
	})
	// A resized window resizes the session.
	onMain(func() { msgSetSize(w.win, sel("setContentSize:"), size{cellW*60 + 2*pad, cellH*20 + 2*pad}) }, true)
	until("the session to take 60 columns", func() bool {
		cols := 0
		onLoop(func() { cols = e.Windows()[0].W })
		return cols == 60
	})
	for _, c := range ":qa!" {
		onMain(func() { press(w, 0, 0, string(c), string(c)) }, true)
	}
	onMain(func() { press(w, 36, 0, "\r", "\r") }, true)
	until("the session to end", func() bool {
		open := true
		onMain(func() { _, open = windows[w.view] }, true)
		return !open
	})
}

// press sends w a key down, as the keyboard does.
func press(w *window, code uint16, mods uint, chars, bare string) {
	n := objc.Send[int](w.win, sel("windowNumber"))
	ev := msgKeyEvent(cls("NSEvent"), sel("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:"),
		10, point{}, mods, 0, n, 0, nsString(chars), nsString(bare), false, code)
	w.win.Send(sel("sendEvent:"), ev)
}

var (
	msgCache     func(objc.ID, objc.SEL, rect, objc.ID)
	msgColorAt   func(objc.ID, objc.SEL, int, int) objc.ID
	msgComponent func(objc.ID, objc.SEL) float64
)

// inked reports whether the cell at column x and row y shows anything but
// the ground, read from what the view draws.
func inked(w *window, x, y int) bool {
	if msgCache == nil {
		lib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL)
		if err != nil {
			panic(err)
		}
		for _, f := range []any{&msgCache, &msgColorAt, &msgComponent} {
			purego.RegisterLibFunc(f, lib, "objc_msgSend")
		}
	}
	b := msgRect(w.view, sel("bounds"))
	rep := msgInitView(w.view, sel("bitmapImageRepForCachingDisplayInRect:"), b)
	msgCache(w.view, sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
	scale := float64(objc.Send[int](rep, sel("pixelsWide"))) / b.Size.W
	rgb := func(px, py int) [3]float64 {
		c := msgColorAt(rep, sel("colorAtX:y:"), px, py)
		return [3]float64{msgComponent(c, sel("redComponent")), msgComponent(c, sel("greenComponent")), msgComponent(c, sel("blueComponent"))}
	}
	ground := rgb(int(scale*2), int(scale*2))
	for py := int(scale * (pad + float64(y)*cellH)); py < int(scale*(pad+float64(y+1)*cellH)); py++ {
		for px := int(scale * (pad + float64(x)*cellW)); px < int(scale*(pad+float64(x+1)*cellW)); px++ {
			if rgb(px, py) != ground {
				return true
			}
		}
	}
	return false
}
