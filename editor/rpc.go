package editor

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// Command is an edit by names, as clients send it. It becomes the keys it
// names, so it does what typing them does (B70).
type Command struct {
	Count    int    `json:"count,omitempty"`
	Register string `json:"register,omitempty"`
	Op       string `json:"op,omitempty"` // "" moves the cursor
	Target   Target `json:"target"`
	Text     string `json:"text,omitempty"` // typed after a change
}

// Target is where a command acts.
type Target struct {
	Name string `json:"name"`
	Arg  string `json:"arg,omitempty"` // the character for find
}

// Request is a command for a buffer. With At, it acts there instead of at
// the cursor, which stays where it was (B75). At is an offset in the text at
// version Base, mapped through the edits since (B76). A proposal changes
// nothing until a user accepts it (B71).
type Request struct {
	Buffer   int            `json:"buffer,omitempty"` // 0 for the current one
	At       *int           `json:"at,omitempty"`
	Base     kernel.Version `json:"base,omitempty"`
	Proposal bool           `json:"proposal,omitempty"`
	Command  Command        `json:"command"`
}

// ops are the keys of each operator a command may name.
var ops = map[string]string{
	"": "", "delete": "d", "change": "c", "yank": "y", "indent": ">", "outdent": "<",
	"lowercase": "gu", "uppercase": "gU", "comment": "gc", "format": "gq",
}

// targets are the keys of each target a command may name.
var targets = map[string]string{
	"word": "w", "inner_word": "iw", "inner_quote": "i\"", "line_end": "$", "find": "f",
}

// keys returns the keys c names.
func (c Command) keys() (string, error) {
	op, ok := ops[c.Op]
	if !ok {
		return "", fmt.Errorf("no operator named %q", c.Op)
	}
	target, ok := targets[c.Target.Name]
	switch {
	case c.Target.Name == "lines" && op != "":
		// An operator on lines is the operator doubled.
		target = op[len(op)-1:]
	case !ok:
		return "", fmt.Errorf("no target named %q", c.Target.Name)
	}
	keys := op + target + c.Target.Arg
	if c.Count > 0 {
		keys = strconv.Itoa(c.Count) + keys
	}
	if c.Register != "" {
		keys = "\"" + c.Register + keys
	}
	return keys, nil
}

// Request runs r's command in the session.
func (e *Session) Request(r Request) error {
	keys, err := r.Command.keys()
	if err != nil {
		return err
	}
	b := e.win.buf
	if r.Buffer != 0 {
		i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.id == r.Buffer })
		if i < 0 {
			return fmt.Errorf("no buffer %d", r.Buffer)
		}
		b = e.bufs[i]
	}
	if shown := e.win.buf; b != shown {
		e.win.show(b)
		defer e.win.show(shown)
	}
	at := e.Cursor()
	if r.At != nil {
		var ok bool
		if at, ok = b.text.Map(*r.At, r.Base); !ok {
			return fmt.Errorf("the text at %d changed since version %d", *r.At, r.Base)
		}
	}
	if r.Proposal {
		return e.propose(b, at, keys, r.Command.Text)
	}
	if r.At != nil {
		cursor := b.text.SetMark(e.Cursor(), kernel.Right)
		defer func() {
			e.setCursor(b.text.Mark(cursor))
			b.text.DeleteMark(cursor)
		}()
		e.setCursor(at)
	}
	e.command(keys, r.Command.Text)
	return nil
}

// command feeds a command's keys, then types its text if they began an
// insert.
func (e *Session) command(keys, text string) {
	e.keys(keys)
	if text != "" && e.vim.Mode() == vim.Insert {
		e.Paste(text)
		e.Feed("<Esc>")
	}
}

// keys feeds keys as one complete input, so keys held for a longer mapping
// run at once.
func (e *Session) keys(keys string) {
	for _, k := range vim.Keys(keys) {
		e.Feed(k)
	}
	if e.vim.Waiting() {
		e.Timeout()
	}
}

// SocketPath is where fude listens for clients: a socket named by its
// process in XDG_RUNTIME_DIR, or else the temporary directory.
func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, fmt.Sprintf("fude.%d.sock", os.Getpid()))
}

// state is what the server tells a client after each call.
type state struct {
	Buffer    int            `json:"buffer"`
	Name      string         `json:"name"`
	Version   kernel.Version `json:"version"`
	Text      string         `json:"text"`
	Cursor    int            `json:"cursor"`
	Mode      string         `json:"mode"`
	Message   string         `json:"message,omitempty"`
	Proposals []proposal     `json:"proposals,omitempty"` // the session's, on its buffer
}

// proposal is a proposal as a client reads it.
type proposal struct {
	ID    int    `json:"id"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
	Stale bool   `json:"stale,omitempty"`
}

var modes = []string{vim.Normal: "normal", vim.Insert: "insert", vim.Cmdline: "cmdline", vim.Visual: "visual", vim.Replace: "replace"}

// Serve listens at path for clients, each a session of its own, until the
// returned func is called. It tells the user when it cannot listen.
func (e *Session) Serve(path string) func() {
	// Only a crashed fude with the same process id can have left a socket at path.
	os.Remove(path)
	l, err := net.Listen("unix", path)
	if err != nil {
		e.message = fmt.Sprintf("clients cannot attach: %v", err)
		return func() {}
	}
	go func() {
		for n := 1; ; n++ {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go e.serve(c, kernel.Origin("client"+strconv.Itoa(n)))
		}
	}()
	return func() { l.Close() }
}

// serve answers one client's calls, one line of JSON each, in order. Each
// call runs on the editor loop.
func (e *Session) serve(c net.Conn, o kernel.Origin) {
	defer c.Close()
	var s *Session
	on := func(f func()) {
		done := make(chan struct{})
		e.queue <- func() { f(); close(done) }
		<-done
	}
	on(func() { s = e.Attach(o) })
	defer on(func() { s.leave() })
	in, out := bufio.NewScanner(c), json.NewEncoder(c)
	in.Buffer(nil, 1<<26)
	for in.Scan() {
		var call struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		reply, left := map[string]any{}, false
		if err := json.Unmarshal(in.Bytes(), &call); err != nil {
			reply["error"] = fmt.Sprintf("read call: %v", err)
		} else {
			reply["id"] = call.ID
			on(func() {
				if err := s.call(call.Method, call.Params); err != nil {
					reply["error"] = err.Error()
					return
				}
				t := s.Text()
				st := state{s.win.buf.id, s.win.buf.name(), t.Version(), t.Slice(0, t.Len()), s.Cursor(), modes[s.Mode()], s.message, nil}
				for _, d := range s.decos[s] {
					if d.buf == s.win.buf {
						st.Proposals = append(st.Proposals, proposal{d.id, d.buf.text.Mark(d.start), d.buf.text.Mark(d.end), d.note, d.style == "proposal.stale"})
					}
				}
				reply["result"] = st
				left = s.done
			})
		}
		if out.Encode(reply) != nil || left {
			return
		}
	}
}

// call runs one call of a client's: keys to type, a request, or state to
// read.
func (e *Session) call(method string, params json.RawMessage) error {
	switch method {
	case "keys":
		var p struct {
			Keys string `json:"keys"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return fmt.Errorf("read keys: %w", err)
		}
		e.keys(p.Keys)
	case "request":
		var r Request
		if err := json.Unmarshal(params, &r); err != nil {
			return fmt.Errorf("read request: %w", err)
		}
		return e.Request(r)
	case "state":
	default:
		return errors.New("no method " + method)
	}
	return nil
}
