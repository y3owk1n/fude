package editor_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
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
// an error diagnostic, a definition or type definition is the first "target"
// in it, references and implementations are every "target", its symbols are
// f with x inside, rename changes every "target" and puts a line at the top
// of other.go beside it, code actions are an edit that puts "hi " first and
// a command that asks fude to put "cmd " first, signature help is
// f(a int, b string) on b, and hover
// is markdown that says where it was asked. Completion offers apple, apricot
// and banana, and formatting the whole document puts a tab before each line,
// as gopls formats only whole documents. Positions are in bytes.
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
				NewName      string   `json:"newName"`
				Arguments    []string `json:"arguments"`
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
		case "textDocument/definition", "textDocument/typeDefinition", "textDocument/references", "textDocument/implementation":
			uri := m.Params.TextDocument.URI
			var locs []any
			for line, l := range strings.Split(opened[uri], "\n") {
				if i := strings.Index(l, "target"); i >= 0 {
					r := map[string]any{"start": map[string]int{"line": line, "character": i}, "end": map[string]int{"line": line, "character": i + 6}}
					locs = append(locs, map[string]any{"uri": uri, "range": r})
				}
			}
			if strings.HasSuffix(m.Method, "efinition") {
				locs = locs[:1]
			}
			reply(m.ID, locs)
		case "textDocument/rename":
			uri := m.Params.TextDocument.URI
			var edits []any
			for line, l := range strings.Split(opened[uri], "\n") {
				if i := strings.Index(l, "target"); i >= 0 {
					r := map[string]any{"start": map[string]int{"line": line, "character": i}, "end": map[string]int{"line": line, "character": i + 6}}
					edits = append(edits, map[string]any{"range": r, "newText": m.Params.NewName})
				}
			}
			top := map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": map[string]int{"line": 0, "character": 0}}
			other := strings.TrimSuffix(uri, "a.go") + "other.go"
			reply(m.ID, map[string]any{"changes": map[string]any{uri: edits, other: []any{map[string]any{"range": top, "newText": "// renamed\n"}}}})
		case "textDocument/codeAction":
			uri := m.Params.TextDocument.URI
			top := map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": map[string]int{"line": 0, "character": 0}}
			reply(m.ID, []any{
				map[string]any{"title": "Add hi", "edit": map[string]any{"changes": map[string]any{uri: []any{map[string]any{"range": top, "newText": "hi "}}}}},
				map[string]any{"title": "Run cmd", "command": "fake.run", "arguments": []any{uri}},
			})
		case "workspace/executeCommand":
			top := map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": map[string]int{"line": 0, "character": 0}}
			edit := map[string]any{"changes": map[string]any{m.Params.Arguments[0]: []any{map[string]any{"range": top, "newText": "cmd "}}}}
			apply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": "apply-1", "method": "workspace/applyEdit", "params": map[string]any{"edit": edit}})
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(apply), apply)
			reply(m.ID, nil)
		case "textDocument/signatureHelp":
			reply(m.ID, map[string]any{"activeSignature": 0, "activeParameter": 1, "signatures": []any{
				map[string]any{"label": "f(a int, b string)", "parameters": []any{map[string]any{"label": "a int"}, map[string]any{"label": []int{9, 17}}}},
			}})
		case "textDocument/documentSymbol":
			at := func(line int) map[string]any {
				p := map[string]int{"line": line, "character": 0}
				return map[string]any{"start": p, "end": p}
			}
			x := map[string]any{"name": "x", "kind": 13, "range": at(1), "selectionRange": at(1)}
			reply(m.ID, []any{map[string]any{"name": "f", "kind": 12, "range": at(0), "selectionRange": at(0), "children": []any{x}}})
		case "textDocument/formatting":
			var edits []any
			for line := range strings.Split(opened[m.Params.TextDocument.URI], "\n") {
				at := map[string]int{"line": line, "character": 0}
				edits = append(edits, map[string]any{"range": map[string]any{"start": at, "end": at}, "newText": "\t"})
			}
			reply(m.ID, edits)
		case "textDocument/completion":
			reply(m.ID, map[string]any{"items": []any{
				map[string]any{"label": "apple", "detail": "fruit", "documentation": "red fruit"},
				map[string]any{"label": "apricot"},
				map[string]any{"label": "banana"},
			}})
		case "completionItem/resolve":
			reply(m.ID, map[string]any{"label": "apricot", "documentation": map[string]any{"kind": "markdown", "value": "**orange** fruit"}})
		case "textDocument/hover":
			p := m.Params.Position
			value := fmt.Sprintf("```go\nfunc target()\n```\n---\n**hover** at %d:%d", p.Line, p.Character)
			reply(m.ID, map[string]any{"contents": map[string]any{"kind": "markdown", "value": value}})
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
			reply(m.ID, map[string]any{"capabilities": map[string]any{
				"positionEncoding": os.Getenv("FUDE_FAKE_ENCODING"), "documentFormattingProvider": true, "completionProvider": map[string]any{"triggerCharacters": []string{"."}, "resolveProvider": true},
			}})
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
// server configured for Go and answering in encoding, and settings added to
// config.toml, and returns the editor and the server's log.
func withServer(t *testing.T, text, encoding string, settings ...string) (*editor.Session, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "server.log")
	t.Setenv("FUDE_FAKE_LSP", "1")
	t.Setenv("FUDE_FAKE_LOG", log)
	t.Setenv("FUDE_FAKE_ENCODING", encoding)
	config(t, strings.Join(settings, "\n")+fmt.Sprintf("\n[filetype.go]\nlsp = [%q, \"-test.run=^TestFakeServer$\"]\n", os.Args[0]))
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

