// Package lsp runs language servers and talks to them over stdio. Nothing it
// does waits on a server on the editor loop.
package lsp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/protocol/framing"
)

// Client is one running language server.
type Client struct {
	queue chan<- func()
	stdin io.WriteCloser
	log   func(string)
	trace func(text string, took time.Duration)
	name  string

	// Requests the editor loop sent and the server has yet to answer, which
	// only the loop touches.
	next    int
	pending map[int]pending

	// Messages waiting for the writer, built once the server chose its
	// position encoding.
	mu       sync.Mutex
	outbox   []func(enc string) any
	encoding string
	caps     map[string]json.RawMessage // what the server offers, once it answered
	version  string                     // the server's version, if it gave one
	wake     chan struct{}
	done     chan struct{}
}

// Text is a document as the client reads it. Lines count from 0. core's
// Snapshot satisfies it as is.
type Text interface {
	Len() int
	Slice(start, end int) string
	Lines() int
	LineStart(line int) int
	LineOf(pos int) int
}

// Edit replaces the bytes from Start to End with Text.
type Edit struct {
	Start, End int
	Text       string
}

// Position is a place in a text as the protocol counts it.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is [Start, End) as the protocol counts it.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Start runs command, the server called name, for the files under root,
// and sends options with initialize. Three funcs run on the editor loop,
// through queue. notify gets each notification the server sends. serve gets
// each request, and its result goes back as the answer. exited runs once
// the server exits, with why. Lines the server writes to stderr go to log,
// after its name. trace hears each message sent and received, and how long
// an answer took.
func Start(name string, command []string, root string, options any, queue chan<- func(), log func(string), trace func(text string, took time.Duration), notify func(method string, params json.RawMessage), serve func(method string, params json.RawMessage) any, exited func(error)) (*Client, error) {
	cmd := exec.Command(command[0], command[1:]...)
	// Wait returns once stderr is read to its end, or a moment after the
	// server exits when a child it left keeps it open.
	stderr := &framing.Lines{Log: log, Name: name}
	cmd.Dir, cmd.Stderr, cmd.WaitDelay = root, stderr, time.Second
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	if err := errors.Join(err1, err2); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	c := &Client{
		queue: queue, stdin: stdin, log: log, trace: trace, name: name, pending: map[int]pending{},
		wake: make(chan struct{}, 1), done: make(chan struct{}),
	}
	initialized := make(chan answer, 1)
	go c.write(root, options, initialized, notify)
	go c.read(stdout, initialized, notify, serve)
	go func() {
		err := cmd.Wait()
		stderr.Flush()
		close(c.done)
		c.queue <- func() {
			// The server exited, so no answer to these will come.
			for id, p := range c.pending {
				delete(c.pending, id)
				p.answer(nil)
			}
			exited(err)
		}
	}()
	return c, nil
}

// Close asks the server to shut down and exit.
func (c *Client) Close() {
	c.send(func(string) any { return request{ID: -1, Method: "shutdown"} })
	c.send(func(string) any { return message{Method: "exit"} })
}

// URI is the file URI of path.
func URI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return (&url.URL{Scheme: "file", Path: abs}).String()
}

// Open tells the server about a document and its text.
func (c *Client) Open(uri, language string, t Text) {
	text := t.Slice(0, t.Len())
	c.send(func(string) any {
		return message{Method: "textDocument/didOpen", Params: map[string]any{
			"textDocument": map[string]any{"uri": uri, "languageId": language, "version": 0, "text": text},
		}}
	})
}

// Change tells the server about edits to before, the text ahead of the
// document's change number version. The protocol applies changes in order.
// Sending the last first keeps each range a range of before. A server that
// asks for whole texts gets the text after the edits (B60).
func (c *Client) Change(uri string, version int, before Text, edits []Edit) {
	c.send(func(enc string) any {
		doc := map[string]any{"uri": uri, "version": version}
		if c.wholeText() {
			var after strings.Builder
			at := 0
			for _, e := range edits {
				after.WriteString(before.Slice(at, e.Start))
				after.WriteString(e.Text)
				at = e.End
			}
			after.WriteString(before.Slice(at, before.Len()))
			return message{Method: "textDocument/didChange", Params: map[string]any{
				"textDocument": doc, "contentChanges": []map[string]any{{"text": after.String()}},
			}}
		}
		changes := make([]map[string]any, 0, len(edits))
		for i := len(edits) - 1; i >= 0; i-- {
			e := edits[i]
			changes = append(changes, map[string]any{
				"range": Range{Point(before, e.Start, enc), Point(before, e.End, enc)}, "text": e.Text,
			})
		}
		return message{Method: "textDocument/didChange", Params: map[string]any{
			"textDocument": doc, "contentChanges": changes,
		}}
	})
}

