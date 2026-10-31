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
	"github.com/y3owk1n/fude/frontend/wire"
	helpmod "github.com/y3owk1n/fude/modules/help"
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

// next runs the next thing the editor queues, or fails after editortest.Patience.
func next(t *testing.T, e *editor.Session) {
	t.Helper()
	select {
	case f := <-e.Queue():
		f()
	case <-time.After(editortest.Patience):
		t.Fatal("nothing queued")
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
	case <-time.After(editortest.Patience):
		t.Fatal("the plugin did not load")
	}
	// Five edits made before the loop runs again come as one event, about
	// the text after all of them.
	feed(e, "Ahello<Esc>")
	settle(t, e, func() bool { d := decorations(e); return len(d) == 1 && d[0] == "abchello keyword" })
	editortest.Quiet(t, e)
	if n := strings.Count(fudeLog(), "listen: buffer.changed"); n != 1 {
		t.Fatalf("the plugin heard %d events, want one for five edits", n)
	}
	// It hears an event it emits under its own name.
	feed(e, "gZ")
	settle(t, e, func() bool { return strings.Contains(fudeLog(), "listen: listen.ping") })
	// While it handles an event longer than half a key's budget, its key
	// says it is busy.
	// The loop hands it the event once the work queued before it has run.
	// Its log shows it began, and it then loops until its budget runs out.
	feed(e, "A!<Esc>")
	settle(t, e, func() bool { return strings.Count(fudeLog(), "listen: buffer.changed") >= 2 })
	if feed(e, "gZ"); !strings.Contains(e.Message(), "busy") {
		t.Fatalf("gZ while the plugin handled an event said %q", e.Message())
	}
}

