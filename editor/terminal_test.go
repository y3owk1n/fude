package editor_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/modules/task"
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
	// terminal_height sets how tall the window opens.
	config(t, "terminal_height = 50\n")
	e = start(t, path)
	e.SetSize(80, 24)
	feed(e, ":terminal cat<CR>")
	if vs := e.Views(); len(vs) != 2 || vs[1].H != 12 {
		t.Errorf("with terminal_height = 50 the terminal is %d rows tall, want 12", vs[len(vs)-1].H)
	}
}

// B228
func TestB228Tasks(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	change(t, filepath.Join(dir, "Makefile"), "build:\n\techo b\n")
	if err := os.MkdirAll(filepath.Join(dir, ".fude"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, ".fude", "tasks.toml"), "[task.lint]\ncommand = \"echo l\"\n")
	change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	t.Chdir(dir)
	config(t, "[task.say]\ncommand = \"echo said {line}\"\n")
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	// :pick tasks lists config.toml's tasks, the project's and the
	// Makefile's targets (B233).
	feed(e, ":pick tasks<CR>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return strings.Contains(f.Text, "say  config") && strings.Contains(f.Text, "lint  project") && strings.Contains(f.Text, "build  make")
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
	// An unknown key in the project's tasks says where it is (B233).
	change(t, filepath.Join(dir, ".fude", "tasks.toml"), "[task.lint]\ncmd = \"echo l\"\n")
	feed(e, ":pick tasks<CR>")
	settle(t, e, func() bool { return e.Message() == ".fude/tasks.toml line 2: unknown key task.lint.cmd" })
}

// B230
func TestB230TaskPlaces(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	t.Chdir(dir)
	config(t, "[task.check]\ncommand = \"printf 'ok\\\\na.txt:2:3: bad thing\\\\nnope.txt:1: gone\\\\n'; exit 1\"\n")
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	// The places in a task's output that name a file there become the last
	// list, and ]q goes to the first.
	feed(e, ":task check<CR>")
	settle(t, e, func() bool {
		vs := e.Views()
		return len(vs) == 2 && strings.HasPrefix(vs[1].Name, "[task check ✗ 1")
	})
	settle(t, e, func() bool { return strings.Contains(e.Message(), "It named 1 places") })
	if feed(e, "]q"); e.Views()[0].Name != "a.txt" || e.Cursor() != 6 {
		t.Errorf("]q went to %q at %d, want a.txt at 6", e.Views()[0].Name, e.Cursor())
	}
	if !strings.Contains(e.Message(), "(1 of 1) a.txt:2:3: bad thing") {
		t.Errorf("]q said %q", e.Message())
	}
}

// B231
func TestB231BackgroundTasks(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	config(t, "terminal_height = 50\n[task.wait]\ncommand = \"sleep 0.5\"\n")
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	// A running task shows on the status line, and q in its window hides
	// the window while the task goes on.
	feed(e, ":task wait<CR>")
	settle(t, e, func() bool { return slices.Equal(e.Views()[0].Tasks, []string{"wait"}) })
	if feed(e, "<C-w>j<C-\\><C-n>q"); len(e.Views()) != 1 || !slices.Equal(e.Views()[0].Tasks, []string{"wait"}) {
		t.Fatalf("q left %d windows and the tasks %v", len(e.Views()), e.Views()[0].Tasks)
	}
	// :pick tasks marks it, and picking it shows its window again.
	feed(e, ":pick tasks<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "wait  config, running") })
	feed(e, "<CR>")
	settle(t, e, func() bool { return len(e.Views()) == 2 })
	if e.Views()[1].H != 12 {
		t.Fatalf("picking the running task shows %d windows, the last %d rows tall, want 12", len(e.Views()), e.Views()[len(e.Views())-1].H)
	}
	// Once it ends, the message says so and the status line drops it.
	settle(t, e, func() bool {
		return strings.HasPrefix(e.Message(), "the wait task ended in") && e.Views()[0].Tasks == nil
	})
}
