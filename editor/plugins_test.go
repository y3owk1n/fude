package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
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
	wasm, err := filepath.Abs(filepath.Join("..", "plugin", "testdata", name+".wasm"))
	if err != nil {
		t.Fatal(err)
	}
	quoted := make([]string, len(caps))
	for i, c := range caps {
		quoted[i] = fmt.Sprintf("%q", c)
	}
	config(t, fmt.Sprintf("[[plugin]]\npath = %q\ncapabilities = [%s]\n", wasm, strings.Join(quoted, ", ")))
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
		case <-deadline:
			t.Fatal("still waiting after 5s")
		}
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
	(<-e.Queue())()
	for strings.Count(fudeLog(), "listen: buffer.changed") < 2 {
		time.Sleep(time.Millisecond)
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
}

// withKit starts an editor on "ab cd\nef" with the kit plugin loaded.
func withKit(t *testing.T) *editor.Session {
	t.Helper()
	usePlugin(t, "kit", "buffer.read", "buffer.write")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab cd\nef\n")
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

// B54
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
