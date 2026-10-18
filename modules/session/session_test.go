package session_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/session"
	"github.com/y3owk1n/fude/modules/start"
	"github.com/y3owk1n/fude/modules/window"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B226
func TestB226ProjectSession(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\nline two\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := editortest.Start(t, "a.txt")
	e.Use(session.New())
	e.Use(window.New())
	e.SetSize(80, 24)
	editortest.Feed(e, "j$:vsplit<CR>:e b.txt<CR>:qa<CR>")
	// fude started again with no file offers the session, and s restores
	// its windows, the current one and the cursors.
	e = editortest.Start(t)
	e.SetSize(80, 24)
	e.Use(start.New())
	e.Use(session.New())
	e.Use(window.New())
	e.Welcome()
	editortest.Settle(t, e, func() bool { return strings.Contains(editortest.Text(e), "s  restore the last session here") })
	editortest.Feed(e, "s")
	vs := e.Views()
	if len(vs) != 2 || vs[0].Name != "a.txt" || vs[1].Name != "b.txt" || !vs[1].Current || vs[0].Cursor != 9 {
		t.Fatalf("s restored %d windows, the first %q at %d", len(vs), vs[0].Name, vs[0].Cursor)
	}
	// A listing's window comes back too, with its cursor.
	editortest.Feed(e, ":only<CR>:e .<CR>j:qa<CR>")
	e = editortest.Start(t)
	e.Use(start.New())
	e.Use(session.New())
	e.Use(window.New())
	e.Welcome()
	editortest.Settle(t, e, func() bool { return strings.Contains(editortest.Text(e), "s  restore the last session here") })
	editortest.Feed(e, "s")
	if vs := e.Views(); len(vs) != 1 || editortest.Text(e) != "a.txt\nb.txt" || e.Text().LineOf(e.Cursor()) != 1 {
		t.Fatalf("s restored %d windows showing %q, the cursor on line %d", len(vs), editortest.Text(e), e.Text().LineOf(e.Cursor()))
	}
}
