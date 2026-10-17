package editor

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/plugin"
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
	"lowercase": "gu", "uppercase": "gU", "swap_case": "g~", "comment": "gc", "format": "gq",
}

// targets are the keys of each target a command may name.
var targets = map[string]string{
	"word": "w", "big_word": "W", "word_end": "e", "word_back": "b",
	"line_start": "0", "first_char": "^", "line_end": "$", "down": "j", "up": "k",
	"file_start": "gg", "file_end": "G",
	"find": "f", "find_back": "F", "till": "t", "till_back": "T",
	"inner_word": "iw", "a_word": "aw", "inner_paragraph": "ip", "a_paragraph": "ap",
	"inner_quote": "i\"", "a_quote": "a\"", "inner_single_quote": "i'", "a_single_quote": "a'",
	"inner_paren": "i(", "a_paren": "a(", "inner_brace": "i{", "a_brace": "a{",
	"inner_bracket": "i[", "a_bracket": "a[",
	"inner_function": "if", "a_function": "af", "inner_class": "ic", "a_class": "ac",
	"inner_argument": "ia", "an_argument": "aa",
}

// change is the newest version of the buffer a client shows, as the
// server tells it (B78).
type change struct {
	Buffer  int            `json:"buffer"`
	Version kernel.Version `json:"version"`
}