// B58
func TestB58StatuslineSegments(t *testing.T) {
	e := withPlugin(t, "seg", "ui")
	e.Load()
	settle(t, e, func() bool { return len(e.Windows()[0].Segments) > 0 })
	if got := e.Windows()[0].Segments; len(got) != 1 || got[0] != "ready" {
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
	f, text := floatAt(e, "cursor")
	if text != "hi" || len(f.Decorations) != 1 || f.Decorations[0].Name != "markup.strong" {
		t.Fatalf("gP showed %q with %v, want hi in bold", text, f.Decorations)
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
	// sleep outlasts the test's patience, so only its budget ends the call.
	editortest.FakeProgram(t, "sleep")
	t.Setenv("FUDE_FAKE_SLEEP", (2 * editortest.Patience).String())
	e := start(t, "")
	e.Load()
	next(t, e)
	if !strings.Contains(e.Message(), "budget") {
		t.Fatalf("the plugin said %q, want its budget to stop sleep", e.Message())
	}
	if log := fudeLog(); !strings.Contains(log, "proc: hi\n") || !strings.Contains(log, "proc: refused") {
		t.Fatalf("the plugin logged:\n%s", log)
	}
	// A program that leaves a child holding its output ends with it, though
	// the child outlasts the test's patience.
	t.Setenv("FUDE_FAKE_SLEEP", "")
	t.Setenv("FUDE_FAKE_CHILD", (2 * editortest.Patience).String())
	e = start(t, "")
	e.Load()
	next(t, e)
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

// withNotes makes a home folder for ~ holding notes/a.txt, and starts fude in a
// project of its own with the files plugin, whose table lists more and
// grants caps. It returns the home folder and the project.
func withNotes(t *testing.T, more string, caps ...string) (*editor.Session, string, string) {
	t.Helper()
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if err := os.Mkdir(filepath.Join(home, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(home, "notes", "a.txt"), "* a")
	useTable(t, "files", more, caps...)
	t.Chdir(project)
	e := start(t)
	e.Load()
	next(t, e)
	return e, home, project
}

// B585
func TestB585PluginPaths(t *testing.T) {
	// A folder neither absolute nor under ~ keeps the plugin from loading.
	useTable(t, "files", "paths = [\"notes\"]\n", "fs.read", "ui")
	e := start(t)
	e.Load()
	if next(t, e); !strings.Contains(e.Message(), `path "notes"`) {
		t.Errorf("paths = [\"notes\"] said %q, want it named", e.Message())
	}
	e, home, _ := withNotes(t, "paths = [\"~/notes\"]\n", "fs.read", "ui")
	change(t, filepath.Join(home, "secret.txt"), "secret")
	if err := os.Symlink(filepath.Join(home, "secret.txt"), filepath.Join(home, "notes", "link")); err != nil {
		t.Fatal(err)
	}
	// A file it opens that a buffer holds already shows in that buffer,
	// though a link leads to the plugin's folder, as one leads to macOS's
	// temporary folders.
	feed(e, ":e ~/notes/a.txt<CR>")
	open := e.Windows()[0].Buffer
	if feed(e, ":Open ~/notes/a.txt<CR>"); e.Windows()[0].Buffer != open {
		t.Errorf(":Open of the open file showed buffer %d, want %d", e.Windows()[0].Buffer, open)
	}
	for _, read := range []struct{ path, want string }{
		{"~/notes/a.txt", "* a"},
		{filepath.Join(home, "notes", "a.txt"), "* a"},
		{"~/secret.txt", "error"},
		{"~/notes/../secret.txt", "error"},
		{"~/notes/link", "error"},
	} {
		if feed(e, ":Read "+read.path+"<CR>"); !strings.HasPrefix(e.Message(), read.want) {
			t.Errorf(":Read %s said %q, want %s", read.path, e.Message(), read.want)
		}
	}
}

// B586
func TestB586ListAndStat(t *testing.T) {
	e, home, _ := withNotes(t, "paths = [\"~/notes\"]\n", "fs.read", "ui")
	if err := os.Mkdir(filepath.Join(home, "notes", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if feed(e, ":List ~/notes<CR>"); e.Message() != "a.txt sub/" {
		t.Errorf(":List ~/notes said %q, want a.txt sub/", e.Message())
	}
	info, err := os.Stat(filepath.Join(home, "notes", "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"~/notes/a.txt": fmt.Sprintf("3 %d", info.ModTime().UnixNano()),
		"~/notes/sub":   "dir",
		"~/notes/none":  "none",
	} {
		if feed(e, ":Stat "+path+"<CR>"); e.Message() != want {
			t.Errorf(":Stat %s said %q, want %q", path, e.Message(), want)
		}
	}
}

// B587
func TestB587WriteFiles(t *testing.T) {
	e, home, project := withNotes(t, "paths = [\"~/notes\"]\n", "fs.read", "fs.write", "buffer.write", "ui")
	notes := filepath.Join(home, "notes")
	for _, step := range []struct{ cmd, want string }{
		{":Mkdir ~/notes/new/deep", "made"},
		{":Write ~/notes/new/deep/b.txt hello there", "wrote"},
		{":Mv ~/notes/new/deep/b.txt ~/notes/c.txt", "moved"},
		{":Rm ~/notes/new", "error"},
		{":Write ~/outside.txt no", "error"},
		{":Write here.txt in the project", "wrote"},
	} {
		if feed(e, step.cmd+"<CR>"); !strings.HasPrefix(e.Message(), step.want) {
			t.Fatalf("%s said %q, want %s", step.cmd, e.Message(), step.want)
		}
	}
	if disk(t, filepath.Join(notes, "c.txt")) != "hello there" || disk(t, filepath.Join(project, "here.txt")) != "in the project" {
		t.Fatal("the writes did not reach the disk")
	}
	if _, err := os.Stat(filepath.Join(home, "outside.txt")); err == nil {
		t.Fatal("a write outside the paths made its file")
	}
	if feed(e, ":Rm ~/notes/c.txt<CR>"); e.Message() != "removed" {
		t.Fatalf(":Rm said %q", e.Message())
	}
	if _, err := os.Stat(filepath.Join(notes, "c.txt")); err == nil {
		t.Fatal(":Rm left the file")
	}
	// A buffer open unchanged reloads, and one with changes is refused.
	feed(e, ":e ~/notes/a.txt<CR>")
	if feed(e, ":Write ~/notes/a.txt * b<CR>"); text(e) != "* b" {
		t.Fatalf("the open buffer shows %q after the write, want * b", text(e))
	}
	feed(e, "A!<Esc>")
	if feed(e, ":Write ~/notes/a.txt * c<CR>"); !strings.Contains(e.Message(), "a.txt") || disk(t, filepath.Join(notes, "a.txt")) != "* b" {
		t.Errorf("a write over unwritten changes said %q and left %q", e.Message(), disk(t, filepath.Join(notes, "a.txt")))
	}
	// So is one to a file a buffer holds by a path relative to the project.
	feed(e, ":e here.txt<CR>A!<Esc>")
	if feed(e, ":Write here.txt over<CR>"); !strings.Contains(e.Message(), "here.txt") || disk(t, filepath.Join(project, "here.txt")) != "in the project" {
		t.Errorf("a write over a relative buffer's changes said %q and left %q", e.Message(), disk(t, filepath.Join(project, "here.txt")))
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
	if e.Load(); e.Idle() {
		t.Error("Load waited for the plugin to load, want it to return first")
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
		"\x0a\x09\x01\x07\x00\x03\x40\x0c\x00\x0b\x0b" + // loop br 0 end end
		manifest("loop")
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
	if err := os.WriteFile(wasm, []byte("\x00asm\x01\x00\x00\x00"+manifest("bare")), 0o644); err != nil {
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

// manifest is the custom section of a manifest that names a module name
// and asks for nothing.
func manifest(name string) string {
	j := `{"name":"` + name + `"}`
	return string([]byte{0, byte(5 + len(j)), 4}) + "fude" + j
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
	case <-time.After(editortest.Patience):
		t.Fatal("the plugin did not load")
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

// B54, B329, and B173 as kit is written with the sdk
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
	// A module command's short names show on its line, not as entries of
	// their own (B329).
	withCommands(e)
	if feed(e, ":h :cn<CR>"); !strings.Contains(text(e), "`:cnext  :cn`\n  goes to the next place") || strings.Contains(text(e), "`:cn`\n") {
		t.Error("help does not show :cn on the line of :cnext")
	}
}

// B375 and B376, as a plugin writes them
func TestB375PluginWritesHelp(t *testing.T) {
	e := loaded(t, "full", "f", "a\n")
	e.Use(helpmod.New())
	if feed(e, ":h :Whole<CR>"); !strings.Contains(text(e), "Types x at the start of the cursor's line.") {
		t.Error("help does not show the Doc of :Whole")
	}
	if feed(e, ":h guide-full<CR>"); !strings.Contains(text(e), "# The full plugin") {
		t.Error("help does not show the plugin's page")
	}
}

// B377, as a plugin registers them
func TestB377PluginOptions(t *testing.T) {
	useTable(t, "full", "options = { depth = 5 }\n", "buffer.write")
	e := opened(t, "f", "a\n")
	e.SetSize(80, 24)
	if feed(e, ":Opts<CR>"); text(e) != "5|false|5|a" {
		t.Errorf(":Opts typed %q, want 5|false|5|a", text(e))
	}
	// :set changes a window option in the window and those opened after it.
	feed(e, ":set full.depth 7<CR>:set full.bold true<CR><C-w>v:set full.bold false<CR><C-w>h")
	for name, want := range map[string]string{"full.bold": "full.bold true", "full.depth": "full.depth 7"} {
		if feed(e, ":set "+name+"<CR>"); e.Message() != want {
			t.Errorf(":set %s said %q, want %q", name, e.Message(), want)
		}
	}
	if feed(e, ":set full.depth deep<CR>"); e.Message() != `full.depth takes a number of 0 or more, not "deep"` {
		t.Errorf(":set full.depth deep said %q", e.Message())
	}
	e.Use(helpmod.New())
	if feed(e, ":h full.depth<CR>"); !strings.Contains(text(e), "`full.depth = 3`\n  how deep it looks") {
		t.Error("help does not list full.depth")
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
	path := e.Windows()[0].Name
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
	useTable(t, "ctx", "options = { depth = 3, on = true, skip = [\"markdown\", \"text\"] }\n", "buffer.write")
	e := opened(t, "f", "a\n")
	if feed(e, ":Opt ctx.depth<CR>:Opt ctx.on<CR>:Opt ctx.skip<CR>"); text(e) != "markdown,text|true|3|a" {
		t.Errorf(":Opt typed %q, want markdown,text|true|3|a", text(e))
	}
	// A name that is no option says so.
	useTable(t, "ctx", "options = { deep = 3 }\n", "buffer.write")
	if e = opened(t, "f", "a\n"); e.Message() != "ctx.deep is no option of ctx" {
		t.Errorf("a setting of no option said %q", e.Message())
	}
	// A list of anything but text, or a table, keeps the plugin from loading.
	for src, want := range map[string]string{
		"options = { depth = [3] }\n":       "option depth lists 3, and a list holds only text with no commas",
		"options = { depth = { a = 1 } }\n": "option depth is not a number, string, boolean or list",
	} {
		useTable(t, "ctx", src, "buffer.write")
		e = start(t, filepath.Join(t.TempDir(), "f"))
		if e.Load(); !strings.Contains(e.Message(), want) {
			t.Errorf("%s said %q, want %q", strings.TrimSpace(src), e.Message(), want)
		}
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

// B385
func TestB385PluginTalksToAProgram(t *testing.T) {
	useTable(t, "later", "programs = [\"cat\", \"echo\", \"sleep\"]\n", "buffer.write", "process")
	e := opened(t, "f", "a\n")
	feed(e, ":Talk<CR>")
	settle(t, e, func() bool { return text(e) == "y|x|a" })
	feed(e, ":Hush<CR>")
	settle(t, e, func() bool { return text(e) == "end|y|x|a" })
	// Eight programs it talks to hold up no job, and fude refuses a ninth.
	feed(e, strings.Repeat(":Talk<CR>", 9)+":Jobs<CR>")
	settle(t, e, func() bool {
		s := text(e)
		return strings.Count(s, "x|") == 9 && strings.Count(s, "end|") == 2 && strings.Contains(s, "hi\n")
	})
}

// B386
func TestB386PluginReadsTheClock(t *testing.T) {
	e := loaded(t, "later", "f", "a\n", "buffer.write")
	if feed(e, ":Now<CR>"); text(e) != time.Now().Format("2006 -0700")+"a" {
		t.Errorf(":Now typed %q, want this year in the local time zone", text(e))
	}
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

// B341
func TestB341PluginReadsTheMachine(t *testing.T) {
	e := loaded(t, "full", "f", "abc\n", "buffer.read", "buffer.write")
	feed(e, "\"ayiw/c<CR>qq:State<CR>q")
	// "% is the file's path, which the test's temporary folder starts.
	before, after, ok := strings.Cut(text(e), "%:")
	if !ok || before != "ctrue|\":abc," || !strings.HasSuffix(after, "f,/:c,a:abc,|c|q|abc") {
		t.Errorf(":State typed %q", text(e))
	}
}

// B342
func TestB342PluginEditsAnotherBuffer(t *testing.T) {
	e := loaded(t, "full", "f", "a\n", "buffer.read", "buffer.write", "ui")
	// The window beside keeps showing f while a scratch buffer is current.
	feed(e, ":Act split<CR>:Act scratch<CR>:Other<CR>")
	for _, v := range e.Windows() {
		if !v.Current {
			if got := v.Text.Slice(0, v.Text.Len()); got != "xa" {
				t.Errorf(":Other left %q in the other buffer, want xa", got)
			}
		}
	}
}

// B343
func TestB343PluginActsOnBuffersAndWindows(t *testing.T) {
	e := loaded(t, "full", "f", "a\n", "buffer.read", "buffer.write", "ui")
	if feed(e, ":Act split<CR>"); len(e.Windows()) != 2 {
		t.Fatalf(":Act split left %d windows", len(e.Windows()))
	}
	if feed(e, ":Act close<CR>"); len(e.Windows()) != 1 {
		t.Fatalf(":Act close left %d windows", len(e.Windows()))
	}
	if feed(e, "ix<Esc>:Act write<CR>"); e.Windows()[0].Modified {
		t.Errorf(":Act write left the buffer modified")
	}
	if feed(e, ":Act scratch<CR>"); text(e) != "hi" {
		t.Errorf(":Act scratch shows %q, want hi", text(e))
	}
	if feed(e, ":Act drop<CR>"); text(e) != "xa" {
		t.Errorf(":Act drop shows %q, want xa", text(e))
	}
}

// B344
func TestB344PluginSetsTheMachine(t *testing.T) {
	e := loaded(t, "full", "f", "aba\n", "buffer.read", "buffer.write", "ui", "keys")
	feed(e, "l:Mach<CR>")
	if text(e) != "zba" {
		t.Fatalf(":Mach's :s left %q, want zba", text(e))
	}
	// n finds the search the plugin set, `b goes to its mark, and "rp puts
	// its register.
	if feed(e, "0n`b\"rp:Wrap<CR>"); text(e) != "false|zbhia" {
		t.Errorf("after :Mach the keys left %q, want false|zbhia", text(e))
	}
}

// B346
func TestB346PluginHearsWindowsAndTheMachine(t *testing.T) {
	e := loaded(t, "full", "f", "abc\n", "buffer.write")
	feed(e, "<C-w>v<C-w>w<C-w>c/a<CR>qqqvl<Esc>:set theme light<CR>")
	want := "macro.recorded,search.changed,selection.changed,theme.changed,window.closed,window.focused,window.opened|"
	settle(t, e, func() bool {
		feed(e, ":Heard<CR>")
		return strings.HasPrefix(text(e), want)
	})
}

// B182
func TestB182PluginHearsTheCursor(t *testing.T) {
	e := loaded(t, "watch", "f", "a\nb\nc\n", "ui")
	e.SetSize(80, 10)
	feed(e, "jj")
	settle(t, e, func() bool { s := e.Windows()[0].Segments; return len(s) == 1 && s[0] == "4 0" })
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
		v := e.Windows()[0]
		return v.Signs[1] == wire.Sign{Text: "+", Name: "git.add"} && v.Notes[1].Text == "changed"
	})
	// A new change moves them, and the line they left shows none.
	feed(e, "kix<Esc>")
	settle(t, e, func() bool {
		v := e.Windows()[0]
		return v.Notes[0].Text == "changed" && v.Notes[1].Text == "" && v.Signs[1].Text == ""
	})
	// A window on another buffer shows none of them.
	if feed(e, "<C-w>v:e "+filepath.Join(t.TempDir(), "g")+"<CR>"); len(e.Windows()) != 2 || e.Windows()[1].Notes[0].Text != "" || e.Windows()[1].Signs[0].Text != "" {
		t.Errorf("a window on a new buffer shows another buffer's notes or signs")
	}
}

// B186
func TestB186PluginWindows(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n", "ui")
	e.SetSize(80, 20)
	feed(e, ":Show one<CR>:Show two<CR>")
	vs := e.Windows()
	i := slices.IndexFunc(vs, func(v wire.Window) bool { return v.Name == "[shown]" })
	if len(vs) != 2 || i < 0 || vs[i].Text.Slice(0, vs[i].Text.Len()) != "two" || vs[i].Filetype != "go" {
		t.Fatalf("after two :Show, the windows are %d and [shown] is %d", len(vs), i)
	}
	// It closes without asking to save.
	if feed(e, ":q<CR>"); len(e.Windows()) != 1 {
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
	name := e.Windows()[0].Name
	if feed(e, ":Open ../go.mod<CR>"); e.Windows()[0].Name != name {
		t.Errorf("a file outside the project opened: %s", e.Windows()[0].Name)
	}
	if feed(e, ":Open guide/01-start.md<CR>"); !strings.HasSuffix(e.Windows()[0].Name, filepath.Join("guide", "01-start.md")) || e.Cursor() != 2 {
		t.Errorf(":Open opened %s with the cursor at %d", e.Windows()[0].Name, e.Cursor())
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
		f, _ := e.Menu()
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
	feed(e, ":Opt wrap<CR>:set wrap false<CR>:Opt wrap<CR>:Opt leader<CR>:Opt filetype<CR>:set theme light<CR>:Opt theme<CR>")
	if got := text(e); got != "light|none|<Space>|false|true|a" {
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
	return []api.Registration{
		{Kind: api.Key, Name: "gX", Help: "does nothing, as the host", ID: 1, Host: true},
		{Kind: api.Plug, Name: "none", Keys: "gY", Help: "does nothing, as the host", ID: 2, Host: true},
	}
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
	// A plug is refused by its name, whatever keys map to it.
	if feed(guest, "gY"); guest.Message() != "a guest may not run <Plug>(idle.none)" {
		t.Errorf("the guest's gY said %q", guest.Message())
	}
	if feed(e, "gX"); e.Message() != "" {
		t.Errorf("the host's gX said %q", e.Message())
	}
}

// B327, for a plugin's keys
func TestB327PluginKeysYieldToConfig(t *testing.T) {
	useTable(t, "kit", "\n[keymap.normal]\ngS = \"x\"\nQ = \"<Plug>(kit.shout)\"\n", "buffer.read", "buffer.write")
	e := opened(t, "f", "ab cd\nef\n")
	if feed(e, "gS"); text(e) != "b cd\nef" {
		t.Errorf("gS, mapped to x, left %q", text(e))
	}
	if feed(e, "Q"); text(e) != "!b cd\nef" {
		t.Errorf("Q, mapped to <Plug>(kit.shout), left %q", text(e))
	}
}

// B278
func TestB278PluginPlug(t *testing.T) {
	useTable(t, "ctx", "\n[keymap.normal]\nQ = \"<Plug>(ctx.hi)\"\n", "buffer.write")
	e := opened(t, "f", "a\n")
	if feed(e, "Q"); text(e) != "hia" {
		t.Errorf("Q, mapped to <Plug>(ctx.hi), left %q, want hia", text(e))
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
// slow is a module whose job before a write ends once open is closed.
type slow struct {
	idle
	open chan struct{}
}

func (slow) Subscribed() []string { return []string{"buffer.saving"} }

func (slow) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{Jobs: []api.Job{{ID: 1, Argv: []string{"sleep"}}}}, nil
}

func (s slow) Work(context.Context, api.Job) (string, error) {
	<-s.open
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
	m := slow{open: make(chan struct{})}
	e.Use(m)
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return strings.Contains(e.Message(), "written as it was") })
	if disk(t, path) != "b\n" {
		t.Errorf("the file holds %q, want it written as it was", disk(t, path))
	}
	// The job ends only after the write gave up on it.
	close(m.open)
	editortest.Quiet(t, e)
	if text(e) != "b" {
		t.Errorf("the late edit left %q, want it dropped", text(e))
	}
}

// B282, a plugin hears it
func TestB282PluginHearsSaving(t *testing.T) {
	e := loaded(t, "listen", "f", "a\n", "buffer.read", "ui")
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return strings.Contains(fudeLog(), "listen: buffer.saving") && !e.Windows()[0].Modified })
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
		f, _ := e.Menu()
		return strings.Contains(f.Text, "a1") && strings.Contains(f.Text, "a2")
	})
	feed(e, "b")
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "ab2") })
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
	e := withCommands(start(t, path))
	e.SetSize(80, 20)
	e.Use(spots{path: path})
	feed(e, ":pick spots<CR>")
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "second") })
	feed(e, "<CR>")
	if line := e.Text().LineOf(e.Cursor()); line != 1 || e.Windows()[0].Name != path {
		t.Errorf("picking the place put the cursor on line %d of %s, want line 2 of the file", line+1, e.Windows()[0].Name)
	}
	// <C-q> lists the places.
	feed(e, ":pick spots<CR>")
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "second") })
	feed(e, "<C-q>")
	settle(t, e, func() bool {
		return slices.ContainsFunc(e.Windows(), func(v wire.Window) bool { return v.Name == "[spots]" })
	})
	if !slices.ContainsFunc(e.Windows(), func(v wire.Window) bool { return v.Name == "[spots]" }) {
		t.Errorf("<C-q> opened %v, want a [spots] list", e.Windows())
	}
}

// B287, a plugin's source
func TestB287PluginSource(t *testing.T) {
	e := loaded(t, "ctx", "f", "x\n", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":pick letters a<CR>")
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "a2") })
	feed(e, "<C-n><CR>")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "a2") })
	feed(e, "<Esc>:pick<CR>")
	settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "letters") })
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

