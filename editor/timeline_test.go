package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/format"
	timelinemod "github.com/y3owk1n/fude/modules/timeline"
)

// B30
func TestB30UndoRedoBranches(t *testing.T) {
	cases := [][3]string{
		{"abcd", "llihello<Esc>u", "ab|cd"},
		{"abcd", "llihello<Esc>u<C-r>", "ab|hellocd"},
		{"a\nbcd\ne", "jlddu", "a\nb|cd\ne"},
		{"a\nbcd\ne", "jlddu<C-r>", "a\n|e"},
		{"abc", "lliX<Esc>uiY<Esc>u<C-r>", "ab|Yc"},
		{"abc", "lliX<Esc>iY<Esc>uu<C-r>", "ab|Xc"},
		{"a b c", "dwwdwuu", "|a b c"},
	}
	run(t, cases)
}

// B35
func TestB35TimeOrder(t *testing.T) {
	run(t, [][3]string{
		{"x", "A1<Esc>uA2<Esc>g-", "x|1"},
		{"x", "A1<Esc>uA2<Esc>g-g-", "|x"},
		{"x", "A1<Esc>uA2<Esc>g-g-g+", "x|1"},
		{"x", "A1<Esc>uA2<Esc>g-g-g+g+", "x|2"},
	})
	e := open(t, filepath.Join(t.TempDir(), "f"))
	feed(e, "g-")
	if e.Message() != "already at the oldest change" {
		t.Errorf("g- with no history said %q", e.Message())
	}
}

// B33
func TestB33TimelineAcrossSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	feed(open(t, path), "A two<Esc>:w<CR>A three<Esc>u:q<CR>")

	e := open(t, path)
	e.Use(timelinemod.New())
	feed(e, ":timeline<CR>")
	if lines := strings.Split(text(e), "\n"); len(lines) != 3 || !strings.HasPrefix(lines[1], "* 1") {
		t.Fatalf("the next session's :timeline listed %q, want three states at 1", lines)
	}
	feed(e, ":q<CR>u")
	if text(e) != "one" {
		t.Errorf("undo past the restart left %q, want one", text(e))
	}

	// A file changed outside fude starts a new timeline.
	if err := os.WriteFile(path, []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	e = open(t, path)
	feed(e, ":timeline<CR>")
	if lines := strings.Split(text(e), "\n"); len(lines) != 1 {
		t.Errorf("after an outside change, :timeline listed %q, want only the start", lines)
	}
}

// B198
func TestB198TimelineLogCut(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	for range 15 {
		feed(e, "o")
		e.Paste(strings.Repeat("x", 100<<10))
		feed(e, "<Esc>")
	}
	feed(e, "A last<Esc>:w<CR>")
	want := text(e)
	feed(e, "u")
	before := text(e)
	feed(e, "<C-r>:q<CR>")
	logs, err := filepath.Glob(filepath.Join(state, "fude", "timeline", "*.log"))
	if err != nil || len(logs) != 1 {
		t.Fatalf("the timeline logs are %q, %v", logs, err)
	}
	info, err := os.Stat(logs[0])
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1<<20 {
		t.Fatalf("after closing, the log is %d bytes, want at most 1MB", info.Size())
	}
	e = open(t, path)
	if text(e) != want || e.Message() != "" {
		t.Fatalf("reopening said %q", e.Message())
	}
	if feed(e, "u"); text(e) != before {
		t.Errorf("undo after reopening left %d bytes, want %d", len(text(e)), len(before))
	}

	// A session that ends at its last save without :q, as a crash would,
	// cuts nothing, so the next open cuts the log.
	path = filepath.Join(t.TempDir(), "g")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	e = open(t, path)
	for range 15 {
		feed(e, "o")
		e.Paste(strings.Repeat("x", 100<<10))
		feed(e, "<Esc>")
	}
	feed(e, "A last<Esc>:w<CR>")
	want = text(e)
	feed(e, "u")
	before = text(e)
	feed(e, "<C-r>")
	if e = open(t, path); text(e) != want || e.Message() != "" {
		t.Fatalf("reopening after a crash said %q", e.Message())
	}
	logs, err = filepath.Glob(filepath.Join(state, "fude", "timeline", "*.log"))
	if err != nil || len(logs) != 2 {
		t.Fatalf("the timeline logs are %q, %v", logs, err)
	}
	for _, log := range logs {
		if info, err := os.Stat(log); err != nil || info.Size() > 1<<20 {
			t.Fatalf("after reopening, %s is %v bytes (%v), want at most 1MB", log, info.Size(), err)
		}
	}
	if feed(e, "u"); text(e) != before {
		t.Errorf("undo after the cut left %d bytes, want %d", len(text(e)), len(before))
	}
}

// B34
func TestB34Recovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The session ends without :q, as a crash would.
	feed(open(t, path), "A two<Esc>:w<CR>A three<Esc>")

	e := open(t, path)
	e.Use(timelinemod.New())
	if !strings.Contains(e.Message(), ":recover") || text(e) != "one two" {
		t.Fatalf("after a crash, opening said %q with %q", e.Message(), text(e))
	}
	feed(e, ":recover<CR>")
	if text(e) != "one two three" {
		t.Errorf(":recover left %q", text(e))
	}

	// Declining keeps the change on the timeline as a branch.
	e = open(t, path)
	e.Use(timelinemod.New())
	feed(e, ":timeline<CR>")
	if lines := strings.Split(text(e), "\n"); len(lines) != 3 || !strings.HasPrefix(lines[1], "* 1") {
		t.Errorf("without :recover, :timeline listed %q, want the unsaved state kept", lines)
	}

	// A formatter that answers after its buffer closed changes nothing, so
	// there is nothing to recover. up writes nothing, which would empty the
	// buffer. Its timeout is long, so a slow machine never stops it first.
	path = filepath.Join(t.TempDir(), "g.txt")
	change(t, path, "one\n")
	editortest.FakeProgram(t, "up")
	config(t, "format_timeout = 60000\n[filetype.txt]\nformat = [\"up\"]\n")
	e = withCommands(start(t, path))
	e.Use(format.New())
	feed(e, ":format<CR>:bd!<CR>")
	editortest.Quiet(t, e)
	if e = start(t, path); strings.Contains(e.Message(), ":recover") {
		t.Errorf("after a formatter answered a closed buffer, opening said %q", e.Message())
	}
}

// run opens a file holding each case's text, feeds its keys and compares the
// text with | at the cursor.
