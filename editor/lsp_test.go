package editor_test

import (
	"bufio"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	buffersmod "github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/colours"
	explorermod "github.com/y3owk1n/fude/modules/explorer"
	"github.com/y3owk1n/fude/modules/format"
	"github.com/y3owk1n/fude/modules/inlay"
	lspmod "github.com/y3owk1n/fude/modules/lsp"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// TestFakeServer is a language server when FUDE_FAKE_LSP is set, which tests
// run as a process of its own. It answers initialize with the encoding in
// FUDE_FAKE_ENCODING, appends every message it gets to FUDE_FAKE_LOG, and
// crashes when a change inserts "#". Each "bad" in an opened document gets
// an error diagnostic, a definition or type definition is the first "target"
// in it and a declaration the last, references and implementations are
// every "target", its symbols are f with x inside, rename changes every "target" and puts a line at the top
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
	fmt.Fprintf(log, "args %q\n", os.Args[1:])
	r := bufio.NewReader(os.Stdin)
	reply := func(id *int, result any) {
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
	}
	// progress reports the work "load" as begun with title, half done, or
	// as ended.
	progress := func(kind, title string) {
		value := map[string]any{"kind": kind}
		if kind == "begin" {
			value["title"], value["percentage"] = title, 50
		}
		body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "$/progress", "params": map[string]any{"token": "load", "value": value}})
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
				Query        string   `json:"query"`
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
		case "textDocument/definition", "textDocument/typeDefinition", "textDocument/references", "textDocument/implementation", "textDocument/declaration":
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
			if m.Method == "textDocument/declaration" {
				locs = locs[len(locs)-1:]
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
				map[string]any{"label": "f(a int, b string)", "documentation": "adds **things**", "parameters": []any{
					map[string]any{"label": "a int"}, map[string]any{"label": []int{9, 17}, "documentation": map[string]any{"kind": "markdown", "value": "the name"}},
				}},
			}})
		case "textDocument/documentColor":
			var found []any
			for line, l := range strings.Split(opened[m.Params.TextDocument.URI], "\n") {
				if i := strings.Index(l, "teal"); i >= 0 {
					r := map[string]any{"start": map[string]int{"line": line, "character": i}, "end": map[string]int{"line": line, "character": i + 4}}
					found = append(found, map[string]any{"range": r, "color": map[string]float64{"red": 0, "green": 0.5, "blue": 0.5, "alpha": 1}})
				}
			}
			reply(m.ID, found)
		case "textDocument/inlayHint":
			// A parameter's name before what each call of f( passes.
			var found []any
			for line, l := range strings.Split(opened[m.Params.TextDocument.URI], "\n") {
				if i := strings.Index(l, "f("); i >= 0 {
					found = append(found, map[string]any{"position": map[string]int{"line": line, "character": i + 2}, "label": []any{map[string]string{"value": "n:"}}, "paddingRight": true})
				}
			}
			reply(m.ID, found)
		case "workspace/symbol":
			// Each word of an opened file that holds the query is a function
			// there.
			var found []any
			for uri, text := range opened {
				for line, l := range strings.Split(text, "\n") {
					for _, w := range strings.Fields(l) {
						if i := strings.Index(l, w); m.Params.Query != "" && strings.Contains(w, m.Params.Query) {
							p := map[string]int{"line": line, "character": i}
							found = append(found, map[string]any{"name": w, "kind": 12, "location": map[string]any{"uri": uri, "range": map[string]any{"start": p, "end": p}}})
						}
					}
				}
			}
			reply(m.ID, found)
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
			// Some servers send the whole text formatted, ending in a line break.
			if os.Getenv("FUDE_FAKE_WHOLE") != "" {
				lines := strings.Split(opened[m.Params.TextDocument.URI], "\n")
				end := map[string]int{"line": len(lines) - 1, "character": len(lines[len(lines)-1])}
				whole := map[string]any{"start": map[string]int{"line": 0, "character": 0}, "end": end}
				var text strings.Builder
				for _, l := range lines {
					text.WriteString("\t" + strings.TrimPrefix(l, "\t") + "\n")
				}
				edits = []any{map[string]any{"range": whole, "newText": text.String()}}
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
					ds = append(ds, map[string]any{"range": r, "severity": 1, "message": "bad word", "source": "fake", "code": 7})
				}
			}
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{"uri": m.Params.TextDocument.URI, "diagnostics": ds}})
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
		case "initialize":
			reply(m.ID, map[string]any{"serverInfo": map[string]any{"name": "fake", "version": "v1"}, "capabilities": map[string]any{
				"positionEncoding": os.Getenv("FUDE_FAKE_ENCODING"), "textDocumentSync": json.RawMessage(cmp.Or(os.Getenv("FUDE_FAKE_SYNC"), "2")), "documentFormattingProvider": true, "completionProvider": map[string]any{"triggerCharacters": []string{"."}, "resolveProvider": true},
				"definitionProvider": true, "typeDefinitionProvider": true, "implementationProvider": true, "referencesProvider": true, "renameProvider": true,
				"codeActionProvider": true, "colorProvider": true, "signatureHelpProvider": map[string]any{"triggerCharacters": []string{"("}, "retriggerCharacters": []string{","}}, "documentSymbolProvider": true, "workspaceSymbolProvider": true, "inlayHintProvider": true, "hoverProvider": true,
				"declarationProvider": true,
			}})
		case "shutdown":
			reply(m.ID, nil)
		case "exit":
			os.Exit(0)
		case "initialized":
			progress("begin", "indexing")
			// fude's answer goes to the log, as everything fude sends does.
			body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "workspace/configuration", "params": map[string]any{"items": []any{map[string]string{"section": "fake.answer"}}}})
			fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(body), body)
		case "textDocument/didChange":
			if strings.Contains(string(body), `"text":"#"`) {
				fmt.Fprint(os.Stderr, "crashed on #")
				os.Exit(3)
			}
			if strings.Contains(string(body), `"text":"~"`) {
				progress("end", "")
			}
		}
	}
}