// B378
func TestB378PluginDisabled(t *testing.T) {
	later, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "later", "later.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	useTable(t, "ctx", fmt.Sprintf("disabled = true\n[[plugin]]\npath = %q\ncapabilities = [\"buffer.write\"]\n", later), "buffer.write")
	e := opened(t, "f", "a\n")
	feed(e, ":Twice<CR>")
	settle(t, e, func() bool { return text(e) == "tta" })
	if feed(e, ":Ctx<CR>"); !strings.Contains(e.Message(), "not a command") {
		t.Errorf(":Ctx of the disabled plugin said %q", e.Message())
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
			case <-time.After(editortest.Patience):
				t.Fatal("the plugin did not load")
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
	settle(t, e, func() bool { return maps.Equal(e.Windows()[0].Folds, map[int]int{0: 2}) })
	// j passes over the fold, and a search into it opens it.
	if feed(e, "j"); e.Text().LineOf(e.Cursor()) != 3 {
		t.Errorf("j over the fold went to line %d", e.Text().LineOf(e.Cursor()))
	}
	if feed(e, "gg/c<CR>"); e.Windows()[0].Folds != nil {
		t.Errorf("searching into the fold left %v", e.Windows()[0].Folds)
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
	shows := func(p int) bool { return slices.Contains(e.Windows()[0].Ghosts, heading(p)) }
	settle(t, e, func() bool { return shows(4) })
	if shows(0) {
		t.Errorf("the cursor's line shows %v", e.Windows()[0].Ghosts)
	}
	if feed(e, "j"); !shows(0) || shows(4) {
		t.Errorf("with the cursor on the second line the ghosts are %v", e.Windows()[0].Ghosts)
	}
	// A selection's lines show their text.
	if feed(e, "ggVj"); shows(0) || shows(4) {
		t.Errorf("with both lines selected the ghosts are %v", e.Windows()[0].Ghosts)
	}
	// 2gj counts the rows of the line below as the window draws them, where
	// its second row starts after the 9 x's beside §.
	if feed(e, "<Esc>j2gj"); e.Cursor() != 11+2+9 {
		t.Errorf("2gj went to %d, want the second drawn row's start", e.Cursor())
	}
	// The window scrolls by the rows the lines draw: the cursor's takes two,
	// and each one above it takes one.
	if feed(e, "G"); e.Text().LineOf(e.Windows()[0].Top) != e.Text().LineOf(e.Cursor())-2 {
		t.Errorf("at the last line the window starts at line %d", e.Text().LineOf(e.Windows()[0].Top))
	}
}

// B295, B296, from a plugin
func TestB295PluginReplaceAndShade(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\nbcd\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Replace 1<CR>:Shade 1<CR>")
	settle(t, e, func() bool {
		v := e.Windows()[0]
		return slices.Contains(v.Ghosts, wire.Ghost{Pos: 2, End: 4, Text: "§", Style: "markdown.h1"}) && v.Grounds[1] == "markdown.code"
	})
}

// tabbed is a module whose <Tab> types x after an a, and does nothing
// elsewhere.
type tabbed struct{ idle }

func (tabbed) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Typed, Name: "<Tab>", ID: 1}}
}

