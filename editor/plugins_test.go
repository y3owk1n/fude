package editor_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	helpmod "github.com/y3owk1n/fude/modules/help"
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

// B272
func TestB272PluginMessage(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "ui")
	if feed(e, ":Say hello there<CR>"); e.Message() != "hello there" {
		t.Errorf(":Say hello there shows %q", e.Message())
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
	if want := "write: Edits needs buffer.write, which it did not declare"; !strings.Contains(fudeLog(), want) {
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
	e.Use(helpmod.New())
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

// B339
func TestB339PluginStreams(t *testing.T) {
	useTable(t, "later", "programs = [\"printf\"]\n", "buffer.write", "process")
	e := opened(t, "f", "a\n")
	feed(e, ":Lines<CR>")
	settle(t, e, func() bool { return text(e) == "end|b|a|a" })
}

// B340
func TestB340PluginReadsWindowsAndSelection(t *testing.T) {
	e := loaded(t, "full", "f", "abc\n", "buffer.read", "buffer.write")
	if feed(e, "l<C-w>v:Where<CR>"); text(e) != "1:2|abc" {
		t.Errorf(":Where typed %q, want 1:2|abc", text(e))
	}
	if feed(e, "u0vlgW"); text(e) != "v0-2true|abc" {
		t.Errorf("gW typed %q, want v0-2true|abc", text(e))
	}
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
		return v.Signs[1] == wire.Sign{Text: "+", Name: "git.add"} && v.Notes[1].Text == "changed"
	})
	// A new change moves them, and the line they left shows none.
	feed(e, "kix<Esc>")
	settle(t, e, func() bool {
		v := e.Views()[0]
		return v.Notes[0].Text == "changed" && v.Notes[1].Text == "" && v.Signs[1].Text == ""
	})
	// A window on another buffer shows none of them.
	if feed(e, ":vnew<CR>"); len(e.Views()) != 2 || e.Views()[1].Notes[0].Text != "" || e.Views()[1].Signs[0].Text != "" {
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

// B273
func TestB273PluginReadsLanguageAndCaptures(t *testing.T) {
	e := loaded(t, "ctx", "a.go", "package p\n", "buffer.read", "buffer.write")
	e.SetSize(80, 20)
	captures(t, e)
	if feed(e, ":Lang<CR>"); !strings.HasPrefix(text(e), "go|keyword|") {
		t.Errorf(":Lang typed %q, want go and the keyword capture", text(e))
	}
}

// B275
func TestB275PluginReadsCommentSyntax(t *testing.T) {
	e := loaded(t, "ctx", "a.go", "package p\n", "buffer.read", "buffer.write")
	if feed(e, ":Lang<CR>"); !strings.Contains(text(e), "|// %s|") {
		t.Errorf(":Lang typed %q, want Go's comment syntax", text(e))
	}
}

// B274
func TestB274PluginReadsOptions(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "buffer.write")
	feed(e, ":Opt wrap<CR>:set wrap false<CR>:Opt wrap<CR>:Opt leader<CR>:Opt filetype<CR>")
	if got := text(e); got != "none|<Space>|false|true|a" {
		t.Errorf("the options typed %q", got)
	}
}

// B276
func TestB276PluginStylesAWindow(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "ui")
	e.SetSize(80, 20)
	if feed(e, ":Styled abc<CR>"); !slices.Contains(decorations(e), "abc diff.plus") {
		t.Errorf(":Styled abc decorated %q, want abc in diff.plus", decorations(e))
	}
}

// B277
func TestB277PluginHearsFocus(t *testing.T) {
	e := loaded(t, "listen", "f", "a\n", "buffer.read", "ui")
	e.Focus()
	settle(t, e, func() bool { return strings.Contains(fudeLog(), "listen: editor.focused") })
}

// idle is a module that does nothing, for modules in tests to build on.
type idle struct{}

func (idle) Name() string                                        { return "idle" }
func (idle) Registered() []api.Registration                      { return nil }
func (idle) Subscribed() []string                                { return nil }
func (idle) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }
func (idle) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

func (idle) Event(context.Context, string, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (idle) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}
func (idle) Work(context.Context, api.Job) (string, error) { return "", nil }
func (idle) Close(context.Context) error                   { return nil }

// hostOnly is a module whose key gX only the host may run (B244).
type hostOnly struct{ idle }

func (hostOnly) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Key, Name: "gX", Help: "does nothing, as the host", ID: 1, Host: true}}
}

