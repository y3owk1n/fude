package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	terminalmod "github.com/y3owk1n/fude/modules/terminal"
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
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "file\n")
	e := open(t, path)
	e.Use(terminalmod.New())
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
	e.Use(terminalmod.New())
	e.SetSize(80, 24)
	feed(e, ":terminal cat<CR>")
	if vs := e.Views(); len(vs) != 2 || vs[1].H != 12 {
		t.Errorf("with terminal_height = 50 the terminal is %d rows tall, want 12", vs[len(vs)-1].H)
	}
}

// B355
func TestB355TerminalEnv(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	t.Setenv("FUDE_SOCKET", "")
	t.Setenv("FUDE_SESSION", "")
	e := open(t, "")
	e.Use(terminalmod.New())
	e.SetSize(80, 24)
	echoed := func() string {
		feed(e, ":terminal echo [$FUDE_SOCKET $FUDE_SESSION]<CR>")
		settle(t, e, func() bool { return strings.HasSuffix(e.Views()[len(e.Views())-1].Name, "exited 0]") })
		got := strings.TrimSpace(text(e))
		feed(e, ":q<CR>")
		return got
	}
	if got := echoed(); got != "[ ]" {
		t.Errorf("without a socket, a terminal's program has %s", got)
	}
	// A socket path must be short, so it goes in a directory of its own.
	dir, err := os.MkdirTemp("", "fude")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	defer e.Serve(path)()
	if got, want := echoed(), "["+path+" user]"; got != want {
		t.Errorf("a terminal's program has %s, want %s", got, want)
	}
}
