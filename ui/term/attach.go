package term

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/y3owk1n/fude/clipboard"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// Attached is a session of another fude. The terminal client draws it from
// the frames that fude sends, and sends the user's input back (B239, D39).
type Attached struct {
	c     net.Conn
	enc   *json.Encoder
	queue chan func()
	f     editor.Frame
	texts map[int]*kernel.Buffer // each buffer's text, as the frames bring it
	w, h  int                    // the size last sent
	clip  *string                // for the terminal's clipboard, once
	done  bool
	ended string // why the session ended, for after the screen is gone
}

var _ Session = (*Attached)(nil)

// Attach makes a session of the fude at socket and returns it once its
// first frame has come.
func Attach(socket string) (*Attached, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return nil, fmt.Errorf("attach: %w", err)
	}
	a := &Attached{c: c, enc: json.NewEncoder(c), queue: make(chan func(), 64), texts: map[int]*kernel.Buffer{}}
	in := bufio.NewScanner(c)
	in.Buffer(nil, 1<<26)
	if err := a.enc.Encode(map[string]any{"id": 1, "method": "attach"}); err != nil {
		c.Close()
		return nil, fmt.Errorf("attach %s: %w", socket, err)
	}
	var reply struct {
		Error string `json:"error"`
	}
	switch {
	case !in.Scan():
		c.Close()
		return nil, fmt.Errorf("attach %s: %w", socket, cmp.Or(in.Err(), io.EOF))
	case json.Unmarshal(in.Bytes(), &reply) != nil:
		c.Close()
		return nil, fmt.Errorf("attach %s: fude answered %s", socket, in.Text())
	case reply.Error != "":
		c.Close()
		return nil, fmt.Errorf("attach %s: %s", socket, reply.Error)
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
			Method string       `json:"method"`
			Params editor.Frame `json:"params"`
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
func (a *Attached) apply(f editor.Frame) {
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
	for i, v := range f.Views {
		if b := a.texts[v.Buffer]; b != nil {
			f.Views[i].Text = b.Snapshot()
		}
	}
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
func (a *Attached) ShowHints()        { a.send("hints", nil) }

// SetSize tells fude the screen's size only when it changes, because the
// client calls it on every frame.
func (a *Attached) SetSize(w, h int) {
	if w != a.w || h != a.h {
		a.w, a.h = w, h
		a.send("size", [2]int{w, h})
	}
}

func (a *Attached) Queue() <-chan func() { return a.queue }
func (a *Attached) Load()                {}
func (a *Attached) Done() bool           { return a.done }

func (a *Attached) Wait() (time.Duration, bool)    { return wait(a.f.Wait) }
func (a *Attached) HintsIn() (time.Duration, bool) { return wait(a.f.Hints) }

// wait reads a frame's wait, which it leaves out when nothing waits.
func wait(d *time.Duration) (time.Duration, bool) {
	if d == nil {
		return 0, false
	}
	return *d, true
}

func (a *Attached) Views() []editor.View { return a.f.Views }

func (a *Attached) Float() (editor.Float, bool)     { return orZero(a.f.Float) }
func (a *Attached) Signature() (editor.Float, bool) { return orZero(a.f.Signature) }

// orZero reads a frame's float, which it leaves out when none shows.
func orZero(f *editor.Float) (editor.Float, bool) {
	if f == nil {
		return editor.Float{}, false
	}
	return *f, true
}

func (a *Attached) Selection() (vim.Selection, bool) {
	if a.f.Selection == nil {
		return vim.Selection{}, false
	}
	return *a.f.Selection, true
}

func (a *Attached) Line() string                { return a.f.Line }
func (a *Attached) LineCursor() int             { return a.f.LineCursor }
func (a *Attached) Mode() vim.Mode              { return a.f.Mode }
func (a *Attached) Message() string             { return a.f.Message }
func (a *Attached) Cursors() []int              { return a.f.Cursors }
func (a *Attached) Recording() rune             { return a.f.Recording }
func (a *Attached) Substituting() bool          { return a.f.Substituting }
func (a *Attached) Terminal() bool              { return a.f.Terminal }
func (a *Attached) Shown() string               { return a.f.Shown }
func (a *Attached) Theme() (string, *[16]int32) { return a.f.Theme, a.f.Base16 }

// TerminalClipboard returns a copy for the terminal's clipboard once.
func (a *Attached) TerminalClipboard() (string, bool) {
	clip := a.clip
	a.clip = nil
	if clip == nil {
		return "", false
	}
	return *clip, true
}