func (tabbed) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	if c := args[0]; c > 0 && s.Text.Slice(c-1, c) == "a" {
		return api.Result{Edits: []api.Edit{{Start: c, End: c, Text: "x"}}}, nil
	}
	return api.Result{}, nil
}

// B323: a module's <Tab> runs with expand_tab on too, and with no edits
// <Tab> types its spaces.
func TestB323TypedTab(t *testing.T) {
	config(t, "expand_tab = true\nindent_width = 4\n")
	e := start(t)
	e.Use(tabbed{})
	if feed(e, "ia<Tab>b<Tab>"); text(e) != "axb " {
		t.Errorf("a<Tab>b<Tab> made %q, want %q", text(e), "axb ")
	}
}

// starred is a module whose x and Q, in a notes file, put ! at the end of
// a line that starts with *, and whose Q of no filetype shows global.
type starred struct{ idle }

func (starred) Name() string { return "starred" }

func (starred) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Plug, Name: "global", Keys: "Q", ID: 1},
		{Kind: api.Plug, Name: "cut", Keys: "x", Filetype: "notes", ID: 2},
		{Kind: api.Plug, Name: "mark", Keys: "Q", Filetype: "notes", ID: 3},
	}
}

func (starred) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if id == 1 {
		return api.Result{Message: "global"}, nil
	}
	t := s.Text
	start := t.LineStart(t.LineOf(args[0]))
	line, _, _ := strings.Cut(t.Slice(start, t.Len()), "\n")
	if !strings.HasPrefix(line, "*") {
		return api.Result{}, nil
	}
	end := start + len(line)
	return api.Result{Edits: []api.Edit{{Start: end, End: end, Text: "!"}}}, nil
}

