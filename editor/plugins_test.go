package editor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/wire"
)

// withPlugin writes a config that loads the test plugin name with caps, and
// starts an editor on a file holding "abc".
func withPlugin(t *testing.T, name string, caps ...string) *editor.Session {
	t.Helper()
	usePlugin(t, name, caps...)
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	return start(t, path)
}

// usePlugin writes a config that loads the test plugin name with caps.
func usePlugin(t *testing.T, name string, caps ...string) {
	t.Helper()
	useTable(t, name, "", caps...)
}

// useTable is usePlugin with more lines of the plugin's table, such as its
// settings.
func useTable(t *testing.T, name, more string, caps ...string) {
	t.Helper()
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", name, name+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, len(caps))
	for i, c := range caps {
		quoted[i] = fmt.Sprintf("%q", c)
	}
	config(t, fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [%s]\n%s", wasm, strings.Join(quoted, ", "), more))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

// settle runs the work queued for the editor loop until done reports true.
func settle(t *testing.T, e *editor.Session, done func() bool) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for !done() {
		select {
		case f := <-e.Queue():
			f()
		case <-time.After(10 * time.Millisecond):
			// done may wait on something other than the loop, such as a log.
		case <-deadline:
			t.Fatal("still waiting after 5s")
		}
	}
}

// next runs the next thing the editor queues, or fails after 5s.
func next(t *testing.T, e *editor.Session) {
	t.Helper()
	select {
	case f := <-e.Queue():
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("nothing queued after 5s")
	}
}

// fudeLog returns fude's log, or "" before anything is logged.
func fudeLog() string {
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fude", "log"))
	return string(b)
}

// B57
func TestB57Events(t *testing.T) {
	e := withPlugin(t, "listen", "buffer.read", "ui")
	e.SetSize(80, 10)
	e.Load()
	select {
	case f := <-e.Queue():
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not load in 5s")
	}
	// Five edits made before the loop runs again come as one event, about
	// the text after all of them.
	feed(e, "Ahello<Esc>")
	settle(t, e, func() bool { d := decorations(e); return len(d) == 1 && d[0] == "abchello keyword" })
	// Any later event would come within a moment.
	for quiet := time.After(200 * time.Millisecond); ; {
		select {
		case f := <-e.Queue():
			f()
			continue
		case <-quiet:
		}
		break
	}
	if n := strings.Count(fudeLog(), "listen: buffer.changed"); n != 1 {
		t.Fatalf("the plugin heard %d events, want one for five edits", n)
	}
	// It hears an event it emits under its own name.
	feed(e, "gZ")
	settle(t, e, func() bool { return strings.Contains(fudeLog(), "listen: listen.ping") })
	// While it handles an event, its key says it is busy instead of waiting.
	// The loop hands it the event, and its log shows it began. It then loops
	// until its budget runs out.
	feed(e, "A!<Esc>")
	next(t, e)
	for deadline := time.Now().Add(5 * time.Second); strings.Count(fudeLog(), "listen: buffer.changed") < 2; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the plugin never began the event")
		}
	}
	if feed(e, "gZ"); !strings.Contains(e.Message(), "busy") {
		t.Fatalf("gZ while the plugin handled an event said %q", e.Message())
	}
}

// B58
func TestB58StatuslineSegments(t *testing.T) {
	e := withPlugin(t, "seg", "ui")
	e.Load()
	settle(t, e, func() bool { return len(e.Views()[0].Segments) > 0 })
	if got := e.Views()[0].Segments; len(got) != 1 || got[0] != "ready" {
		t.Fatalf("the statusline shows %q, want ready", got)
	}
}

// B59
func TestB59PluginFloat(t *testing.T) {
	e := withPlugin(t, "pop", "ui")
	e.Load()
	next(t, e)
	feed(e, "gP")
	f, ok := e.Float()
	if !ok || f.Text != "hi" || len(f.Spans) != 1 || f.Spans[0].Name != "markup.strong" {
		t.Fatalf("gP showed %q with %v, want hi in bold", f.Text, f.Spans)
	}
}

