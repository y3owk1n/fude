package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/frontend/wire"
	completionmod "github.com/y3owk1n/fude/modules/completion"
	filesmod "github.com/y3owk1n/fude/modules/files"
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

// B556
func TestB556Pick(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, f := range []string{"a.txt", "b.txt"} {
		change(t, filepath.Join(dir, f), "hello helium\n")
	}
	e := open(t, "a.txt")
	e.Use(completionmod.New())
	e.Use(filesmod.New())
	e.SetSize(80, 24)
	// line returns the item on line n of the menu that shows.
	line := func(n int) string {
		m, _ := e.Menu()
		lines := strings.Split(m.Text, "\n")
		if n >= len(lines) {
			return ""
		}
		return strings.Fields(lines[n] + " .")[0]
	}
	// The command line's menu puts the item in the line.
	feed(e, ":se<Tab>")
	want := line(1)
	if e.Pick(1); e.Line() != ":"+want {
		t.Errorf("picking %q from the command line's menu left the line %q", want, e.Line())
	}
	// A completion menu puts the item in the text and closes.
	feed(e, "<Esc>Ohe<C-n>")
	want = line(1)
	e.Pick(1)
	if _, open := e.Menu(); open || !strings.HasPrefix(text(e), want+"\n") {
		t.Errorf("picking %q from the completion menu left %q, the menu open %v", want, text(e), open)
	}
	// The finder opens the file on the line.
	feed(e, "<Esc>:pick files<CR>")
	at := -1
	settle(t, e, func() bool {
		m, _ := e.Menu()
		for i, it := range m.Items {
			if it.Label == "b.txt" {
				at = i
			}
		}
		return at >= 0
	})
	e.Pick(at)
	settle(t, e, func() bool { return filepath.Base(e.Windows()[0].Name) == "b.txt" })
}

// B557
func TestB557MoveSelection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one two three\nb\nc\n")
	e := open(t, path)
	e.SetSize(40, 12)
	v := e.Windows()[0]
	// Dragging the selection to the end of its line moves it there.
	e.Click(v.ID, 0, 0)
	e.Drag(v.ID, 3, 0)
	e.Release(v.ID, 3, 0)
	e.Click(v.ID, 1, 0)
	e.Drag(v.ID, 20, 0)
	e.Release(v.ID, 20, 0)
	if text(e) != "two threeone \nb\nc" || e.Mode() != vim.Normal {
		t.Errorf("moving the selection gave %q in mode %v", text(e), e.Mode())
	}
	// It is one change.
	if feed(e, "u"); text(e) != "one two three\nb\nc" {
		t.Errorf("u after the move gave %q", text(e))
	}
	// A line selected with V moves before the line it goes to.
	feed(e, "ggV<Esc>gv")
	e.Click(v.ID, 0, 0)
	e.Release(v.ID, 0, 2)
	if text(e) != "b\none two three\nc" {
		t.Errorf("moving a line gave %q", text(e))
	}
	// A selection let go on itself puts the cursor there.
	feed(e, "j0vll")
	e.Click(v.ID, 1, 1)
	if e.Release(v.ID, 1, 1); e.Mode() != vim.Normal || e.Cursor() != 3 || text(e) != "b\none two three\nc" {
		t.Errorf("letting go on the selection left mode %v at %d with %q", e.Mode(), e.Cursor(), text(e))
	}
}

// B560
func TestB560MenuItems(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	change(t, filepath.Join(dir, "a.txt"), "hello helium\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "sub", "bat.txt"), "x\n")
	e := open(t, "a.txt")
	e.Use(completionmod.New())
	e.Use(filesmod.New())
	e.SetSize(80, 24)
	// rows checks that a menu's items are the rows of its text after skip,
	// and that Selected is the row its selection starts.
	rows := func(what string, skip int) wire.Menu {
		t.Helper()
		m, _ := e.Menu()
		// Blank rows may follow, as the finder's do to be as tall as its
		// preview.
		lines := strings.Split(m.Text, "\n")[skip:]
		for len(lines) > len(m.Items) && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		if len(m.Items) != len(lines) {
			t.Errorf("%s has %d items for %d rows", what, len(m.Items), len(lines))
			return m
		}
		for i, it := range m.Items {
			if !strings.Contains(lines[i], it.Label) {
				t.Errorf("%s's item %q is not on its row %q", what, it.Label, lines[i])
			}
		}
		sel := -1
		for _, sp := range m.Spans {
			if sp.Name == "menu.selected" {
				sel = strings.Count(m.Text[:sp.Start], "\n") - skip
			}
		}
		if m.Selected != sel {
			t.Errorf("%s selects item %d, its text row %d", what, m.Selected, sel)
		}
		return m
	}
	feed(e, ":se<Tab>")
	rows("the command line's menu", 0)
	feed(e, "<Esc>Ohe<C-n>")
	if m := rows("the completion menu", 0); len(m.Items) == 0 || m.Items[0].Detail != "buffer" {
		t.Errorf("the completion menu's items are %+v", m.Items)
	}
	// The finder's items are its list, after its query and the rule under
	// it, and a file's marks are those in its name.
	feed(e, "<Esc><Esc>:pick files<CR>bt")
	settle(t, e, func() bool {
		m, _ := e.Menu()
		return m.Query == "bt" && slices.ContainsFunc(m.Items, func(it wire.Item) bool { return it.Label == "bat.txt" })
	})
	m := rows("the finder", 2)
	i := slices.IndexFunc(m.Items, func(it wire.Item) bool { return it.Label == "bat.txt" })
	if it := m.Items[i]; it.Detail != "sub" || it.Path != filepath.Join("sub", "bat.txt") || it.Kind != "file" || len(it.Marks) == 0 || m.Count == "" || m.Prompt == "" {
		t.Errorf("the finder's item is %+v, its count %q and prompt %q", it, m.Count, m.Prompt)
	} else {
		for _, at := range it.Marks {
			if at >= len(it.Label) || !strings.ContainsRune("bt", rune(it.Label[at])) {
				t.Errorf("the finder marks %d of %q", at, it.Label)
			}
		}
	}
}
