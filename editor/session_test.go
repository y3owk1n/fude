package editor_test

import (
	"path/filepath"
	"strings"
	"testing"

	startmod "github.com/y3owk1n/fude/modules/start"

	"github.com/y3owk1n/fude/editor"
)

// B73
func TestB73SessionsConverge(t *testing.T) {
	e := open(t, "")
	agent := e.Attach("agent", editor.Host)
	feed(e, "ione")
	feed(agent, "0itwo <Esc>")
	feed(e, " three<Esc>")
	if got := text(e); got != "two one three" || text(agent) != got {
		t.Fatalf("got %q and %q, want both %q", got, text(agent), "two one three")
	}
}

// B77
func TestB77UndoOwnChanges(t *testing.T) {
	e := open(t, "")
	agent := e.Attach("agent", editor.Host)
	// The agent's change lands while the first session is still typing.
	feed(e, "ihello")
	feed(agent, "A world<Esc>")
	feed(e, "<Esc>u")
	if got := text(e); got != " world" {
		t.Fatalf("after the first session's undo got %q, want %q", got, " world")
	}
	feed(agent, "u")
	if got := text(e); got != "" {
		t.Fatalf("after the agent's undo got %q, want it empty", got)
	}
}

// B226
func TestB226ProjectSession(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\nline two\n")
	change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	feed(e, "j$:vsplit<CR>:e b.txt<CR>:qa<CR>")
	// fude started again with no file offers the session, and s restores
	// its windows, the current one and the cursors.
	e = start(t)
	e.SetSize(80, 24)
	e.Use(startmod.New())
	e.Welcome()
	settle(t, e, func() bool { return strings.Contains(text(e), "s  restore the last session here") })
	feed(e, "s")
	vs := e.Views()
	if len(vs) != 2 || vs[0].Name != "a.txt" || vs[1].Name != "b.txt" || !vs[1].Current || vs[0].Cursor != 9 {
		t.Fatalf("s restored %d windows, the first %q at %d", len(vs), vs[0].Name, vs[0].Cursor)
	}
}

// B170
func TestB170Restart(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	change(t, a, "one\ntwo\n")
	change(t, b, "three\n")
	e := start(t, a, b)
	e.SetSize(80, 20)
	feed(e, "j:vsplit<CR>:b 2<CR>ix<Esc>:set number true<CR>:restart<CR>")
	if !e.Done() || e.Restart() == "" {
		t.Fatal(":restart did not quit with a saved session")
	}
	t.Setenv(editor.RestartEnv, e.Restart())
	e = start(t)
	e.SetSize(80, 20)
	vs := e.Views()
	if len(vs) != 2 || vs[0].Name != a || vs[1].Name != b {
		t.Fatalf("the restarted session shows %d windows", len(vs))
	}
	// The unsaved change, the cursor, the option and the current window come back.
	if got := vs[1].Text.Slice(0, vs[1].Text.Len()); !vs[1].Current || got != "xthree" || !vs[1].Number || !vs[1].Modified {
		t.Errorf("the current window shows %q, current %v, number %v", got, vs[1].Current, vs[1].Number)
	}
	if vs[0].Cursor != 4 {
		t.Errorf("the other window's cursor is at %d, want 4", vs[0].Cursor)
	}
	e = start(t, a)
	if feed(e, "ZR"); !e.Done() || e.Restart() == "" {
		t.Error("ZR did not quit with a saved session")
	}
}