// B244, a module's registration marked host-only
func TestB244GuestRefusesAModuleKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\n")
	e := start(t, path)
	e.Use(hostOnly{})
	guest := e.Attach("guest", editor.Guest)
	if feed(guest, "gX"); guest.Message() != "a guest may not run gX" {
		t.Errorf("the guest's gX said %q", guest.Message())
	}
	if feed(e, "gX"); e.Message() != "" {
		t.Errorf("the host's gX said %q", e.Message())
	}
}

// B278
func TestB278PluginPlug(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "buffer.write")
	if feed(e, ":plug ctx.hi<CR>"); text(e) != "hia" {
		t.Errorf(":plug ctx.hi left %q, want hia", text(e))
	}
}

// B279
func TestB279PluginReadsConfig(t *testing.T) {
	useTable(t, "ctx", "\n[filetype.txt]\nformat = [\"y\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n", "buffer.write")
	e := opened(t, "f.txt", "a\n")
	feed(e, ":Cfg filetype.txt.format<CR>:Cfg formatter.y.command<CR>:Cfg filetype.txt.format_on_save<CR>:Cfg leader<CR>:Cfg nothing.here<CR>")
	if got := text(e); got != `none|"<Space>"|none|["sed","s/x/y/"]|["y"]|a` {
		t.Errorf("the config values typed %q", got)
	}
}

// B280
func TestB280PluginHearsIndentWidths(t *testing.T) {
	useTable(t, "ctx", "\n[filetype.txt]\nindent_width = 2\n", "buffer.write")
	e := opened(t, "f.txt", "a\n")
	if feed(e, ":Indent<CR>"); text(e) != "2|8|a" {
		t.Errorf(":Indent typed %q, want 2|8|a", text(e))
	}
}

// B283
func TestB283PluginSetsOptions(t *testing.T) {
	e := loaded(t, "ctx", "f", "x\n", "buffer.write")
	feed(e, ":SetOpt expand_tab true<CR>:SetOpt indent_width 3<CR>:SetOpt tab_width 5<CR>>>")
	if text(e) != "   x" {
		t.Errorf(">> after the plugin set the options gave %q, want three spaces", text(e))
	}
	if feed(e, "u:Indent<CR>"); text(e) != "3|5|x" {
		t.Errorf(":Indent typed %q, want 3|5|x", text(e))
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

// B281
func TestB281PluginAsksAboutARange(t *testing.T) {
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "ctx", "ctx.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	e, log := withServer(t, "a\nbc\n", "utf-8", fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"buffer.write\", \"lsp\"]", wasm))
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "jl")
	settle(t, e, func() bool { feed(e, ":Range<CR>"); return e.Message() == "" })
	// The server hears the range in its encoding, and the plugin gets its
	// answer.
	heard(t, log, `"range":{"start":{"line":1,"character":1},"end":{"line":1,"character":2}}`, 1)
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "[{") })
}

// asker is a module that asks the server for hover at the cursor on gA and
// shows the answer's encoding in the message area.
type asker struct{ idle }

func (asker) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Key, Name: "gA", Help: "asks the server for hover", ID: 1}}
}

func (asker) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	return api.Result{Requests: []api.Request{{ID: 1, Method: "textDocument/hover", Pos: args[0], End: -1}}}, nil
}

func (asker) Callback(_ context.Context, _ uint32, s api.Scope) (api.Result, error) {
	return api.Result{Message: "encoding " + s.About["encoding"]}, nil
}

// B281, the encoding an answer comes with
func TestB281ModuleHearsTheEncoding(t *testing.T) {
	e, log := withServer(t, "a\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	e.Use(asker{})
	feed(e, "gA")
	settle(t, e, func() bool { return e.Message() == "encoding utf-8" })
}

// saver is a module that puts ! at the start of the text as it is saved.
type saver struct{ idle }

func (saver) Subscribed() []string { return []string{"buffer.saving"} }

func (saver) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{Edits: []api.Edit{{Text: "!"}}, Message: "saved with !"}, nil
}

// slow is a module whose job on saving takes longer than the write waits,
// and then asks for an edit.
type slow struct{ idle }

func (slow) Subscribed() []string { return []string{"buffer.saving"} }

func (slow) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{Jobs: []api.Job{{ID: 1, Argv: []string{"sleep"}}}}, nil
}

func (slow) Work(context.Context, api.Job) (string, error) {
	time.Sleep(300 * time.Millisecond)
	return "", nil
}

func (slow) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{Edits: []api.Edit{{Text: "late"}}}, nil
}

