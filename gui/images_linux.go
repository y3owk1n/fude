package gui

import (
	"bytes"
	"image"
	"image/png"
	"unsafe"
)

// keep makes cairo surfaces of the images the loop sent and frees the
// ones the session dropped.
func (w *window) keep(pngs map[int][]byte, freed []int) {
	for _, id := range freed {
		if s := w.images[id]; s != 0 {
			cairoSurfaceFree(s)
			delete(w.images, id)
		}
	}
	for id, data := range pngs {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			continue
		}
		w.images[id] = surfaceOf(img)
	}
}

// surfaceOf is img as a cairo surface, its pixels premultiplied in the
// byte order cairo's ARGB32 has on a little-endian machine.
func surfaceOf(img image.Image) uintptr {
	b := img.Bounds()
	s := cairoImageNew(0, int32(b.Dx()), int32(b.Dy()))
	stride := int(cairoImageStride(s))
	px := unsafe.Slice(cairoImageData(s), stride*b.Dy())
	for y := range b.Dy() {
		for x := range b.Dx() {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			o := y*stride + x*4
			px[o], px[o+1], px[o+2], px[o+3] = byte(bl>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	cairoSurfaceDirty(s)
	return s
}

// drawPics draws ps over cells whose first is at o, each scaled into its
// cells (B303).
func (w *window) drawPics(ps []pic, o point) {
	for _, p := range ps {
		s := w.images[p.id]
		if s == 0 {
			continue
		}
		r := at(p.x, p.y, p.cols, o)
		r.Size.H = float64(p.rows) * cellH
		cairoSave(cr)
		cairoTranslate(cr, r.Origin.X, r.Origin.Y)
		cairoScale(cr, r.Size.W/float64(cairoImageWidth(s)), r.Size.H/float64(cairoImageHeight(s)))
		cairoSetSource(cr, s, 0, 0)
		cairoPaint(cr)
		cairoRestore(cr)
	}
}
