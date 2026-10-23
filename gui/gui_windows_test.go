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
	"runtime"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/bufferline"
	"github.com/y3owk1n/fude/modules/buffers"
	filesmod "github.com/y3owk1n/fude/modules/files"
	windowmod "github.com/y3owk1n/fude/modules/window"
	"github.com/y3owk1n/fude/vim"
)

// TestMain runs the windows' messages on the main thread and the tests
// beside it, which reach it through onMain.
func TestMain(m *testing.M) {
	if err := load(); err != nil {
		panic(err)
	}
	// paint draws into WIC's bitmaps, which COM makes.
	call(ole32.NewProc("CoInitializeEx"), 0, 2)
	if err := failed("make WIC's factory", call(ole32.NewProc("CoCreateInstance"), uintptr(unsafe.Pointer(&clsidWIC)), 0, 1, uintptr(unsafe.Pointer(&iidWIC)), uintptr(unsafe.Pointer(&wic)))); err != nil {
		panic(err)
	}
	startApp()
	code := 0
	go func() {
		code = editortest.Main(m)
		onMain(func() { call(postQuit, 0) }, false)
	}()
	pump()
	os.Exit(code)
}

// B573
func TestB573WindowsWindow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "one\n")
	e := editortest.Open(t, path)
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	// Keys typed in the window reach the session as Vim keys.
	typeIn(w, "A!\x1b")
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
				img := paint(p.frame.Size, func(sz size) { drawPane(p, sz) })
				drawn = img != nil && inked(img, 0, 0) && !inked(img, 40, 10)
			}
		}, true)
		return drawn
	})
	// A window made smaller makes the session smaller.
	onMain(func() { w.fit(cellW*80+2*pad, stripH+cellH*24+2*pad+statusH) }, true)
	until(t, "the session to take 80 columns", func() bool {
		cols := 0
		w.onLoop(func() { cols = e.Windows()[0].W })
		return cols == 80
	})
	// Closing the window quits as :qa does.
	typeIn(w, ":w\r")
	until(t, "the file to be written", func() bool { b, _ := os.ReadFile(path); return string(b) == "one!\n" })
	onMain(func() { call(sendMessage, w.hwnd, wmClose, 0, 0) }, true)
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

