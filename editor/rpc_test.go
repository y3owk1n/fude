package editor_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/editor"
)

// client is a connection to an editor's socket, whose loop runs the calls
// and what do hands it.
type client struct {
	t  *testing.T
	c  net.Conn
	in *bufio.Scanner
	do chan func()
}

// dial serves e on a socket and connects to it.
func dial(t *testing.T, e *editor.Session) *client {
	// A socket path must be short, so it goes in a directory of its own.
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Cleanup(e.Serve(filepath.Join(dir, "s")))
	do, done := make(chan func()), make(chan struct{})
	t.Cleanup(func() { close(done) })
	go func() {
		for {
			select {
			case f := <-e.Queue():
				f()
			case f := <-do:
				f()
			case <-done:
				return
			}
		}
	}()
	c, err := net.Dial("unix", filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &client{t, c, bufio.NewScanner(c), do}
}

// result is what a test reads of a reply.
type result struct {
	Version int    `json:"version"`
	Text    string `json:"text"`
}

// call sends line and returns the reply's result, skipping notifications.
func (cl *client) call(line string) result {
	cl.t.Helper()
	fmt.Fprintln(cl.c, line)
	for cl.in.Scan() {
		var reply struct {
			Method string  `json:"method"`
			Result *result `json:"result"`
		}
		if json.Unmarshal(cl.in.Bytes(), &reply) == nil && reply.Method == "" {
			if reply.Result == nil {
				cl.t.Fatalf("call %s got %s", line, cl.in.Text())
			}
			return *reply.Result
		}
	}
	cl.t.Fatalf("call %s got no reply", line)
	return result{}
}

// B70
func TestB70CommandOverSocket(t *testing.T) {
	typed := open(t, "")
	feed(typed, `isay "hi" now<Esc>0f"lci"hello<Esc>`)

	e := open(t, "")
	feed(e, `isay "hi" now<Esc>`)
	cl := dial(t, e)
	base := cl.call(`{"id":1,"method":"state"}`).Version
	got := cl.call(fmt.Sprintf(`{"id":2,"method":"request","params":{"at":5,"base":%d,"command":{"op":"change","target":{"name":"inner_quote"},"text":"hello"}}}`, base))
	if want := text(typed); got.Text != want {
		t.Fatalf("the command made %q, the keys %q", got.Text, want)
	}
}

// B78
func TestB78ClientHearsChanges(t *testing.T) {
	e := open(t, "")
	cl := dial(t, e)
	cl.call(`{"id":1,"method":"state"}`)
	done := make(chan struct{})
	cl.do <- func() { feed(e, "ihi<Esc>"); close(done) }
	<-done
	// The client hears of the edit's two keys, and of the second last.
	cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for cl.in.Scan() {
		if cl.in.Text() == `{"method":"changed","params":{"buffer":1,"version":2}}` {
			return
		}
	}
	t.Fatalf("after another session's edit the client last heard %q", cl.in.Text())
}

// B75
func TestB75RequestAtLeavesCursors(t *testing.T) {
	e := open(t, "")
	feed(e, "ione two three<Esc>0")
	agent := e.Attach("agent")
	feed(agent, "$")
	at := 4
	if err := agent.Request(editor.Request{At: &at, Base: e.Text().Version(), Command: editor.Command{Op: "delete", Target: editor.Target{Name: "word"}}}); err != nil {
		t.Fatal(err)
	}
	if got := text(e); got != "one three" || e.Cursor() != 0 || agent.Cursor() != 8 {
		t.Fatalf("got %q with cursors at %d and %d, want %q with 0 and 8", got, e.Cursor(), agent.Cursor(), "one three")
	}
}

// B76
func TestB76RequestMapsOldPosition(t *testing.T) {
	e := open(t, "")
	feed(e, "ione two<Esc>")
	agent := e.Attach("agent")
	deleteWord := func(at int, base kernel.Version) error {
		return agent.Request(editor.Request{At: &at, Base: base, Command: editor.Command{Op: "delete", Target: editor.Target{Name: "word"}}})
	}
	base := agent.Text().Version()
	feed(e, "0izero <Esc>")
	if err := deleteWord(4, base); err != nil || text(e) != "zero one " {
		t.Fatalf("deleting the word at 4 as it was made %q: %v", text(e), err)
	}
	// An edit since that removed the position refuses the request.
	base = agent.Text().Version()
	feed(e, "0wdw")
	if err := deleteWord(6, base); err == nil || text(e) != "zero " {
		t.Fatalf("a request inside deleted text made %q: %v", text(e), err)
	}
}
