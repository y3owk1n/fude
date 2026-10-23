package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/vim"
)

// B552
func TestB552Mouse(t *testing.T) {
	var lines []string
	for i := range 30 {
		lines = append(lines, fmt.Sprint("line ", i))
	}
	long := strings.Repeat("abcdefghij", 6)
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(long+"\none\n\ttwo\n"+strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	e.SetSize(40, 12)
	feed(e, ":set number true<CR>")
	v := e.Windows()[0]
	if v.Gutter == 0 {
		t.Fatalf("line numbers left no gutter, saying %q", e.Message())
	}
	cols := v.W - v.Gutter
	one := len(long) + 1
	two := one + len("one\n")
	for _, c := range []struct {
		what string
		x, y int
		want int
	}{
		{"the second row of a wrapped line", v.Gutter + 2, 1, cols + 2},
		{"the gutter", 0, 2, one},
		{"past the end of a line", v.Gutter + 20, 2, one + 2},
		{"a tab's later column", v.Gutter + 2, 3, two},
	} {
		if e.Click(v.ID, c.x, c.y); e.Cursor() != c.want {
			t.Errorf("a click on %s put the cursor at %d, want %d", c.what, e.Cursor(), c.want)
		}
	}
	// A drag selects from the click.
	e.Click(v.ID, v.Gutter, 2)
	e.Drag(v.ID, v.Gutter+1, 2)
	if s, ok := e.Selection(); !ok || s.Start != one || s.End != one+2 {
		t.Errorf("the drag selected %+v, %v", s, ok)
	}
	// A click ends the selection, and keeps insert mode.
	if e.Click(v.ID, v.Gutter, 0); e.Mode() != vim.Normal {
		t.Errorf("a click in visual mode left mode %v", e.Mode())
	}
	feed(e, "i")
	if e.Click(v.ID, v.Gutter+1, 2); e.Mode() != vim.Insert || e.Cursor() != one+1 {
		t.Errorf("a click in insert mode left mode %v at %d", e.Mode(), e.Cursor())
	}
	feed(e, "<Esc>")
	// While the command line shows, a click does nothing.
	feed(e, ":")
	at := e.Cursor()
	if e.Click(v.ID, v.Gutter, 0); e.Cursor() != at {
		t.Errorf("a click while typing a command moved the cursor to %d", e.Cursor())
	}
	feed(e, "<Esc><C-w>v")
	// Scrolling the other window moves its view and keeps the focus.
	other := e.Windows()[0]
	for _, w := range e.Windows() {
		if !w.Current {
			other = w
		}
	}
	e.Scroll(other.ID, 5)
	for _, w := range e.Windows() {
		if w.ID == other.ID && (w.Top <= other.Top || w.Current) {
			t.Errorf("scrolling the other window left its top at %d, current %v", w.Top, w.Current)
		}
	}
	// A click on a row past the window's text gives it the focus alone.
	e.Click(other.ID, 0, 100)
	for _, w := range e.Windows() {
		if w.ID == other.ID && !w.Current {
			t.Error("a click on the other window's status line left it without the focus")
		}
	}
}