// B47
func TestB47ReadFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "project"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "out.txt"), "secret")
	change(t, filepath.Join(dir, "project", "data.txt"), "hello")
	if err := os.Symlink(filepath.Join(dir, "out.txt"), filepath.Join(dir, "project", "link")); err != nil {
		t.Fatal(err)
	}
	usePlugin(t, "fsread", "fs.read")
	t.Chdir(filepath.Join(dir, "project"))
	e := start(t, "data.txt")
	e.Load()
	next(t, e)
	if log := fudeLog(); !strings.Contains(log, "fsread: hello") || !strings.Contains(log, "fsread: refused") || !strings.Contains(log, "fsread: unlinked") || strings.Contains(log, "secret") {
		t.Fatalf("the plugin logged:\n%s", log)
	}
}

// B48
func TestB48RunPrograms(t *testing.T) {
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "proc", "proc.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	config(t, fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"process\"]\nprograms = [\"echo\", \"sleep\"]\n", wasm))
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	e := start(t, "")
	e.Load()
	began := time.Now()
	next(t, e)
	if took := time.Since(began); took > 2*time.Second || !strings.Contains(e.Message(), "budget") {
		t.Fatalf("sleep 5 ran %v and the plugin said %q, want its budget to stop it", took, e.Message())
	}
	if log := fudeLog(); !strings.Contains(log, "proc: hi\n") || !strings.Contains(log, "proc: refused") {
		t.Fatalf("the plugin logged:\n%s", log)
	}
	// A program that leaves a child holding its output ends with it.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "sleep"), []byte("#!/bin/sh\n/bin/sleep 5 &\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	e = start(t, "")
	e.Load()
	began = time.Now()
	next(t, e)
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("a program whose child kept its output ran %v, want it to end with the program", took)
	}
}

// B49
func TestB49HTTPGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/away":
			http.Redirect(w, r, "http://example.invalid/", http.StatusFound)
			return
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
			return
		}
		fmt.Fprint(w, "pong")
	}))
	defer srv.Close()
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "net", "net.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	config(t, fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"buffer.read\", \"net\"]\nhosts = [\"127.0.0.1\"]\n", wasm))
	for url, want := range map[string]string{srv.URL: "net: pong", srv.URL + "/away": "redirect to example.invalid", srv.URL + "/loop": "stopped after 10 redirects"} {
		t.Setenv("XDG_STATE_HOME", t.TempDir())
		path := filepath.Join(t.TempDir(), "url")
		change(t, path, url)
		e := start(t, path)
		e.Load()
		next(t, e)
		if log := fudeLog(); !strings.Contains(log, want) || !strings.Contains(log, "net: refused") {
			t.Fatalf("getting %s, the plugin logged:\n%s", url, log)
		}
	}
}

// B50
func TestB50Capabilities(t *testing.T) {
	e := withPlugin(t, "write")
	e.Load()
	settle(t, e, func() bool { return fudeLog() != "" })
	if text(e) != "abc" {
		t.Errorf("a denied edit left %q", text(e))
	}
	if want := "write: edit needs buffer.write, which it did not declare"; !strings.Contains(fudeLog(), want) {
		t.Errorf("the log has %q, want %q", fudeLog(), want)
	}

	e = withPlugin(t, "write", "buffer.write")
	e.Load()
	settle(t, e, func() bool { return text(e) != "abc" })
	if text(e) != "hiabc" {
		t.Errorf("a granted edit left %q, want hiabc", text(e))
	}
}

// B51, B56
func TestB51PluginOverBudget(t *testing.T) {
	e := withPlugin(t, "spin")
	at := time.Now()
	e.Load()
	if d := time.Since(at); d > 50*time.Millisecond {
		t.Errorf("Load took %v, want it to return before the plugin loads", d)
	}
	feed(e, "ix<Esc>")
	if text(e) != "xabc" {
		t.Errorf("typing while the plugin loads left %q", text(e))
	}
	settle(t, e, func() bool { return e.Message() != "" })
	if want := "plugin spin ran past its budget, so it was unloaded"; e.Message() != want {
		t.Errorf("said %q, want %q", e.Message(), want)
	}
	feed(e, "x")
	if text(e) != "abc" {
		t.Errorf("editing after the unload left %q", text(e))
	}
	// A start section that never returns runs under the budget too.
	wasm := filepath.Join(t.TempDir(), "loop.wasm")
	module := "\x00asm\x01\x00\x00\x00" + // header
		"\x01\x04\x01\x60\x00\x00" + // a func type of no params or results
		"\x03\x02\x01\x00" + // one func of it
		"\x08\x01\x00" + // which is the start function
		"\x0a\x09\x01\x07\x00\x03\x40\x0c\x00\x0b\x0b" // loop br 0 end end
	if err := os.WriteFile(wasm, []byte(module), 0o644); err != nil {
		t.Fatal(err)
	}
	config(t, fmt.Sprintf("[[plugin]]\npath = %q\n", wasm))
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	e = start(t, path)
	e.Load()
	settle(t, e, func() bool { return e.Message() != "" })
	if !strings.Contains(e.Message(), "plugin loop ran past its budget") {
		t.Errorf("a start section that loops said %q, want its budget to stop it", e.Message())
	}
}

