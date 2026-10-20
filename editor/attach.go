package editor

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// frame returns what e's client draws. It brings the text of each shown
// buffer that changed since sent, the versions the client has, and moves
// sent on.
func (e *Session) frame(sent map[int]kernel.Version) wire.Frame {
	f := wire.Frame{
		Windows: e.Windows(), Buffers: e.Buffers(), Line: e.Line(), LineCursor: e.LineCursor(), Mode: e.Mode(), Message: e.Message(), Confirm: e.Confirm(),
		Cursors: e.Cursors(), Recording: e.Recording(), Substituting: e.Substituting(), Terminal: e.Terminal(), Shown: e.Shown(),
	}
	if fl, ok := e.Menu(); ok {
		f.Menu = &fl
	}
	if sel, ok := e.Selection(); ok {
		f.Selection = &sel
	}
	if text, ok := e.TerminalClipboard(); ok {
		f.Clipboard = &text
	}
	if d, ok := e.Wait(); ok {
		f.Wait = &d
	}
	f.Theme, f.Base16, f.Colors = e.Theme()
	f.Styles, f.Restyled = e.Styles()
	// A session that left is gone from the sessions. One that fude quit is
	// still there.
	if f.Done = e.done; f.Done && slices.Contains(e.sessions, e) {
		f.Message = "fude quit"
	}
	// An image goes to each client once.
	give := func(id int) {
		if p := e.pictures.byID[id]; p != nil && !e.sentImages[id] {
			if f.Images == nil {
				f.Images = map[int][]byte{}
			}
			if e.sentImages == nil {
				e.sentImages = map[int]bool{}
			}
			f.Images[id], e.sentImages[id] = p.png, true
		}
	}
	for _, v := range f.Windows {
		for _, rows := range v.Virtual {
			for _, r := range rows {
				give(r.Image)
			}
		}
	}
	if f.Menu != nil {
		if p := f.Menu.DocImage; p != nil {
			give(p.ID)
		}
	}
	f.Freed = e.Freed()
	for _, v := range f.Windows {
		b, err := e.buffer(v.Buffer)
		if err != nil {
			continue
		}
		t := b.text.Snapshot()
		have, known := sent[b.id]
		if known && have == t.Version() {
			continue
		}
		text := wire.Text{Buffer: b.id, Version: t.Version()}
		// A client with no copy, or one older than the edits the kernel
		// keeps, takes the whole text.
		if edits, ok := b.text.Since(have); known && ok {
			text.Edits = edits
		} else {
			whole := t.Slice(0, t.Len())
			text.Whole = &whole
		}
		f.Texts = append(f.Texts, text)
		sent[b.id] = t.Version()
	}
	return f
}

// attached runs the input of s's client on the loop through on, one notice
// a line. It sends the client a frame after each notice, and every 50ms,
// when what it draws has changed (B239). out writes one line to the
// client.
func (e *Session) attached(s *Session, in *bufio.Scanner, out func(any) error, on func(func())) {
	kick, gone := make(chan struct{}, 1), make(chan struct{})
	handled := 0 // on the loop, as the frames read it there
	defer close(gone)
	e.clients.Add(1)
	go func() {
		defer e.clients.Add(-1)
		// One goroutine makes and sends the frames, so the edits of each
		// reach the client in order.
		sent, last := map[int]kernel.Version{}, []byte(nil)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			var b []byte
			var done bool
			var err error
			on(func() {
				f := s.frame(sent)
				f.Input = handled
				done = f.Done
				b, err = json.Marshal(f)
			})
			if err != nil {
				logLine(fmt.Sprintf("attached client: %v", err))
				return
			}
			if !bytes.Equal(b, last) {
				if out(map[string]any{"method": "frame", "params": json.RawMessage(b)}) != nil {
					return
				}
				s.record("frame", fmt.Sprintf("sent %d bytes", len(b)), 0)
				last = b
			}
			if done {
				return
			}
			select {
			case <-kick:
			case <-tick.C:
			case <-gone:
				return
			}
		}
	}()
	for in.Scan() {
		var n struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(in.Bytes(), &n); err != nil {
			logLine(fmt.Sprintf("attached client: %v", err))
			continue
		}
		on(func() {
			handled++
			at := time.Now()
			if err := s.input(n.Method, n.Params); err != nil {
				logLine(fmt.Sprintf("attached client: %v", err))
			}
			// Feed already traces each key.
			if n.Method != "keys" {
				s.record("rpc", fmt.Sprintf("sent %s, %d bytes", n.Method, len(n.Params)), time.Since(at))
			}
		})
		select {
		case kick <- struct{}{}:
		default:
		}
	}
}

// Finish runs the loop's work until each attached client has had its last
// frame, for at most d, so a client hears that fude quit before fude
// exits (B242).
func (e *Session) Finish(d time.Duration) {
	for deadline := time.Now().Add(d); e.clients.Load() > 0 && time.Now().Before(deadline); {
		select {
		case f := <-e.queue:
			f()
		case <-time.After(time.Millisecond):
		}
	}
}

// input is one notice of an attached client's: keys typed, a paste, its
// size, focus, a key that timed out or hints to show.
func (e *Session) input(method string, params json.RawMessage) error {
	switch method {
	case "keys":
		var keys []vim.Key
		if err := json.Unmarshal(params, &keys); err != nil {
			return fmt.Errorf("read keys: %w", err)
		}
		for _, k := range keys {
			e.Feed(k)
		}
	case "paste":
		var text string
		if err := json.Unmarshal(params, &text); err != nil {
			return fmt.Errorf("read paste: %w", err)
		}
		e.Paste(text)
	case "size":
		var size [2]int
		if err := json.Unmarshal(params, &size); err != nil {
			return fmt.Errorf("read size: %w", err)
		}
		e.SetSize(size[0], size[1])
	case "cell":
		var cell [2]int
		if err := json.Unmarshal(params, &cell); err != nil {
			return fmt.Errorf("read cell: %w", err)
		}
		e.SetCell(cell[0], cell[1])
	case "focus":
		e.Focus()
	case "timeout":
		e.Timeout()
	default:
		return fmt.Errorf("no notice %q", method)
	}
	return nil
}
