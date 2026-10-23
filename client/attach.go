package client

import (
	"bufio"
	"cmp"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/clipboard"
	"github.com/y3owk1n/fude/internal/relay"
	tlsshare "github.com/y3owk1n/fude/internal/share"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// Attached is a session of another fude. A frontend draws it from the
// frames that fude sends, and sends the user's input back (B239, D39).
type Attached struct {
	c     net.Conn
	enc   *json.Encoder
	queue chan func()
	f     wire.Frame
	texts map[int]*kernel.Buffer // each buffer's text, as the frames bring it
	w, h  int                    // the size last sent
	cell  [2]int                 // the cell's pixels last sent (B303)
	pics  map[int][]byte         // the images the frames brought, by id
	freed []int                  // the images frames dropped since Freed was called
	clip  *string                // for the terminal's clipboard, once
	stop  bool                   // the user asked to suspend, until Suspend says so (B424)
	done  bool
	ended string     // why the session ended, for after the screen is gone
	theme wire.Theme // a guest's own theme, which it draws in (B254)
}

var _ Session = (*Attached)(nil)

// Attach makes a session called name of the fude at socket and returns it
// once its first frame has come.
func Attach(socket, name string) (*Attached, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("attach: %w", err)
	}
	a, err := begin(c, map[string]any{"id": 1, "method": "attach", "params": map[string]string{"name": name}})
	if err != nil {
		return nil, fmt.Errorf("attach %s: %w", socket, err)
	}
	return a, nil
}

// Join makes a guest session called name of the fude that a share string
// names. The session takes own, draws in theme and is read-only when
// readOnly says. Join returns it once the host lets the guest in and its
// first frame has come (B248, B254, B255).
func Join(share, name string, own any, theme wire.Theme, readOnly bool) (*Attached, error) {
	inv, err := tlsshare.Parse(share)
	if err != nil {
		return nil, fmt.Errorf("join: %w", err)
	}
	c, err := dialShare(inv.Addr, tlsshare.Client(inv.Print))
	if err != nil {
		return nil, fmt.Errorf("join %s: %w", inv.Addr, err)
	}
	a, err := begin(c, wire.Hello{Secret: inv.Secret, Name: name, Own: own, ReadOnly: readOnly})
	if err != nil {
		return nil, fmt.Errorf("join %s: %w", inv.Addr, err)
	}
	a.theme = theme
	return a, nil
}