// B53
func TestB53PluginTraps(t *testing.T) {
	e := withPlugin(t, "trap", "buffer.write")
	e.Load()
	settle(t, e, func() bool { return e.Message() != "" })
	if !strings.HasPrefix(e.Message(), "plugin trap: ") || !strings.HasSuffix(e.Message(), ", so it was unloaded") {
		t.Errorf("said %q, want it to name the plugin and the unload", e.Message())
	}
	if text(e) != "abc" {
		t.Errorf("a trap left %q, want no change", text(e))
	}
	// A module that exports no alloc or memory does not load.
	wasm := filepath.Join(t.TempDir(), "bare.wasm")
	if err := os.WriteFile(wasm, []byte("\x00asm\x01\x00\x00\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	config(t, fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"buffer.read\"]\n", wasm))
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	e = start(t, path)
	e.Load()
	settle(t, e, func() bool { return e.Message() != "" })
	if !strings.Contains(e.Message(), "plugin bare exports no alloc or no memory") {
		t.Errorf("a bare module said %q, want that it exports no alloc", e.Message())
	}
}

// withKit starts an editor on "ab cd\nef" with the kit plugin loaded.
func withKit(t *testing.T) *editor.Session {
	t.Helper()
	return loaded(t, "kit", "f", "ab cd\nef\n", "buffer.read", "buffer.write")
}

// loaded starts an editor on the file named file holding text, with the
// test plugin name loaded with caps.
func loaded(t *testing.T, name, file, text string, caps ...string) *editor.Session {
	t.Helper()
	usePlugin(t, name, caps...)
	return opened(t, file, text)
}

// opened starts an editor on the file named file holding text, and waits
// for the plugin config.toml lists to load.
func opened(t *testing.T, file, text string) *editor.Session {
	t.Helper()
	path := filepath.Join(t.TempDir(), file)
	change(t, path, text)
	e := start(t, path)
	e.Load()
	select {
	case f := <-e.Queue():
		f()
	case <-time.After(5 * time.Second):
		t.Fatal("the plugin did not load in 5s")
	}
	return e
}

// B52, named in a client's command
func TestB52PluginNamesInCommands(t *testing.T) {
	e := withKit(t)
	if err := e.Request(editor.Request{Command: editor.Command{Op: "kit.gs", Target: editor.Target{Name: "kit.aL"}}}); err != nil || text(e) != "AB CD\nEF" {
		t.Fatalf("kit.gs on kit.aL made %q: %v", text(e), err)
	}
}

// B52
func TestB52PluginOperatorsAndObjects(t *testing.T) {
	for _, c := range [][2]string{
		{"gsiw", "AB cd\nef"},
		{"gss", "AB CD\nef"},
		{"gsaL", "AB CD\nEF"},
		{"daL", ""},
		{"gsiww.", "AB CD\nef"},
		{"vegs", "AB cd\nef"},
	} {
		e := withKit(t)
		if feed(e, c[0]); text(e) != c[1] {
			t.Errorf("%s left %q, want %q", c[0], text(e), c[1])
		}
	}
}

