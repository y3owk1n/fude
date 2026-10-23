package tui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/frontend/wire"
)

// images is how the client shows images. In kitty and Ghostty it sends
// each image to the terminal once and draws it as cells of Unicode
// placeholders, so images scroll, split and clip as text does. In WezTerm
// and iTerm2 it paints each image over cells it locks, after the frame
// shows (B303, B310, D50).
type images struct {
	tty     tcell.Tty      // asked for the cell's pixels, nil when the terminal shows no images
	out     io.Writer      // where the images go, the terminal
	mode    int            // how the terminal shows them
	tmux    bool           // the terminal is tmux, which passes the images on
	cell    [2]int         // a cell's pixels now, 0 when none show
	shown   [2]int         // the cell's pixels the session last heard
	ids     map[[3]int]int // the terminal's id of each image at each size, by fude's id, rows and columns
	next    int            // the last id the terminal got
	pending bytes.Buffer   // what goes to the terminal before the frame

	// What the painted modes paint over cells: this frame's images, the
	// boxes drawn over the windows, and the images painted and locked.
	frame   map[[3]int]*overlay
	boxes   [][4]int
	painted []overlay
	screen  [2]int         // the screen's size when they were painted
	scaled  map[int][]byte // by the terminal's id, the PNG iTerm2 gets, at its size
}

// How a terminal shows images: none, kitty's Unicode placeholders, kitty's
// placements at the cursor as WezTerm takes them, or iTerm2's inline
// images.
const (
	noImages = iota
	placeholders
	placements
	inline
)

// overlay is an image a painted mode paints over the cells from x, y, rows
// by cols. after is how many boxes the frame drew before it. A box drawn
// after it covers it.
type overlay struct {
	id, tid, x, y, rows, cols int
	seen, after               int
}

// imageBase is the first id this client gives the terminal. The terminal
// keeps ids by window, so another fude in the same window starts elsewhere.
var imageBase = os.Getpid() % 4096 << 12

// newImages returns how the client at tty shows images, from what the
// environment says the terminal is. kitty and Ghostty take placeholders,
// through tmux too, when the terminal shows true colour, which carries each
// image's id. WezTerm takes placements and iTerm2 inline images, outside
// tmux, whose redraws would erase them.
func newImages(tty tcell.Tty, colours int) images {
	term, program, tmux := os.Getenv("TERM"), os.Getenv("TERM_PROGRAM"), os.Getenv("TMUX") != ""
	mode := noImages
	switch {
	case os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || term == "xterm-kitty" || term == "xterm-ghostty":
		if colours >= 1<<24 {
			mode = placeholders
		}
	case tmux:
	case program == "WezTerm":
		mode = placements
	case program == "iTerm.app" || os.Getenv("LC_TERMINAL") == "iTerm2":
		mode = inline
	}
	if mode == noImages {
		tty = nil
	}
	return images{tty: tty, out: tty, mode: mode, tmux: tmux}
}

// size returns a cell's pixels now, or 0 when the terminal shows no images
// or does not say.
func (im *images) size() [2]int {
	im.cell = [2]int{}
	if im.tty == nil {
		return im.cell
	}
	if ws, err := im.tty.WindowSize(); err == nil {
		im.cell[0], im.cell[1] = ws.CellDimensions()
	}
	return im.cell
}

// place returns the terminal's id for image id at rows by cols cells. The
// first time, it queues the image for the terminal. Each size is a
// placement of its own, so each gets its own id. It reports false until
// the image's PNG has arrived.
func (im *images) place(id, rows, cols int, png func(id int) ([]byte, bool)) (int, bool) {
	at := [3]int{id, rows, cols}
	if tid, ok := im.ids[at]; ok {
		return tid, true
	}
	b, ok := png(id)
	if !ok {
		return 0, false
	}
	if im.ids == nil {
		im.ids = map[[3]int]int{}
	}
	im.next++
	tid := imageBase + im.next
	im.ids[at] = tid
	switch im.mode {
	case inline:
		// iTerm2 gets the image each time it paints it, so it gets it at the
		// size it shows, not as large as the file.
		if small, err := shrink(b, cols*im.cell[0], rows*im.cell[1]); err == nil {
			b = small
		}
		if im.scaled == nil {
			im.scaled = map[int][]byte{}
		}
		im.scaled[tid] = b
	default:
		im.send(tid, rows, cols, b)
	}
	return tid, true
}