// B282
func TestB282WriteWaitsForModules(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\n")
	e := start(t, path)
	e.Use(saver{})
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "!a\n" })
	if m := e.Message(); !strings.HasSuffix(m, ", saved with !") {
		t.Errorf("the write said %q, want the module's message after its own", m)
	}
	// A module that takes too long leaves the text, which is written anyway,
	// and its late edit is dropped.
	config(t, "format_timeout = 50\n")
	change(t, path, "b\n")
	e = start(t, path)
	e.Use(slow{})
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "written as it was") })
	if disk(t, path) != "b\n" {
		t.Errorf("the file holds %q, want it written as it was", disk(t, path))
	}
	for end := time.After(400 * time.Millisecond); ; {
		select {
		case f := <-e.Queue():
			f()
			continue
		case <-time.After(10 * time.Millisecond):
			continue
		case <-end:
		}
		break
	}
	if text(e) != "b" {
		t.Errorf("the late edit left %q, want it dropped", text(e))
	}
}

// B282, a plugin hears it
func TestB282PluginHearsSaving(t *testing.T) {
	e := loaded(t, "listen", "f", "a\n", "buffer.read", "ui")
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return strings.Contains(fudeLog(), "listen: buffer.saving") && !e.Views()[0].Modified })
}

// B284
func TestB284PluginHearsAServerAttach(t *testing.T) {
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "listen", "listen.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	e, _ := withServer(t, "a\n", "utf-8", fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [\"buffer.read\", \"ui\"]", wasm))
	settle(t, e, func() bool { return strings.Contains(fudeLog(), "listen: lsp.attached") })
}

// letters is a source whose live list is the query with 1 and 2 after it,
// and which says what was picked.
type letters struct{ idle }

func (letters) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Source, Name: "letters", Help: "lists the query with a digit after it", ID: 1}}
}

func (letters) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	return api.Result{Pick: &api.Pick{ID: 7, Title: "letters", Hint: "type a letter", Query: s.About["args"], Live: true}}, nil
}

func (letters) Callback(_ context.Context, _ uint32, s api.Scope) (api.Result, error) {
	if q, live := s.About["query"]; live {
		return api.Result{Pick: &api.Pick{ID: 7, Items: []string{q + "1", q + "2"}}}, nil
	}
	return api.Result{Message: "picked " + s.About["item"] + " " + s.About["ok"]}, nil
}

// B285, B287
func TestB285LivePick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "x\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.Use(letters{})
	feed(e, ":pick letters a<CR>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return strings.Contains(f.Text, "a1") && strings.Contains(f.Text, "a2")
	})
	feed(e, "b")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "ab2") })
	feed(e, "<C-n><CR>")
	settle(t, e, func() bool { return e.Message() == "picked ab2 true" })
}

// spots is a source whose places are the second line of the file.
type spots struct {
	idle
	path string
}

func (spots) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Source, Name: "spots", Help: "lists a place", ID: 1}}
}

func (m spots) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{Pick: &api.Pick{ID: 9, Title: "spots", Items: []string{m.path + ":2:1:second"}, Places: true}}, nil
}