// B54, and B173 as kit is written with the sdk
func TestB54PluginCommandsAndKeys(t *testing.T) {
	for _, c := range [][2]string{
		{":Shout<CR>", "!ab cd\nef"},
		{"gS", "!ab cd\nef"},
		{"gS.", "!!ab cd\nef"},
		{"qagSq@a", "!!ab cd\nef"},
		{"qa:Shout<CR>q@a", "!!ab cd\nef"},
		{"gSu", "ab cd\nef"},
	} {
		e := withKit(t)
		if feed(e, c[0]); text(e) != c[1] {
			t.Errorf("%s left %q, want %q", c[0], text(e), c[1])
		}
	}
	// Help says what a plugin's command does, as the plugin describes it.
	e := withKit(t)
	if feed(e, ":h :Shout<CR>"); !strings.Contains(text(e), "`:Shout`\n  types ! at the cursor") {
		t.Error("help does not describe :Shout")
	}
}

// B175
func TestB175PluginCommandArguments(t *testing.T) {
	for _, c := range [][2]string{
		{":2,3Ctx! x  y<CR>", "x  y|true|2|4|truea\nb\nc"},
		{":Ctx<CR>", "|false|0|0|falsea\nb\nc"},
	} {
		e := loaded(t, "ctx", "f", "a\nb\nc\n", "buffer.write")
		if feed(e, c[0]); text(e) != c[1] {
			t.Errorf("%s left %q, want %q", c[0], text(e), c[1])
		}
	}
}

// B176
func TestB176PluginReadsWhereItRuns(t *testing.T) {
	e := loaded(t, "ctx", "f.go", "a\nbc\nd\n", "buffer.read", "buffer.write")
	path := e.Views()[0].Name
	feed(e, "j:Where<CR>")
	if want := "1|" + path + "|go|normal|3|1|5"; !strings.Contains(text(e), want) {
		t.Errorf(":Where typed %q, want %q", text(e), want)
	}
}

// B177
func TestB177PluginInsertKeys(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "buffer.write")
	if feed(e, "i<C-j>z<Esc>"); text(e) != "[insert]za" {
		t.Errorf("i<C-j>z left %q, want [insert]za", text(e))
	}
}

// B178
func TestB178PluginSettings(t *testing.T) {
	useTable(t, "ctx", "settings = { depth = 3, on = true }\n", "buffer.write")
	e := opened(t, "f", "a\n")
	if feed(e, ":Set depth<CR>:Set on<CR>:Set none<CR>"); text(e) != "nonetrue3a" {
		t.Errorf(":Set typed %q, want nonetrue3a", text(e))
	}
	// A list is no setting, so the plugin does not load.
	useTable(t, "ctx", "settings = { depth = [3] }\n", "buffer.write")
	e = start(t, filepath.Join(t.TempDir(), "f"))
	if e.Load(); !strings.Contains(e.Message(), "setting depth is not a number, string or boolean") {
		t.Errorf("a list as a setting said %q", e.Message())
	}
}

// B180
func TestB180PluginTimers(t *testing.T) {
	e := loaded(t, "later", "f", "a\n", "buffer.write")
	feed(e, ":Twice<CR>")
	settle(t, e, func() bool { return text(e) == "tta" })
}

// B181
func TestB181PluginJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "pong") }))
	defer srv.Close()
	useTable(t, "later", "programs = [\"echo\", \"sleep\", \"cat\"]\nhosts = [\"127.0.0.1\"]\n", "buffer.write", "process", "net")
	e := opened(t, "f", "a\n")
	// sleep runs past the budget of a command, which the job does not have.
	feed(e, ":Jobs<CR>:Fetch "+srv.URL+"<CR>")
	settle(t, e, func() bool {
		s := text(e)
		return strings.Contains(s, "hi\n") && strings.Contains(s, "slept") && strings.Contains(s, "pong") && strings.Contains(s, "piped")
	})
}

// B182
func TestB182PluginHearsTheCursor(t *testing.T) {
	e := loaded(t, "watch", "f", "a\nb\nc\n", "ui")
	e.SetSize(80, 10)
	feed(e, "jj")
	settle(t, e, func() bool { s := e.Views()[0].Segments; return len(s) == 1 && s[0] == "4 0" })
}

// B183
func TestB183PluginHearsWhatChanged(t *testing.T) {
	e := loaded(t, "watch", "f", "abc\nde\n", "ui")
	e.SetSize(80, 10)
	// Both changes come before the plugin hears the first, so it hears one
	// range that covers both, in the text after them.
	feed(e, "Axy<Esc>0ihi<Esc>")
	settle(t, e, func() bool { d := decorations(e); return len(d) == 1 && d[0] == "hiabcxy keyword" })
}

