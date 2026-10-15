package client_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/client"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// TestMain keeps the editor's timeline logs, config and caches in a
// directory of their own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fude-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// served starts an editor on the files args name, or an empty buffer, serves it on a socket and
// runs its loop. typed runs keys in its session on the loop and returns
// the text after them.
func served(t *testing.T, args ...string) (typed func(keys string) string, path string) {
	t.Helper()
	e, err := editor.Start(args)
	if err != nil {
		t.Fatal(err)
	}
	do, done := make(chan func()), make(chan struct{})
	t.Cleanup(func() { close(done) })
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path = filepath.Join(dir, "s")
	t.Cleanup(e.Serve(path))
	go func() {
		for {
			select {
			case f := <-e.Queue():
				f()
			case f := <-do:
				f()
			case <-done:
				return
			}
		}
	}()
	typed = func(keys string) string {
		var text string
		ran := make(chan struct{})
		do <- func() {
			for _, k := range vim.Keys(keys) {
				e.Feed(k)
			}
			s := e.Text()
			text = s.Slice(0, s.Len())
			close(ran)
		}
		<-ran
		return text
	}
	return typed, path
}

// dial connects to the editor at path.
func dial(t *testing.T, path string) *client.Conn {
	t.Helper()
	c, err := client.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

// B79
func TestB79Replica(t *testing.T) {
	typed, path := served(t)
	typed("ihello world<Esc>")

	r, err := dial(t, path).Open(0)
	if err != nil {
		t.Fatal(err)
	}
	// Its own edit shows at once, and another client's in between lands in
	// both.
	typed("0ibig <Esc>")
	if err := r.Edit(kernel.Edit{Start: 11, End: 11, Text: "!"}); err != nil || r.Text() != "hello world!" {
		t.Fatalf("the replica's own edit left %q: %v", r.Text(), err)
	}
	if dropped, err := r.Sync(); err != nil || len(dropped) != 0 {
		t.Fatalf("sync dropped %v: %v", dropped, err)
	}
	if server := typed(""); r.Text() != "big hello world!" || server != r.Text() {
		t.Fatalf("after sync the replica has %q and the server %q", r.Text(), server)
	}
	// An edit of its own whose text another client changed first is dropped
	// and reported.
	typed("0wwcwthere<Esc>")
	if err := r.Edit(kernel.Edit{Start: 10, End: 15}); err != nil {
		t.Fatal(err)
	}
	dropped, err := r.Sync()
	if want := []kernel.Edit{{Start: 10, End: 15}}; err != nil || !slices.Equal(dropped, want) {
		t.Fatalf("sync dropped %v, want %v: %v", dropped, want, err)
	}
	if server := typed(""); r.Text() != "big hello there!" || server != r.Text() {
		t.Fatalf("after sync the replica has %q and the server %q", r.Text(), server)
	}
}

// feed sends keys to ed.
func feed(t *testing.T, ed *client.Editor, keys string) {
	t.Helper()
	for _, k := range vim.Keys(keys) {
		if err := ed.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
}

// B98
func TestB98EditorOnReplica(t *testing.T) {
	typed, path := served(t)
	typed("ione two<Esc>")
	r, err := dial(t, path).Open(0)
	if err != nil {
		t.Fatal(err)
	}
	ed := client.NewEditor(r, 22)
	// Its keys edit the copy at once.
	if feed(t, ed, "wciwX<Esc>"); ed.Text() != "one X" {
		t.Fatalf("ciwX made %q", ed.Text())
	}
	if _, err := ed.Sync(); err != nil || typed("") != "one X" {
		t.Fatalf("after sync the server has %q: %v", typed(""), err)
	}
	// The insert is one node of the client's, so its u reverts it whole and
	// leaves the terminal's later edit.
	typed("A!<Esc>")
	if feed(t, ed, "u"); ed.Text() != "one two!" || typed("") != ed.Text() {
		t.Fatalf("u from the client left %q, and the server %q", ed.Text(), typed(""))
	}
}

// B110, through a client running the grammar on a replica
func TestB110GrammarCases(t *testing.T) {
	src, err := os.ReadFile("../vim/testdata/grammar.txt")
	if err != nil {
		t.Fatal(err)
	}
	_, path := served(t)
	c := dial(t, path)
	base, err := c.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range strings.Split(strings.TrimRight(string(src), "\n"), "\n") {
		if l == "" || strings.HasPrefix(l, "# ") {
			continue
		}
		var cs [3]string
		if _, err := fmt.Sscanf(l, "%q %q %q", &cs[0], &cs[1], &cs[2]); err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		// The server's text becomes the case's.
		if err := base.Edit(kernel.Edit{End: len(base.Text()), Text: strings.Replace(cs[0], "|", "", 1)}); err != nil {
			t.Fatal(err)
		}
		if _, err := base.Sync(); err != nil {
			t.Fatal(err)
		}
		r, err := c.Open(0)
		if err != nil {
			t.Fatal(err)
		}
		ed := client.NewEditor(r, 22)
		feed(t, ed, fmt.Sprintf("%dgo%s", strings.Index(cs[0], "|")+1, cs[1]))
		if got := ed.Text()[:ed.Cursor()] + "|" + ed.Text()[ed.Cursor():]; got != cs[2] {
			t.Errorf("%q on %q: got %q, want %q", cs[1], cs[0], got, cs[2])
		}
	}
}

// B99
func TestB99ClientJumpsChangesAndSteps(t *testing.T) {
	typed, path := served(t)
	typed("ione<CR>two<CR>three<Esc>")
	r, err := dial(t, path).Open(0)
	if err != nil {
		t.Fatal(err)
	}
	ed := client.NewEditor(r, 22)
	line := func() int { return strings.Count(ed.Text()[:ed.Cursor()], "\n") + 1 }
	if feed(t, ed, "Gxgg<C-o>"); line() != 3 {
		t.Fatalf("<C-o> after gg went to line %d, want 3", line())
	}
	if feed(t, ed, "ggg;"); line() != 3 {
		t.Fatalf("g; went to line %d, want the change on 3", line())
	}
	// g- moves the server's timeline, which the copy follows.
	if _, err := ed.Sync(); err != nil {
		t.Fatal(err)
	}
	if feed(t, ed, "g-"); ed.Text() != "one\ntwo\nthree" || typed("") != ed.Text() {
		t.Fatalf("g- left %q, and the server %q", ed.Text(), typed(""))
	}
}

// B36, in a client, with fake clipboard programs that keep the clipboard
// in a file
func TestB36ClientClipboard(t *testing.T) {
	bin := t.TempDir()
	board := filepath.Join(bin, "board")
	for name, script := range map[string]string{
		"pbcopy": "cat > " + board, "wl-copy": "cat > " + board,
		"pbpaste": "cat " + board, "wl-paste": "cat " + board,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	t.Setenv("WAYLAND_DISPLAY", "fude-test")
	typed, path := served(t)
	typed("ione two<Esc>")
	r, err := dial(t, path).Open(0)
	if err != nil {
		t.Fatal(err)
	}
	ed := client.NewEditor(r, 22)
	if feed(t, ed, `"+yiw`); readFile(t, board) != "one" {
		t.Fatalf(`"+yiw put %q on the clipboard`, readFile(t, board))
	}
	if err := os.WriteFile(board, []byte("Z"), 0o644); err != nil {
		t.Fatal(err)
	}
	if feed(t, ed, `$"+p`); ed.Text() != "one twoZ" {
		t.Fatalf(`"+p made %q`, ed.Text())
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// B94, in a client
func TestB94ClientIndentWidth(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "fude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fude", "config.toml"), []byte("[filetype.go]\nindent_width = 4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "a.go")
	if err := os.WriteFile(file, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, path := served(t, file)
	r, err := dial(t, path).Open(0)
	if err != nil {
		t.Fatal(err)
	}
	if feed(t, client.NewEditor(r, 22), ">>"); r.Text() != "    x" {
		t.Fatalf(">> in a Go file made %q, want four spaces", r.Text())
	}
}