// B588
func TestB588FiletypeKeys(t *testing.T) {
	dir := t.TempDir()
	notes, txt := filepath.Join(dir, "a.notes"), filepath.Join(dir, "a.txt")
	change(t, notes, "* a\n")
	change(t, txt, "* a\n")
	e := start(t, txt)
	e.Use(starred{})
	if feed(e, "xQ"); text(e) != " a" || e.Message() != "global" {
		t.Fatalf("xQ in a text file made %q and said %q, want x's own and global", text(e), e.Message())
	}
	feed(e, ":e "+notes+"<CR>")
	if feed(e, "xQ"); text(e) != "* a!!" {
		t.Fatalf("xQ in a notes file made %q, want the module's", text(e))
	}
	feed(e, ":w<CR>:e "+txt+"<CR>")
	if feed(e, "Q"); e.Message() != "global" {
		t.Errorf("Q back in the text file said %q, want global", e.Message())
	}
}

// B589
func TestB589KeysGiveBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.notes")
	change(t, path, "* a\nabcdefgh\n")
	e := start(t, path)
	e.Use(starred{})
	if feed(e, "j2x."); text(e) != "* a\nefgh" {
		t.Fatalf("2x. off a heading made %q, want x's own twice", text(e))
	}
	if feed(e, "Q"); e.Message() != "global" {
		t.Errorf("Q off a heading said %q, want the key that names no filetype", e.Message())
	}
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
	settle(t, e, func() bool { return len(e.Windows()[0].Virtual[8]) == 1 })
	// The window shows four rows, and the row above the line before the
	// last takes one, so G shows three lines.
	if feed(e, "ggG"); e.Text().LineOf(e.Windows()[0].Top) != 7 {
		t.Errorf("at the last line the window starts at line %d", e.Text().LineOf(e.Windows()[0].Top))
	}
}

