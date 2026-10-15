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

	"github.com/y3owk1n/fude/api"
)

// Client is one running language server.
type Client struct {
	queue chan<- func()
	stdin io.WriteCloser

	// Messages waiting for the writer, built once the server chose its
	// position encoding.
	mu     sync.Mutex
	outbox []func(enc string) any
	wake   chan struct{}
	done   chan struct{}
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

// Start runs command for the files under root. exited runs on the editor
// loop, through queue, once the server exits, with why. Lines the server
// writes to stderr go to log.
func Start(command []string, root string, queue chan<- func(), log func(string), exited func(error)) (*Client, error) {
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = root
	stdin, err1 := cmd.StdinPipe()
	stdout, err2 := cmd.StdoutPipe()
	stderr, err3 := cmd.StderrPipe()
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}
	c := &Client{queue: queue, stdin: stdin, wake: make(chan struct{}, 1), done: make(chan struct{})}
	initialized := make(chan string, 1)
	go c.write(root, initialized)
	go c.read(stdout, initialized)
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			log(filepath.Base(command[0]) + ": " + s.Text())
		}
	}()
	go func() {
		err := cmd.Wait()
		close(c.done)
		c.queue <- func() { exited(err) }
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
func (c *Client) Open(uri, language string, t api.Text) {
	text := t.Slice(0, t.Len())
	c.send(func(string) any {
		return message{Method: "textDocument/didOpen", Params: map[string]any{
			"textDocument": map[string]any{"uri": uri, "languageId": language, "version": 0, "text": text},
		}}
	})
}

// Change tells the server about edits to before, the text ahead of the
// document's change number version. The protocol applies changes in order.
// Sending the last first keeps each range a range of before.
func (c *Client) Change(uri string, version int, before api.Text, edits []api.Edit) {
	c.send(func(enc string) any {
		changes := make([]map[string]any, 0, len(edits))
		for i := len(edits) - 1; i >= 0; i-- {
			e := edits[i]
			changes = append(changes, map[string]any{
				"range": Range{Point(before, e.Start, enc), Point(before, e.End, enc)}, "text": e.Text,
			})
		}
		return message{Method: "textDocument/didChange", Params: map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": version},
			"contentChanges": changes,
		}}
	})
}

// CloseDocument tells the server a document closed.
func (c *Client) CloseDocument(uri string) {
	c.send(func(string) any {
		return message{Method: "textDocument/didClose", Params: map[string]any{"textDocument": map[string]any{"uri": uri}}}
	})
}

// Point returns pos in t as the protocol counts it in encoding enc.
func Point(t api.Text, pos int, enc string) Position {
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
	ID     *int            `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
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

// write sends initialize, waits for the server's answer, then sends what is
// queued, in order, until the server exits.
func (c *Client) write(root string, initialized <-chan string) {
	params := map[string]any{
		"processId": nil, "rootUri": URI(root),
		"capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-8", "utf-16"}}},
	}
	if c.frame(request{ID: 0, Method: "initialize", Params: params}) != nil {
		return
	}
	var enc string
	select {
	case enc = <-initialized:
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
	switch m := v.(type) {
	case message:
		m.JSONRPC = "2.0"
		v = m
	case request:
		m.JSONRPC = "2.0"
		v = m
	}
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n%s", len(body), body)
	return err
}

// read takes messages from the server until it exits. The answer to
// initialize goes to the writer.
func (c *Client) read(r io.Reader, initialized chan<- string) {
	br := bufio.NewReader(r)
	for {
		body, err := readFrame(br)
		if err != nil {
			return
		}
		var m incoming
		if json.Unmarshal(body, &m) != nil || m.ID == nil || *m.ID != 0 || m.Method != "" {
			continue
		}
		var res struct {
			Capabilities struct {
				PositionEncoding string `json:"positionEncoding"`
			} `json:"capabilities"`
		}
		json.Unmarshal(m.Result, &res)
		// Without an answer the protocol's default holds, which is utf-16.
		enc := res.Capabilities.PositionEncoding
		if enc != "utf-8" {
			enc = "utf-16"
		}
		initialized <- enc
	}
}

// readFrame reads one message body after its headers.
func readFrame(r *bufio.Reader) ([]byte, error) {
	n := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			if n, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, fmt.Errorf("bad Content-Length %q", v)
			}
		}
	}
	if n < 0 {
		return nil, errors.New("a message with no Content-Length")
	}
	body := make([]byte, n)
	_, err := io.ReadFull(r, body)
	return body, err
}
