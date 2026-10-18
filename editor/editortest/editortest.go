// Package editortest drives a headless editor in tests, for a module or any
// program that builds on the editor. Keys go in, and the text, views and
// message come out, with no terminal.
package editortest

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/syntax/syntaxtest"
	"github.com/y3owk1n/fude/vim"
)

// Main runs the tests with the timeline logs, config and caches in a
// directory of their own, Go's grammar built and no other fetched, and
// returns the exit code for os.Exit. A test file's TestMain calls it.
func Main(m *testing.M) int {
	dir, err := os.MkdirTemp("", "fude-test")
	if err != nil {
		panic(err)
	}
	// Go's grammar builds once into the user's cache, before the cache
	// directory moves.
	if err := syntaxtest.InstallGo(filepath.Join(dir, "data")); err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	if err := os.MkdirAll(filepath.Join(dir, "config", "fude"), 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "fude", "config.toml"), []byte("auto_install = false\n"), 0o600); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	return code
}

// Config writes src as config.toml in a new config directory, with no
// grammar fetched.
func Config(t testing.TB, src string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte("auto_install = false\n"+src), 0o600); err != nil {
		t.Fatal(err)
	}
}

// Start starts an editor as the fude command does with args, and quits it
// when the test ends.
func Start(t testing.TB, args ...string) *editor.Session {
	t.Helper()
	e, err := editor.Start(args)
	if err != nil {
		t.Fatal(err)
	}
	QuitAtEnd(t, e)
	return e
}

// Open opens the file at path in an editor, and quits it when the test
// ends.
func Open(t testing.TB, path string) *editor.Session {
	t.Helper()
	e, err := editor.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	QuitAtEnd(t, e)
	return e
}

// QuitAtEnd quits e when the test ends, before its temp directories go, so
// nothing it started writes into them or outlives the test.
func QuitAtEnd(t testing.TB, e *editor.Session) {
	t.Cleanup(func() {
		if !e.Done() {
			Feed(e, "<Esc><Esc>:qa!<CR>")
		}
	})
}

// Feed types keys, in the notation a keymap uses, such as "jj:w<CR>".
func Feed(e *editor.Session, keys string) {
	for _, k := range vim.Keys(keys) {
		e.Feed(k)
	}
}

// Text is the text of the current buffer.
func Text(e *editor.Session) string {
	s := e.Text()
	return s.Slice(0, s.Len())
}

// Settle runs the work queued for the editor loop, such as a module's
// answer, until done reports true, or fails after five seconds.
func Settle(t testing.TB, e *editor.Session, done func() bool) {
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

// Change writes text to the file at path, dated an hour ahead, so a buffer
// that holds the file sees it changed.
func Change(t testing.TB, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

// Disk is the text of the file at path.
func Disk(t testing.TB, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Decorations lists the decorations every view shows, each as its text and
// its style.
func Decorations(e *editor.Session) []string {
	var out []string
	for _, v := range e.Views() {
		for _, d := range v.Decorations {
			out = append(out, v.Text.Slice(d.Start, d.End)+" "+d.Name)
		}
	}
	return out
}