// B299, B300, from a plugin
func TestB299PluginRowsAndBlock(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\nbcd\nef\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Rows 1<CR>:Block 1 2<CR>")
	settle(t, e, func() bool {
		return slices.Equal(e.Windows()[0].Virtual[1], []wire.Virtual{{Text: "──", Style: "markdown.rule"}})
	})
	// With the cursor on the block's other line, its row goes.
	if feed(e, "3G"); len(e.Windows()[0].Virtual[1]) != 0 {
		t.Errorf("with the cursor in the block, line 2 draws %v", e.Windows()[0].Virtual[1])
	}
}

// suggesting is a module that shows ghost text of two lines after the
// first line's a.
type suggesting struct{ idle }

func (suggesting) Init(context.Context, api.Scope) (api.Result, error) {
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: 1, End: 1, Style: "proposal", Note: "bc", Lines: []string{"de"}, Insert: true}}}, nil
}

// B384
func TestB384GhostText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\nz\n")
	e := start(t, path)
	e.SetSize(80, 20)
	e.Use(suggesting{})
	// The note and its row show on the cursor's line.
	feed(e, "A")
	settle(t, e, func() bool {
		v := e.Windows()[0]
		return slices.Contains(v.Ghosts, wire.Ghost{Pos: 1, Text: "bc", Style: "proposal"}) &&
			slices.Equal(v.Virtual[0], []wire.Virtual{{Text: "de", Style: "proposal"}})
	})
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
	if len(e.Windows()[0].Virtual[0]) != 0 {
		t.Fatalf("with no cell size, line 1 draws %v", e.Windows()[0].Virtual[0])
	}
	// With 8 by 16 pixel cells, 80 by 32 pixels take 10 columns and two
	// rows, on the cursor's line too.
	e.SetCell(8, 16)
	settle(t, e, func() bool { return len(e.Windows()[0].Virtual[0]) == 2 })
	v := e.Windows()[0].Virtual[0]
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
	e := withCommands(start(t, path))
	e.SetSize(80, 20)
	e.SetCell(8, 16)
	e.Use(pictured{path: pic})
	settle(t, e, func() bool { return len(e.Windows()[0].Virtual[0]) == 2 })
	id := e.Windows()[0].Virtual[0][0].Image
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
	if len(e.Windows()[0].Virtual[0]) != 0 {
		t.Fatal("with remote_images off the image took rows")
	}
	feed(e, ":set remote_images true<CR>")
	settle(t, e, func() bool { return len(e.Windows()[0].Virtual[0]) == 2 })
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
		f, text := floatAt(e, "cursor")
		rows := f.Virtual[0]
		return text == "see pic here" && len(rows) == 2 && rows[0].Image != 0 && rows[0].Cols == 10
	})
}

