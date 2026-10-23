package task_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/places"
	"github.com/y3owk1n/fude/modules/task"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B228
func TestB228Tasks(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "Makefile"), "build:\n\techo b\n")
	if err := os.MkdirAll(filepath.Join(dir, ".fude"), 0o755); err != nil {
		t.Fatal(err)
	}
	editortest.Change(t, filepath.Join(dir, ".fude", "tasks.toml"), "[task.lint]\ncommand = \"echo l\"\n")
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	t.Chdir(dir)
	editortest.Config(t, "[task.say]\ncommand = \"echo said {line}\"\n")
	e := editortest.Start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	e.Use(places.New())
	// :pick tasks lists config.toml's tasks, the project's and the
	// Makefile's targets (B233).
	editortest.Feed(e, ":pick tasks<CR>")
	editortest.Settle(t, e, func() bool {
		f, _ := e.Menu()
		return strings.Contains(f.Text, "say  config") && strings.Contains(f.Text, "lint  project") && strings.Contains(f.Text, "build  make")
	})
	editortest.Feed(e, "<Esc>")
	// A task runs below, the cursor staying where it ran from, and its
	// window names how it ended.
	editortest.Feed(e, "j:task say<CR>")
	editortest.Settle(t, e, func() bool { vs := e.Windows(); return len(vs) == 2 && strings.HasPrefix(vs[1].Name, "[task say ✓") })
	if vs := e.Windows(); !vs[0].Current {
		t.Errorf("the task took the cursor")
	}
	// :task runs the last one again, in place of its window.
	editortest.Feed(e, ":task<CR>")
	editortest.Settle(t, e, func() bool { vs := e.Windows(); return len(vs) == 2 && strings.HasPrefix(vs[1].Name, "[task say ✓") })
	editortest.Feed(e, "<C-w>j")
	if !strings.Contains(editortest.Text(e), "said 2") {
		t.Errorf("the task's window shows %q", editortest.Text(e))
	}
	// q closes the window of a task that is done.
	if editortest.Feed(e, "q"); len(e.Windows()) != 1 {
		t.Errorf("q left %d windows", len(e.Windows()))
	}
	// An unknown key in the project's tasks says where it is (B233).
	editortest.Change(t, filepath.Join(dir, ".fude", "tasks.toml"), "[task.lint]\ncmd = \"echo l\"\n")
	editortest.Feed(e, ":pick tasks<CR>")
	editortest.Settle(t, e, func() bool { return e.Message() == ".fude/tasks.toml line 2: unknown key task.lint.cmd" })
}

// B230
func TestB230TaskPlaces(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\ntwo\n")
	t.Chdir(dir)
	editortest.Config(t, "[task.check]\ncommand = \"printf 'ok\\\\na.txt:2:3: bad thing\\\\nnope.txt:1: gone\\\\n'; exit 1\"\n")
	e := editortest.Start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	e.Use(places.New())
	// The places in a task's output that name a file there become the last
	// list, and ]q goes to the first.
	editortest.Feed(e, ":task check<CR>")
	editortest.Settle(t, e, func() bool {
		vs := e.Windows()
		return len(vs) == 2 && strings.HasPrefix(vs[1].Name, "[task check ✗ 1")
	})
	editortest.Settle(t, e, func() bool { return strings.Contains(e.Message(), "It named 1 places") })
	if editortest.Feed(e, "]q"); e.Windows()[0].Name != "a.txt" || e.Cursor() != 6 {
		t.Errorf("]q went to %q at %d, want a.txt at 6", e.Windows()[0].Name, e.Cursor())
	}
	if !strings.Contains(e.Message(), "(1 of 1) a.txt:2:3: bad thing") {
		t.Errorf("]q said %q", e.Message())
	}
}

// B231
func TestB231BackgroundTasks(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	editortest.Config(t, "terminal_height = 50\n[task.wait]\ncommand = \"sleep 0.5\"\n")
	e := editortest.Start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	e.Use(places.New())
	// A running task shows on the status line, and q in its window hides
	// the window while the task goes on.
	editortest.Feed(e, ":task wait<CR>")
	editortest.Settle(t, e, func() bool { return slices.Equal(e.Windows()[0].Tasks, []string{"wait"}) })
	if editortest.Feed(e, "<C-w>j<C-\\><C-n>q"); len(e.Windows()) != 1 || !slices.Equal(e.Windows()[0].Tasks, []string{"wait"}) {
		t.Fatalf("q left %d windows and the tasks %v", len(e.Windows()), e.Windows()[0].Tasks)
	}
	// :pick tasks marks it, and picking it shows its window again.
	editortest.Feed(e, ":pick tasks<CR>")
	editortest.Settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "wait  config, running") })
	editortest.Feed(e, "<CR>")
	editortest.Settle(t, e, func() bool { return len(e.Windows()) == 2 })
	if e.Windows()[1].H != 11 {
		t.Fatalf("picking the running task shows %d windows, the last %d rows tall, want 11", len(e.Windows()), e.Windows()[len(e.Windows())-1].H)
	}
	// Once it ends, the message says so and the status line drops it.
	editortest.Settle(t, e, func() bool {
		return strings.HasPrefix(e.Message(), "the wait task ended in") && e.Windows()[0].Tasks == nil
	})
}

// B564
func TestB564TaskEndsAlert(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	editortest.Config(t, "[task.quick]\ncommand = \"true\"\n")
	e := editortest.Start(t, "a.txt")
	e.SetSize(80, 24)
	e.Use(task.New())
	editortest.Feed(e, ":task quick<CR>")
	// The first alert is the task's end, and it comes once.
	alert := ""
	editortest.Settle(t, e, func() bool {
		if a, ok := e.Alert(); ok && alert == "" {
			alert = a
		}
		return alert != ""
	})
	if !strings.HasPrefix(alert, "the quick task ended in") {
		t.Errorf("the alert is %q", alert)
	}
	if a, ok := e.Alert(); ok {
		t.Errorf("the alert came again, %q", a)
	}
}