// wholeText reports whether the server syncs documents by their whole text.
// That is kind 1 of textDocumentSync, sent as a number or as the change
// field of an object.
func (c *Client) wholeText() bool {
	raw := c.Capability("textDocumentSync")
	var kind int
	if json.Unmarshal(raw, &kind) != nil {
		var sync struct {
			Change int `json:"change"`
		}
		json.Unmarshal(raw, &sync)
		kind = sync.Change
	}
	return kind == 1
}

// Notify sends the server a notification.
func (c *Client) Notify(method string, params any) {
	c.send(func(string) any { return message{Method: method, Params: params} })
}

// Request sends method with the params build makes in the server's encoding,
// and runs answer on the editor loop with the server's result. An error
// answer goes to the log, and answer gets nil, as it does when the server
// exits first.
func (c *Client) Request(method string, build func(enc string) any, answer func(json.RawMessage)) {
	c.next++
	id := c.next
	c.pending[id] = pending{answer, method, time.Now()}
	c.send(func(enc string) any { return request{ID: id, Method: method, Params: build(enc)} })
}

// Supports reports whether the server offers capability, such as
// documentRangeFormattingProvider.
func (c *Client) Supports(capability string) bool {
	v := c.Capability(capability)
	return v != nil && string(v) != "false" && string(v) != "null"
}

// Capability returns what the server said of capability, or nil.
func (c *Client) Capability(capability string) json.RawMessage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caps[capability]
}

// Version returns the version the server gave, or "".
func (c *Client) Version() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.version
}

// Encoding is the position encoding the server chose, or "" before it
// answered initialize.
func (c *Client) Encoding() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.encoding
}

// Offset returns where p is in t, p being counted in encoding enc.
func Offset(t Text, p Position, enc string) int {
	if p.Line >= t.Lines() {
		return t.Len()
	}
	ls := t.LineStart(p.Line)
	end := t.Len()
	if p.Line+1 < t.Lines() {
		end = t.LineStart(p.Line+1) - 1
	}
	if enc != "utf-16" {
		return min(ls+p.Character, end)
	}
	n := 0
	for i, r := range t.Slice(ls, end) {
		if n >= p.Character {
			return ls + i
		}
		n++
		if r > 0xFFFF {
			n++
		}
	}
	return end
}

// Point returns pos in t as the protocol counts it in encoding enc.
func Point(t Text, pos int, enc string) Position {
	line := t.LineOf(pos)
	ls := t.LineStart(line)
	col := pos - ls
	if enc == "utf-16" {
		col = 0
		for _, r := range t.Slice(ls, pos) {
			col++
			if r > 0xFFFF {
				col++
			}
		}
	}
	return Position{line, col}
}

type message struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type incoming struct {
	ID     json.RawMessage `json:"id"` // a number or a string, from the server
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *responseError  `json:"error"`
}

type responseError struct {
	Message string `json:"message"`
}

