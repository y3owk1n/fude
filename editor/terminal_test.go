package editor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
)

// screen returns the text of the terminal window's screen, or "" when no
// window shows one in terminal mode.
func screen(e *editor.Session) string {
	for _, v := range e.Views() {
		if v.Cells == nil {
			continue
		}
		var rows []string
		for _, r := range v.Cells {
			var s strings.Builder
			for _, c := range r {
				s.WriteString(c.Text)
			}
			rows = append(rows, strings.TrimRight(s.String(), " "))
		}
		return strings.Join(rows, "\n")
	}
	return ""
}

// B205
func TestB205Terminal(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "file\n")
	e := open(t, path)
	e.SetSize(80, 24)
	feed(e, ":terminal cat<CR>")
	vs := e.Views()
	if len(vs) != 2 || !e.Terminal() || vs[1].H != 8 || vs[1].Name != "[terminal cat]" {
		t.Fatalf(":terminal cat made %d windows, the last %q %d rows tall, terminal mode %v", len(vs), vs[len(vs)-1].Name, vs[len(vs)-1].H, e.Terminal())
	}
	// cat echoes what it reads, under the line the terminal echoes as typed.
	feed(e, "hi<CR>")
	settle(t, e, func() bool { return strings.Count(screen(e), "hi") == 2 })
	// The text ends on the program's cursor, on the line after the output.
	if feed(e, `<C-\><C-n>`); e.Terminal() || text(e) != "hi\nhi\n" {
		t.Fatalf(`<C-\><C-n> left terminal mode %v with %q`, e.Terminal(), text(e))
	}
	if feed(e, "i"); !e.Terminal() {
		t.Fatal("i did not go back to terminal mode")
	}
	// The cursor stays after a character of more than one byte.
	feed(e, "é")
	settle(t, e, func() bool { return strings.Contains(screen(e), "é") })
	if feed(e, "<Esc><Esc>"); e.Terminal() || e.Cursor() != len(text(e)) {
		t.Fatalf("<Esc><Esc> left terminal mode %v with the cursor at %d of %q", e.Terminal(), e.Cursor(), text(e))
	}
	// The program ends, and its window stays with how it ended.
	feed(e, "i<C-d><C-d>")
	settle(t, e, func() bool { return e.Views()[1].Name == "[terminal cat, exited 0]" })
	if e.Terminal() || !strings.HasPrefix(text(e), "hi\nhi") {
		t.Errorf("after cat ended, terminal mode is %v with %q", e.Terminal(), text(e))
	}
}

// B228
func TestB228Tasks(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	change(t, filepath.Join(dir, "Makefile"), "build:\n\techo b\n")
	change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	t.Chdir(dir)
	config(t, "[task.say]\ncommand = \"echo said {line}\"\n")
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	// :pick tasks lists config.toml's tasks and the Makefile's targets.
	feed(e, ":pick tasks<CR>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return strings.Contains(f.Text, "say  config") && strings.Contains(f.Text, "build  make")
	})
	feed(e, "<Esc>")
	// A task runs below, the cursor staying where it ran from, and its
	// window names how it ended.
	feed(e, "j:task say<CR>")
	settle(t, e, func() bool { vs := e.Views(); return len(vs) == 2 && strings.HasPrefix(vs[1].Name, "[task say ✓") })
	if vs := e.Views(); !vs[0].Current {
		t.Errorf("the task took the cursor")
	}
	// :task runs the last one again, in place of its window.
	feed(e, ":task<CR>")
	settle(t, e, func() bool { vs := e.Views(); return len(vs) == 2 && strings.HasPrefix(vs[1].Name, "[task say ✓") })
	feed(e, "<C-w>j")
	if !strings.Contains(text(e), "said 2") {
		t.Errorf("the task's window shows %q", text(e))
	}
	// q closes the window of a task that is done.
	if feed(e, "q"); len(e.Views()) != 1 {
		t.Errorf("q left %d windows", len(e.Views()))
	}
}
