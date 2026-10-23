package gui

import (
	"bytes"
	"image/png"
	"unsafe"
)

// bitmap is an image the window shows. px holds its pixels as
// premultiplied BGRA, which Direct2D takes. bmp is the bitmap made of them
// on the render target of generation gen, and drawPics makes it again on
// another target.
type bitmap struct {
	w, h int
	px   []byte
	bmp  unsafe.Pointer
	gen  int
}

// gens counts the render targets made, and rtGen is the generation of the
// one being drawn on.
var rtGen, gens int

// keep keeps the pixels of the images the loop sent, and frees the ones
// the session dropped.
func (w *window) keep(pngs map[int][]byte, freed []int) {
	for _, id := range freed {
		if b := w.images[id]; b != nil {
			release(b.bmp)
			delete(w.images, id)
		}
	}
	for id, data := range pngs {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		r := img.Bounds()
		b := &bitmap{w: r.Dx(), h: r.Dy(), px: make([]byte, 4*r.Dx()*r.Dy())}
		for y := range b.h {
			for x := range b.w {
				red, g, bl, a := img.At(r.Min.X+x, r.Min.Y+y).RGBA()
				o := 4 * (y*b.w + x)
				b.px[o], b.px[o+1], b.px[o+2], b.px[o+3] = byte(bl>>8), byte(g>>8), byte(red>>8), byte(a>>8)
			}
		}
		w.images[id] = b
	}
}

// drawPics draws ps over cells whose first is at o, each scaled into its
// cells (B303).
func (w *window) drawPics(ps []pic, o point) {
	for _, p := range ps {
		b := w.images[p.id]
		if b == nil {
			continue
		}
		if b.gen != rtGen {
			release(b.bmp)
			props := bitmapProps{format: 87, alpha: 1}
			b.bmp, b.gen = nil, rtGen
			com(rt, 4, uintptr(b.w)|uintptr(b.h)<<32, uintptr(unsafe.Pointer(&b.px[0])), uintptr(4*b.w), uintptr(unsafe.Pointer(&props)), uintptr(unsafe.Pointer(&b.bmp)))
		}
		r := at(p.x, p.y, p.cols, o)
		r.Size.H = float64(p.rows) * cellH
		bind[func(this, bmp unsafe.Pointer, dest *rectF, opacity float32, mode uint32, src uintptr)](rt, 26)(rt, b.bmp, rf(r), 1, 1, 0)
	}
}
