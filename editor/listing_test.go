package editor_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/wire"
)

// B127
func TestB127ListingNotes(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "plain"), "")
	change(t, filepath.Join(dir, "run"), "")
	if err := os.Chmod(filepath.Join(dir, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("plain", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	e := open(t, dir)
	e.SetSize(40, 10)
	want := map[int]wire.Note{0: {Text: " -> plain"}, 2: {Text: "*"}}
	if got := e.Views()[0].Notes; !maps.Equal(got, want) {
		t.Fatalf("the notes are %v, want %v", got, want)
	}
}

// B118
func TestB118List(t *testing.T) {
	e := open(t, "")
	if feed(e, ":set list true<CR>"); !e.Views()[0].List {
		t.Fatal(":set list true left the window without it")
	}
}