// withServer starts an editor on a Go file holding text, with the fake
// server configured for Go and answering in encoding, and settings added to
// config.toml, and returns the editor and the server's log.
func withServer(t *testing.T, text, encoding string, settings ...string) (*editor.Session, string) {
	t.Helper()
	return withServerAt(t, filepath.Join(t.TempDir(), "a.go"), text, encoding, settings...)
}

// withServerAt is withServer on the Go file at path. The server is named
// fake, and settings come before any table, so a line such as
// lsp.fake.root = ["go.mod"] adds to its table.
func withServerAt(t *testing.T, path, text, encoding string, settings ...string) (*editor.Session, string) {
	t.Helper()
	log := fakeLog(t, "server.log")
	t.Setenv("FUDE_FAKE_LSP", "1")
	t.Setenv("FUDE_FAKE_LOG", log)
	t.Setenv("FUDE_FAKE_ENCODING", encoding)
	// The last argument tests B167's ~/.
	command := fmt.Sprintf("lsp.fake.command = [%q, \"-test.run=^TestFakeServer$\", \"~/fake\"]\n", os.Args[0])
	config(t, command+strings.Join(settings, "\n")+"\n[filetype.go]\nlsp = [\"fake\"]\n")
	change(t, path, text)
	e := start(t, path)
	e.SetSize(80, 20)
	e.Use(lspmod.New())
	t.Cleanup(func() { feed(e, ":qa!<CR>") })
	e.Load()
	return e, log
}

// fakeLog returns the path of a fake server's log, name, in a folder of its
// own. The server may still be ending when the test does, and Windows will
// not delete a file a process has open, so the log goes first, once the
// server has closed it.
func fakeLog(t *testing.T, name string) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), name)
	t.Cleanup(func() {
		for end := time.Now().Add(editortest.Patience); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
			if err := os.Remove(log); err == nil || errors.Is(err, fs.ErrNotExist) {
				return
			}
		}
	})
	return log
}

