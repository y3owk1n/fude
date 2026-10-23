package gui

import (
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