// B184
func TestB184PluginSignsAndNotes(t *testing.T) {
	e := loaded(t, "watch", "f", "a\nb\n", "ui")
	e.SetSize(80, 10)
	feed(e, "jix<Esc>")
	settle(t, e, func() bool {
		v := e.Views()[0]
		return v.Signs[1] == wire.Sign{Text: "+", Name: "git.add"} && v.Notes[1] == "changed"
	})
	// A new change moves them, and the line they left shows none.
	feed(e, "kix<Esc>")
	settle(t, e, func() bool {
		v := e.Views()[0]
		return v.Notes[0] == "changed" && v.Notes[1] == "" && v.Signs[1].Text == ""
	})
	// A window on another buffer shows none of them.
	if feed(e, ":vnew<CR>"); len(e.Views()) != 2 || e.Views()[1].Notes[0] != "" || e.Views()[1].Signs[0].Text != "" {
		t.Errorf("a window on a new buffer shows another buffer's notes or signs")
	}
}

// B186
func TestB186PluginWindows(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "ui")
	e.SetSize(80, 20)
	feed(e, ":Show one<CR>:Show two<CR>")
	vs := e.Views()
	i := slices.IndexFunc(vs, func(v wire.View) bool { return v.Name == "[shown]" })
	if len(vs) != 2 || i < 0 || vs[i].Text.Slice(0, vs[i].Text.Len()) != "two" || vs[i].Filetype != "go" {
		t.Fatalf("after two :Show, the windows are %d and [shown] is %d", len(vs), i)
	}
	// It closes without asking to save.
	if feed(e, ":q<CR>"); len(e.Views()) != 1 {
		t.Errorf(":q in [shown] said %q", e.Message())
	}
}

// B187
func TestB187PluginMovesTheCursor(t *testing.T) {
	e := loaded(t, "ctx", "f", "abc\n", "ui")
	if feed(e, ":Go 2<CR>"); e.Cursor() != 2 {
		t.Errorf(":Go 2 left the cursor at %d", e.Cursor())
	}
	// The tests run in editor/, which is the project.
	name := e.Views()[0].Name
	if feed(e, ":Open ../go.mod<CR>"); e.Views()[0].Name != name {
		t.Errorf("a file outside the project opened: %s", e.Views()[0].Name)
	}
	if feed(e, ":Open guide/01-start.md<CR>"); !strings.HasSuffix(e.Views()[0].Name, "guide/01-start.md") || e.Cursor() != 2 {
		t.Errorf(":Open opened %s with the cursor at %d", e.Views()[0].Name, e.Cursor())
	}
}

// B185
func TestB185PluginPicksAndAsks(t *testing.T) {
	e := loaded(t, "later", "f", "a\n", "buffer.write", "ui")
	e.SetSize(80, 20)
	// The list narrows as the file finder's does, off the loop, as frames
	// ask for it.
	feed(e, ":Pick<CR>ban")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return strings.Contains(f.Text, "fruit ban") && strings.Contains(f.Text, "1 / 3")
	})
	feed(e, "<CR>")
	settle(t, e, func() bool { return text(e) == "bananaa" })
	feed(e, ":Pick<CR><Esc>")
	settle(t, e, func() bool { return text(e) == "nonebananaa" })
	feed(e, ":Ask<CR>bob<CR>")
	settle(t, e, func() bool { return text(e) == "bobnonebananaa" })
}

// B188
func TestB188PluginReadsOtherBuffers(t *testing.T) {
	e := loaded(t, "ctx", "f", "ab\n", "buffer.read", "buffer.write")
	other := filepath.Join(t.TempDir(), "g")
	change(t, other, "xy\n")
	if feed(e, ":e "+other+"<CR>:Bufs<CR>"); text(e) != "1=ab|2=xyxy" {
		t.Errorf(":Bufs typed %q, want 1=ab|2=xy", text(e))
	}
}

// B189
func TestB189PluginReadsSyntaxNodes(t *testing.T) {
	e := loaded(t, "ctx", "a.go", "package p\n\nfunc F() {}\n", "buffer.read", "buffer.write")
	e.SetSize(80, 20)
	captures(t, e)
	if feed(e, "3Gw:Node<CR>"); !strings.Contains(text(e), "func identifier|function_declaration|F()") {
		t.Errorf(":Node typed %q, want the kinds of F and its function", text(e))
	}
}

