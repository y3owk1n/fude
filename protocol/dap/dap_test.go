package dap_test

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/protocol/dap"
	"github.com/y3owk1n/fude/protocol/framing"
)

// TestMain runs the test binary as a debug adapter when FUDE_FAKE_DAP is
// set. It talks on stdio, or on the port -port= names. It answers
// evaluate with the expression, sends a stopped event after launch, asks
// to runInTerminal, which the client refuses, and exits on disconnect.
func TestMain(m *testing.M) {
	if os.Getenv("FUDE_FAKE_DAP") != "" {
		adapter()
		return
	}
	os.Exit(m.Run())
}

func adapter() {
	var r io.Reader = os.Stdin
	var w io.Writer = os.Stdout
	for _, a := range os.Args {
		if port, ok := strings.CutPrefix(a, "-port="); ok {
			l, err := net.Listen("tcp", "127.0.0.1:"+port)
			if err != nil {
				os.Exit(3)
			}
			conn, err := l.Accept()
			if err != nil {
				os.Exit(3)
			}
			r, w = conn, conn
		}
	}
	seq := 0
	send := func(m map[string]any) {
		seq++
		m["seq"] = seq
		body, _ := json.Marshal(m)
		framing.Write(w, body)
	}
	in := bufio.NewReader(r)
	for {
		body, err := framing.Read(in)
		if err != nil {
			os.Exit(0)
		}
		var m struct {
			Seq       int    `json:"seq"`
			Type      string `json:"type"`
			Command   string `json:"command"`
			Success   *bool  `json:"success"`
			Arguments struct {
				Expression string `json:"expression"`
			} `json:"arguments"`
		}
		json.Unmarshal(body, &m)
		respond := func(ok bool, b any) {
			send(map[string]any{"type": "response", "request_seq": m.Seq, "command": m.Command, "success": ok, "body": b, "message": "no " + m.Command})
		}
		switch {
		case m.Type == "response" && m.Command == "runInTerminal":
			// The client's refusal comes back as an event, for the test.
			send(map[string]any{"type": "event", "event": "refused", "body": map[string]bool{"success": m.Success != nil && *m.Success}})
		case m.Command == "launch":
			respond(true, nil)
			send(map[string]any{"type": "request", "command": "runInTerminal", "arguments": map[string]any{"args": []string{"sh"}}})
			send(map[string]any{"type": "event", "event": "stopped", "body": map[string]any{"reason": "entry", "threadId": 1}})
		case m.Command == "evaluate":
			respond(true, map[string]string{"result": "the value of " + m.Arguments.Expression})
		case m.Command == "disconnect":
			respond(true, nil)
			os.Exit(0)
		default:
			respond(false, nil)
		}
	}
}

// session is a running fake adapter, and what came back from it.
type session struct {
	c      *dap.Client
	queue  chan func()
	events chan string
	exited chan error
}

// start runs the fake adapter, on a port when port says.
func start(t *testing.T, port bool) *session {
	t.Helper()
	t.Setenv("FUDE_FAKE_DAP", "1")
	command := []string{os.Args[0], "-test.run=^$"}
	if port {
		command = append(command, "-port={port}")
	}
	s := &session{queue: make(chan func(), 16), events: make(chan string, 16), exited: make(chan error, 1)}
	var err error
	s.c, err = dap.Start("fake", command, t.TempDir(), s.queue, func(line string) { t.Log(line) }, func(string, time.Duration) {},
		func(name string, body json.RawMessage) { s.events <- name + " " + string(body) },
		func(err error) { s.exited <- err })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// answer sends command and runs the loop until its answer comes.
func (s *session) answer(t *testing.T, command string, args any) (json.RawMessage, error) {
	t.Helper()
	type reply struct {
		body json.RawMessage
		err  error
	}
	got := make(chan reply, 1)
	s.c.Request(command, args, func(body json.RawMessage, err error) { got <- reply{body, err} })
	for {
		select {
		case f := <-s.queue:
			f()
		case r := <-got:
			return r.body, r.err
		case <-time.After(5 * time.Second):
			t.Fatalf("no answer to %s after 5s", command)
		}
	}
}

// event runs the loop until the next event comes.
func (s *session) event(t *testing.T) string {
	t.Helper()
	for {
		select {
		case f := <-s.queue:
			f()
		case e := <-s.events:
			return e
		case <-time.After(5 * time.Second):
			t.Fatal("no event after 5s")
		}
	}
}

// B261: an adapter talks on stdio, or on a port its command names.
// Requests get their answers, events come through the loop, the client
// refuses a request of the adapter's, and it tells when the process ends.
func TestClient(t *testing.T) {
	for _, port := range []bool{false, true} {
		s := start(t, port)
		if _, err := s.answer(t, "launch", nil); err != nil {
			t.Fatalf("port %v: launch failed: %v", port, err)
		}
		if e := s.event(t); !strings.HasPrefix(e, "stopped ") {
			t.Errorf("port %v: after launch the event is %q, want stopped", port, e)
		}
		if e := s.event(t); e != `refused {"success":false}` {
			t.Errorf("port %v: runInTerminal got %q, want a refusal", port, e)
		}
		body, err := s.answer(t, "evaluate", map[string]string{"expression": "a + b"})
		if err != nil || !strings.Contains(string(body), "the value of a + b") {
			t.Errorf("port %v: evaluate answered %s, %v", port, body, err)
		}
		if _, err := s.answer(t, "unknown", nil); err == nil || err.Error() != "no unknown" {
			t.Errorf("port %v: a failed request said %v, want its message", port, err)
		}
		s.answer(t, "disconnect", nil)
		for done := false; !done; {
			select {
			case f := <-s.queue:
				f()
			case <-s.exited:
				done = true
			case <-time.After(5 * time.Second):
				t.Fatalf("port %v: no exit after disconnect", port)
			}
		}
	}
}
