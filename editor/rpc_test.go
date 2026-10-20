package editor_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/y3owk1n/fude/internal/trace"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"

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

// B70, and B24 for a client's keys
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

// frames reads an attached client's frames, keeping its copy of each
// buffer's text from them, until done reports true of the newest.
func (cl *client) frames(texts map[int]string, done func(wire.Frame) bool) wire.Frame {
	cl.t.Helper()
	for cl.in.Scan() {
		var n struct {
			Method string     `json:"method"`
			Params wire.Frame `json:"params"`
		}
		if json.Unmarshal(cl.in.Bytes(), &n) != nil || n.Method != "frame" {
			continue
		}
		for _, tx := range n.Params.Texts {
			switch {
			case tx.Whole != nil:
				texts[tx.Buffer] = *tx.Whole
			default:
				for _, edits := range tx.Edits {
					t := texts[tx.Buffer]
					for _, ed := range slices.Backward(edits) {
						t = t[:ed.Start] + ed.Text + t[ed.End:]
					}
					texts[tx.Buffer] = t
				}
			}
		}
		if done(n.Params) {
			return n.Params
		}
	}
	cl.t.Fatal("the frames ended")
	return wire.Frame{}
}

// B239, the frames an attached client gets
func TestB239AttachFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one\n")
	e := open(t, path)
	cl := dial(t, e)
	fmt.Fprintln(cl.c, `{"id":1,"method":"attach"}`)
	fmt.Fprintln(cl.c, `{"method":"size","params":[80,24]}`)
	texts := map[int]string{}
	// The first frame brings the whole text, and later ones its edits.
	f := cl.frames(texts, func(f wire.Frame) bool { return len(f.Windows) == 1 && f.Windows[0].H == 24 })
	if b := f.Windows[0].Buffer; texts[b] != "one" {
		t.Fatalf("the first frame gave the text %q", texts[b])
	}
	fmt.Fprintln(cl.c, `{"method":"keys","params":["A","!","<Esc>"]}`)
	f = cl.frames(texts, func(f wire.Frame) bool {
		for _, tx := range f.Texts {
			if tx.Whole != nil {
				t.Errorf("a frame after the first sent the whole text again")
			}
		}
		return texts[f.Windows[0].Buffer] == "one!" && f.Mode == vim.Normal
	})
	cl.do <- func() {}
	if got := text(e); got != "one!" {
		t.Errorf("the attached session's edit left fude's buffer %q", got)
	}
	// :q leaves, and the last frame says the session is done.
	fmt.Fprintln(cl.c, `{"method":"keys","params":[":","q","<CR>"]}`)
	if f = cl.frames(texts, func(f wire.Frame) bool { return f.Done }); f.Message != "" {
		t.Errorf("leaving said %q", f.Message)
	}
	// When fude quits, an attached client's last frame says so.
	e = open(t, path)
	cl = dial(t, e)
	fmt.Fprintln(cl.c, `{"id":1,"method":"attach"}`)
	cl.frames(texts, func(wire.Frame) bool { return true })
	cl.do <- func() { feed(e, ":qa!<CR>") }
	if f = cl.frames(texts, func(f wire.Frame) bool { return f.Done }); f.Message != "fude quit" {
		t.Errorf("when fude quit, the last frame said %q", f.Message)
	}
	// The loop is done with the quit before the test reads the session.
	cl.do <- func() {}
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

// B174
func TestB174Tail(t *testing.T) {
	e := open(t, "")
	cl := dial(t, e)
	fmt.Fprintln(cl.c, `{"id":1,"method":"tail"}`)
	done := make(chan struct{})
	cl.do <- func() { feed(e, "ix<Esc>"); close(done) }
	<-done
	// A key from the client names the client, and a local key names no one.
	fmt.Fprintln(cl.c, `{"id":2,"method":"keys","params":{"keys":"iy<Esc>"}}`)
	want := map[string]bool{`"x" in insert`: true, `client1: "y" in insert`: true}
	cl.c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for len(want) > 0 && cl.in.Scan() {
		var line struct {
			Method string
			Params trace.Event
		}
		if json.Unmarshal(cl.in.Bytes(), &line) == nil && line.Method == "event" && line.Params.Src == "key" {
			delete(want, line.Params.Text)
		}
	}
	if len(want) > 0 {
		t.Fatalf("a client tailing fude never heard of the keys %v, and last heard %q", want, cl.in.Text())
	}
}

