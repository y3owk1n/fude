package editor_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/modules/grep"
)

// B20
func TestB20Marks(t *testing.T) {
	run(t, [][3]string{
		{"x\nfoobar", "jlllmaggOnew<Esc>`a", "new\nx\nfoo|bar"},
		{"x\ny\nfoobar", "jjlllmaggddG`a", "y\nfoo|bar"},
		{"foobar", "lllma0iXX<Esc>`a", "XXfoo|bar"},
		{"  foo\nbar", "llmaj'a", "  |foo\nbar"},
		{"foo bar\nbaz", "wmajd`a", "foo |z"},
		{"foo bar\nbaz", "jmaggd'a", "|"},
	})
	e := open(t, filepath.Join(t.TempDir(), "f"))
	feed(e, "`b")
	if e.Message() != "mark not set" {
		t.Errorf("jumping to an unset mark gave %q", e.Message())
	}
}

// B90
func TestB90JumpsAndChanges(t *testing.T) {
	var lines []string
	for i := 1; i <= 20; i++ {
		lines = append(lines, fmt.Sprintf("l%d x", i))
	}
	dir := t.TempDir()
	path, other := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("b1\nb2\nb3"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		keys      string
		text      string // the first line of the buffer shown at the end
		line, col int
	}{
		{"G<C-o>", "l1 x", 1, 0},
		{"G<C-o><C-i>", "l1 x", 20, 0},
		{"5G10G15G<C-o><C-o>", "l1 x", 5, 0},
		{"5G10G15G<C-o><C-o><C-i>", "l1 x", 10, 0},
		{"jjj<C-o>", "l1 x", 4, 0},
		{"/l9<CR><C-o>", "l1 x", 1, 0},
		{"5Gjj10G<C-o>", "l1 x", 7, 0},
		{"G<C-o><C-o>", "l1 x", 1, 0},
		{"Ggg<C-o>", "l1 x", 20, 0},
		{"G\t", "l1 x", 20, 0},
		{"3Gx8Gx12Gxg;", "l1 x", 12, 0},
		{"3Gx8Gx12Gxg;g;", "l1 x", 8, 0},
		{"3Gx8Gx12Gxg;g;g,", "l1 x", 12, 0},
		{"3Gx8Gx12Gx2g;", "l1 x", 8, 0},
		{"llx3Gx8Gg;g;", "l1x", 1, 2},
		{"j:e " + other + "<CR><C-o>", "l1 x", 2, 0},
		{"j:e " + other + "<CR><C-o><C-i>", "b1", 1, 0},
	} {
		e := open(t, path)
		feed(e, c.keys)
		got := text(e)
		line := strings.Count(got[:e.Cursor()], "\n") + 1
		col := e.Cursor() - strings.LastIndexByte(got[:e.Cursor()], '\n') - 1
		if first, _, _ := strings.Cut(got, "\n"); first != c.text || line != c.line || col != c.col {
			t.Errorf("%q: in %q at line %d col %d, want %q at %d and %d", c.keys, first, line, col, c.text, c.line, c.col)
		}
	}
	// The jumplist keeps the newest 100 places.
	lines = nil
	var jumps strings.Builder
	for i := 1; i <= 130; i++ {
		lines = append(lines, fmt.Sprintf("l%d", i))
		fmt.Fprintf(&jumps, "%dG", i)
	}
	change(t, path, strings.Join(lines, "\n"))
	e := open(t, path)
	if feed(e, jumps.String()+"99<C-o><C-o>"); e.Text().LineOf(e.Cursor()) != 30 {
		t.Errorf("<C-o> past the oldest of 100 jumps went to line %d, want 31", e.Text().LineOf(e.Cursor())+1)
	}
}

// windows describes each window as x,y wxh and * for the current one.

// B152
func TestB152Marks(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "editor"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "editor", "flash.go"), "")
	t.Chdir(dir)
	e := open(t, "")
	e.Use(grep.New())
	e.SetSize(100, 24)
	// marked is the letters a float marks as matching, in order.
	marked := func() string {
		f, _ := e.Float()
		var b strings.Builder
		for _, s := range f.Spans {
			if s.Name == "match" {
				b.WriteString(f.Text[s.Start:s.End])
			}
		}
		return b.String()
	}
	feed(e, ":pick files<CR>edfl")
	settle(t, e, func() bool { return marked() == "edfl" })
	feed(e, "<Esc>:tim<Tab>")
	if marked() != "tim" {
		t.Fatalf("the command menu marks %q", marked())
	}
	// A grep result longer than the box is cut, and marks what is left.
	change(t, filepath.Join(dir, "long.txt"), "x needle "+strings.Repeat("y", 300)+" needle\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	feed(e, "<Esc>:pick grep needle<CR>")
	settle(t, e, func() bool { return marked() == "needle" })
}

// B157
func TestB157InfoViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one\ntwo\nthree\n")
	e := open(t, path)
	e.SetSize(80, 24)
	// :messages lists the messages shown so far.
	for _, keys := range []string{":set number<CR>", ":set wrap<CR>"} {
		feed(e, keys)
		e.Message()
	}
	if feed(e, ":messages<CR>"); text(e) != "number false\nwrap true" {
		t.Fatalf(":messages listed %q", text(e))
	}
	// :marks lists the marks, and <CR> jumps to one.
	feed(e, ":q<CR>majjmb:marks<CR>")
	if text(e) != "a  1:1  one\nb  3:1  three" {
		t.Fatalf(":marks listed %q", text(e))
	}
	if feed(e, "j<CR>"); e.Cursor() != 8 {
		t.Fatalf("<CR> on mark b went to %d", e.Cursor())
	}
	// :jumps and :changes list where the cursor jumped and the text changed.
	feed(e, ":only<CR>ggGx:jumps<CR>")
	if !strings.Contains(text(e), "1:1  one") {
		t.Fatalf(":jumps listed %q", text(e))
	}
	if feed(e, ":q<CR>:changes<CR>"); text(e) != "1  3:1  hree" {
		t.Fatalf(":changes listed %q", text(e))
	}
}