// B286
func TestB286PlacesPick(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "first\nsecond\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.Use(spots{path: path})
	feed(e, ":pick spots<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "second") })
	feed(e, "<CR>")
	if line := e.Text().LineOf(e.Cursor()); line != 1 || e.Views()[0].Name != path {
		t.Errorf("picking the place put the cursor on line %d of %s, want line 2 of the file", line+1, e.Views()[0].Name)
	}
	// <C-q> lists the places.
	feed(e, ":pick spots<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "second") })
	feed(e, "<C-q>")
	if !slices.ContainsFunc(e.Views(), func(v wire.View) bool { return v.Name == "[spots]" }) {
		t.Errorf("<C-q> opened %v, want a [spots] list", e.Views())
	}
}

// B287, a plugin's source
func TestB287PluginSource(t *testing.T) {
	e := loaded(t, "ctx", "f", "x\n", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":pick letters a<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "a2") })
	feed(e, "<C-n><CR>")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "a2") })
	feed(e, "<Esc>:pick<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "letters") })
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

// decorations is editortest.Decorations, by the short name the tests use.
var decorations = editortest.Decorations

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

// B288
func TestB288PluginFolds(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n b\n c\nd\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Fold 0 2<CR>")
	settle(t, e, func() bool { return maps.Equal(e.Views()[0].Folds, map[int]int{0: 2}) })
	// j passes over the fold, and a search into it opens it.
	if feed(e, "j"); e.Text().LineOf(e.Cursor()) != 3 {
		t.Errorf("j over the fold went to line %d", e.Text().LineOf(e.Cursor()))
	}
	if feed(e, "gg/c<CR>"); e.Views()[0].Folds != nil {
		t.Errorf("searching into the fold left %v", e.Views()[0].Folds)
	}
}

// hider is a module that shows each "# " starting a line as "§" (B295).
type hider struct{ idle }

func (hider) Init(_ context.Context, s api.Scope) (api.Result, error) {
	var res api.Result
	for line := range s.Text.Lines() {
		if p := s.Text.LineStart(line); s.Text.Slice(p, min(p+2, s.Text.Len())) == "# " {
			res.Decorate = append(res.Decorate, api.Decoration{ID: line + 1, Start: p, End: p + 2, Style: "markup.heading", Note: "§", Replace: true})
		}
	}
	return res, nil
}

// B295
func TestB295Replace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	// At 10 columns the line of 15 x's draws on two rows either way, but
	// they break at other places. Each line of 9 x's draws on one row, not
	// two.
	change(t, path, "# a\n# b\nab\n# "+strings.Repeat("x", 15)+"\n"+strings.Repeat("# "+strings.Repeat("x", 9)+"\n", 4))
	e := start(t, path)
	e.SetSize(10, 5)
	e.Use(hider{})
	heading := func(p int) wire.Ghost { return wire.Ghost{Pos: p, End: p + 2, Text: "§", Style: "markup.heading"} }
	// The cursor's line shows its text as it is, and the others the
	// replacement.
	shows := func(p int) bool { return slices.Contains(e.Views()[0].Ghosts, heading(p)) }
	settle(t, e, func() bool { return shows(4) })
	if shows(0) {
		t.Errorf("the cursor's line shows %v", e.Views()[0].Ghosts)
	}
	if feed(e, "j"); !shows(0) || shows(4) {
		t.Errorf("with the cursor on the second line the ghosts are %v", e.Views()[0].Ghosts)
	}
	// A selection's lines show their text.
	if feed(e, "ggVj"); shows(0) || shows(4) {
		t.Errorf("with both lines selected the ghosts are %v", e.Views()[0].Ghosts)
	}
	// 2gj counts the rows of the line below as the window draws them, where
	// its second row starts after the 9 x's beside §.
	if feed(e, "<Esc>j2gj"); e.Cursor() != 11+2+9 {
		t.Errorf("2gj went to %d, want the second drawn row's start", e.Cursor())
	}
	// The view scrolls by the rows the lines draw: the cursor's takes two,
	// and each one above it takes one.
	if feed(e, "G"); e.Text().LineOf(e.Views()[0].Top) != e.Text().LineOf(e.Cursor())-2 {
		t.Errorf("at the last line the view starts at line %d", e.Text().LineOf(e.Views()[0].Top))
	}
}

// B295, B296, from a plugin
func TestB295PluginReplaceAndShade(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\nbcd\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Replace 1<CR>:Shade 1<CR>")
	settle(t, e, func() bool {
		v := e.Views()[0]
		return slices.Contains(v.Ghosts, wire.Ghost{Pos: 2, End: 4, Text: "§", Style: "markdown.h1"}) && v.Grounds[1] == "markdown.code"
	})
}

// ruled is a module that draws a row of its own above the line before the
// last.
type ruled struct{ idle }

func (ruled) Init(_ context.Context, s api.Scope) (api.Result, error) {
	p := s.Text.LineStart(s.Text.Lines() - 2)
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: p, End: p, Style: "markdown.rule", Lines: []string{"──"}, Above: true}}}, nil
}

// B299
func TestB299VirtualRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, strings.Repeat("a\n", 9)+"z\n")
	e := start(t, path)
	e.SetSize(10, 5)
	e.Use(ruled{})
	// The row shows once its line is in view.
	feed(e, "G")
	settle(t, e, func() bool { return len(e.Views()[0].Virtual[8]) == 1 })
	// The window shows four rows, and the row above the line before the
	// last takes one, so G shows three lines.
	if feed(e, "ggG"); e.Text().LineOf(e.Views()[0].Top) != 7 {
		t.Errorf("at the last line the view starts at line %d", e.Text().LineOf(e.Views()[0].Top))
	}
}

// B299, B300, from a plugin
func TestB299PluginRowsAndBlock(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\nbcd\nef\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Rows 1<CR>:Block 1 2<CR>")
	settle(t, e, func() bool {
		return slices.Equal(e.Views()[0].Virtual[1], []wire.Virtual{{Text: "──", Style: "markdown.rule"}})
	})
	// With the cursor on the block's other line, its row goes.
	if feed(e, "3G"); len(e.Views()[0].Virtual[1]) != 0 {
		t.Errorf("with the cursor in the block, line 2 draws %v", e.Views()[0].Virtual[1])
	}
}