// B159
func TestB159LspCommand(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	if feed(e, ":lsp<CR>"); !strings.Contains(text(e), "a.go") {
		t.Fatalf(":lsp listed %q", text(e))
	}
	// :lsp restart stops the server and opens the file in a new one.
	feed(e, ":q<CR>:lsp restart<CR>")
	heard(t, log, `"method":"shutdown"`, 1)
	heard(t, log, `"method":"textDocument/didOpen"`, 2)
	// It also starts a server that stayed off after crashing twice.
	feed(e, "i#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "was restarted") })
	feed(e, "a#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "stays off") })
	if feed(e, ":lsp<CR>"); !strings.Contains(text(e), "not running") {
		t.Fatalf(":lsp with the server off listed %q", text(e))
	}
	feed(e, ":q<CR>:lsp restart<CR>")
	heard(t, log, `"method":"textDocument/didOpen"`, 4)
}

// B160
func TestB160LspStopAndDisable(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, ":lsp stop<CR>")
	heard(t, log, `"method":"shutdown"`, 1)
	// A disabled server stays off, even through :lsp restart.
	feed(e, ":lsp disable<CR>:lsp restart<CR>:lsp<CR>")
	if !strings.Contains(text(e), "disable") || strings.Contains(text(e), "a.go") {
		t.Fatalf(":lsp with the server disabled listed %q", text(e))
	}
	feed(e, ":q<CR>:lsp enable<CR>")
	heard(t, log, `"method":"textDocument/didOpen"`, 2)
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

// B119
func TestB119SignColumn(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	if v := e.Views()[0]; v.SignCols != 0 {
		t.Fatalf("before any diagnostics the sign column takes %d columns", v.SignCols)
	}
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	v := e.Views()[0]
	if v.SignCols != 2 || v.Gutter != 2 || len(v.Signs) != 1 || v.Signs[1] != (editor.Sign{Text: "E", Name: "diagnostic.error"}) {
		t.Fatalf("with two errors on line 2 the view has %d sign columns and signs %v", v.SignCols, v.Signs)
	}
}

// B128
func TestB128Icons(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	if feed(e, ":set icons<CR>"); e.Views()[0].Signs[1] != (editor.Sign{Text: "\uf057", Name: "diagnostic.error"}) {
		t.Fatalf("with icons the error sign is %v", e.Views()[0].Signs[1])
	}

	// A listing's icons take two columns at the gutter's right.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "main.go"), "")
	change(t, filepath.Join(dir, "notes"), "")
	feed(e, ":e "+dir+"<CR>")
	v := e.Views()[0]
	want := map[int]editor.Sign{0: {Text: "\U000f07d3", Name: "icon.azure"}, 1: {Text: "\U000f0214", Name: "icon.grey"}, 2: {Text: "\U000f024b", Name: "icon.azure"}}
	if v.Gutter != v.SignCols+2 || !maps.Equal(v.Icons, want) {
		t.Fatalf("the listing has gutter %d and icons %v", v.Gutter, v.Icons)
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
	settle(t, e, func() bool { _, ok := e.Float(); return ok })
	// Hover's markdown shows with its markers gone and its styles as spans.
	f, _ := e.Float()
	float, spans := f.Text, f.Spans
	if want := "func target()\n---\nhover at 1:2"; float != want {
		t.Errorf("hover showed %q, want %q", float, want)
	}
	styled := map[string]string{}
	for _, s := range spans {
		styled[float[s.Start:s.End]] = s.Name
	}
	if styled["func"] != "keyword" || styled["---"] != "markup.rule" || styled["hover"] != "markup.strong" {
		t.Errorf("hover's styles are %v", styled)
	}
	if feed(e, "l"); e.Cursor() != 5 {
		t.Errorf("l with hover shown went to %d, want 5", e.Cursor())
	}
	if _, ok := e.Float(); ok {
		t.Error("a key left the hover shown")
	}
	// K again moves the cursor into the float, where keys move, and q
	// leaves it.
	feed(e, "K")
	settle(t, e, func() bool { _, ok := e.Float(); return ok })
	if feed(e, "Kj"); text(e) != "func target()\n---\nhover at 1:3" || e.Cursor() != 14 || len(e.Views()) != 1 {
		t.Errorf("K, j on a shown hover left %q with the cursor at %d in %d windows", text(e), e.Cursor(), len(e.Views()))
	}
	if feed(e, "q"); e.Cursor() != 5 {
		t.Errorf("q in the float went to %d, want back to 5", e.Cursor())
	}
}

// B64, with what Neovim does given the same completions
func TestB64Completion(t *testing.T) {
	for _, c := range [][2]string{
		{"<Esc>", "x apple"},
		{"<C-n><Esc>", "x apricot"},
		{"<C-n><C-n><Esc>", "x banana"},
		{"<C-n><C-n><C-n><Esc>", "x ap"},
		{"<C-p><Esc>", "x ap"},
		{"<C-n><C-y><Esc>", "x apricot"},
		{"<C-e><Esc>", "x ap"},
		{"z<Esc>", "x applez"},
	} {
		e, log := withServer(t, "x ap\n", "utf-8")
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		feed(e, "A<C-x><C-o>")
		settle(t, e, func() bool { return text(e) == "x apple" })
		if feed(e, c[0]); text(e) != c[1] {
			t.Errorf("%s left %q, want %q", c[0], text(e), c[1])
		}
		if feed(e, "u"); text(e) != "x ap" {
			t.Errorf("u after %s left %q, want the insert undone", c[0], text(e))
		}
	}
}

// B67
func TestB67Autocomplete(t *testing.T) {
	e, log := withServer(t, "x \n", "utf-8", "autocomplete = true")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "Aa")
	settle(t, e, func() bool { _, ok := e.Float(); return ok })
	if text(e) != "x a" {
		t.Fatalf("the menu put %q in the text, want nothing", text(e))
	}
	if feed(e, "<C-n>"); text(e) != "x apple" {
		t.Fatalf("<C-n> made %q, want the first item", text(e))
	}
	// A character the server names as a trigger asks too.
	feed(e, ".")
	heard(t, log, `"method":"textDocument/completion"`, 2)
}

// B68
func TestB68CompletionDocs(t *testing.T) {
	e, log := withServer(t, "x ap\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "A<C-x><C-o>")
	settle(t, e, func() bool { f, _ := e.Float(); return f.Doc != "" })
	if f, _ := e.Float(); f.Doc != "fruit\nred fruit" {
		t.Fatalf("apple's documentation is %q, want its detail and documentation", f.Doc)
	}
	// An item without documentation is resolved for it.
	feed(e, "<C-n>")
	settle(t, e, func() bool { f, _ := e.Float(); return f.Doc != "" })
	if f, _ := e.Float(); f.Doc != "orange fruit" {
		t.Fatalf("apricot's documentation is %q, want it resolved", f.Doc)
	}
}

// B65
func TestB65Format(t *testing.T) {
	for _, c := range [][2]string{
		{"gqq", "\ta\nb\nc"},
		{"gqj", "\ta\n\tb\nc"},
		{"jgqG", "a\n\tb\n\tc"},
		{"Vjgq", "\ta\n\tb\nc"},
	} {
		e, log := withServer(t, "a\nb\nc", "utf-8")
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		feed(e, c[0])
		settle(t, e, func() bool { return text(e) != "a\nb\nc" })
		if text(e) != c[1] {
			t.Errorf("%s left %q, want %q", c[0], text(e), c[1])
		}
		if feed(e, "u"); text(e) != "a\nb\nc" {
			t.Errorf("u after %s left %q, want it undone in one step", c[0], text(e))
		}
	}
}

// B136, without format commands
func TestB136FormatOnSaveServer(t *testing.T) {
	e, log := withServer(t, "a\nb", "utf-8", "format_on_save = true")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return !e.Views()[0].Modified && text(e) == "\ta\n\tb" })
	if got := disk(t, e.Views()[0].Name); got != "\ta\n\tb" {
		t.Fatalf(":w wrote %q", got)
	}
}

// B142, for a float the cursor is in
func TestB142FloatKeymap(t *testing.T) {
	e, log := withServer(t, "a\nb target\n", "utf-8", "[keymap.float]\n\"<Esc>\" = \"<Plug>(float.close)\"")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "jwK")
	settle(t, e, func() bool { _, ok := e.Float(); return ok })
	// Hover again moves the cursor into the float, and the float's keymap
	// closes it.
	if feed(e, "K"); !strings.Contains(text(e), "hover at") {
		t.Fatalf("after K the cursor is in %q, want the float", text(e))
	}
	if feed(e, "<Esc>"); text(e) != "a\nb target" {
		t.Fatalf("after <Esc> the cursor is in %q, want the file", text(e))
	}
}