// B97
func TestB97SocketSetting(t *testing.T) {
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	// Its folder may not exist yet, as on a new machine.
	path := filepath.Join(dir, "state", "s")
	config(t, fmt.Sprintf("socket = %q\n", path))
	e := start(t)
	defer e.Serve(e.SocketPath())()
	c, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("no socket at the path the setting names: %v", err)
	}
	c.Close()
	// A second fude leaves the first one's socket alone, and says nothing.
	other := start(t)
	defer other.Serve(other.SocketPath())()
	if c, err := net.Dial("unix", path); err != nil || other.Message() != "" {
		t.Fatalf("a second fude said %q, and dialing the first gave %v", other.Message(), err)
	} else {
		c.Close()
	}
}

// B75
func TestB75RequestAtLeavesCursors(t *testing.T) {
	e := open(t, "")
	feed(e, "ione two three<Esc>0")
	agent := e.Attach("agent", editor.Host)
	feed(agent, "$")
	at := 4
	if err := agent.Request(editor.Request{At: &at, Base: e.Text().Version(), Command: editor.Command{Op: "delete", Target: editor.Target{Name: "word"}}}); err != nil {
		t.Fatal(err)
	}
	if got := text(e); got != "one three" || e.Cursor() != 0 || agent.Cursor() != 8 {
		t.Fatalf("got %q with cursors at %d and %d, want %q with 0 and 8", got, e.Cursor(), agent.Cursor(), "one three")
	}
	// A request on another buffer leaves the window's # as it was.
	other := filepath.Join(t.TempDir(), "other")
	change(t, other, "x\n")
	feed(e, ":e "+other+"<CR>:b 1<CR>")
	if err := agent.Request(editor.Request{Buffer: 2, Command: editor.Command{Op: "delete", Target: editor.Target{Name: "word"}}}); err != nil {
		t.Fatal(err)
	}
	if feed(agent, "<C-^>"); agent.Windows()[0].Name == other {
		t.Errorf("after a request on %s, <C-^> went to it", other)
	}
}

// B76
func TestB76RequestMapsOldPosition(t *testing.T) {
	e := open(t, "")
	feed(e, "ione two<Esc>")
	agent := e.Attach("agent", editor.Host)
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

// fails sends line and returns its reply's error, "" for none.
func (cl *client) fails(line string) string {
	cl.t.Helper()
	fmt.Fprintln(cl.c, line)
	for cl.in.Scan() {
		var reply struct{ Method, Error string }
		if json.Unmarshal(cl.in.Bytes(), &reply) == nil && reply.Method == "" {
			return reply.Error
		}
	}
	cl.t.Fatalf("call %s got no reply", line)
	return ""
}

// B352, a client's session call
func TestB352Session(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a")
	change(t, path, "one\n")
	// The terminal's session is the first, its origin user and its name kyle.
	for _, name := range []string{"", "user", "kyle"} {
		e := open(t, "")
		e.SetName("kyle")
		cl := dial(t, e)
		if err := cl.fails(fmt.Sprintf(`{"id":1,"method":"session","params":{"name":%q}}`, name)); err != "" {
			t.Fatalf("session %q failed with %s", name, err)
		}
		cl.call(fmt.Sprintf(`{"id":2,"method":"open","params":{"paths":[%q]}}`, path))
		shown := make(chan string)
		cl.do <- func() { shown <- text(e) }
		if got := <-shown; got != "one" {
			t.Errorf("after session %q and open, the terminal's session shows %q", name, got)
		}
	}
	cl := dial(t, open(t, ""))
	if err := cl.fails(`{"id":1,"method":"session","params":{"name":"nobody"}}`); err != "no session is called nobody" {
		t.Errorf("a session call for no session failed with %q", err)
	}
}
