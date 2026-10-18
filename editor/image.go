package editor

import (
	"bytes"
	"fmt"
	"image"
	// The formats an image decoration may name, beside PNG (B303).
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"slices"
)

// picture is an image file a decoration shows, as PNG, which is what a
// client sends the terminal (B303).
type picture struct {
	id   int
	png  []byte
	w, h int // its size in pixels
}

// pictures are the images decorations show, loaded once each.
type pictures struct {
	byPath map[string]*picture // nil while one loads or after it failed
	byID   map[int]*picture
	next   int
}

// pictureBase is the first of this process's image ids. A terminal keeps
// image ids by window, so another fude in the same window starts elsewhere.
var pictureBase = os.Getpid() % 4096 << 12

// picture returns the image at path once it has loaded, or nil. The first
// ask loads it off the loop, as a file may be megabytes.
func (e *Editor) picture(path string) *picture {
	if p, asked := e.pictures.byPath[path]; asked {
		return p
	}
	if e.pictures.byPath == nil {
		e.pictures.byPath, e.pictures.byID = map[string]*picture{}, map[int]*picture{}
	}
	e.pictures.byPath[path] = nil
	go func() {
		p, err := loadPicture(path)
		e.queue <- func() {
			if err != nil {
				logLine(err.Error())
				return
			}
			e.pictures.next++
			p.id = pictureBase + e.pictures.next
			e.pictures.byPath[path], e.pictures.byID[p.id] = p, p
		}
	}()
	return nil
}

// loadPicture reads the image at path, a PNG, JPEG or GIF, and makes it a
// PNG.
func loadPicture(path string) (*picture, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("image: %w", err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	switch {
	case err != nil:
		return nil, fmt.Errorf("image %s: %w", path, err)
	case cfg.Width == 0 || cfg.Height == 0:
		return nil, fmt.Errorf("image %s: it has no pixels", path)
	}
	if format != "png" {
		m, _, err := image.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("image %s: %w", path, err)
		}
		var out bytes.Buffer
		if err := png.Encode(&out, m); err != nil {
			return nil, fmt.Errorf("image %s: %w", path, err)
		}
		b = out.Bytes()
	}
	return &picture{png: b, w: cfg.Width, h: cfg.Height}, nil
}

// release drops each image at paths that nothing shows any more, and has
// every session's client free it (B306).
func (e *Editor) release(paths []string) {
	for _, path := range paths {
		p := e.pictures.byPath[path]
		if p == nil || e.showsImage(path) {
			continue
		}
		delete(e.pictures.byPath, path)
		delete(e.pictures.byID, p.id)
		for _, s := range e.sessions {
			s.freed = append(s.freed, p.id)
			delete(s.sentImages, p.id)
		}
	}
}

// showsImage reports whether a decoration or a finder shows the image at
// path.
func (e *Editor) showsImage(path string) bool {
	for _, ds := range e.decos {
		if slices.ContainsFunc(ds, func(d decoration) bool { return d.image == path }) {
			return true
		}
	}
	return slices.ContainsFunc(e.sessions, func(s *Session) bool { return s.finder != nil && s.finder.image == path })
}

// Freed returns the images dropped since it was last called, by id, for
// the client to free (B306).
func (e *Session) Freed() []int {
	out := e.freed
	e.freed = nil
	return out
}

// previewed returns the image f previews once it has loaded, or nil.
func (e *Session) previewed(f *finder) *picture {
	if f.image == "" || e.cell[0] == 0 || e.cell[1] == 0 {
		return nil
	}
	return e.picture(f.image)
}

// Image returns the PNG of the image numbered id (B303).
func (e *Session) Image(id int) ([]byte, bool) {
	if p := e.pictures.byID[id]; p != nil {
		return p.png, true
	}
	return nil, false
}

// SetCell sets how many pixels wide and high a cell of the client's
// terminal is, or 0 when it shows no images. Images take rows only once
// it is set (B303).
func (e *Session) SetCell(w, h int) { e.cell = [2]int{w, h} }

// imageRows returns the rows and columns p takes in w, at most all but two
// of its rows.
func (e *Session) imageRows(w *window, p *picture) (rows, cols int) {
	return e.fitImage(p, w.cols(), max(w.h-2, 1))
}

// fitImage returns the rows and columns p takes: as many columns as its pixels
// fill, at most wide, and the rows that keep its shape, at most high.
func (e *Session) fitImage(p *picture, wide, high int) (rows, cols int) {
	cw, ch := e.cell[0], e.cell[1]
	cols = min((p.w+cw-1)/cw, wide)
	rows = (p.h*cols*cw/p.w + ch - 1) / ch
	if rows > high {
		rows, cols = high, max(high*ch*p.w/(p.h*cw), 1)
	}
	return max(rows, 1), cols
}