// B289
func TestB289PluginReadsItsDecorations(t *testing.T) {
	e := loaded(t, "ctx", "f", "a\n b\n c\nd\n", "buffer.read", "buffer.write", "ui")
	e.SetSize(80, 20)
	feed(e, ":Fold 0 2<CR>")
	settle(t, e, func() bool { return e.Windows()[0].Folds != nil })
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
	for _, want := range []string{"full: Keys needs keys, which it did not declare", "full: register § needs buffer.read, which it did not declare"} {
		if !strings.Contains(fudeLog(), want) {
			t.Errorf("the log has %q, want %q", fudeLog(), want)
		}
	}
	e = loaded(t, "full", "f", "ab\n", "buffer.read", "buffer.write")
	if feed(e, "lma:Marked<CR>"); text(e) != "a:1|ab" {
		t.Errorf(":Marked typed %q, want a:1|ab", text(e))
	}
}

// B336, by a plugin
func TestB336PluginCompletionSource(t *testing.T) {
	// With completion_sources unset, fude asks a plugin's sources too. The
	// menu waits for the items of later, then shows them with those of words.
	e := loaded(t, "words", "f", "\n", "buffer.read", "ui")
	feed(e, "ifu<C-x><C-o>")
	settle(t, e, func() bool { return text(e) == "fuzzy" })
	if f, _ := e.Menu(); !strings.Contains(f.Text, "fudge  [words]") {
		t.Errorf("the menu is %q, want fudge from words", f.Text)
	}
	// Once the plugin loads, its name is a source, and fude reports a name that is not.
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", "words", "words.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	config(t, fmt.Sprintf("completion_sources = [\"words\", \"nope\"]\n[[plugin]]\npath = %q\ncapabilities = [\"buffer.read\", \"ui\"]\n", wasm))
	if e = opened(t, "f", "\n"); !strings.HasSuffix(e.Message(), `not "nope"`) {
		t.Errorf("completion_sources words,nope said %q", e.Message())
	}
}

