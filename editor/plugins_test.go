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
func withPlugin(t *testing.T, name string, caps ...string) *editor.Editor {
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
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	return start(t, path)
}

// settle runs the work queued for the editor loop until done reports true.
func settle(t *testing.T, e *editor.Editor, done func() bool) {
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
