package term

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/y3owk1n/fude/wire"
)

// images is how the client shows images, by kitty's graphics protocol. It
// sends each image to the terminal once and draws it as cells of Unicode
// placeholders, so images scroll, split and clip as text does (B303, D50).
type images struct {
	tty     tcell.Tty    // asked for the cell's pixels, nil when the terminal shows no images
	out     io.Writer    // where the images go, the terminal
	tmux    bool         // the terminal is tmux, which passes the images on
	cell    [2]int       // a cell's pixels now, 0 when none show
	shown   [2]int       // the cell's pixels the session last heard
	sent    map[int]bool // the images the terminal has, by id
	pending bytes.Buffer // what goes to the terminal before the frame
}

// newImages returns how the client at tty shows images. It shows none
// unless the environment says the terminal is kitty or Ghostty, through
// tmux too, and the terminal shows true colour, which carries each image's
// id.
func newImages(tty tcell.Tty, colours int) images {
	term := os.Getenv("TERM")
	kittyLike := os.Getenv("KITTY_WINDOW_ID") != "" || os.Getenv("GHOSTTY_RESOURCES_DIR") != "" || term == "xterm-kitty" || term == "xterm-ghostty"
	if !kittyLike || colours < 1<<24 {
		tty = nil
	}
	return images{tty: tty, out: tty, tmux: os.Getenv("TMUX") != "", sent: map[int]bool{}}
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

// send queues the image id for the terminal, as rows by cols cells of
// placeholders, unless the terminal has it.
func (im *images) send(id, rows, cols int, png []byte) {
	if im.sent[id] {
		return
	}
	im.sent[id] = true
	data := base64.StdEncoding.EncodeToString(png)
	for at := 0; at < len(data) || at == 0; at += kitty.MaxChunkSize {
		chunk, more := data[at:min(at+kitty.MaxChunkSize, len(data))], "0"
		if at+kitty.MaxChunkSize < len(data) {
			more = "1"
		}
		opts := []string{"m=" + more}
		if at == 0 {
			opts = append([]string{"a=T", "U=1", "f=100", "q=2", fmt.Sprintf("i=%d", id), fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows)}, opts...)
		}
		seq := ansi.KittyGraphics([]byte(chunk), opts...)
		if im.tmux {
			seq = "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
		}
		im.pending.WriteString(seq)
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

// image draws row r of an image at row y of v, as placeholder cells in the
// colour of its id, and sends the image first when the terminal lacks it.
func (c *client) image(v wire.View, line int, r wire.Virtual, y int) {
	rows := 0
	for _, o := range v.Virtual[line] {
		if o.Image == r.Image {
			rows++
		}
	}
	if png, ok := c.e.Image(r.Image); ok {
		c.img.send(r.Image, rows, r.Cols, png)
	}
	style := base.Foreground(color.NewRGBColor(int32(r.Image>>16&0xff), int32(r.Image>>8&0xff), int32(r.Image&0xff)))
	for col := range min(r.Cols, v.W) {
		c.s.SetContent(v.X+col, y, kitty.Placeholder, []rune{kitty.Diacritic(r.Row), kitty.Diacritic(col)}, style)
	}
}
