package gui

import (
	"unsafe"
)

// keep makes the images the loop sent and drops the ones it freed.
func (w *window) keep(pngs map[int][]byte, freed []int) {
	for _, id := range freed {
		if img := w.images[id]; img != 0 {
			img.Send(sel("release"))
			delete(w.images, id)
		}
	}
	for id, png := range pngs {
		data := cls("NSData").Send(sel("dataWithBytes:length:"), uintptr(unsafe.Pointer(unsafe.SliceData(png))), uintptr(len(png)))
		if img := cls("NSImage").Send(sel("alloc")).Send(sel("initWithData:"), data); img != 0 {
			w.images[id] = img
		}
	}
}

// drawPics draws ps over cells whose first is at o.
func (w *window) drawPics(ps []pic, o point) {
	for _, p := range ps {
		if img := w.images[p.id]; img != 0 {
			r := at(p.x, p.y, p.cols, o)
			r.Size.H = float64(p.rows) * cellH
			msgDrawImage(img, sel("drawInRect:fromRect:operation:fraction:respectFlipped:hints:"), r, rect{}, 2, 1, true, 0)
		}
	}
}
