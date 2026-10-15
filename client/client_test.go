package client_test

import (
	"os"
	"path/filepath"
	"slices"
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

// B79
func TestB79Replica(t *testing.T) {
	e, err := editor.Open("")
	if err != nil {
		t.Fatal(err)
	}
	// type runs keys in the editor's session on its loop.
	do, done := make(chan func()), make(chan struct{})
	defer close(done)
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	defer e.Serve(filepath.Join(dir, "s"))()
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
	typed := func(keys string) string {
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
	typed("ihello world<Esc>")

	c, err := client.Dial(filepath.Join(dir, "s"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r, err := c.Open(0)
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