// commandKeys returns the keys c names. A name in neither table can name
// what a plugin registered, as plugin.key: kit.gs is the operator kit
// registered as gs.
func (e *Session) commandKeys(c Command) (string, error) {
	op, ok := ops[c.Op]
	if !ok {
		if op, ok = e.registered(c.Op, plugin.Operator); !ok {
			return "", fmt.Errorf("no operator named %q", c.Op)
		}
	}
	target, ok := targets[c.Target.Name]
	if !ok {
		target, ok = e.registered(c.Target.Name, plugin.Object)
	}
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

// registered returns the keys of what a plugin registered of kind, named as
// plugin.key.
func (e *Session) registered(name string, kind plugin.Kind) (string, bool) {
	pname, key, _ := strings.Cut(name, ".")
	for _, p := range e.plugins {
		for _, r := range p.Registered {
			if p.Name == pname && r.Kind == kind && r.Name == key {
				return key, true
			}
		}
	}
	return "", false
}

// Request runs r's command in the session.
func (e *Session) Request(r Request) error {
	keys, err := e.commandKeys(r.Command)
	if err != nil {
		return err
	}
	b, err := e.buffer(r.Buffer)
	if err != nil {
		return err
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
		if r.Command.Op == "format" {
			return errors.New("formatting waits for the language server, so it cannot be proposed")
		}
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

// buffer returns the buffer numbered id, or the current one for 0.
func (e *Session) buffer(id int) (*buffer, error) {
	if id == 0 {
		return e.win.buf, nil
	}
	i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.id == id })
	if i < 0 {
		return nil, fmt.Errorf("no buffer %d", id)
	}
	return e.bufs[i], nil
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

// SocketPath is where fude listens for clients: the socket setting, or else
// a socket named by its process in XDG_RUNTIME_DIR or the temporary
// directory. A fixed path lets a client on another machine reach it
// through ssh -L (B97).
func (ed *Editor) SocketPath() string {
	if ed.cfg.Socket != "" {
		return ed.cfg.Socket
	}
	return processSocket()
}

// processSocket is a socket named by this process, in socketDir.
func processSocket() string {
	return filepath.Join(socketDir(), fmt.Sprintf("fude.%d.sock", os.Getpid()))
}

// socketDir is where fude listens without the socket setting:
// XDG_RUNTIME_DIR, or else the temporary directory.
func socketDir() string {
	return cmp.Or(os.Getenv("XDG_RUNTIME_DIR"), os.TempDir())
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
// returned func is called. When another fude answers at path, it listens
// at a socket named by its process instead, as fude does without the
// socket setting (B97). It tells the user when it cannot listen.
func (e *Session) Serve(path string) func() {
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		path = processSocket()
	}
	// No one answers this socket, so a fude that crashed left it.
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
// call runs on the editor loop. Between replies it tells the client when the
// text of the buffer it shows changes (B78).
func (e *Session) serve(c net.Conn, o kernel.Origin) {
	defer c.Close()
	var s *Session
	on := func(f func()) {
		done := make(chan struct{})
		e.queue <- func() { f(); close(done) }
		<-done
	}
	changed := make(chan change, 1)
	on(func() {
		s = e.Attach(o)
		s.changed = changed
	})
	api.Trace("rpc", string(o)+" attached", 0)
	defer api.Trace("rpc", string(o)+" left", 0)
	defer on(func() { s.leave() })
	in, enc := bufio.NewScanner(c), json.NewEncoder(c)
	in.Buffer(nil, 1<<26)
	var mu sync.Mutex
	out := func(v any) error {
		mu.Lock()
		defer mu.Unlock()
		return enc.Encode(v)
	}
	left := make(chan struct{})
	defer close(left)
	go func() {
		for {
			select {
			case c := <-changed:
				if out(map[string]any{"method": "changed", "params": c}) != nil {
					return
				}
			case <-left:
				return
			}
		}
	}()
	for in.Scan() {
		var call struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		reply, quit := map[string]any{}, false
		if err := json.Unmarshal(in.Bytes(), &call); err != nil {
			reply["error"] = fmt.Sprintf("read call: %v", err)
		} else if call.Method == "tail" {
			// This writes the events off the loop, as they come, until the
			// client leaves.
			kept, next, stop := api.Watch()
			defer stop()
			if out(map[string]any{"id": call.ID}) != nil {
				return
			}
			for _, ev := range kept {
				if out(map[string]any{"method": "event", "params": ev}) != nil {
					return
				}
			}
			go func() {
				for {
					select {
					case ev := <-next:
						if out(map[string]any{"method": "event", "params": ev}) != nil {
							return
						}
					case <-left:
						return
					}
				}
			}()
			continue
		} else {
			reply["id"] = call.ID
			on(func() {
				at := time.Now()
				res, err := s.call(call.Method, call.Params)
				traced("rpc", string(o)+" called "+call.Method, at, err)
				switch {
				case err != nil:
					reply["error"] = err.Error()
					return
				case res != nil:
					reply["result"] = res
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
				quit = s.done
			})
		}
		if out(reply) != nil || quit {
			return
		}
	}
}

// call runs one call of a client's: keys to type, a request, state to
// read, or a replica's pull or push. It returns the result of a call that
// has one of its own, and nil for those whose result is the state.
func (e *Session) call(method string, params json.RawMessage) (any, error) {
	switch method {
	case "keys":
		var p struct {
			Keys string `json:"keys"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("read keys: %w", err)
		}
		e.keys(p.Keys)
	case "request":
		var r Request
		if err := json.Unmarshal(params, &r); err != nil {
			return nil, fmt.Errorf("read request: %w", err)
		}
		return nil, e.Request(r)
	case "pull":
		var p struct {
			Buffer  int             `json:"buffer"`
			Version *kernel.Version `json:"version"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("read pull: %w", err)
		}
		return e.pull(p.Buffer, p.Version)
	case "push":
		var p struct {
			Buffer  int            `json:"buffer"`
			Version kernel.Version `json:"version"`
			Changes [][]wireEdit   `json:"changes"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, fmt.Errorf("read push: %w", err)
		}
		return e.push(p.Buffer, p.Version, p.Changes)
	case "state":
	default:
		return nil, errors.New("no method " + method)
	}
	return nil, nil
}

// wireEdit replaces [Start, End) with Text, as a replica pulls and pushes
// it.
type wireEdit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// pulled is a buffer's edits since the version a replica holds, or its
// whole text when the version is too old or not given (B79).
type pulled struct {
	Buffer  int            `json:"buffer"`
	Version kernel.Version `json:"version"`
	Edits   [][]wireEdit   `json:"edits,omitempty"`
	Text    *string        `json:"text,omitempty"`
	Indent  int            `json:"indent"` // the columns > and < shift by, from config.toml
	Tab     int            `json:"tab"`    // the columns a tab shows as, from config.toml
}

// pull returns buffer id's edits since version, and shows the buffer in the
// session's window so the client hears when it changes.
func (e *Session) pull(id int, version *kernel.Version) (pulled, error) {
	b, err := e.buffer(id)
	if err != nil {
		return pulled{}, err
	}
	if b != e.win.buf {
		e.win.show(b)
	}
	t := b.text.Snapshot()
	p := pulled{Buffer: b.id, Version: t.Version(), Indent: host{e}.IndentWidth(), Tab: host{e}.TabWidth()}
	if version != nil {
		if lists, ok := b.text.Since(*version); ok {
			for _, l := range lists {
				edits := make([]wireEdit, len(l))
				for i, ed := range l {
					edits[i] = wireEdit(ed)
				}
				p.Edits = append(p.Edits, edits)
			}
			return p, nil
		}
	}
	text := t.Slice(0, t.Len())
	p.Text = &text
	return p, nil
}

// pushed is whether a replica's edits applied, and the buffer's version.
// Edits made against an older version do not apply, and the replica pulls
// and pushes them again (B79).
type pushed struct {
	Applied bool           `json:"applied"`
	Version kernel.Version `json:"version"`
}

// push applies changes, each a list of edits, made against version of
// buffer id one after another, as one node of the session's.
func (e *Session) push(id int, version kernel.Version, changes [][]wireEdit) (pushed, error) {
	b, err := e.buffer(id)
	if err != nil {
		return pushed{}, err
	}
	if b.file.readonly {
		return pushed{}, errors.New("cannot make changes, the file is read-only")
	}
	if b.text.Snapshot().Version() != version {
		return pushed{false, b.text.Snapshot().Version()}, nil
	}
	b.text.BeginGroup(e.origin)
	defer func() {
		b.text.EndGroup(e.origin)
		b.head = b.text.Head()
	}()
	for _, c := range changes {
		t := kernel.Transaction{Base: b.text.Snapshot().Version(), Origin: e.origin, Label: "push", Time: e.now()}
		for _, ed := range c {
			t.Edits = append(t.Edits, kernel.Edit(ed))
		}
		if err := b.text.Apply(t); err != nil {
			return pushed{}, fmt.Errorf("push: %w", err)
		}
		if len(c) > 0 {
			b.pushChange(c[0].Start)
		}
	}
	return pushed{true, b.text.Snapshot().Version()}, nil
}
