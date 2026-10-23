package gui

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/ebitengine/purego"

	"github.com/y3owk1n/fude/editor/editortest"
)

// TestMain runs GTK on the main thread and the tests beside it, which reach
// it through onMain.
func TestMain(m *testing.M) {
	if err := load(); err != nil {
		panic(err)
	}
	cairo, err := purego.Dlopen("libcairo.so.2", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&cairoImageSurface, cairo, "cairo_image_surface_create")
	purego.RegisterLibFunc(&cairoCreate, cairo, "cairo_create")
	purego.RegisterLibFunc(&cairoDestroy, cairo, "cairo_destroy")
	purego.RegisterLibFunc(&cairoSurfaceDestroy, cairo, "cairo_surface_destroy")
	purego.RegisterLibFunc(&cairoWritePNG, cairo, "cairo_surface_write_to_png")
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		panic(err)
	}
	purego.RegisterLibFunc(&gtkWindowClose, gtk, "gtk_window_close")
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
				img := paint(t, p)
				drawn = img != nil && inked(img, 0, 0) && !inked(img, 40, 10)
			}
		}, true)
		return drawn
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

var (
	cairoImageSurface   func(format, w, h int32) uintptr
	cairoCreate         func(surface uintptr) uintptr
	cairoDestroy        func(cr uintptr)
	cairoSurfaceDestroy func(surface uintptr)
	cairoWritePNG       func(surface uintptr, path string) int32
	gtkWindowClose      func(w uintptr)
)

// paint draws p as GTK would, into an image, or returns nil before p has
// a size.
func paint(t *testing.T, p *pane) image.Image {
	width, height := gtkWidgetGetWidth(p.area), gtkWidgetGetHeight(p.area)
	if width == 0 || height == 0 {
		return nil
	}
	surface := cairoImageSurface(0, width, height)
	defer cairoSurfaceDestroy(surface)
	c := cairoCreate(surface)
	drawPane(p.area, c, width, height, 0)
	cairoDestroy(c)
	path := filepath.Join(t.TempDir(), "pane.png")
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
