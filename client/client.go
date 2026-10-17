// Package client talks to a running fude over its socket. A Replica keeps
// its own copy of a buffer, so its edits show at once, and rebases them on
// the server's order when others edit in between (D24, B79).
package client

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/y3owk1n/fude/core/kernel"
)

// Conn is a connection to fude, a session of its own on the server.
type Conn struct {
	c       net.Conn
	enc     *json.Encoder
	mu      sync.Mutex // one call at a time
	next    int
	replies chan json.RawMessage
	changed chan struct{}
	err     error // why the connection ended, once replies is closed
}

// Dial connects to the fude listening at path.
func Dial(path string) (*Conn, error) {
	c, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial fude at %s: %w", path, err)
	}
	conn := &Conn{c: c, enc: json.NewEncoder(c), replies: make(chan json.RawMessage), changed: make(chan struct{}, 1)}
	go conn.read()
	return conn, nil
}

// read routes each line the server sends: a notice to Changed, and a reply
// to the call waiting for it.
func (c *Conn) read() {
	in := bufio.NewScanner(c.c)
	in.Buffer(nil, 1<<26)
	for in.Scan() {
		var line struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(in.Bytes(), &line); err != nil {
			c.err = fmt.Errorf("read from fude: %w", err)
			break
		}
		if line.Method != "" {
			select {
			case c.changed <- struct{}{}:
			default: // the client has yet to hear of an earlier change
			}
			continue
		}
		c.replies <- append(json.RawMessage(nil), in.Bytes()...)
	}
	if c.err == nil {
		c.err = cmp.Or(in.Err(), errors.New("fude closed the connection"))
	}
	close(c.replies)
}

// Close ends the connection and its session.
func (c *Conn) Close() error { return c.c.Close() }

// Changed is told when a buffer the session shows changes on the server.
func (c *Conn) Changed() <-chan struct{} { return c.changed }