// B147, for the diagnostics the status line counts
func TestB147Diagnostics(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	if v := e.Views()[0]; v.Errors != 2 || v.Warnings != 0 {
		t.Fatalf("the view counts %d errors and %d warnings, want 2 and 0", v.Errors, v.Warnings)
	}
}

// B66, for the keys that list or jump to places, and N3
func TestB66Places(t *testing.T) {
	e, log := withServer(t, "a target\nb target\nc\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	path := e.Views()[0].Name

	feed(e, "grr")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), path) })
	if want := path + ":1:3: a target\n" + path + ":2:3: b target"; text(e) != want {
		t.Errorf("grr listed %q, want %q", text(e), want)
	}
	if feed(e, "j<CR>"); e.Views()[1].Name != "[references]" || text(e) != "a target\nb target\nc" || e.Cursor() != 11 {
		t.Errorf("<CR> on the second reference left %q at %d", text(e), e.Cursor())
	}
	feed(e, "ggjgrt")
	settle(t, e, func() bool { return e.Cursor() == 2 })
	feed(e, "gO")
	settle(t, e, func() bool { return text(e) != "a target\nb target\nc" })
	if want := "1:1: [Function] f\n2:1:   [Variable] x"; text(e) != want {
		t.Errorf("gO listed %q, want %q", text(e), want)
	}
	// A list window has no server, so gr is free there (N3).
	feed(e, "gr")
	if _, waiting := e.Wait(); waiting {
		t.Error("gr waits in a window with no language server")
	}
}