// dialShare dials the host at addr with TLS, or through the relay and room
// that addr names as relay/room (B251).
func dialShare(addr string, cfg *tls.Config) (net.Conn, error) {
	via, room, ok := strings.Cut(addr, "/")
	if !ok {
		return dialFirst(strings.Split(addr, ","), cfg)
	}
	raw, err := relay.Join(via, room)
	if err != nil {
		return nil, err
	}
	c := tls.Client(raw, cfg)
	if err := c.Handshake(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// dialFirst dials every address at once and returns the first whose TLS
// passes cfg, closing the rest, since an address from another network may
// never answer (B248). When none does it says why each failed (B249).
func dialFirst(addrs []string, cfg *tls.Config) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type dialed struct {
		c   net.Conn
		err error
	}
	results := make(chan dialed, len(addrs))
	for _, addr := range addrs {
		go func() {
			raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
			if err != nil {
				results <- dialed{nil, err}
				return
			}
			c := tls.Client(raw, cfg)
			if err := c.HandshakeContext(ctx); err != nil {
				raw.Close()
				results <- dialed{nil, fmt.Errorf("%s: %w", addr, err)}
				return
			}
			results <- dialed{c, nil}
		}()
	}
	var why []string
	for left := len(addrs); left > 0; left-- {
		d := <-results
		if d.err != nil {
			why = append(why, d.err.Error())
			continue
		}
		go func() {
			for range left - 1 {
				if d := <-results; d.c != nil {
					d.c.Close()
				}
			}
		}()
		return d.c, nil
	}
	return nil, fmt.Errorf("no address of the host answered, %s", strings.Join(why, ", "))
}

// begin sends hello on c and, once fude answers, reads its frames and
// returns the session when the first has come. It closes c on an error.
func begin(c net.Conn, hello any) (*Attached, error) {
	a := &Attached{c: c, enc: json.NewEncoder(c), queue: make(chan func(), 64), texts: map[int]*kernel.Buffer{}}
	in := bufio.NewScanner(c)
	in.Buffer(nil, 1<<26)
	if err := a.enc.Encode(hello); err != nil {
		c.Close()
		return nil, err
	}
	var reply struct {
		Error string `json:"error"`
	}
	switch {
	case !in.Scan():
		c.Close()
		return nil, cmp.Or(in.Err(), io.EOF)
	case json.Unmarshal(in.Bytes(), &reply) != nil:
		c.Close()
		return nil, fmt.Errorf("fude answered %s", in.Text())
	case reply.Error != "":
		c.Close()
		return nil, errors.New(reply.Error)
	}
	go a.read(in)
	(<-a.queue)()
	return a, nil
}

// read queues each frame for the loop, and queues the session's end when
// the connection breaks.
func (a *Attached) read(in *bufio.Scanner) {
	for in.Scan() {
		var n struct {
			Method string     `json:"method"`
			Params wire.Frame `json:"params"`
		}
		if json.Unmarshal(in.Bytes(), &n) != nil || n.Method != "frame" {
			continue
		}
		f := n.Params
		a.queue <- func() { a.apply(f) }
		if f.Done {
			return
		}
	}
	err := cmp.Or(in.Err(), io.EOF)
	a.queue <- func() { a.end(fmt.Sprintf("the connection to fude broke: %v", err)) }
}

// apply brings the texts up to date with f and draws from it.
func (a *Attached) apply(f wire.Frame) {
	for _, t := range f.Texts {
		if t.Whole != nil {
			a.texts[t.Buffer] = kernel.New(*t.Whole, nil)
			continue
		}
		b := a.texts[t.Buffer]
		for _, edits := range t.Edits {
			if b == nil || b.Apply(kernel.Transaction{Base: b.Snapshot().Version(), Edits: edits}) != nil {
				a.end("fude sent edits that do not fit the text")
				return
			}
		}
	}
	for i, v := range f.Windows {
		if b := a.texts[v.Buffer]; b != nil {
			f.Windows[i].Text = b.Snapshot()
		}
	}
	a.stop = a.stop || f.Suspend
	// A copy goes on this machine's clipboard, or else the terminal's
	// (B241).
	if f.Clipboard != nil {
		if err := clipboard.Write(*f.Clipboard); errors.Is(err, clipboard.ErrNone) {
			a.clip = f.Clipboard
		}
	}
	// The client redraws everything when the colours' pointer changes, so
	// equal colours keep the old pointer.
	if f.Base16 != nil && a.f.Base16 != nil && *f.Base16 == *a.f.Base16 {
		f.Base16 = a.f.Base16
	}
	for id, b := range f.Images {
		if a.pics == nil {
			a.pics = map[int][]byte{}
		}
		a.pics[id] = b
	}
	for _, id := range f.Freed {
		delete(a.pics, id)
	}
	a.freed = append(a.freed, f.Freed...)
	a.f = f
	if f.Done {
		a.end(f.Message)
	}
}

// end ends the session, saying why once the screen is gone.
func (a *Attached) end(why string) {
	if !a.done {
		a.done, a.ended = true, why
	}
}

// Ended returns why the session ended, or "" when it left of its own
// accord or has not ended (B242).
func (a *Attached) Ended() string { return a.ended }

// Handled returns how many of the user's notices fude has handled, as of
// the last frame.
func (a *Attached) Handled() int { return a.f.Input }

// Close ends the connection.
func (a *Attached) Close() error { return a.c.Close() }

// send writes one notice of the user's to fude. A write that fails is
// left to read, which then hears the connection break.
func (a *Attached) send(method string, params any) {
	a.enc.Encode(map[string]any{"method": method, "params": params})
}

func (a *Attached) Feed(k vim.Key)    { a.send("keys", []vim.Key{k}) }
func (a *Attached) Paste(text string) { a.send("paste", text) }
func (a *Attached) Focus()            { a.send("focus", nil) }
func (a *Attached) Timeout()          { a.send("timeout", nil) }

// SetSize tells fude the screen's size only when it changes, because the
// client calls it on every frame.
func (a *Attached) SetSize(w, h int) {
	if w != a.w || h != a.h {
		a.w, a.h = w, h
		a.send("size", [2]int{w, h})
	}
}

// SetCell tells fude a cell's pixels when they change.
func (a *Attached) SetCell(w, h int) {
	if [2]int{w, h} != a.cell {
		a.cell = [2]int{w, h}
		a.send("cell", a.cell)
	}
}

// Freed returns the images frames dropped since it was last called.
func (a *Attached) Freed() []int {
	out := a.freed
	a.freed = nil
	return out
}

// Image returns the PNG of an image a frame brought.
func (a *Attached) Image(id int) ([]byte, bool) {
	b, ok := a.pics[id]
	return b, ok
}

func (a *Attached) Queue() <-chan func() { return a.queue }
func (a *Attached) Load()                {}
func (a *Attached) Done() bool           { return a.done }

func (a *Attached) Wait() (time.Duration, bool) { return wait(a.f.Wait) }

// wait reads a frame's wait, which it leaves out when nothing waits.
func wait(d *time.Duration) (time.Duration, bool) {
	if d == nil {
		return 0, false
	}
	return *d, true
}

func (a *Attached) Windows() []wire.Window { return a.f.Windows }
func (a *Attached) Buffers() []wire.Tab    { return a.f.Buffers }

func (a *Attached) Menu() (wire.Menu, bool) { return orZero(a.f.Menu) }

// orZero reads a frame's menu, which it leaves out when none shows.
func orZero(f *wire.Menu) (wire.Menu, bool) {
	if f == nil {
		return wire.Menu{}, false
	}
	return *f, true
}

func (a *Attached) Selection() (vim.Selection, bool) {
	if a.f.Selection == nil {
		return vim.Selection{}, false
	}
	return *a.f.Selection, true
}

func (a *Attached) Line() string       { return a.f.Line }
func (a *Attached) LineCursor() int    { return a.f.LineCursor }
func (a *Attached) Mode() vim.Mode     { return a.f.Mode }
func (a *Attached) Pending() bool      { return a.f.Pending }
func (a *Attached) Message() string    { return a.f.Message }
func (a *Attached) Confirm() string    { return a.f.Confirm }
func (a *Attached) Cursors() []int     { return a.f.Cursors }
func (a *Attached) Recording() rune    { return a.f.Recording }
func (a *Attached) Substituting() bool { return a.f.Substituting }
func (a *Attached) Terminal() bool     { return a.f.Terminal }
func (a *Attached) Shown() string      { return a.f.Shown }
func (a *Attached) Title() string      { return a.f.Title }

func (a *Attached) Styles() (map[string]wire.Style, int) { return a.f.Styles, a.f.Restyled }

// Theme returns a guest's own theme, or else the one fude draws in.
func (a *Attached) Theme() (string, *[16]int32, *wire.Colors) {
	if a.theme.Name != "" {
		return a.theme.Name, a.theme.Base16, a.theme.Colors
	}
	return a.f.Theme, a.f.Base16, a.f.Colors
}

// Suspend reports, once, that the user asked to suspend to the shell.
func (a *Attached) Suspend() bool {
	ok := a.stop
	a.stop = false
	return ok
}

// TerminalClipboard returns a copy for the terminal's clipboard once.
func (a *Attached) TerminalClipboard() (string, bool) {
	clip := a.clip
	a.clip = nil
	if clip == nil {
		return "", false
	}
	return *clip, true
}