// call sends method with params and reads its result into result.
func (c *Conn) call(method string, params, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	if err := c.enc.Encode(map[string]any{"id": c.next, "method": method, "params": params}); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	raw, ok := <-c.replies
	if !ok {
		return fmt.Errorf("%s: %w", method, c.err)
	}
	var reply struct {
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	if reply.Error != "" {
		return fmt.Errorf("%s: %s", method, reply.Error)
	}
	if err := json.Unmarshal(reply.Result, result); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

// edit is an edit as the protocol carries it.
type edit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// pulled is the server's answer to pull: a buffer's edits since a version,
// or its whole text.
type pulled struct {
	Buffer  int            `json:"buffer"`
	Version kernel.Version `json:"version"`
	Edits   [][]edit       `json:"edits"`
	Text    *string        `json:"text"`
	Indent  int            `json:"indent"`
	Tab     int            `json:"tab"`
}

// Replica is a copy of one buffer. Its own edits apply to it at once, and
// Sync sends them to the server.
type Replica struct {
	c         *Conn
	buf       int
	text      *kernel.Buffer
	confirmed kernel.Version // the server's version the copy has heard of
	pending   []pending      // nodes the server has yet to take, oldest first
	open      int            // the groups Begin opened
	indent    int            // the columns > and < shift by, as the server's config says
	tab       int            // the columns a tab shows as, as the server's config says
}

// pending is one node of the copy's that the server has yet to take: the
// changes of one Edit call, or of every call in a group.
type pending []change

// change is one Edit call.
type change struct {
	edits []kernel.Edit
	old   []string       // the text each edit replaced
	at    kernel.Version // the copy's version before it
}

// Open copies buffer, or the session's current one for 0.
func (c *Conn) Open(buffer int) (*Replica, error) {
	var p pulled
	if err := c.call("pull", map[string]any{"buffer": buffer}, &p); err != nil {
		return nil, err
	}
	if p.Text == nil {
		return nil, errors.New("pull: no text")
	}
	return &Replica{c: c, buf: p.Buffer, text: kernel.New(*p.Text, nil), confirmed: p.Version, indent: p.Indent, tab: p.Tab}, nil
}

// Text returns the copy's text.
func (r *Replica) Text() string {
	t := r.text.Snapshot()
	return t.Slice(0, t.Len())
}

// Begin starts a group of Edit calls that the server takes as one node, as
// an insert is. Groups nest.
func (r *Replica) Begin() {
	if r.open == 0 {
		r.pending = append(r.pending, nil)
	}
	r.open++
	r.text.BeginGroup("local")
}

// End ends a group Begin started.
func (r *Replica) End() {
	r.open--
	r.text.EndGroup("local")
	if n := len(r.pending); r.open == 0 && len(r.pending[n-1]) == 0 {
		r.pending = r.pending[:n-1]
	}
}

// Edit applies edits, sorted and apart, to the copy at once.
func (r *Replica) Edit(edits ...kernel.Edit) error {
	if len(edits) == 0 {
		return nil
	}
	t := r.text.Snapshot()
	c := change{edits: edits, at: t.Version()}
	for _, e := range edits {
		if e.Start < 0 || e.End > t.Len() || e.Start > e.End {
			return fmt.Errorf("edit [%d, %d) is outside the text", e.Start, e.End)
		}
		c.old = append(c.old, t.Slice(e.Start, e.End))
	}
	if err := r.text.Apply(kernel.Transaction{Base: t.Version(), Origin: "local", Edits: edits}); err != nil {
		return fmt.Errorf("edit: %w", err)
	}
	if r.open > 0 {
		r.pending[len(r.pending)-1] = append(r.pending[len(r.pending)-1], c)
	} else {
		r.pending = append(r.pending, pending{c})
	}
	return nil
}

// Sync takes the server's edits and sends the copy's own. An edit of its
// own whose text another client changed first is dropped, with the rest of
// its node, and Sync returns them. Inside a group it does nothing, as the
// group is not a node yet.
func (r *Replica) Sync() ([]kernel.Edit, error) {
	if r.open > 0 {
		return nil, nil
	}
	dropped, _, err := r.pull()
	if err != nil {
		return dropped, err
	}
	for len(r.pending) > 0 {
		var res struct {
			Applied bool           `json:"applied"`
			Version kernel.Version `json:"version"`
		}
		changes := make([][]edit, len(r.pending[0]))
		for i, c := range r.pending[0] {
			for _, e := range c.edits {
				changes[i] = append(changes[i], edit(e))
			}
		}
		if err := r.c.call("push", map[string]any{"buffer": r.buf, "version": r.confirmed, "changes": changes}, &res); err != nil {
			return dropped, err
		}
		if res.Applied {
			// The server's text is now the copy's after this node.
			r.confirmed, r.pending = res.Version, r.pending[1:]
			continue
		}
		more, _, err := r.pull()
		dropped = append(dropped, more...)
		if err != nil {
			return dropped, err
		}
	}
	return dropped, nil
}

// pull takes the server's edits since the confirmed version, moves the
// pending nodes over them, and returns those it drops and the server's
// edits.
func (r *Replica) pull() ([]kernel.Edit, [][]kernel.Edit, error) {
	// A group is not a node yet, so the pending nodes miss its edits.
	if r.open > 0 {
		return nil, nil, errors.New("cannot take the server's edits inside a group")
	}
	var p pulled
	if err := r.c.call("pull", map[string]any{"buffer": r.buf, "version": r.confirmed}, &p); err != nil {
		return nil, nil, err
	}
	if p.Version == r.confirmed {
		return nil, nil, nil
	}
	// Without the pending nodes, the copy is the server's text at the
	// confirmed version.
	for range r.pending {
		if _, ok := r.text.Undo(); !ok {
			return nil, nil, errors.New("the copy lost its pending edits")
		}
	}
	remote := make([][]kernel.Edit, len(p.Edits))
	for i, l := range p.Edits {
		for _, e := range l {
			remote[i] = append(remote[i], kernel.Edit(e))
		}
	}
	if p.Text != nil {
		// The server no longer has the edits since, so its text replaces
		// the copy's.
		remote = [][]kernel.Edit{{{End: r.text.Snapshot().Len(), Text: *p.Text}}}
	}
	for _, edits := range remote {
		if err := r.text.Apply(kernel.Transaction{Base: r.text.Snapshot().Version(), Origin: "server", Edits: edits}); err != nil {
			return nil, nil, fmt.Errorf("apply the server's edits: %w", err)
		}
	}
	r.confirmed = p.Version
	dropped, err := r.replay()
	return dropped, remote, err
}

// replay applies each pending node again where Map moves its edits, as one
// node, unless the text an edit of it replaced has changed, and returns the
// edits of the nodes it drops.
func (r *Replica) replay() ([]kernel.Edit, error) {
	var kept []pending
	var dropped []kernel.Edit
	for _, p := range r.pending {
		moved, err := r.again(p)
		if err != nil {
			return dropped, err
		}
		if moved == nil {
			for _, c := range p {
				dropped = append(dropped, c.edits...)
			}
			continue
		}
		kept = append(kept, moved)
	}
	r.pending = kept
	return dropped, nil
}

// again applies node p again as one node, or undoes what it applied of it
// and returns nil when the text an edit of it replaced has changed.
func (r *Replica) again(p pending) (pending, error) {
	r.text.BeginGroup("local")
	var moved pending
	for _, c := range p {
		t := r.text.Snapshot()
		next := change{at: t.Version(), old: c.old}
		for i, e := range c.edits {
			start, ok1 := r.text.Map(e.Start, c.at)
			end, ok2 := r.text.Map(e.End, c.at)
			if !ok1 || !ok2 || t.Slice(start, end) != c.old[i] {
				r.text.EndGroup("local")
				if len(moved) > 0 {
					r.text.Undo()
				}
				return nil, nil
			}
			next.edits = append(next.edits, kernel.Edit{Start: start, End: end, Text: e.Text})
		}
		if err := r.text.Apply(kernel.Transaction{Base: t.Version(), Origin: "local", Edits: next.edits}); err != nil {
			r.text.EndGroup("local")
			return nil, fmt.Errorf("apply an edit again: %w", err)
		}
		moved = append(moved, next)
	}
	r.text.EndGroup("local")
	return moved, nil
}
