package editor_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/modules/debug"
	"github.com/y3owk1n/fude/protocol/framing"
)

// TestFakeAdapter is a debug adapter when FUDE_FAKE_DAP is set, which tests
// run as a process of its own. It talks on stdio, or on the port -port=
// names. It appends each message it gets to FUDE_FAKE_DAP_LOG. It sends
// initialized on launch and answers launch only after configurationDone,
// as some adapters do, and asks to runInTerminal. The program writes hello
// and stops at each breakpoint in turn, next and the steps go to the line
// after, and continue past the last breakpoint ends it. Its stack is
// main.main over main.caller, whose locals are a = 1 and s, with x = 2 in
// it, and z = 9.
func TestFakeAdapter(t *testing.T) {
	if os.Getenv("FUDE_FAKE_DAP") == "" {
		return
	}
	log, err := os.OpenFile(os.Getenv("FUDE_FAKE_DAP_LOG"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
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
	respond := func(req int, command string, body any) {
		send(map[string]any{"type": "response", "request_seq": req, "command": command, "success": true, "body": body})
	}
	var path string
	var breaks []int
	line, launchSeq := 0, 0
	stop := func(reason string) {
		send(map[string]any{"type": "event", "event": "stopped", "body": map[string]any{"reason": reason, "threadId": 1}})
	}
	br := bufio.NewReader(r)
	for {
		body, err := framing.Read(br)
		if err != nil {
			os.Exit(0)
		}
		fmt.Fprintf(log, "%s\n", body)
		var m struct {
			Seq       int             `json:"seq"`
			Type      string          `json:"type"`
			Command   string          `json:"command"`
			Arguments json.RawMessage `json:"arguments"`
		}
		json.Unmarshal(body, &m)
		if m.Type != "request" {
			continue
		}
		switch m.Command {
		case "initialize":
			respond(m.Seq, m.Command, map[string]any{"supportsConfigurationDoneRequest": true})
		case "launch":
			launchSeq = m.Seq
			send(map[string]any{"type": "request", "command": "runInTerminal", "arguments": map[string]any{"args": []string{"sh"}}})
			send(map[string]any{"type": "event", "event": "initialized"})
		case "setBreakpoints":
			var a struct {
				Source struct {
					Path string `json:"path"`
				} `json:"source"`
				Breakpoints []struct {
					Line int `json:"line"`
				} `json:"breakpoints"`
			}
			json.Unmarshal(m.Arguments, &a)
			path, breaks = a.Source.Path, nil
			var verified []any
			for _, b := range a.Breakpoints {
				breaks = append(breaks, b.Line)
				verified = append(verified, map[string]any{"verified": true, "line": b.Line})
			}
			slices.Sort(breaks)
			respond(m.Seq, m.Command, map[string]any{"breakpoints": verified})
		case "setExceptionBreakpoints":
			respond(m.Seq, m.Command, nil)
		case "configurationDone":
			respond(m.Seq, m.Command, nil)
			respond(launchSeq, "launch", nil)
			send(map[string]any{"type": "event", "event": "output", "body": map[string]any{"category": "stdout", "output": "hello\n"}})
			if len(breaks) > 0 {
				line = breaks[0]
				stop("breakpoint")
			}
		case "stackTrace":
			respond(m.Seq, m.Command, map[string]any{"stackFrames": []any{
				map[string]any{"id": 1, "name": "main.main", "line": line, "column": 1, "source": map[string]any{"path": path}},
				map[string]any{"id": 2, "name": "main.caller", "line": 1, "column": 1, "source": map[string]any{"path": path}},
			}})
		case "scopes":
			var a struct {
				Frame int `json:"frameId"`
			}
			json.Unmarshal(m.Arguments, &a)
			respond(m.Seq, m.Command, map[string]any{"scopes": []any{map[string]any{"name": "Locals", "variablesReference": 10 * a.Frame}}})
		case "variables":
			var a struct {
				Ref int `json:"variablesReference"`
			}
			json.Unmarshal(m.Arguments, &a)
			vars := map[int][]any{
				10: {map[string]any{"name": "a", "value": "1"}, map[string]any{"name": "s", "value": "{...}", "variablesReference": 11}},
				11: {map[string]any{"name": "x", "value": "2"}},
				20: {map[string]any{"name": "z", "value": "9"}},
			}[a.Ref]
			respond(m.Seq, m.Command, map[string]any{"variables": vars})
		case "next", "stepIn", "stepOut":
			respond(m.Seq, m.Command, nil)
			line++
			stop("step")
		case "continue":
			respond(m.Seq, m.Command, nil)
			i := slices.IndexFunc(breaks, func(b int) bool { return b > line })
			if i < 0 {
				send(map[string]any{"type": "event", "event": "terminated"})
				continue
			}
			line = breaks[i]
			stop("breakpoint")
		case "evaluate":
			var a struct {
				Expression string `json:"expression"`
			}
			json.Unmarshal(m.Arguments, &a)
			respond(m.Seq, m.Command, map[string]any{"result": "the value of " + a.Expression})
		case "disconnect":
			respond(m.Seq, m.Command, nil)
			os.Exit(0)
		}
	}
}

// withAdapter starts fude on main.go in a project whose launch settings,
// run, debug it with the fake adapter, on a port when port says, and
// returns the adapter's log.
func withAdapter(t *testing.T, port bool) (*editor.Session, string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	change(t, "main.go", "package main\n\nfunc main() {\n\ta := 1\n\tb := 2\n\t_ = a + b\n}\n")
	if err := os.MkdirAll(".fude", 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(".fude", "launch.toml"), "[launch.run]\nargs = { program = \"{file}\" }\n")
	log := fakeLog(t, "dap.log")
	t.Setenv("FUDE_FAKE_DAP", "1")
	t.Setenv("FUDE_FAKE_DAP_LOG", log)
	command := fmt.Sprintf("[%q, \"-test.run=^TestFakeAdapter$\"]", os.Args[0])
	if port {
		command = fmt.Sprintf("[%q, \"-test.run=^TestFakeAdapter$\", \"--\", \"-port={port}\"]", os.Args[0])
	}
	config(t, "[debug.fake]\ncommand = "+command+"\n[filetype.go]\ndebug = [\"fake\"]\n")
	e := start(t, "main.go")
	e.SetSize(80, 20)
	e.Use(debug.New())
	t.Cleanup(func() { feed(e, ":debug stop<CR>") })
	return e, log
}

// signAt returns the sign the first window shows on line, from 0.
func signAt(e *editor.Session, line int) string {
	return e.Views()[0].Signs[line].Text
}

// B261, B262
func TestB262Breakpoints(t *testing.T) {
	for _, port := range []bool{false, true} {
		t.Run("port "+strconv.FormatBool(port), func(t *testing.T) {
			e, log := withAdapter(t, port)
			// A breakpoint shows hollow until the adapter takes it, and stays on
			// its line as a line goes in above it.
			feed(e, "4G<Space>db6G:debug break<CR>")
			if signAt(e, 3) != "○" || signAt(e, 5) != "○" {
				t.Fatalf("the breakpoints show %q and %q", signAt(e, 3), signAt(e, 5))
			}
			if feed(e, "ggOx<Esc>"); signAt(e, 4) != "○" {
				t.Errorf("after a line went in above, line 5 shows %q", signAt(e, 4))
			}
			feed(e, "u")
			// The command line completes the subcommand, then the launch's name.
			feed(e, ":debug sta<Tab>")
			if e.Line() != ":debug start" {
				t.Errorf(":debug sta<Tab> made %q", e.Line())
			}
			feed(e, " <Tab>")
			settle(t, e, func() bool { return e.Line() == ":debug start run" })
			feed(e, "<Esc>")
			// The program stops at the first, its line lit with → beside it.
			feed(e, ":debug start<CR>")
			settle(t, e, func() bool { return e.Message() == "run stopped: breakpoint" })
			if signAt(e, 3) != "→" || signAt(e, 5) != "●" || e.Text().LineOf(e.Cursor()) != 3 {
				t.Errorf("stopped, the window shows %q and %q with the cursor on line %d", signAt(e, 3), signAt(e, 5), e.Text().LineOf(e.Cursor())+1)
			}
			if e.Views()[0].Grounds[3] != "debug.line" {
				t.Error("the stopped line is not lit")
			}
			// The launch settings' args reach the adapter with {file} made the
			// file, breakpoints go before configurationDone, and the adapter's
			// request to run a terminal is refused.
			got := disk(t, log)
			abs, _ := filepath.Abs("main.go")
			// JSON writes a Windows path's backslashes doubled.
			program, _ := json.Marshal(abs)
			if !strings.Contains(got, `"program":`+string(program)) {
				t.Errorf("the adapter got no program %s in:\n%s", abs, got)
			}
			refused := func(l string) bool {
				return strings.Contains(l, `"type":"response","command":"runInTerminal"`) && strings.Contains(l, `"success":false`)
			}
			if !slices.ContainsFunc(strings.Split(got, "\n"), refused) {
				t.Errorf("the adapter's runInTerminal got no refusal in:\n%s", got)
			}
			if strings.Index(got, `"setBreakpoints"`) > strings.Index(got, `"configurationDone"`) {
				t.Error("the breakpoints went after configurationDone")
			}
			// :debug stop ends the session and takes the light off.
			if feed(e, ":debug stop<CR>"); e.Message() != "stopped debugging run" || signAt(e, 3) != "○" {
				t.Errorf(":debug stop said %q and left %q", e.Message(), signAt(e, 3))
			}
			settle(t, e, func() bool { return strings.Contains(disk(t, log), `"disconnect"`) })
		})
	}
}

// B263
func TestB263Stepping(t *testing.T) {
	e, _ := withAdapter(t, false)
	if feed(e, ":debug next<CR>"); e.Message() != "no debug session runs" {
		t.Errorf(":debug next with no session said %q", e.Message())
	}
	// <Leader>dc starts a session, and the program stops at the breakpoint.
	feed(e, "4G<Space>db6G<Space>db<Space>dc")
	settle(t, e, func() bool { return e.Message() == "run stopped: breakpoint" })
	line := func() int { return e.Text().LineOf(e.Cursor()) + 1 }
	for _, c := range []struct {
		keys, why string
		line      int
	}{
		{"<Space>dn", "run stopped: step", 5},
		{"<Space>dc", "run stopped: breakpoint", 6},
	} {
		feed(e, c.keys)
		settle(t, e, func() bool { return e.Message() == c.why })
		if line() != c.line {
			t.Errorf("%s stopped on line %d, want %d", c.keys, line(), c.line)
		}
	}
	// Going on past the last breakpoint ends the program.
	feed(e, ":debug continue<CR>")
	settle(t, e, func() bool { return e.Message() == "run ended" })
	if feed(e, ":debug next<CR>"); e.Message() != "no debug session runs" {
		t.Errorf(":debug next after the end said %q", e.Message())
	}
}

// B264
func TestB264Panel(t *testing.T) {
	e, _ := withAdapter(t, false)
	feed(e, "4G<Space>db<Space>dc")
	settle(t, e, func() bool { return e.Message() == "run stopped: breakpoint" })
	// The panel shows the frames, the chosen one marked, the first scope
	// open and the program's output.
	feed(e, "<Space>dp")
	panel := func() string {
		for _, v := range e.Views() {
			if v.Name == "[debug]" {
				return v.Text.Slice(0, v.Text.Len())
			}
		}
		return ""
	}
	settle(t, e, func() bool { return strings.Contains(panel(), "a = 1") })
	want := "Frames\n→ main.main  main.go:4\n  main.caller  main.go:1\nVariables\n▾ Locals\n    a = 1\n  ▸ s = {...}\nOutput\n  hello"
	if panel() != want {
		t.Fatalf("the panel shows\n%s\nwant\n%s", panel(), want)
	}
	for _, v := range e.Views() {
		if v.Name == "[debug]" && v.Modified {
			t.Error("the panel shows as modified")
		}
	}
	// <CR> on a variable with members shows them, and again hides them.
	feed(e, "/s = <CR><CR>")
	settle(t, e, func() bool { return strings.Contains(panel(), "x = 2") })
	if feed(e, "<CR>"); strings.Contains(panel(), "x = 2") {
		t.Error("<CR> again left the members showing")
	}
	// <CR> on a frame chooses it, shows its line and its variables.
	feed(e, "gg/caller<CR><CR>")
	settle(t, e, func() bool { return strings.Contains(panel(), "z = 9") })
	if !strings.Contains(panel(), "→ main.caller") {
		t.Errorf("choosing main.caller left the panel\n%s", panel())
	}
	var shown string
	for _, v := range e.Views() {
		if v.Current {
			shown = v.Name
		}
	}
	if shown != "main.go" || e.Text().LineOf(e.Cursor()) != 0 {
		t.Errorf("choosing main.caller shows %s with the cursor on line %d", shown, e.Text().LineOf(e.Cursor())+1)
	}
}

// B265
func TestB265Eval(t *testing.T) {
	e, log := withAdapter(t, false)
	feed(e, "4G<Space>db<Space>dc")
	settle(t, e, func() bool { return e.Message() == "run stopped: breakpoint" })
	// :debug eval puts the value in the message.
	feed(e, ":debug eval a + b<CR>")
	settle(t, e, func() bool { return e.Message() == "a + b = the value of a + b" })
	// <Leader>de shows the value of the name under the cursor, or of the
	// selection, in a float.
	for _, c := range []struct{ keys, want string }{
		{"0w<Space>de", "the value of a"},
		{"0wv$<Space>de", "the value of a := 1"},
	} {
		feed(e, "<Esc>"+c.keys)
		settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, c.want) })
	}
	if got := disk(t, log); !strings.Contains(got, `"context":"repl"`) || !strings.Contains(got, `"context":"hover"`) {
		t.Errorf("the adapter's evaluates were not asked as repl and hover:\n%s", got)
	}
}
