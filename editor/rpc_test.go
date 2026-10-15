package editor_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/editor"
)

// B70
func TestB70CommandOverSocket(t *testing.T) {
	typed := open(t, "")
	feed(typed, `isay "hi" now<Esc>0f"lci"hello<Esc>`)

	e := open(t, "")
	feed(e, `isay "hi" now<Esc>`)
	// A socket path must be short, so it goes in a directory of its own.
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	stop := e.Serve(filepath.Join(dir, "s"))
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case f := <-e.Queue():
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
	defer c.Close()
	in := bufio.NewScanner(c)
	var reply struct {
		Result struct {
			Version int    `json:"version"`
			Text    string `json:"text"`
		} `json:"result"`
		Error string `json:"error"`
	}
	call := func(line string) {
		t.Helper()
		fmt.Fprintln(c, line)
		if !in.Scan() || json.Unmarshal(in.Bytes(), &reply) != nil || reply.Error != "" {
			t.Fatalf("call %s got %q", line, in.Text())
		}
	}
	call(`{"id":1,"method":"state"}`)
	call(fmt.Sprintf(`{"id":2,"method":"request","params":{"at":5,"base":%d,"command":{"op":"change","target":{"name":"inner_quote"},"text":"hello"}}}`, reply.Result.Version))
	if want := text(typed); reply.Result.Text != want {
		t.Fatalf("the command made %q, the keys %q", reply.Result.Text, want)
	}
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