// pictured is a module that shows the image at path below the first line.
type pictured struct {
	idle
	path string
}

func (m pictured) Init(context.Context, api.Scope) (api.Result, error) {
	return api.Result{Decorate: []api.Decoration{{ID: 1, Image: m.path}}}, nil
}

// B303
func TestB303ImageRows(t *testing.T) {
	dir := t.TempDir()
	pic := filepath.Join(dir, "a.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pic, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f")
	change(t, path, "a\nb\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.Use(pictured{path: pic})
	// With no cell size from the client, the image takes no rows.
	if len(e.Views()[0].Virtual[0]) != 0 {
		t.Fatalf("with no cell size, line 1 draws %v", e.Views()[0].Virtual[0])
	}
	// With 8 by 16 pixel cells, 80 by 32 pixels take 10 columns and two
	// rows, on the cursor's line too.
	e.SetCell(8, 16)
	settle(t, e, func() bool { return len(e.Views()[0].Virtual[0]) == 2 })
	v := e.Views()[0].Virtual[0]
	if v[0].Image == 0 || v[1].Image != v[0].Image || v[0].Row != 0 || v[1].Row != 1 || v[0].Cols != 10 {
		t.Errorf("the image's rows are %+v", v)
	}
}

// B306
func TestB306FreeImages(t *testing.T) {
	dir := t.TempDir()
	pic := filepath.Join(dir, "a.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pic, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f")
	change(t, path, "a\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.SetCell(8, 16)
	e.Use(pictured{path: pic})
	settle(t, e, func() bool { return len(e.Views()[0].Virtual[0]) == 2 })
	id := e.Views()[0].Virtual[0][0].Image
	// Closing the buffer drops the image it showed, once.
	if feed(e, ":bd<CR>"); !slices.Equal(e.Freed(), []int{id}) || e.Freed() != nil {
		t.Errorf("after :bd the images freed are not %d once", id)
	}
}

// B308
func TestB308RemoteImage(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(b.Bytes()) }))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.SetCell(8, 16)
	// With remote_images off, an image at an address does not load.
	feed(e, ":set remote_images false<CR>")
	e.Use(pictured{path: srv.URL + "/a.png"})
	if len(e.Views()[0].Virtual[0]) != 0 {
		t.Fatal("with remote_images off the image took rows")
	}
	feed(e, ":set remote_images true<CR>")
	settle(t, e, func() bool { return len(e.Views()[0].Virtual[0]) == 2 })
}

// floater is a module whose :Float shows markdown with an image in a float.
type floater struct{ idle }

func (floater) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "Float", Help: "shows an image in a float", ID: 1}}
}

func (floater) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{Float: &api.Float{Text: "see ![pic](a.png) here"}}, nil
}

// B309
func TestB309FloatImage(t *testing.T) {
	dir := t.TempDir()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "f")
	change(t, path, "a\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.SetCell(8, 16)
	e.Use(floater{})
	// The image beside the file shows under the float's text, which gives
	// its description.
	feed(e, ":Float<CR>")
	settle(t, e, func() bool {
		f, ok := e.Float()
		return ok && f.Text == "see pic here" && f.Image != nil && f.Image.Rows == 2 && f.Image.Cols == 10
	})
}

// B289
func TestB289PluginReadsItsDecorations(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n b\n c\nd\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Fold 0 2<CR>")
	settle(t, e, func() bool { return e.Views()[0].Folds != nil })
	feed(e, "G:Decos<CR>")
	settle(t, e, func() bool { return strings.Contains(text(e), "1:0-5:true|") })
}

// B338
func TestB338PluginDoesWhatAModuleDoes(t *testing.T) {
	e := loaded(t, "full", "f", "a\n", "keys")
	if feed(e, ":wh<CR>"); text(e) != "xa" {
		t.Errorf(":wh left %q, want xa", text(e))
	}
	e = loaded(t, "full", "f", "a\n")
	if feed(e, ":wh<CR>"); text(e) != "a" {
		t.Errorf(":wh without keys left %q", text(e))
	}
	if want := "full: Keys needs keys, which it did not declare"; !strings.Contains(fudeLog(), want) {
		t.Errorf("the log has %q, want %q", fudeLog(), want)
	}
	e = loaded(t, "full", "f", "ab\n", "buffer.read", "buffer.write")
	if feed(e, "lma:Marked<CR>"); text(e) != "a:1|ab" {
		t.Errorf(":Marked typed %q, want a:1|ab", text(e))
	}
}
