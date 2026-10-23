package gui

import (
	"unsafe"

	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/compose"
	"github.com/y3owk1n/fude/frontend/wire"
)

// pic is an image a window or a box shows, in its cells. It has the id the
// session gives it, the cell of its top left, which may be above the first
// row shown, and how many columns and rows it takes (B303).
type pic struct{ id, x, y, cols, rows int }

// pics are the images out says v shows, each once.
func pics(v wire.Window, out compose.Composed) []pic {
	var ps []pic
	seen := map[[2]int]bool{}
	for _, r := range out.Images {
		if k := [2]int{r.Line, r.Row.Image}; !seen[k] {
			seen[k] = true
			rows := 0
			for _, o := range v.Virtual[r.Line] {
				if o.Image == r.Row.Image {
					rows++
				}
			}
			ps = append(ps, pic{r.Row.Image, v.Gutter, r.Y - r.Row.Row, min(r.Row.Cols, v.W-v.Gutter), rows})
		}
	}
	return ps
}

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

// images sends the main thread the frame's images it lacks, and the ids of
// the ones the session dropped.
func (p *painter) images(s client.Session, f *frame, ps []pic) {
	if p.sent == nil {
		p.sent = map[int]bool{}
	}
	for _, id := range s.Freed() {
		delete(p.sent, id)
		f.freed = append(f.freed, id)
	}
	for _, pc := range ps {
		if p.sent[pc.id] {
			continue
		}
		if png, ok := s.Image(pc.id); ok {
			p.sent[pc.id] = true
			f.pngs[pc.id] = png
		}
	}
}