// free queues a delete of image id at every size, which frees its data in
// the terminal (B306).
func (im *images) free(id int) {
	for at, tid := range im.ids {
		if at[0] == id {
			delete(im.ids, at)
			delete(im.scaled, tid)
			if im.mode != inline {
				im.write(ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", tid), "q=2"))
			}
		}
	}
}

// write queues seq for the terminal, wrapped for tmux to pass on.
func (im *images) write(seq string) {
	if im.tmux {
		seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	im.pending.WriteString(seq)
}

// send queues the image id for the terminal, as rows by cols cells of
// placeholders.
func (im *images) send(id, rows, cols int, png []byte) {
	data := base64.StdEncoding.EncodeToString(png)
	for at := 0; at < len(data) || at == 0; at += kitty.MaxChunkSize {
		chunk, more := data[at:min(at+kitty.MaxChunkSize, len(data))], "0"
		if at+kitty.MaxChunkSize < len(data) {
			more = "1"
		}
		opts := []string{"m=" + more}
		switch {
		case at > 0:
		case im.mode == placeholders:
			opts = append([]string{"a=T", "U=1", "f=100", "q=2", fmt.Sprintf("i=%d", id), fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows)}, opts...)
		default:
			// A placement puts it at the cursor later.
			opts = append([]string{"a=t", "f=100", "q=2", fmt.Sprintf("i=%d", id)}, opts...)
		}
		im.write(ansi.KittyGraphics([]byte(chunk), opts...))
	}
}

// flush sends the terminal what the frame's images need before it shows.
func (im *images) flush() error {
	if im.pending.Len() == 0 || im.out == nil {
		return nil
	}
	defer im.pending.Reset()
	if _, err := im.pending.WriteTo(im.out); err != nil {
		return fmt.Errorf("show images: %w", err)
	}
	return nil
}

// image draws row r of an image at row y of v.
func (c *client) image(v wire.Window, line int, r wire.Virtual, y int) {
	rows := 0
	for _, o := range v.Virtual[line] {
		if o.Image == r.Image {
			rows++
		}
	}
	tid, ok := c.img.place(r.Image, rows, r.Cols, c.e.Image)
	switch {
	case !ok:
	case c.img.mode == placeholders:
		c.placeholders(tid, r.Row, v.X, y, min(r.Cols, v.W))
	default:
		// The image paints over its rows once all of them show.
		o := c.img.over(r.Image, tid, v.X, y-r.Row, rows, min(r.Cols, v.W))
		o.seen++
	}
}

// picture draws p in a box whose text starts at x, y, at most width wide
// (B305).
func (c *client) picture(p wire.Picture, x, y, width int) {
	tid, ok := c.img.place(p.ID, p.Rows, p.Cols, c.e.Image)
	switch {
	case !ok:
	case c.img.mode == placeholders:
		for row := range p.Rows {
			c.placeholders(tid, row, x, y+p.Row+row, min(p.Cols, width))
		}
	default:
		c.img.over(p.ID, tid, x, y+p.Row, p.Rows, min(p.Cols, width)).seen = p.Rows
	}
}

// over returns this frame's overlay of image id at x, y, and makes it for
// the image's first row.
func (im *images) over(id, tid, x, y, rows, cols int) *overlay {
	key := [3]int{id, x, y}
	if im.frame == nil {
		im.frame = map[[3]int]*overlay{}
	}
	o, ok := im.frame[key]
	if !ok {
		o = &overlay{id: id, tid: tid, x: x, y: y, rows: rows, cols: cols, after: len(im.boxes)}
		im.frame[key] = o
	}
	return o
}

// box notes a box the frame draws, which covers an image painted under it.
func (im *images) box(x, y, w, h int) {
	if im.mode == placements || im.mode == inline {
		im.boxes = append(im.boxes, [4]int{x, y, w, h})
	}
}

// release unlocks the cells of each painted image this frame does not
// paint at the same place, so the frame's cells draw over it. It returns
// the images to paint after the frame shows, those whose rows all show and
// that no later box covers.
func (im *images) release(s tcell.Screen) []overlay {
	var now []overlay
	for _, o := range im.frame {
		covered := slices.ContainsFunc(im.boxes[o.after:], func(b [4]int) bool {
			return b[0] < o.x+o.cols && o.x < b[0]+b[2] && b[1] < o.y+o.rows && o.y < b[1]+b[3]
		})
		if o.seen >= o.rows && !covered {
			now = append(now, overlay{id: o.id, tid: o.tid, x: o.x, y: o.y, rows: o.rows, cols: o.cols})
		}
	}
	im.frame, im.boxes = nil, im.boxes[:0]
	gone := map[int]bool{} // the images whose placements went, by the terminal's id
	var keep []overlay
	for _, o := range im.painted {
		if slices.Contains(now, o) {
			keep = append(keep, o)
			continue
		}
		s.LockRegion(o.x, o.y, o.cols, o.rows, false)
		if im.mode == placements && !gone[o.tid] {
			gone[o.tid] = true
			im.write(ansi.KittyGraphics(nil, "a=d", "d=i", fmt.Sprintf("i=%d", o.tid), "q=2"))
		}
	}
	// Deleting an image's placements takes all of them, so the rest paint
	// again.
	im.painted = slices.DeleteFunc(keep, func(o overlay) bool { return gone[o.tid] })
	return now
}

// paint paints each image in now not painted already, at its cells, after
// the frame shows, and locks those cells so the next frames leave them.
func (im *images) paint(s tcell.Screen, now []overlay) error {
	var b strings.Builder
	for _, o := range now {
		if slices.Contains(im.painted, o) {
			continue
		}
		fmt.Fprintf(&b, "\x1b7\x1b[%d;%dH", o.y+1, o.x+1)
		switch im.mode {
		case placements:
			b.WriteString(ansi.KittyGraphics(nil, "a=p", fmt.Sprintf("i=%d", o.tid), fmt.Sprintf("c=%d", o.cols), fmt.Sprintf("r=%d", o.rows), "C=1", "q=2"))
		case inline:
			png := im.scaled[o.tid]
			fmt.Fprintf(&b, "\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=0:%s\a", len(png), o.cols, o.rows, base64.StdEncoding.EncodeToString(png))
		}
		b.WriteString("\x1b8")
		s.LockRegion(o.x, o.y, o.cols, o.rows, true)
		im.painted = append(im.painted, o)
	}
	if b.Len() == 0 || im.out == nil {
		return nil
	}
	if _, err := io.WriteString(im.out, b.String()); err != nil {
		return fmt.Errorf("show images: %w", err)
	}
	return nil
}

// shrink returns the PNG b scaled to w by h pixels, by the nearest pixel.
func shrink(b []byte, w, h int) ([]byte, error) {
	if w <= 0 || h <= 0 {
		return b, nil
	}
	src, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("scale image: %w", err)
	}
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			dst.Set(x, y, src.At(sb.Min.X+x*sb.Dx()/w, sb.Min.Y+y*sb.Dy()/h))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, dst); err != nil {
		return nil, fmt.Errorf("scale image: %w", err)
	}
	return out.Bytes(), nil
}

// placeholders draws row row of the terminal's image tid, cols cells from
// x, y, as placeholder cells in the colour of its id.
func (c *client) placeholders(tid, row, x, y, cols int) {
	style := base.Foreground(color.NewRGBColor(int32(tid>>16&0xff), int32(tid>>8&0xff), int32(tid&0xff)))
	for col := range cols {
		c.s.SetContent(x+col, y, kitty.Placeholder, []rune{kitty.Diacritic(row), kitty.Diacritic(col)}, style)
	}
}
