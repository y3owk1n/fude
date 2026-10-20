package editor_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	terminalmod "github.com/y3owk1n/fude/modules/terminal"
)

// screen returns the text of the terminal window's screen, or "" when no
// window shows one in terminal mode.
func screen(e *editor.Session) string {
	for _, v := range e.Windows() {
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
	vs := e.Windows()
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
	settle(t, e, func() bool { return e.Windows()[1].Name == "[terminal cat, exited 0]" })
	if e.Terminal() || !strings.HasPrefix(text(e), "hi\nhi") {
		t.Errorf("after cat ended, terminal mode is %v with %q", e.Terminal(), text(e))
	}
	// terminal_height sets how tall the window opens.
	config(t, "terminal_height = 50\n")
	e = start(t, path)
	e.Use(terminalmod.New())
	e.SetSize(80, 24)
	feed(e, ":terminal cat<CR>")
	if vs := e.Windows(); len(vs) != 2 || vs[1].H != 12 {
		t.Errorf("with terminal_height = 50 the terminal is %d rows tall, want 12", vs[len(vs)-1].H)
	}
	// A terminal opened from a file whose filetype sets options shows no
	// line numbers either.
	config(t, "number = true\n[filetype.go]\nexpand_tab = true\n")
	goPath := filepath.Join(filepath.Dir(path), "a.go")
	change(t, goPath, "package a\n")
	e = start(t, goPath)
	e.Use(terminalmod.New())
	e.SetSize(80, 24)
	if feed(e, `:terminal cat<CR><C-\><C-n>`); len(e.Windows()) != 2 || e.Windows()[1].Number {
		t.Errorf("a terminal opened from a.go shows line numbers %v", e.Windows()[len(e.Windows())-1].Number)
	}
}

// B205, B364, for the shells :terminal shows and hides
func TestB205Shells(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	e := withCommands(open(t, ""))
	e.Use(terminalmod.New())
	e.SetSize(80, 24)
	feed(e, ":terminal<CR>echo one<CR>")
	settle(t, e, func() bool { return strings.Count(screen(e), "one") == 2 })
	// The shell hides, and shows again where it was and as tall, its
	// program going on.
	if feed(e, `<C-\><C-n>:resize 5<CR>:terminal<CR>`); len(e.Windows()) != 1 {
		t.Fatalf(":terminal in the shell left %d windows", len(e.Windows()))
	}
	feed(e, ":terminal<CR>")
	if vs := e.Windows(); len(vs) != 2 || vs[1].H != 5 || vs[1].Name != "[shell 1]" || !e.Terminal() || strings.Count(screen(e), "one") != 2 {
		t.Fatalf(":terminal again showed %d windows, the last %q %d rows tall, terminal mode %v", len(vs), vs[len(vs)-1].Name, vs[len(vs)-1].H, e.Terminal())
	}
	// Under windows side by side, it shows again under both.
	feed(e, `<C-\><C-n><C-w>k:vsplit<CR><C-w>j:terminal<CR>:terminal<CR>`)
	if vs := e.Windows(); len(vs) != 3 || vs[2].Name != "[shell 1]" || vs[2].W != 80 {
		t.Fatalf("under a vsplit the shell showed again %d columns wide in %d windows", vs[len(vs)-1].W, len(vs))
	}
	// :terminal 2 is another shell.
	if feed(e, `<C-\><C-n>:terminal 2<CR>`); len(e.Windows()) != 4 || e.Windows()[3].Name != "[shell 2]" {
		t.Fatalf(":terminal 2 left %d windows, the last %q", len(e.Windows()), e.Windows()[len(e.Windows())-1].Name)
	}
	// A shell in a float leaves terminal mode as one in a split does.
	if feed(e, `<C-\><C-n>:window float<CR>i<C-\><C-n>`); !floated(e) || e.Terminal() {
		t.Fatalf("leaving terminal mode in a float left a float current %v, terminal mode %v", floated(e), e.Terminal())
	}
	// A shell's window closes once it exits.
	feed(e, "iexit<CR>")
	settle(t, e, func() bool { return len(e.Windows()) == 3 && !floated(e) })
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
		settle(t, e, func() bool { return strings.HasSuffix(e.Windows()[len(e.Windows())-1].Name, "exited 0]") })
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

// Promise: a terminal leaves nothing behind once its program ends, so fude
// is as big after many terminals as after one (product.md, Stable).
func TestPromiseTerminalsLeaveNothing(t *testing.T) {
	// sh from PATH, which on Windows comes with Git for Windows.
	t.Setenv("SHELL", "sh")
	e := withCommands(open(t, ""))
	e.Use(terminalmod.New())
	e.SetSize(100, 30)
	one := func() bool { return len(e.Windows()) == 1 }
	cycles := func(n int) {
		for range n {
			feed(e, ":terminal<CR>exit<CR>")
			settle(t, e, one)
			feed(e, ":window float terminal<CR>exit<CR>")
			settle(t, e, one)
			feed(e, ":terminal echo hi<CR>")
			settle(t, e, func() bool { return strings.HasSuffix(e.Windows()[len(e.Windows())-1].Name, "exited 0]") })
			feed(e, "q")
			settle(t, e, one)
		}
		// What the programs wrote last reaches the loop too.
		for drained := false; !drained; {
			select {
			case f := <-e.Queue():
				f()
			case <-time.After(50 * time.Millisecond):
				drained = true
			}
		}
	}
	measure := func() (int, uint64) {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return runtime.NumGoroutine(), m.HeapAlloc
	}
	cycles(3)
	g0, h0 := measure()
	cycles(20)
	g1, h1 := measure()
	if g1 > g0+2 || h1 > h0+1<<20 {
		t.Errorf("60 more terminals took goroutines from %d to %d and the heap from %.1fMB to %.1fMB", g0, g1, float64(h0)/(1<<20), float64(h1)/(1<<20))
	}
}
