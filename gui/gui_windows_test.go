package gui

import (
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/y3owk1n/fude/editor/editortest"
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