// send queues a message for the writer, which builds it once the server
// chose its encoding. It never blocks.
func (c *Client) send(m func(enc string) any) {
	c.mu.Lock()
	c.outbox = append(c.outbox, m)
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// answer is what the server said to initialize.
type answer struct {
	encoding string
	caps     map[string]json.RawMessage
	version  string
}

// write sends initialize, waits for the server's answer, then sends what is
// queued, in order, until the server exits.
func (c *Client) write(root string, options any, initialized <-chan answer, notify func(string, json.RawMessage)) {
	params := map[string]any{
		"processId": nil, "rootUri": URI(root), "initializationOptions": options,
		"workspaceFolders": []map[string]string{{"uri": URI(root), "name": filepath.Base(root)}},
		"capabilities": map[string]any{
			"general":      map[string]any{"positionEncodings": []string{"utf-8", "utf-16"}},
			"textDocument": map[string]any{"completion": map[string]any{"completionItem": map[string]any{"snippetSupport": true}}, "inlayHint": map[string]any{}},
			"window":       map[string]any{"workDoneProgress": true},
			"workspace":    map[string]any{"configuration": true, "didChangeConfiguration": map[string]any{}},
		},
	}
	if c.frame(request{ID: 0, Method: "initialize", Params: params}) != nil {
		return
	}
	var a answer
	select {
	case a = <-initialized:
	case <-c.done:
		return
	}
	enc := a.encoding
	c.mu.Lock()
	c.encoding, c.caps, c.version = enc, a.caps, a.version
	c.mu.Unlock()
	// The editor hears initialized once the server's capabilities are known.
	select {
	case c.queue <- func() { notify("initialized", nil) }:
	case <-c.done:
		return
	}
	if c.frame(message{Method: "initialized", Params: map[string]any{}}) != nil {
		return
	}
	for {
		c.mu.Lock()
		out := c.outbox
		c.outbox = nil
		c.mu.Unlock()
		for _, m := range out {
			if c.frame(m(enc)) != nil {
				return
			}
		}
		select {
		case <-c.wake:
		case <-c.done:
			return
		}
	}
}

// frame writes v as one message with its Content-Length header.
func (c *Client) frame(v any) error {
	what := "an answer"
	switch m := v.(type) {
	case message:
		m.JSONRPC, what = "2.0", m.Method
		v = m
	case request:
		m.JSONRPC, what = "2.0", fmt.Sprintf("%s #%d", m.Method, m.ID)
		v = m
	}
	body, err := json.Marshal(v)
	if err == nil {
		c.trace(fmt.Sprintf("sent %s %s, %d bytes", c.name, what, len(body)), 0)
		err = framing.Write(c.stdin, body)
	}
	select {
	case <-c.done:
		// A send to a server that has exited fails, and is not logged.
	default:
		if err != nil {
			c.log(fmt.Sprintf("%s: send %s: %v", c.name, what, err))
		}
	}
	return err
}

// read takes messages from the server until it exits. The answer to
// initialize goes to the writer, and notifications and requests to the editor
// loop.
func (c *Client) read(r io.Reader, initialized chan<- answer, notify func(string, json.RawMessage), serve func(string, json.RawMessage) any) {
	br := bufio.NewReader(r)
	for {
		body, err := framing.Read(br)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.log(fmt.Sprintf("%s: read: %v", c.name, err))
			}
			return
		}
		var m incoming
		if err := json.Unmarshal(body, &m); err != nil {
			c.trace(fmt.Sprintf("%s sent a message fude cannot read: %v", c.name, err), 0)
			continue
		}
		if m.Method != "" {
			c.trace(fmt.Sprintf("%s sent %s, %d bytes", c.name, m.Method, len(body)), 0)
			method, params := m.Method, m.Params
			f := func() { notify(method, params) }
			if m.ID != nil {
				id := m.ID
				f = func() {
					result := serve(method, params)
					c.send(func(string) any { return map[string]any{"jsonrpc": "2.0", "id": id, "result": result} })
				}
			}
			select {
			case c.queue <- f:
			case <-c.done:
			}
			continue
		}
		// What is left is an answer to one of fude's requests, whose ids are
		// numbers.
		id, err := strconv.Atoi(string(m.ID))
		if err != nil {
			continue
		}
		if id != 0 {
			result, failed := m.Result, m.Error
			select {
			case c.queue <- func() { c.answered(id, result, failed) }:
			case <-c.done:
			}
			continue
		}
		if m.Error != nil {
			// It goes on with no capabilities, as a server that offers none.
			c.log(fmt.Sprintf("%s: initialize: %s", c.name, m.Error.Message))
		}
		var res struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
			ServerInfo   struct{ Version string }   `json:"serverInfo"`
		}
		json.Unmarshal(m.Result, &res)
		// A Go server may give its build info as its version. Its main
		// module's version is then the server's.
		version := res.ServerInfo.Version
		var build struct{ Main struct{ Version string } }
		if json.Unmarshal([]byte(version), &build) == nil {
			version = build.Main.Version
		}
		var enc string
		json.Unmarshal(res.Capabilities["positionEncoding"], &enc)
		// Without an answer the protocol's default holds, which is utf-16.
		if enc != "utf-8" {
			enc = "utf-16"
		}
		c.trace(fmt.Sprintf("%s answered initialize, %d bytes", c.name, len(body)), 0)
		initialized <- answer{enc, res.Capabilities, version}
	}
}

// answered runs the answer waiting for request id.
func (c *Client) answered(id int, result json.RawMessage, failed *responseError) {
	p, ok := c.pending[id]
	delete(c.pending, id)
	if ok {
		c.trace(fmt.Sprintf("%s answered #%d, %d bytes", c.name, id, len(result)), time.Since(p.at))
	}
	if failed != nil {
		c.log(fmt.Sprintf("%s: %s: %s", c.name, p.method, failed.Message))
		result = nil
	}
	if ok {
		p.answer(result)
	}
}

// pending is a request waiting for its answer, and when it was asked.
type pending struct {
	answer func(json.RawMessage)
	method string
	at     time.Time
}
