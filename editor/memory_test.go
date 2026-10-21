package editor_test

import (
	"path/filepath"
	"testing"
)

// B454
func TestB454Remembered(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	change(t, a, "one\ntwo two\n")
	change(t, b, "x\n")
	e := start(t, a)
	feed(e, "\"ayiwj$mA/two<CR>:echo<CR>:qa!<CR>")
	if !e.Done() {
		t.Fatal("fude did not quit")
	}
	// The next run has the register, the file mark in a file it has not
	// opened, the command line and the search.
	e = start(t, b)
	if feed(e, "\"ap"); text(e) != "xone" {
		t.Errorf("\"ap in the next run made %q", text(e))
	}
	if feed(e, "`A"); text(e) != "one\ntwo two" || e.Cursor() != 10 {
		t.Errorf("`A in the next run went to %d in %q", e.Cursor(), text(e))
	}
	if feed(e, ":<Up><Up>"); e.Line() != ":echo" {
		t.Errorf(":<Up><Up> in the next run recalled %q, want the line before :qa!", e.Line())
	}
	if feed(e, "<Esc>ggn"); e.Cursor() != 4 {
		t.Errorf("n in the next run went to %d, want the last search's match", e.Cursor())
	}
}