// B574
func TestB574WindowsMouse(t *testing.T) {
	lines := []string{"one two", "three"}
	for i := range 100 {
		lines = append(lines, fmt.Sprint("line ", i))
	}
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, strings.Join(lines, "\n")+"\n")
	e := editortest.Open(t, path)
	e.Use(hovering{})
	e.Use(windowmod.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	var p *pane
	until(t, "the pane", func() bool {
		onMain(func() {
			for _, q := range w.panes {
				if len(q.cells) > 0 && q.frame.Size.H > 0 {
					p = q
				}
			}
		}, true)
		return p != nil
	})
	cell := func(x, y int) point {
		return point{p.frame.Origin.X + pad + (float64(x)+0.5)*cellW, p.frame.Origin.Y + pad + (float64(y)+0.5)*cellH}
	}
	send := func(m uint32, wp uintptr, at point) { mouseAt(w, m, wp, at) }
	click := func(from, to point) {
		send(wmLButtonDown, 1, from)
		send(wmMouseMove, 1, to)
		send(wmLButtonUp, 0, to)
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
	click(cell(4, 0), cell(4, 0))
	until(t, "the cursor on two", func() bool {
		c := -1
		w.onLoop(func() { c = e.Cursor() })
		return c == 4
	})
	click(cell(0, 1), cell(2, 1))
	until(t, "thr to be selected", func() bool { return selected(8, 11) })
	// A second click soon after the first selects the word it hits, and a
	// move within its cell does not drag.
	click(cell(5, 0), cell(5, 0))
	click(cell(5, 0), cell(5, 0))
	until(t, "two to be selected", func() bool { return selected(4, 7) })
	typeIn(w, "\x1b")
	until(t, "normal mode", func() bool {
		var m vim.Mode
		w.onLoop(func() { m = e.Mode() })
		return m == vim.Normal
	})
	// A rest on a word shows its hover, once the mouse moved since the
	// last click.
	send(wmMouseMove, 0, cell(6, 0))
	until(t, "the hover of two", func() bool {
		text := ""
		w.onLoop(func() {
			for _, v := range e.Windows() {
				if v.Float == "cursor" {
					text = v.Text.Slice(0, v.Text.Len())
				}
			}
		})
		return text == "at 6"
	})
	// The wheel scrolls the window the lines the system scrolls a notch.
	onMain(func() {
		at := cell(0, 2)
		pt := [2]int32{int32(at.X * w.scale), int32(at.Y * w.scale)}
		call(clientToScreen, w.hwnd, uintptr(unsafe.Pointer(&pt)))
		delta := int16(-240)
		call(sendMessage, w.hwnd, wmMouseWheel, uintptr(uint16(delta))<<16, uintptr(uint16(pt[0]))|uintptr(uint16(pt[1]))<<16)
	}, true)
	notch := uint32(0)
	call(systemParameters, 0x68, 0, uintptr(unsafe.Pointer(&notch)), 0)
	until(t, "the window to scroll two notches", func() bool {
		top := -1
		w.onLoop(func() { top = e.Windows()[0].Text.LineOf(e.Windows()[0].Top) })
		return top == int(2*notch)
	})
	// The right click's Select All selects the text.
	all := slices.IndexFunc(contextItems, func(it struct{ title, keys, needs string }) bool { return it.title == "Select All" })
	onMain(func() { w.choose(all) }, true)
	until(t, "the text to be selected", func() bool {
		var s vim.Selection
		var ok bool
		w.onLoop(func() { s, ok = e.Selection() })
		return ok && s.Kind == "V" && s.Start == 0
	})
	// Dragging the divider between two windows resizes them.
	typeIn(w, "\x1b:vsplit\r")
	var d divider
	until(t, "the divider", func() bool {
		onMain(func() {
			if len(w.dividers) == 1 {
				d = w.dividers[0]
			}
		}, true)
		return d.across
	})
	mid := point{d.r.Origin.X, d.r.Origin.Y + 50}
	send(wmLButtonDown, 1, mid)
	send(wmMouseMove, 1, point{mid.X - 20*cellW, mid.Y})
	send(wmLButtonUp, 0, point{mid.X - 20*cellW, mid.Y})
	until(t, "the left window 20 columns narrower", func() bool {
		var ws []int
		w.onLoop(func() {
			for _, v := range e.Windows() {
				ws = append(ws, v.W)
			}
		})
		return len(ws) == 2 && max(ws[0], ws[1])-min(ws[0], ws[1]) >= 38
	})
	typeIn(w, ":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

// B575
func TestB575WindowsStripAndLook(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "two\n")
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(bufferline.New())
	e.Use(buffers.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	typeIn(w, ":e "+filepath.Join(dir, "b.txt")+"\r")
	// The strip shows a tab for each buffer, and a click on one shows it.
	until(t, "two tabs", func() bool {
		n := 0
		onMain(func() {
			paint(size{w.area().W, stripH}, func(sz size) { w.drawStripBar(sz.W) })
			n = len(w.hits)
		}, true)
		return n == 2
	})
	// Off the tabs the strip is the title bar, and its buttons are the
	// window's. The system's title bar does not show over it.
	var frame rectI
	var top [2]int32
	onMain(func() {
		call(getWindowRect, w.hwnd, uintptr(unsafe.Pointer(&frame)))
		call(clientToScreen, w.hwnd, uintptr(unsafe.Pointer(&top)))
	}, true)
	if top[1] != frame.T {
		t.Errorf("the strip starts %d pixels under the window's top, under the system's title bar", top[1]-frame.T)
	}
	var tab, title, closing uintptr
	onMain(func() {
		width := w.area().W
		tab, title, closing = w.hit(point{(w.hits[0].from + w.hits[0].to) / 2, stripH / 2}), w.hit(point{width / 2, stripH / 2}), w.hit(point{width - captionW/2, stripH / 2})
	}, true)
	if tab != htClient || title != htCaption || closing != htClose {
		t.Errorf("the strip hits %d on a tab, %d off them and %d on the last button", tab, title, closing)
	}
	at := point{(w.hits[0].from+w.hits[0].to)/2 - slotW, stripH / 2}
	mouseAt(w, wmLButtonDown, 1, at)
	mouseAt(w, wmLButtonUp, 0, at)
	until(t, "a.txt to show", func() bool {
		name := ""
		w.onLoop(func() { name = e.Windows()[0].Name })
		return strings.HasSuffix(name, "a.txt")
	})
	// A change to a buffer puts a dot before the title.
	typeIn(w, "ix\x1b")
	until(t, "the title to show the change", func() bool {
		var title [64]uint16
		onMain(func() { call(getWindowText, w.hwnd, uintptr(unsafe.Pointer(&title[0])), 64) }, true)
		return strings.HasPrefix(windows.UTF16ToString(title[:]), "• ")
	})
	// Control with = makes the text bigger, so the window holds fewer
	// columns.
	cols := 0
	w.onLoop(func() { cols = e.Windows()[0].W })
	withControl(w, 0xbb)
	until(t, "fewer columns", func() bool {
		now := 0
		w.onLoop(func() { now = e.Windows()[0].W })
		return now < cols
	})
	withControl(w, '0')
	typeIn(w, ":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

// B576
func TestB576WindowsImages(t *testing.T) {
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
	typeIn(w, ":pick files\rpic.png")
	// The finder's preview shows the image from its pixels.
	until(t, "the image in the finder", func() bool {
		red := false
		onMain(func() {
			b := w.panel
			if b.boxed == nil || len(b.pics) == 0 || w.images[b.pics[0].id] == nil {
				return
			}
			shot := paint(b.frame.Size, func(sz size) { drawBox(b, sz) })
			p, o := b.pics[0], b.cellsOrigin()
			r, g, _, _ := shot.At(int(o.X+(float64(p.x)+0.5)*cellW), int(o.Y+(float64(p.y)+0.5)*cellH)).RGBA()
			red = r>>8 > 200 && g>>8 < 50
		}, true)
		return red
	})
	typeIn(w, "\x1b\x1b:qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

// B577
func TestB577WindowsScrollBar(t *testing.T) {
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
				if q.lines > q.rows {
					p = q
				}
			}
		}, true)
		return p != nil
	})
	// The bar shows where the view is once it moves.
	onMain(func() { p.roll(30) }, true)
	var k rect
	until(t, "the bar at line 30", func() bool {
		shown, top := false, 0
		onMain(func() { k, shown = p.knob(); top = p.top }, true)
		return shown && top == 30
	})
	// Dragging its knob scrolls the window there.
	h := p.frame.Size.H - statusH - 4
	from := point{p.frame.Origin.X + k.Origin.X + 2, p.frame.Origin.Y + k.Origin.Y + k.Size.H/2}
	to := point{from.X, from.Y + 2 + 100/float64(p.lines)*h - k.Origin.Y}
	mouseAt(w, wmLButtonDown, 1, from)
	mouseAt(w, wmMouseMove, 1, to)
	mouseAt(w, wmLButtonUp, 0, to)
	until(t, "the window at line 100", func() bool {
		top := -1
		w.onLoop(func() { top = e.Windows()[0].Text.LineOf(e.Windows()[0].Top) })
		return top == 100
	})
	typeIn(w, ":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

// B578
func TestB578WindowsInputMethods(t *testing.T) {
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
	// What the input method composes draws at the cursor, and goes once
	// it ends.
	inked := func() bool {
		ink := false
		onMain(func() {
			at, _ := w.caret()
			sz := w.area()
			img := paint(size{sz.W, sz.H + stripH}, func(size) {
				ground := rgba(w.look.Base.Bg, 1)
				com(rt, 47, uintptr(unsafe.Pointer(&ground)))
				w.drawMarked()
			})
			ground := img.At(1, 1)
			for x := int(at.X); x < int(at.X+cellW*2); x++ {
				ink = ink || img.At(x, int(at.Y+cellH/2)) != ground
			}
		}, true)
		return ink
	}
	onMain(func() { w.mark("か") }, true)
	if !inked() {
		t.Error("composed text did not show at the cursor")
	}
	onMain(func() { w.mark("") }, true)
	if inked() {
		t.Error("composed text stayed once it ended")
	}
	// What it inserts goes to the session as keys.
	typeIn(w, "A")
	onMain(func() { w.typeText("かé") }, true)
	typeIn(w, "\x1b")
	until(t, "the text it inserted", func() bool {
		text := ""
		w.onLoop(func() { text = editortest.Text(e) })
		return text == "oneかé"
	})
	typeIn(w, ":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

// B583
func TestB583WindowsAccessibility(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "three\n")
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(bufferline.New())
	e.Use(buffers.New())
	var w *window
	onMain(func() { w = open(e, nil) }, true)
	// An assistive app reads the window through UI Automation's client, on
	// a thread of its own.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	call(ole32.NewProc("CoInitializeEx"), 0, 0)
	defer call(ole32.NewProc("CoUninitialize"))
	var uia unsafe.Pointer
	if err := failed("make UI Automation's client", call(ole32.NewProc("CoCreateInstance"), uintptr(unsafe.Pointer(&clsidUIA)), 0, 1, uintptr(unsafe.Pointer(&iidUIA)), uintptr(unsafe.Pointer(&uia)))); err != nil {
		t.Fatal(err)
	}
	defer release(uia)
	var root unsafe.Pointer
	com(uia, 6, w.hwnd, uintptr(unsafe.Pointer(&root)))
	defer release(root)
	// find returns the elements under the window whose property is v.
	find := func(prop int, v variant) []unsafe.Pointer {
		var cond, all unsafe.Pointer
		com(uia, 23, uintptr(prop), uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&cond)))
		defer release(cond)
		com(root, 6, 4, uintptr(cond), uintptr(unsafe.Pointer(&all)))
		if all == nil {
			return nil
		}
		defer release(all)
		var n int32
		com(all, 3, uintptr(unsafe.Pointer(&n)))
		out := make([]unsafe.Pointer, n)
		for i := range out {
			com(all, 4, uintptr(i), uintptr(unsafe.Pointer(&out[i])))
		}
		return out
	}
	text := func(r unsafe.Pointer) string {
		var b uintptr
		com(r, 12, ^uintptr(0), uintptr(unsafe.Pointer(&b)))
		defer call(sysFreeString, b)
		return windows.UTF16PtrToString(out[uint16](b))
	}
	// focus reads the element with the focus: its control type, its name,
	// its text and where its caret is.
	type said struct {
		role       int32
		name, text string
		caret      int
	}
	focus := func() said {
		var s said
		found := find(propHasFocus, variant{vt: vtBool, val: 0xffff})
		if len(found) == 0 {
			return s
		}
		el := found[0]
		for _, f := range found {
			defer release(f)
		}
		var name uintptr
		com(el, 21, uintptr(unsafe.Pointer(&s.role)))
		com(el, 23, uintptr(unsafe.Pointer(&name)))
		s.name = windows.UTF16PtrToString(out[uint16](name))
		call(sysFreeString, name)
		var tp, doc, sel, head, picked unsafe.Pointer
		if com(el, 14, textPattern, uintptr(unsafe.Pointer(&iidTextPatternClient)), uintptr(unsafe.Pointer(&tp))); tp == nil {
			return s
		}
		defer release(tp)
		com(tp, 7, uintptr(unsafe.Pointer(&doc)))
		defer release(doc)
		s.text = text(doc)
		com(tp, 5, uintptr(unsafe.Pointer(&sel)))
		defer release(sel)
		com(sel, 4, 0, uintptr(unsafe.Pointer(&picked)))
		defer release(picked)
		com(doc, 3, uintptr(unsafe.Pointer(&head)))
		defer release(head)
		com(head, 15, 1, uintptr(picked), 0)
		s.caret = len(utf16.Encode([]rune(text(head))))
		return s
	}
	// The current window's text has the focus, with the cursor as its
	// selection.
	until(t, "the text to have the focus", func() bool {
		return focus() == said{ctlDocument, "a.txt", "one\ntwo", 0}
	})
	typeIn(w, "j")
	until(t, "the selection to follow", func() bool { return focus().caret == 4 })
	// The palette's text field takes the focus while it shows.
	typeIn(w, ":ec")
	until(t, "the palette to have the focus", func() bool {
		return focus() == said{ctlEdit, "command line", "ec", 2}
	})
	typeIn(w, "\x1b:e "+filepath.Join(dir, "b.txt")+"\r")
	until(t, "b.txt to have the focus", func() bool { return focus().name == "b.txt" })
	// The strip's tabs show their buffers when invoked, and their close
	// buttons close them.
	press := func(el unsafe.Pointer) {
		var ip unsafe.Pointer
		com(el, 14, invokePattern, uintptr(unsafe.Pointer(&iidInvokeClient)), uintptr(unsafe.Pointer(&ip)))
		com(ip, 3)
		release(ip)
	}
	tabs := func() []unsafe.Pointer { return find(propControlType, variant{vt: vtI4, val: ctlTabItem}) }
	until(t, "two tabs", func() bool {
		ts := tabs()
		for _, tab := range ts {
			release(tab)
		}
		return len(ts) == 2
	})
	ts := tabs()
	press(ts[0])
	for _, tab := range ts {
		release(tab)
	}
	until(t, "a.txt to show", func() bool { return focus().name == "a.txt" })
	// Windows' own buttons of the title bar are buttons too.
	var shut []unsafe.Pointer
	for _, b := range find(propControlType, variant{vt: vtI4, val: ctlButton}) {
		var name uintptr
		com(b, 23, uintptr(unsafe.Pointer(&name)))
		if windows.UTF16PtrToString(out[uint16](name)) == "close" {
			shut = append(shut, b)
		} else {
			release(b)
		}
		call(sysFreeString, name)
	}
	press(shut[1])
	for _, b := range shut {
		release(b)
	}
	until(t, "b.txt to close", func() bool {
		ts := tabs()
		for _, tab := range ts {
			release(tab)
		}
		return len(ts) == 1
	})
	typeIn(w, ":qa!\r")
	until(t, "the session to end", func() bool {
		open := true
		onMain(func() { _, open = opened[w.hwnd] }, true)
		return !open
	})
}

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	clsidUIA             = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidUIA               = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	iidTextPatternClient = windows.GUID{Data1: 0x32eba289, Data2: 0x3583, Data3: 0x42c9, Data4: [8]byte{0x9c, 0x59, 0x3b, 0x6d, 0x9a, 0x1e, 0x9b, 0x6a}}
	iidInvokeClient      = windows.GUID{Data1: 0xfb377fbe, Data2: 0x8ea6, Data3: 0x46d5, Data4: [8]byte{0x9c, 0x73, 0x64, 0x99, 0x64, 0x2d, 0x30, 0x59}}
	getWindowText        = user32.NewProc("GetWindowTextW")
	getWindowRect        = user32.NewProc("GetWindowRect")
	keyboardState        = user32.NewProc("GetKeyboardState")
	setKeyboardState     = user32.NewProc("SetKeyboardState")
	sendMessage          = user32.NewProc("SendMessageW")
	setCursorPos         = user32.NewProc("SetCursorPos")
	wic                  unsafe.Pointer
	clsidWIC             = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidWIC               = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	pbgra                = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}
)

// typeIn types keys into w's window as Windows brings them: a character
// as WM_CHAR, and \r and \x1b as the keys that type them.
func typeIn(w *window, keys string) {
	for _, r := range keys {
		onMain(func() {
			switch r {
			case '\r', '\x1b':
				call(sendMessage, w.hwnd, wmKeyDown, uintptr(r), 0)
			default:
				call(sendMessage, w.hwnd, wmChar, uintptr(r), 0)
			}
		}, true)
	}
}

// mouseAt sends w's window the mouse's message m at a point, with the
// system's pointer there, as Windows tracks it. The window stays on top,
// so no other window is under the pointer.
func mouseAt(w *window, m uint32, wp uintptr, at point) {
	onMain(func() {
		call(setWindowPos, w.hwnd, ^uintptr(0), 0, 0, 0, 0, 0x13)
		pt := [2]int32{int32(at.X * w.scale), int32(at.Y * w.scale)}
		lp := uintptr(uint16(pt[0])) | uintptr(uint16(pt[1]))<<16
		call(clientToScreen, w.hwnd, uintptr(unsafe.Pointer(&pt)))
		call(setCursorPos, uintptr(pt[0]), uintptr(pt[1]))
		call(sendMessage, w.hwnd, uintptr(m), wp, lp)
	}, true)
}

// withControl presses the key vk with Control held, as the keyboard does.
func withControl(w *window, vk uintptr) {
	onMain(func() {
		var keys [256]byte
		call(keyboardState, uintptr(unsafe.Pointer(&keys)))
		keys[0x11] = 0x80
		call(setKeyboardState, uintptr(unsafe.Pointer(&keys)))
		call(sendMessage, w.hwnd, wmKeyDown, vk, 0)
		keys[0x11] = 0
		call(setKeyboardState, uintptr(unsafe.Pointer(&keys)))
	}, true)
}

// paint calls draw on an image sz big, as a window at 96 DPI draws, and
// returns the image, or nil for an empty size.
func paint(sz size, draw func(size)) *image.RGBA {
	width, height := int(math.Ceil(sz.W)), int(math.Ceil(sz.H))
	if width == 0 || height == 0 {
		return nil
	}
	var bmp, target, b unsafe.Pointer
	com(wic, 17, uintptr(width), uintptr(height), uintptr(unsafe.Pointer(&pbgra)), 2, uintptr(unsafe.Pointer(&bmp)))
	defer release(bmp)
	props := rtProps{format: 87, alpha: 1}
	com(d2d, 13, uintptr(bmp), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&target)))
	defer release(target)
	var c colorF
	com(target, 8, uintptr(unsafe.Pointer(&c)), 0, uintptr(unsafe.Pointer(&b)))
	defer release(b)
	gens++
	rt, brush, rtGen = target, b, gens
	com(rt, 48)
	draw(sz)
	com(rt, 49, 0, 0)
	rt, brush = nil, nil
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	com(bmp, 7, 0, uintptr(width*4), uintptr(len(img.Pix)), uintptr(unsafe.Pointer(&img.Pix[0])))
	// WIC's pixels are BGRA.
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
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