// confirmer is a module that asks go on? on gY, and on hearing a question
// asks one of its own, telling heard what it hears.
type confirmer struct {
	idle
	heard chan string
}

func (confirmer) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Key, Name: "gY", Help: "asks go on?", ID: 1}}
}

func (confirmer) Subscribed() []string { return []string{"confirm.asked", "confirm.answered"} }

func (confirmer) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{Confirm: &api.Confirm{ID: 7, Question: "go on?"}}, nil
}

func (c confirmer) Event(_ context.Context, name string, _ api.Scope) (api.Result, error) {
	c.heard <- name
	if !strings.HasPrefix(name, "confirm.asked") {
		return api.Result{}, nil
	}
	return api.Result{Confirm: &api.Confirm{ID: 8, Question: "again?"}}, nil
}

func (c confirmer) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	c.heard <- fmt.Sprint(id, " ", s.About["yes"])
	return api.Result{}, nil
}

// B363
func TestB363ModuleConfirm(t *testing.T) {
	e := open(t, "")
	c := confirmer{heard: make(chan string, 8)}
	e.Use(c)
	next := func(want string) {
		t.Helper()
		var got string
		settle(t, e, func() bool {
			select {
			case got = <-c.heard:
				return true
			default:
				return false
			}
		})
		if got != want {
			t.Fatalf("the module heard %q, want %q", got, want)
		}
	}
	feed(e, "gY")
	if e.Confirm() != "go on?" {
		t.Fatalf("gY asked %q", e.Confirm())
	}
	// fude answers no at once to the question it asks while go on? shows.
	next("confirm.asked go on?")
	next("8 false")
	feed(e, "y")
	next("confirm.answered yes")
	next("7 true")
}

// locker is a module whose :Locked shows a panel it locks in a float.
type locker struct{ idle }

func (locker) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "Locked", Help: "shows a locked panel", ID: 1}}
}

func (locker) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{Show: &api.Panel{Title: "[locked]", Text: "kept", Where: "float", Lock: true}}, nil
}

// B367
func TestB367LockedWindow(t *testing.T) {
	e := withCommands(open(t, ""))
	e.SetSize(80, 24)
	e.Use(locker{})
	// The module's float stays one through a modifier and a move.
	if feed(e, ":window below Locked<CR>"); !floated(e) || len(e.Windows()) != 2 {
		t.Fatalf(":window below Locked made the windows %s", windows(e))
	}
	if feed(e, ":window below<CR>"); !floated(e) || e.Message() != "[locked] cannot move" {
		t.Errorf(":window below in the locked float left a float current %v, saying %q", floated(e), e.Message())
	}
}

// redrawer is a module that styles the word two. On the first change it
// reads the text, closes read, and draws again once held is closed.
type redrawer struct {
	idle
	read, held chan struct{}
}

func (redrawer) Subscribed() []string { return []string{"buffer.changed"} }

func (redrawer) Init(_ context.Context, s api.Scope) (api.Result, error) {
	at := strings.Index(s.Text.Slice(0, s.Text.Len()), "two")
	return api.Result{Decorate: []api.Decoration{{ID: 1, Start: at, End: at + 3, Style: "keyword"}}}, nil
}

func (r redrawer) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	select {
	case <-r.read:
		return api.Result{}, nil
	default:
	}
	at := strings.Index(s.Text.Slice(0, s.Text.Len()), "two")
	close(r.read)
	<-r.held
	return api.Result{Undecorate: []int{1}, Decorate: []api.Decoration{{ID: 2, Start: at, End: at + 3, Style: "keyword"}}}, nil
}

// B291
func TestB291StaleDecorationsMove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one two\n")
	e := start(t, path)
	r := redrawer{read: make(chan struct{}), held: make(chan struct{})}
	e.Use(r)
	settle(t, e, func() bool { return len(decorations(e)) == 1 })
	// A key comes while the module draws again. What it drew moves with the
	// key's edit, and replaces what it drew before.
	feed(e, "I-")
	settle(t, e, func() bool {
		select {
		case <-r.read:
			return true
		default:
			return false
		}
	})
	feed(e, "+")
	close(r.held)
	editortest.Quiet(t, e)
	if got := decorations(e); text(e) != "-+one two" || !slices.Equal(got, []string{"two keyword"}) {
		t.Errorf("%q has decorations %q, want two as keyword", text(e), got)
	}
}