// heard waits until the server's log has want at least times times.
func heard(t *testing.T, log, want string, times int) {
	t.Helper()
	deadline := time.Now().Add(editortest.Patience)
	for time.Now().Before(deadline) {
		if b, _ := os.ReadFile(log); strings.Count(string(b), want) >= times {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, _ := os.ReadFile(log)
	t.Fatalf("the server got %s fewer than %d times, in:\n%s", want, times, b)
}

// B209
func TestB209ServerColours(t *testing.T) {
	e, _ := withServer(t, "x teal\n", "utf-8")
	e.Use(colours.New())
	teal := func(at int) func() bool {
		return func() bool {
			return slices.Contains(e.Windows()[0].Colours, wire.Colour{Start: at, End: at + 4, RGB: 0x008080})
		}
	}
	settle(t, e, teal(2))
	// The colour moves with the text, as the server says again.
	feed(e, "Ia<Esc>")
	settle(t, e, teal(3))
}

// B216
func TestB216InlayHints(t *testing.T) {
	e, _ := withServer(t, "x f(1)\n", "utf-8")
	e.Use(inlay.New())
	hint := func(at int) func() bool {
		return func() bool {
			return slices.Contains(e.Windows()[0].Ghosts, wire.Ghost{Pos: at, Text: "n: ", Style: "inlay"})
		}
	}
	settle(t, e, hint(4))
	// The hint moves with the text, as the server says again.
	feed(e, "Ia<Esc>")
	settle(t, e, hint(5))
	// Once the server answers for the text after a change, only its hints
	// show, so those of a deleted line go.
	feed(e, "ox f(2)<Esc>ggdd")
	settle(t, e, func() bool {
		return slices.Equal(e.Windows()[0].Ghosts, []wire.Ghost{{Pos: 4, Text: "n: ", Style: "inlay"}})
	})
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
	// A server that asks for whole texts, by a number or an object, gets
	// the whole text.
	for _, sync := range []string{"1", `{"change":1}`} {
		t.Setenv("FUDE_FAKE_SYNC", sync)
		e, log := withServer(t, "ab\n", "utf-8")
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		feed(e, "$a!<Esc>")
		heard(t, log, `"contentChanges":[{"text":"ab!"}]`, 1)
	}
	// A server that asks to hear of saves hears of each write, with the
	// text when it asks for that, and one that does not hears nothing.
	t.Setenv("FUDE_FAKE_SYNC", `{"change":2,"save":{"includeText":true}}`)
	e, log := withServer(t, "ab\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "x:w<CR>")
	heard(t, log, `"method":"textDocument/didSave","params":{"text":"b",`, 1)
	t.Setenv("FUDE_FAKE_SYNC", "2")
	e, log = withServer(t, "ab\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "x:w<CR>ix<Esc>")
	heard(t, log, `"text":"x"`, 1)
	heard(t, log, `"method":"textDocument/didSave"`, 0)
}

// B63
func TestB63ServerCrash(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "i#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "was restarted") })
	// What it wrote last, with no line break, is in the log.
	if !strings.Contains(fudeLog(), "fake: crashed on #") {
		t.Errorf("the log has %q, want the server's last words", fudeLog())
	}
	// The restarted server opens the document again.
	heard(t, log, `"method":"textDocument/didOpen"`, 2)
	feed(e, "a#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "stays off") })
	if feed(e, "ay<Esc>"); text(e) != "##yx" {
		t.Errorf("editing after the crashes left %q, want ##yx", text(e))
	}
}

// B167
func TestB167ProjectRoot(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o700); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "go.mod"), "module m\n")
	_, log := withServerAt(t, filepath.Join(dir, "pkg", "a.go"), "x\n", "utf-8", `lsp.fake.root = ["go.work", "go.mod"]`)
	heard(t, log, fmt.Sprintf(`"rootUri":%q`, lsp.URI(dir)), 1)
	// A command's ~/ is the home directory.
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	heard(t, log, fmt.Sprintf("%q", filepath.Join(home, "fake")), 1)
}

// B168
func TestB168ServerSettings(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8", "lsp.fake.settings.fake.answer = 42", `lsp.fake.init_options.mode = "test"`)
	heard(t, log, `"initializationOptions":{"mode":"test"}`, 1)
	heard(t, log, `"method":"workspace/didChangeConfiguration","params":{"settings":{"fake":{"answer":42}}}`, 1)
	// The server asks for the section fake.answer, on the editor loop.
	settle(t, e, func() bool {
		b, _ := os.ReadFile(log)
		return strings.Contains(string(b), `"result":[42]`)
	})
}

// B169
func TestB169SeveralServers(t *testing.T) {
	log := fakeLog(t, "server.log")
	t.Setenv("FUDE_FAKE_LSP", "1")
	t.Setenv("FUDE_FAKE_LOG", log)
	t.Setenv("FUDE_FAKE_ENCODING", "utf-8")
	var c strings.Builder
	for _, name := range []string{"fake", "again", "elsewhere", "rootless"} {
		fmt.Fprintf(&c, "lsp.%s.command = [%q, \"-test.run=^TestFakeServer$\"]\n", name, os.Args[0])
	}
	// One is for other files and one needs a root, so neither starts.
	c.WriteString("lsp.elsewhere.files = [\"other/*.go\"]\nlsp.rootless.root = [\"go.work\"]\nlsp.rootless.workspace_required = true\n")
	c.WriteString("[filetype.go]\nlsp = [\"fake\", \"again\", \"elsewhere\", \"rootless\"]\n")
	config(t, c.String())
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "bad\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.Use(lspmod.New())
	t.Cleanup(func() { feed(e, ":qa!<CR>") })
	e.Load()
	// Each server's diagnostics show beside the other's.
	settle(t, e, func() bool { return e.Windows()[0].Errors == 2 })
	if v := e.Windows()[0]; v.Server != "fake, again" {
		t.Errorf("the statusline names %q, want fake, again", v.Server)
	}
}

// B159
func TestB159LspPanel(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8", `keymap.normal.S = "<Plug>(lsp.stop)"`)
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	// The panel's keys act only in the panel.
	if feed(e, "S"); e.Message() != "not in the language servers panel" {
		t.Errorf("<Plug>(lsp.stop) in a file said %q", e.Message())
	}
	panel := func() string {
		_, text := floatAt(e, "middle")
		return text
	}
	feed(e, ":lsp<CR>")
	settle(t, e, func() bool { return strings.Contains(panel(), "v1") && strings.Contains(panel(), "running") })
	if !strings.Contains(panel(), "a.go") {
		t.Fatalf("the panel showed %q", panel())
	}
	// s stops the server under the cursor, and r starts it again.
	feed(e, "s")
	heard(t, log, `"method":"shutdown"`, 1)
	settle(t, e, func() bool { return strings.Contains(panel(), "stopped") })
	feed(e, "r")
	heard(t, log, `"method":"textDocument/didOpen"`, 2)
	if feed(e, "q"); panel() != "" {
		t.Fatalf("q left the panel showing %q", panel())
	}
	// :lsp restart starts a server that stayed off after crashing twice.
	feed(e, "i#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "was restarted") })
	feed(e, "a#<Esc>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "stays off") })
	if feed(e, ":lsp<CR>"); !strings.Contains(panel(), "crashed twice") {
		t.Fatalf("with the server off the panel showed %q", panel())
	}
	// <Tab> after the word completes the servers' names.
	if feed(e, "q:lsp restart <Tab>"); e.Line() != ":lsp restart fake" {
		t.Fatalf(":lsp restart <Tab> made %q", e.Line())
	}
	feed(e, "<CR>")
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
	if _, text := floatAt(e, "middle"); !strings.Contains(text, "disabled") || strings.Contains(text, "a.go") {
		t.Fatalf("with the server disabled the panel showed %q", text)
	}
	feed(e, "q:lsp enable<CR>")
	heard(t, log, `"method":"textDocument/didOpen"`, 2)
}

// B161
func TestB161Progress(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"initialized"`, 1)
	win := func() wire.Window { return e.Windows()[0] }
	settle(t, e, func() bool { return win().Progress == "indexing 50%" })
	if feed(e, "a~<Esc>"); win().Server == "" {
		t.Fatal("the window names no server")
	}
	settle(t, e, func() bool { return win().Progress == "" })
}

// B61
func TestB61Diagnostics(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	if got := decorations(e); got[0] != "bad diagnostic.error" || got[1] != "bad diagnostic.error" {
		t.Errorf("decorations are %q, want two errors on bad", got)
	}
	// The message shows after its line until lsp.diagnostic_notes is off.
	if n := e.Windows()[0].Notes[1]; n != (wire.Note{Text: "bad word", Name: "diagnostic.error"}) {
		t.Errorf("the line with errors has the note %+v", n)
	}
	feed(e, ":set lsp.diagnostic_notes false<CR>")
	settle(t, e, func() bool { return len(e.Windows()[0].Notes) == 0 })
	feed(e, ":set lsp.diagnostic_notes true<CR>")
	settle(t, e, func() bool { return len(e.Windows()[0].Notes) == 1 })
	for _, c := range []struct {
		keys string
		at   int
	}{{"gg]d", 3}, {"]d", 9}, {"]d", 3}, {"[d", 9}, {"gg2]d", 9}, {"G[D", 3}, {"gg]D", 9}} {
		feed(e, c.keys)
		if f, text := floatAt(e, "cursor"); e.Cursor() != c.at || text != "bad word  fake(7)" || f.Decorations[0].Name != "diagnostic.error.text" || e.Message() != "" {
			t.Errorf("%s put the cursor at %d showing %q, want %d and the diagnostic in a float", c.keys, e.Cursor(), text, c.at)
		}
	}
	feed(e, "ggOx<Esc>gg]d")
	if e.Cursor() != 5 {
		t.Errorf("after a line above it, ]d went to %d, want 5", e.Cursor())
	}
	// <C-w>d shows the cursor's line's diagnostics in full where it is, and
	// <C-w> still starts the window keys.
	if feed(e, "<Esc>0<C-w>d"); e.Cursor() != 5 || func() string { _, text := floatAt(e, "cursor"); return text }() != "bad word  fake(7)\nbad word  fake(7)" {
		_, text := floatAt(e, "cursor")
		t.Errorf("<C-w>d at %d showed %q, want both of the line's diagnostics", e.Cursor(), text)
	}
	if feed(e, "k<C-w>d"); e.Message() != "no diagnostics on this line" {
		t.Errorf("<C-w>d on a line with none said %q", e.Message())
	}
	if feed(e, "<C-w>v<C-w>l"); len(e.Windows()) != 2 || !e.Windows()[1].Current {
		t.Errorf("<C-w>v<C-w>l left %d windows, the second current %v", len(e.Windows()), len(e.Windows()) > 1 && e.Windows()[1].Current)
	}
}

// B119
func TestB119SignColumn(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	if v := e.Windows()[0]; v.SignCols != 0 {
		t.Fatalf("before any diagnostics the sign column takes %d columns", v.SignCols)
	}
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	v := e.Windows()[0]
	if v.SignCols != 2 || v.Gutter != 2 || len(v.Signs) != 1 || v.Signs[1] != (wire.Sign{Text: "E", Name: "diagnostic.error"}) {
		t.Fatalf("with two errors on line 2 the window has %d sign columns and signs %v", v.SignCols, v.Signs)
	}
}

// B128
func TestB128Icons(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	if feed(e, ":set icons true<CR>"); e.Windows()[0].Signs[1] != (wire.Sign{Text: "\uf057", Name: "diagnostic.error"}) {
		t.Fatalf("with icons the error sign is %v", e.Windows()[0].Signs[1])
	}

	// An explorer's icons take two columns at the gutter's right.
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "main.go"), "")
	change(t, filepath.Join(dir, "notes"), "")
	e.Use(explorermod.New())
	feed(e, ":e "+dir+"<CR>")
	want := map[int]wire.Sign{0: {Text: "\U000f07d3", Name: "icon.azure"}, 1: {Text: "\U000f0214", Name: "icon.grey"}, 2: {Text: "\U000f024b", Name: "icon.azure"}}
	settle(t, e, func() bool { return maps.Equal(e.Windows()[0].Icons, want) })
	if v := e.Windows()[0]; v.Gutter != v.SignCols+2 {
		t.Fatalf("the explorer has gutter %d", v.Gutter)
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
	settle(t, e, func() bool { _, text := floatAt(e, "cursor"); return text != "" })
	// Hover's markdown shows with its markers gone and its styles as spans.
	f, float := floatAt(e, "cursor")
	if want := "func target()\n---\nhover at 1:2"; float != want {
		t.Errorf("hover showed %q, want %q", float, want)
	}
	styled := map[string]string{}
	for _, s := range f.Decorations {
		styled[float[s.Start:s.End]] = s.Name
	}
	if styled["func"] != "keyword" || styled["---"] != "markup.rule" || styled["hover"] != "markup.strong" {
		t.Errorf("hover's styles are %v", styled)
	}
	if feed(e, "l"); e.Cursor() != 5 {
		t.Errorf("l with hover shown went to %d, want 5", e.Cursor())
	}
	if _, text := floatAt(e, "cursor"); text != "" {
		t.Error("a key left the hover shown")
	}
	// K again moves the cursor into the float, where keys move, and q
	// leaves it.
	feed(e, "K")
	settle(t, e, func() bool { _, text := floatAt(e, "cursor"); return text != "" })
	if feed(e, "Kj"); text(e) != "func target()\n---\nhover at 1:3" || e.Cursor() != 14 || !floated(e) {
		t.Errorf("K, j on a shown hover left %q with the cursor at %d in %d windows", text(e), e.Cursor(), len(e.Windows()))
	}
	if feed(e, "q"); e.Cursor() != 5 {
		t.Errorf("q in the float went to %d, want back to 5", e.Cursor())
	}
}

// B64
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
	settle(t, e, func() bool { _, ok := e.Menu(); return ok })
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
	settle(t, e, func() bool { f, _ := e.Menu(); return f.Doc != "" })
	if f, _ := e.Menu(); f.Doc != "fruit\nred fruit" {
		t.Fatalf("apple's documentation is %q, want its detail and documentation", f.Doc)
	}
	// An item without documentation is resolved for it.
	feed(e, "<C-n>")
	settle(t, e, func() bool { f, _ := e.Menu(); return f.Doc != "" })
	if f, _ := e.Menu(); f.Doc != "orange fruit" {
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
		e.Use(format.New())
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
	// The server hears the file's indent width and whether it indents with
	// spaces.
	e, log := withServer(t, "a\nb\nc", "utf-8", "expand_tab = true\nindent_width = 2")
	e.Use(format.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "gqq")
	// The module asks the server from the loop, so the loop runs meanwhile.
	settle(t, e, func() bool { return strings.Contains(disk(t, log), `"options":{"insertSpaces":true,"tabSize":2}`) })
	// A formatter formats the whole file, and only its changes to the lines
	// apply.
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "x\nx\nx\n")
	config(t, "[filetype.txt]\nformat = [\"y\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n")
	e = start(t, path)
	e.Use(format.New())
	feed(e, "jgqq")
	settle(t, e, func() bool { return text(e) != "x\nx\nx" })
	if text(e) != "x\ny\nx" {
		t.Errorf("gqq on the second line left %q, want only it formatted", text(e))
	}
}

// B136, without format commands
func TestB136FormatOnSaveServer(t *testing.T) {
	e, log := withServer(t, "a\nb", "utf-8", "format_on_save = true")
	e.Use(format.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return !e.Windows()[0].Modified && text(e) == "\ta\n\tb" })
	if got := disk(t, e.Windows()[0].Name); got != "\ta\n\tb" {
		t.Fatalf(":w wrote %q", got)
	}
	// A server that sends the whole text, ending in a line break, adds no
	// line to a file that ends with one, and the cursor on a line it changes
	// stays on it.
	t.Setenv("FUDE_FAKE_WHOLE", "1")
	e, log = withServer(t, "ab\n\tb\nc\n", "utf-8", "format_on_save = true")
	e.Use(format.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "l:w<CR>")
	settle(t, e, func() bool { return !e.Windows()[0].Modified && text(e) != "ab\n\tb\nc" })
	if got := disk(t, e.Windows()[0].Name); got != "\tab\n\tb\n\tc\n" {
		t.Fatalf(":w wrote %q", got)
	}
	if line := e.Text().LineOf(e.Cursor()); line != 0 {
		t.Errorf("after formatting the cursor is on line %d, want 1", line+1)
	}
}

// B364
func TestB364FloatIsAWindow(t *testing.T) {
	e, log := withServer(t, "a\nb target\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "jwK")
	settle(t, e, func() bool { _, text := floatAt(e, "cursor"); return text != "" })
	// In the float, an ex line acts on its text, as in a split,
	// and :q closes only the float.
	if feed(e, "K:set number true<CR>"); !floated(e) || !func() bool { f, _ := floatAt(e, "cursor"); return f.Number }() || e.Windows()[0].Number {
		f, _ := floatAt(e, "cursor")
		t.Fatalf(":set number true in the float left %d windows, floated %v, the float numbered %v and the file %v", len(e.Windows()), floated(e), f.Number, e.Windows()[0].Number)
	}
	if feed(e, ":q<CR>"); floated(e) || text(e) != "a\nb target" || len(e.Windows()) != 1 || e.Cursor() != 4 {
		t.Errorf(":q in the float left %d windows on %q with the cursor at %d", len(e.Windows()), text(e), e.Cursor())
	}
}

// B142, for a float the cursor is in, which takes no typing
func TestB142FloatKeymap(t *testing.T) {
	e, log := withServer(t, "a\nb target\n", "utf-8", "[keymap.float]\n\"<Esc>\" = \"<Plug>(float.close)\"")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "jwK")
	settle(t, e, func() bool { _, text := floatAt(e, "cursor"); return text != "" })
	// Hover again moves the cursor into the float, and the float's keymap
	// closes it.
	if feed(e, "K"); !strings.Contains(text(e), "hover at") {
		t.Fatalf("after K the cursor is in %q, want the float", text(e))
	}
	// A key that would type in the float does nothing, and a command line
	// shows alone while it is typed.
	float := text(e)
	if feed(e, "i"); text(e) != float || e.Mode() != vim.Normal || e.Message() != "this window takes no typing, and q closes it" {
		t.Errorf("i in the float left %q in mode %v saying %q", text(e), e.Mode(), e.Message())
	}
	if feed(e, "/"); e.Line() != "/" {
		t.Fatalf("/ in the float typed %q", e.Line())
	}
	if f, ok := e.Menu(); ok {
		t.Errorf("while / is typed the float shows %q as the line's menu", f.Text)
	}
	feed(e, "<Esc>")
	if feed(e, "<Esc>"); text(e) != "a\nb target" {
		t.Fatalf("after <Esc> the cursor is in %q, want the file", text(e))
	}
}

// B147, for the diagnostics the status line counts
func TestB147Diagnostics(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\n", "utf-8")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	if v := e.Windows()[0]; v.Errors != 2 || v.Warnings != 0 {
		t.Fatalf("the window counts %d errors and %d warnings, want 2 and 0", v.Errors, v.Warnings)
	}
}

// B66, for the keys that list or jump to places, and N3
func TestB66Places(t *testing.T) {
	e, log := withServer(t, "a target\nb target\nc\n", "utf-8")
	withCommands(e)
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	path := e.Windows()[0].Name

	feed(e, "grr")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), path) })
	if want := path + ":1:3: a target\n" + path + ":2:3: b target"; text(e) != want {
		t.Errorf("grr listed %q, want %q", text(e), want)
	}
	if feed(e, "j<CR>"); e.Windows()[1].Name != "[references]" || text(e) != "a target\nb target\nc" || e.Cursor() != 11 {
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

// B194
func TestB194LastList(t *testing.T) {
	e, log := withServer(t, "target\nb target\ntarget\n", "utf-8")
	withCommands(e)
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "grr")
	settle(t, e, func() bool { return len(e.Windows()) == 2 })
	// The list's window closes, and the list stays.
	feed(e, ":cclose<CR>")
	for _, c := range []struct {
		keys string
		want int
	}{
		{":cfirst<CR>", 0}, {"]q", 9}, {":clast<CR>", 16}, {"[q", 9}, {":cc 3<CR>", 16}, {":cprevious<CR>", 9},
	} {
		if feed(e, c.keys); e.Cursor() != c.want {
			t.Errorf("%s left the cursor at %d, want %d", c.keys, e.Cursor(), c.want)
		}
	}
	if !strings.HasPrefix(e.Message(), "(2 of 3)") {
		t.Errorf("going to place 2 said %q", e.Message())
	}
	if feed(e, "]q]q"); e.Message() != "no more places" {
		t.Errorf("going past the last place said %q", e.Message())
	}
	if feed(e, ":copen<CR>"); len(e.Windows()) != 2 || e.Cursor() != e.Text().LineStart(2) {
		t.Errorf(":copen showed %d windows with the cursor at %d", len(e.Windows()), e.Cursor())
	}
}

// B196, B214
func TestB196PickPlaces(t *testing.T) {
	e, _ := withServer(t, "ok\nbad x bad\nfunc needle\n", "utf-16")
	settle(t, e, func() bool { return len(decorations(e)) == 2 })
	rows := func() []string {
		f, _ := e.Menu()
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[2:], func(l string) bool { return l == "" })
	}
	// Diagnostics show under their file, each its line, severity and
	// message, and <CR> goes to the one picked.
	feed(e, ":pick diagnostics<CR>")
	settle(t, e, func() bool { r := rows(); return len(r) == 3 && r[1] == "2  error  bad word" })
	if feed(e, "<C-n><CR>"); e.Cursor() != 9 {
		t.Errorf("the second diagnostic is at %d, want 9", e.Cursor())
	}
	// Symbols come from the server as the query is typed.
	feed(e, ":pick symbols<CR>need")
	settle(t, e, func() bool { r := rows(); return len(r) == 2 && r[1] == "3  needle  Function" })
	if feed(e, "<CR>"); e.Cursor() != 18 {
		t.Errorf("the symbol is at %d, want 18", e.Cursor())
	}
	// Without a server that finds symbols, it says so.
	e = start(t)
	e.Use(lspmod.New())
	if feed(e, ":pick symbols<CR>"); e.Message() != "no language server offers workspace symbols" {
		t.Errorf(":pick symbols without a server said %q", e.Message())
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
	e.Use(buffersmod.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	other := filepath.Join(filepath.Dir(e.Windows()[0].Name), "other.go")
	change(t, other, "package p\n")

	feed(e, "wgrn")
	if e.Line() != ":rename target" {
		t.Fatalf("grn opened %q, want the command line with the word", e.Line())
	}
	feed(e, "<BS><BS><BS><BS><BS><BS>goal<CR>")
	settle(t, e, func() bool { return text(e) != "a target\nb target\nc" })
	if text(e) != "a goal\nb goal\nc" || e.Message() != "rename changed 2 files" {
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
	// Typed, :rename renames the symbol under the cursor, not where grn was.
	feed(e, "jw:rename other<CR>")
	heard(t, log, `"newName":"other","position":{"line":1,"character":2}`, 1)
}

// B66, for gra and insert <C-s>
func TestB66ActionsAndSignature(t *testing.T) {
	for _, c := range [][2]string{{"1", "hi x"}, {"<C-n><CR>", "cmd x"}} {
		e, log := withServer(t, "x\n", "utf-8")
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		feed(e, "gra")
		settle(t, e, func() bool { _, ok := e.Menu(); return ok })
		if f, _ := e.Menu(); f.Text != "1  Add hi\n2  Run cmd" {
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
	settle(t, e, func() bool { _, text := floatAt(e, "above"); return text != "" })
	if s, text := floatAt(e, "above"); !strings.HasPrefix(text, "f(a int, b string)\n") || text[s.Decorations[0].Start:s.Decorations[0].End] != "b string" {
		t.Errorf("<C-s> showed %q with %v, want the signature with b string bold", text, s.Decorations)
	}
}

// B227
func TestB227ActionMenu(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	// The selected action shows what it changes, and the next what it runs.
	feed(e, "gra")
	settle(t, e, func() bool { f, _ := e.Menu(); return f.Doc != "" })
	if f, _ := e.Menu(); !strings.Contains(f.Doc, "-x\n+hi x") {
		t.Errorf("Add hi previews %q", f.Doc)
	}
	if feed(e, "j"); func() string { f, _ := e.Menu(); return f.Doc }() != "runs fake.run" {
		f, _ := e.Menu()
		t.Errorf("Run cmd previews %q", f.Doc)
	}
	// <Esc> closes the menu and changes nothing.
	if feed(e, "<Esc>"); text(e) != "x" {
		t.Errorf("<Esc> left %q", text(e))
	}
	if _, ok := e.Menu(); ok {
		t.Error("the menu still shows after <Esc>")
	}
}

// B203
func TestB203SignatureWhileTyping(t *testing.T) {
	e, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	shown := func() bool { _, text := floatAt(e, "above"); return text != "" }
	feed(e, "Af(")
	settle(t, e, shown)
	s, text := floatAt(e, "above")
	if !strings.Contains(text, "the name") || !strings.Contains(text, "adds things") || text[s.Decorations[0].Start:s.Decorations[0].End] != "b string" {
		t.Errorf("typing ( showed %q with %v, want the signature, b string bold, and both documentations", text, s.Decorations)
	}
	if feed(e, "<C-s>"); shown() {
		t.Error("<C-s> left the signature showing")
	}
	feed(e, "1,")
	settle(t, e, shown)
	if feed(e, "<Esc>"); shown() {
		t.Error("<Esc> left the signature showing")
	}
}
