package editor_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
)

// TestFakeServer is a language server when FUDE_FAKE_LSP is set, which tests
// run as a process of its own. It answers initialize with the encoding in
// FUDE_FAKE_ENCODING, appends every message it gets to FUDE_FAKE_LOG, and
// crashes when a change inserts "#".
func TestFakeServer(t *testing.T) {
	if os.Getenv("FUDE_FAKE_LSP") == "" {
		return
	}
	log, err := os.OpenFile(os.Getenv("FUDE_FAKE_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	r := bufio.NewReader(os.Stdin)
	reply := func(id *int, result any) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	for {
		n := 0
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				os.Exit(0)
			}
			if line = strings.TrimSpace(line); line == "" {
				break
			}
			if v, ok := strings.CutPrefix(line, "Content-Length: "); ok {
				n, _ = strconv.Atoi(v)
			}
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			os.Exit(0)
		}
		fmt.Fprintf(log, "%s\n", body)
		var m struct {
			ID     *int   `json:"id"`
			Method string `json:"method"`
		}
		json.Unmarshal(body, &m)
		switch m.Method {
		case "initialize":
			reply(m.ID, map[string]any{"capabilities": map[string]any{"positionEncoding": os.Getenv("FUDE_FAKE_ENCODING")}})
		case "shutdown":
			reply(m.ID, nil)
		case "exit":
			os.Exit(0)
		case "textDocument/didChange":
			if strings.Contains(string(body), `"text":"#"`) {
				os.Exit(3)
			}
		}
	}
}

// withServer starts an editor on a Go file holding text, with the fake
// server configured for Go and answering in encoding, and returns the editor
// and the server's log.
func withServer(t *testing.T, text, encoding string) (*editor.Editor, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "server.log")
	t.Setenv("FUDE_FAKE_LSP", "1")
	t.Setenv("FUDE_FAKE_LOG", log)
	t.Setenv("FUDE_FAKE_ENCODING", encoding)
	config(t, fmt.Sprintf("[filetype.go]\nlsp = [%q, \"-test.run=^TestFakeServer$\"]\n", os.Args[0]))
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, text)
	e := start(t, path)
	t.Cleanup(func() { feed(e, ":qa!<CR>") })
	e.Load()
	return e, log
}

// heard waits until the server's log has want at least times times.
func heard(t *testing.T, log, want string, times int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(log); strings.Count(string(b), want) >= times {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(log)
	t.Fatalf("the server got %s fewer than %d times, in:\n%s", want, times, b)
}

// B60
func TestB60ServerAndEncodings(t *testing.T) {
	for enc, col := range map[string]int{"utf-16": 4, "utf-8": 6} {
		e, log := withServer(t, "a😀b\n", enc)
		heard(t, log, `"positionEncodings":["utf-8","utf-16"]`, 1)
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		feed(e, "$a!<Esc>")
		heard(t, log, fmt.Sprintf(`"range":{"start":{"line":0,"character":%d},"end":{"line":0,"character":%d}},"text":"!"`, col, col), 1)
	}
}

// B63
func TestB63ServerCrash(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "i#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "was restarted") })
	// The restarted server opens the document again.
	heard(t, log, `"method":"textDocument/didOpen"`, 2)
	feed(e, "a#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "stays off") })
	if feed(e, "ay<Esc>"); text(e) != "##yx" {
		t.Errorf("editing after the crashes left %q, want ##yx", text(e))
	}
}
