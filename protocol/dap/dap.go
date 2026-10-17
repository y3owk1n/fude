// Package dap runs debug adapters and talks the Debug Adapter Protocol to
// them, over stdio or a port the adapter listens on (D42). Nothing it does
// waits on an adapter on the editor loop.
package dap

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/protocol/framing"
)

// Client is one running debug adapter.
type Client struct {
	queue chan<- func()
	log   func(string)
	trace func(string)
	name  string
	cmd   *exec.Cmd

	// seq and pending are the editor loop's alone.
	seq     int
	pending map[int]func(json.RawMessage, error)

	mu     sync.Mutex // over outbox
	outbox [][]byte
	wake   chan struct{}
	done   chan struct{} // closed once the process has ended
}

// message is a request, a response or an event, as DAP frames each.
type message struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command,omitempty"`
	Arguments  any             `json:"arguments,omitempty"`
	RequestSeq int             `json:"request_seq,omitempty"`
	Success    *bool           `json:"success,omitempty"` // a response's alone
	Message    string          `json:"message,omitempty"`
	Event      string          `json:"event,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// Start runs command in dir as the adapter called name. A {port} in the
// command becomes a free port, which Start dials until the process ends or
// five seconds pass, and without one the adapter talks on stdio. event gets
// each event and exited the end of the process, both on the editor loop
// through queue. log takes what the adapter writes to stderr, and trace
// each message sent.
func Start(name string, command []string, dir string, queue chan<- func(), log, trace func(string), event func(name string, body json.RawMessage), exited func(error)) (*Client, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("debug adapter %s has no command", name)
	}
	port := ""
	if slices.ContainsFunc(command, func(a string) bool { return strings.Contains(a, "{port}") }) {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, fmt.Errorf("find a port for %s: %w", name, err)
		}
		port = strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
		l.Close()
		command = slices.Clone(command)
		for i, a := range command {
			command[i] = strings.ReplaceAll(a, "{port}", port)
		}
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir, cmd.WaitDelay = dir, time.Second
	stderr := &framing.Lines{Log: log, Name: name}
	cmd.Stderr = stderr
	var stdin io.WriteCloser
	var stdout io.ReadCloser
	var err error
	if port == "" {
		if stdin, err = cmd.StdinPipe(); err == nil {
			stdout, err = cmd.StdoutPipe()
		}
		if err != nil {
			return nil, fmt.Errorf("start %s: %w", name, err)
		}
	} else {
		cmd.Stdout = stderr
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", name, err)
	}
	c := &Client{queue: queue, log: log, trace: trace, name: name, cmd: cmd, pending: map[int]func(json.RawMessage, error){}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		stderr.Flush()
		close(c.done)
		c.queue <- func() {
			for seq, answer := range c.pending {
				delete(c.pending, seq)
				answer(nil, errors.New("the debug adapter ended"))
			}
			exited(err)
		}
	}()
	go func() {
		w, r := io.Writer(stdin), io.Reader(stdout)
		if port != "" {
			conn, err := c.dial(port)
			if err != nil {
				log(fmt.Sprintf("%s: %v", name, err))
				c.Close()
				return
			}
			defer conn.Close()
			w, r = conn, conn
		}
		go c.read(bufio.NewReader(r), event)
		c.write(w)
	}()
	return c, nil
}

// dial connects to the adapter's port, trying until it listens, its process
// ends or five seconds pass.
func (c *Client) dial(port string) (net.Conn, error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.Dial("tcp", "127.0.0.1:"+port)
		if err == nil {
			return conn, nil
		}
		select {
		case <-c.done:
			return nil, fmt.Errorf("ended before it listened on port %s", port)
		case <-time.After(50 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("listened on no port %s in 5s: %w", port, err)
		}
	}
}

// Request sends command with args, and answer gets the response's body, or
// why it failed, on the loop. It must be called on the loop.
func (c *Client) Request(command string, args any, answer func(json.RawMessage, error)) {
	c.seq++
	c.pending[c.seq] = answer
	c.send(message{Seq: c.seq, Type: "request", Command: command, Arguments: args})
}

// Close ends the adapter's process.
func (c *Client) Close() {
	c.cmd.Process.Kill()
}

// send queues m for the writer, and never blocks.
func (c *Client) send(m message) {
	body, err := json.Marshal(m)
	if err != nil {
		c.log(fmt.Sprintf("%s: send %s: %v", c.name, m.Command, err))
		return
	}
	c.trace(fmt.Sprintf("sent %s %s", c.name, cmp.Or(m.Command, m.Type)))
	c.mu.Lock()
	c.outbox = append(c.outbox, body)
	c.mu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// write sends what is queued, in order, until the process ends.
func (c *Client) write(w io.Writer) {
	for {
		c.mu.Lock()
		out := c.outbox
		c.outbox = nil
		c.mu.Unlock()
		for _, body := range out {
			if err := framing.Write(w, body); err != nil {
				select {
				case <-c.done:
				default:
					c.log(fmt.Sprintf("%s: send: %v", c.name, err))
				}
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

// read takes the adapter's messages until it ends. Responses and events go
// to the loop. A request of the adapter's, such as runInTerminal, gets a
// refusal, as fude runs nothing for an adapter.
func (c *Client) read(r *bufio.Reader, event func(string, json.RawMessage)) {
	for {
		body, err := framing.Read(r)
		if err != nil {
			return
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			c.log(fmt.Sprintf("%s: read: %v", c.name, err))
			continue
		}
		var f func()
		switch m.Type {
		case "event":
			f = func() { event(m.Event, m.Body) }
		case "response":
			f = func() { c.answered(m) }
		case "request":
			f = func() {
				c.seq++
				c.send(message{Seq: c.seq, Type: "response", RequestSeq: m.Seq, Command: m.Command, Success: new(bool), Message: "fude does not run " + m.Command})
			}
		default:
			continue
		}
		select {
		case c.queue <- f:
		case <-c.done:
			return
		}
	}
}

// answered gives a response to the request it answers.
func (c *Client) answered(m message) {
	answer, ok := c.pending[m.RequestSeq]
	if !ok {
		return
	}
	delete(c.pending, m.RequestSeq)
	if m.Success == nil || !*m.Success {
		answer(nil, errors.New(cmp.Or(m.Message, m.Command+" failed")))
		return
	}
	answer(m.Body, nil)
}