// B190
func TestB190PluginAsksTheServer(t *testing.T) {
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "ctx", "ctx.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	e, log := withServer(t, "a\nb target\n", "utf-8", fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"buffer.write\", \"lsp\"]", wasm))
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "jw")
	// :Hover is not a command until the plugin loads.
	settle(t, e, func() bool { feed(e, ":Hover<CR>"); return e.Message() == "" })
	// The server says where it was asked, which fude put at the cursor.
	settle(t, e, func() bool { return strings.Contains(text(e), "**hover** at 1:2") })
}

// B191
func TestB191PluginReload(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "buffer.write")
	// The config now lists another plugin in place of ctx.
	usePlugin(t, "later", "buffer.write")
	feed(e, ":plugin reload<CR>")
	settle(t, e, func() bool { feed(e, ":Twice<CR>"); return e.Message() == "" })
	settle(t, e, func() bool { return text(e) == "tta" })
	if feed(e, ":Ctx<CR>"); !strings.Contains(e.Message(), "not a command") {
		t.Errorf("after the reload, :Ctx said %q", e.Message())
	}
	// A dropped plugin's operator and object go with it.
	e = withKit(t)
	settle(t, e, func() bool { feed(e, "gsiw"); return text(e) == "AB cd\nef" })
	usePlugin(t, "later", "buffer.write")
	feed(e, ":plugin reload<CR>")
	settle(t, e, func() bool { feed(e, ":Twice<CR>"); return e.Message() == "" })
	for _, keys := range []string{"wgsiw", "yaL"} {
		if feed(e, keys); strings.Contains(e.Message(), "unloaded") {
			t.Errorf("after the reload, %s said %q", keys, e.Message())
		}
	}
}

// decorations returns what the current window shows of plugins'
// decorations, as text and style.
func decorations(e *editor.Session) []string {
	var out []string
	for _, v := range e.Views() {
		for _, d := range v.Decorations {
			out = append(out, v.Text.Slice(d.Start, d.End)+" "+d.Name)
		}
	}
	return out
}

// B55
func TestB55PluginDecorations(t *testing.T) {
	usePlugin(t, "deco", "ui")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	e := start(t, path)
	e.Load()
	settle(t, e, func() bool { return len(decorations(e)) > 0 })
	if got := decorations(e); len(got) != 1 || got[0] != "ab keyword" {
		t.Fatalf("decorations are %q, want ab as keyword", got)
	}
	// One insert before it and one at its end.
	feed(e, "ix<Esc>$iy<Esc>")
	if got := decorations(e); text(e) != "xabyc" || len(got) != 1 || got[0] != "ab keyword" {
		t.Errorf("after edits around it, %q has decorations %q, want ab still", text(e), got)
	}
	feed(e, "gX")
	if got := decorations(e); len(got) != 0 || !strings.HasSuffix(e.Message(), "so it was unloaded") {
		t.Errorf("after the plugin unloaded, decorations are %q and it said %q", got, e.Message())
	}
}

// B246
func TestB246GuestPlugins(t *testing.T) {
	for _, fsRead := range []bool{true, false} {
		t.Run(fmt.Sprintf("fs.read %v", fsRead), func(t *testing.T) {
			caps := []string{"buffer.read", "buffer.write"}
			if fsRead {
				caps = append(caps, "fs.read")
			}
			usePlugin(t, "kit", caps...)
			t.Chdir(t.TempDir())
			change(t, "f", "ab cd\nef\n")
			e := start(t, "f")
			e.Load()
			select {
			case f := <-e.Queue():
				f()
			case <-time.After(5 * time.Second):
				t.Fatal("the plugin did not load in 5s")
			}
			guest := e.Attach("guest", editor.Guest)
			for _, keys := range []string{"gS", ":Shout<CR>", "gsaL"} {
				before := text(e)
				feed(guest, keys)
				if refused := guest.Message() == "a guest may not use plugin kit"; refused != fsRead || refused == (text(e) != before) {
					t.Errorf("%s in a guest said %q and left %q", keys, guest.Message(), text(e))
				}
			}
		})
	}
}
