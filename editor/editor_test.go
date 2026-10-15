package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

func open(t *testing.T, path string) *editor.Editor {
	t.Helper()
	e, err := editor.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func feed(e *editor.Editor, keys string) {
	for _, k := range vim.Keys(keys) {
		e.Feed(k)
	}
}

func text(e *editor.Editor) string {
	s := e.Text()
	return s.Slice(0, s.Len())
}

func disk(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// B1
func TestB1OpenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	e := open(t, path)
	if text(e) != "" {
		t.Fatalf("new file opened with %q", text(e))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening created the file")
	}
	feed(e, "ihi<Esc>:w<CR>")
	if got := disk(t, path); got != "hi\n" {
		t.Fatalf("first write: %q", got)
	}
	if got := text(open(t, path)); got != "hi" {
		t.Fatalf("reopened file shows %q", got)
	}
}

// B2
func TestB2WriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	feed(e, "inew <Esc>:w<CR>")
	if got := disk(t, path); got != "new old\n" {
		t.Fatalf("write: %q", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("write changed the mode to %v", fi.Mode().Perm())
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	feed(e, "ix<Esc>:w<CR>")
	if got := disk(t, path); got != "new old\n" {
		t.Fatalf("failed write changed the file to %q", got)
	}
	if !strings.Contains(e.Message(), path) {
		t.Fatalf("message %q does not name the path", e.Message())
	}
}

// B3
func TestB3KeepsLineEndings(t *testing.T) {
	for _, c := range []struct{ before, after string }{
		{"one\r\ntwo\r\n", "one!\r\ntwo\r\n"},
		{"one\ntwo", "one!\ntwo"},
	} {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(c.before), 0o644); err != nil {
			t.Fatal(err)
		}
		e := open(t, path)
		feed(e, "A!<Esc>:w<CR>")
		if got := disk(t, path); got != c.after {
			t.Errorf("%q became %q, want %q", c.before, got, c.after)
		}
	}
}

// B4
func TestB4InvalidUTF8IsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, []byte("a\xffb"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	if !strings.Contains(e.Message(), "read-only") {
		t.Fatalf("open message %q does not say read-only", e.Message())
	}
	feed(e, "ix<Esc>:w<CR>")
	if text(e) != "a\xffb" || disk(t, path) != "a\xffb" {
		t.Fatal("read-only file changed")
	}
}

// B30
func TestB30UndoRedoBranches(t *testing.T) {
	cases := [][3]string{
		{"abcd", "llihello<Esc>u", "ab|cd"},
		{"abcd", "llihello<Esc>u<C-r>", "ab|hellocd"},
		{"a\nbcd\ne", "jlddu", "a\nb|cd\ne"},
		{"a\nbcd\ne", "jlddu<C-r>", "a\n|e"},
		{"abc", "lliX<Esc>uiY<Esc>u<C-r>", "ab|Yc"},
		{"abc", "lliX<Esc>iY<Esc>uu<C-r>", "ab|Xc"},
		{"a b c", "dwwdwuu", "|a b c"},
	}
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(c[0]), 0o644); err != nil {
			t.Fatal(err)
		}
		e := open(t, path)
		feed(e, c[1])
		got := text(e)
		if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B5
func TestB5QuitRefusesUnsaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	e := open(t, path)
	feed(e, "ix<Esc>:q<CR>")
	if e.Done() || !strings.Contains(e.Message(), path) {
		t.Fatalf("quit with unsaved changes: done %v, message %q", e.Done(), e.Message())
	}
	feed(e, ":q!<CR>")
	if !e.Done() {
		t.Fatal(":q! did not quit")
	}
}
