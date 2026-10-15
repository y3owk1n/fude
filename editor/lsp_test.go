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
// crashes when a change inserts "#". Each "bad" in an opened document gets
// an error diagnostic, a definition is the first "target" in it, and hover
// says where it was asked. Positions are in bytes.
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
	opened := map[string]string{}
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
			Params struct {
				TextDocument struct {
					URI  string `json:"uri"`
					Text string `json:"text"`
				} `json:"textDocument"`
				Position struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"position"`
			} `json:"params"`
		}
		json.Unmarshal(body, &m)
		switch m.Method {
		case "textDocument/definition":
			uri := m.Params.TextDocument.URI
			for line, l := range strings.Split(opened[uri], "\n") {
				if i := strings.Index(l, "target"); i >= 0 {
					r := map[string]any{"start": map[string]int{"line": line, "character": i}, "end": map[string]int{"line": line, "character": i + 6}}
					reply(m.ID, []any{map[string]any{"uri": uri, "range": r}})
					break
				}
			}
		case "textDocument/hover":
			p := m.Params.Position
			reply(m.ID, map[string]any{"contents": map[string]any{"kind": "plaintext", "value": fmt.Sprintf("hover at %d:%d", p.Line, p.Character)}})
		case "textDocument/didOpen":
			opened[m.Params.TextDocument.URI] = m.Params.TextDocument.Text
			var ds []any
			for line, l := range strings.Split(m.Params.TextDocument.Text, "\n") {
				for i := 0; strings.Contains(l[i:], "bad"); i += 3 {
					i += strings.Index(l[i:], "bad")
					r := map[string]any{"start": map[string]int{"line": line, "character": i}, "end": map[string]int{"line": line, "character": i + 3}}
					ds = append(ds, map[string]any{"range": r, "severity": 1, "message": "bad word"})
				}
			}
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": m.Params.TextDocument.URI, "diagnostics": ds}})
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
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
	e.SetSize(80, 20)
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

// B61
func TestB61Diagnostics(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	if got := decorations(e); got[0] != "bad diagnostic.error" || got[1] != "bad diagnostic.error" {
		t.Errorf("decorations are %q, want two errors on bad", got)
	}
	for _, c := range []struct {
		keys string
		at   int
	}{{"gg]d", 3}, {"]d", 9}, {"]d", 3}, {"[d", 9}, {"gg2]d", 9}} {
		if feed(e, c.keys); e.Cursor() != c.at || e.Message() != "bad word" {
			t.Errorf("%s put the cursor at %d saying %q, want %d and the message", c.keys, e.Cursor(), e.Message(), c.at)
		}
	}
	feed(e, "ggOx<Esc>gg]d")
	if e.Cursor() != 5 {
		t.Errorf("after a line above it, ]d went to %d, want 5", e.Cursor())
	}
}

// B62
func TestB62DefinitionAndHover(t *testing.T) {
	e, log := withServer(t, "a\nb target\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "<C-]>")
	settle(t, e, func() bool { return e.Cursor() == 4 })
	feed(e, "<C-o>")
	if e.Cursor() != 0 {
		t.Errorf("<C-o> after <C-]> went to %d, want back to 0", e.Cursor())
	}
	feed(e, "jwK")
	settle(t, e, func() bool { f, _ := e.Float(); return f == "hover at 1:2" })
	if feed(e, "l"); e.Cursor() != 5 {
		t.Errorf("l with hover shown went to %d, want 5", e.Cursor())
	}
	if _, ok := e.Float(); ok {
		t.Error("a key left the hover shown")
	}
	feed(e, "K")
	settle(t, e, func() bool { _, ok := e.Float(); return ok })
	if feed(e, "K"); text(e) != "hover at 1:3" {
		t.Errorf("K on a shown hover showed %q, want it in a window", text(e))
	}
}
