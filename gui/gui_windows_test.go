package gui

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/y3owk1n/fude/editor/editortest"
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
	ole32 := windows.NewLazySystemDLL("ole32.dll")
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
	onMain(func() { resize(w, cellW*80+2*pad, cellH*24+2*pad+statusH) }, true)
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
	send := func(m uint32, wp uintptr, at point) {
		onMain(func() {
			lp := uintptr(uint16(int16(at.X*w.scale))) | uintptr(uint16(int16(at.Y*w.scale)))<<16
			call(sendMessage, w.hwnd, uintptr(m), wp, lp)
		}, true)
	}
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

var (
	sendMessage = user32.NewProc("SendMessageW")
	wic         unsafe.Pointer
	clsidWIC    = windows.GUID{Data1: 0xcacaf262, Data2: 0x9370, Data3: 0x4615, Data4: [8]byte{0xa1, 0x3b, 0x9f, 0x55, 0x39, 0xda, 0x4c, 0x0a}}
	iidWIC      = windows.GUID{Data1: 0xec5ec8a9, Data2: 0xc395, Data3: 0x4314, Data4: [8]byte{0x9c, 0x77, 0x54, 0xd7, 0xa9, 0x35, 0xff, 0x70}}
	pbgra       = windows.GUID{Data1: 0x6fddc324, Data2: 0x4e03, Data3: 0x4bfe, Data4: [8]byte{0xb1, 0x85, 0x3d, 0x77, 0x76, 0x8d, 0xc9, 0x10}}
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

// resize makes w's client area width by height points.
func resize(w *window, width, height float64) {
	dpi := uintptr(96 * w.scale)
	r := rectI{0, 0, int32(math.Ceil(width * w.scale)), int32(math.Ceil(height * w.scale))}
	call(adjustWindowRect, uintptr(unsafe.Pointer(&r)), 0x00CF0000, 0, 0, dpi)
	call(setWindowPos, w.hwnd, 0, 0, 0, uintptr(r.R-r.L), uintptr(r.B-r.T), 0x16)
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
	rt, brush = target, b
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