// N3
func TestB66KeysOnlyWhileAttached(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "x\n")
	e := start(t, path)
	feed(e, "gr")
	if _, waiting := e.Wait(); waiting {
		t.Error("gr waits with no language server attached")
	}
}

// B66, for grn
func TestB66Rename(t *testing.T) {
	e, log := withServer(t, "a target\nb target\nc\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	other := filepath.Join(filepath.Dir(e.Views()[0].Name), "other.go")
	change(t, other, "package p\n")

	feed(e, "wgrn")
	if e.Line() != ":rename target" {
		t.Fatalf("grn opened %q, want the command line with the word", e.Line())
	}
	feed(e, "<BS><BS><BS><BS><BS><BS>goal<CR>")
	settle(t, e, func() bool { return text(e) != "a target\nb target\nc" })
	if text(e) != "a goal\nb goal\nc" || e.Message() != "renamed in 2 files" {
		t.Errorf("rename left %q saying %q", text(e), e.Message())
	}
	if disk(t, other) != "package p\n" {
		t.Errorf("rename wrote other.go: %q", disk(t, other))
	}
	if feed(e, ":b other<CR>"); text(e) != "// renamed\npackage p" {
		t.Errorf("other.go's buffer has %q, want the renamed line", text(e))
	}
	if feed(e, ":b a.go<CR>u"); text(e) != "a target\nb target\nc" {
		t.Errorf("u after the rename left %q, want it undone in one step", text(e))
	}
}

// B66, for gra and insert <C-s>
func TestB66ActionsAndSignature(t *testing.T) {
	for _, c := range [][2]string{{"1<CR>", "hi x"}, {"2<CR>", "cmd x"}} {
		e, log := withServer(t, "x\n", "utf-8")
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		feed(e, "gra")
		settle(t, e, func() bool { _, ok := e.Float(); return ok })
		if f, _ := e.Float(); f.Text != "1. Add hi\n2. Run cmd" {
			t.Errorf("gra showed %q", f.Text)
		}
		feed(e, c[0])
		settle(t, e, func() bool { return text(e) != "x" })
		if text(e) != c[1] {
			t.Errorf("gra %s left %q, want %q", c[0], text(e), c[1])
		}
	}

	e, log := withServer(t, "f(1, \n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "A<C-s>")
	settle(t, e, func() bool { _, ok := e.Float(); return ok })
	fl, _ := e.Float()
	f, spans := fl.Text, fl.Spans
	if f != "f(a int, b string)" || len(spans) != 1 || f[spans[0].Start:spans[0].End] != "b string" {
		t.Errorf("<C-s> showed %q with %v, want the signature with b string bold", f, spans)
	}
}
