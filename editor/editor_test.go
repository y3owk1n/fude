package editor_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// TestMain keeps every test's timeline logs, config and caches in a
// directory of its own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fude-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func open(t *testing.T, path string) *editor.Session {
	t.Helper()
	e, err := editor.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func feed(e *editor.Session, keys string) {
	for _, k := range vim.Keys(keys) {
		e.Feed(k)
	}
}

func text(e *editor.Session) string {
	s := e.Text()
	return s.Slice(0, s.Len())
}

func disk(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// B1
func TestB1OpenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new.txt")
	e := open(t, path)
	if text(e) != "" {
		t.Fatalf("new file opened with %q", text(e))
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("opening created the file")
	}
	feed(e, "ihi<Esc>:w<CR>")
	if got := disk(t, path); got != "hi\n" {
		t.Fatalf("first write: %q", got)
	}
	if got := text(open(t, path)); got != "hi" {
		t.Fatalf("reopened file shows %q", got)
	}
}

// B2
func TestB2WriteIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	feed(e, "inew <Esc>:w<CR>")
	if got := disk(t, path); got != "new old\n" {
		t.Fatalf("write: %q", got)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("write changed the mode to %v", fi.Mode().Perm())
	}

	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	feed(e, "ix<Esc>:w<CR>")
	if got := disk(t, path); got != "new old\n" {
		t.Fatalf("failed write changed the file to %q", got)
	}
	if !strings.Contains(e.Message(), path) {
		t.Fatalf("message %q does not name the path", e.Message())
	}
}

// B3
func TestB3KeepsLineEndings(t *testing.T) {
	for _, c := range []struct{ before, after string }{
		{"one\r\ntwo\r\n", "one!\r\ntwo\r\n"},
		{"one\ntwo", "one!\ntwo"},
	} {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(c.before), 0o644); err != nil {
			t.Fatal(err)
		}
		e := open(t, path)
		feed(e, "A!<Esc>:w<CR>")
		if got := disk(t, path); got != c.after {
			t.Errorf("%q became %q, want %q", c.before, got, c.after)
		}
	}
}

// B4
func TestB4InvalidUTF8IsReadOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, []byte("a\xffb"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	if !strings.Contains(e.Message(), "read-only") {
		t.Fatalf("open message %q does not say read-only", e.Message())
	}
	feed(e, "ix<Esc>:w<CR>")
	if text(e) != "a\xffb" || disk(t, path) != "a\xffb" {
		t.Fatal("read-only file changed")
	}
}

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

// B27
func TestB27Scrolling(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("  %d", i))
	}
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	line := func(e *editor.Session, pos int) int { return strings.Count(text(e)[:pos], "\n") + 1 }
	for _, c := range []struct {
		keys      string
		top, line int
	}{
		{"<C-e>", 2, 2},
		{"3<C-e>", 4, 4},
		{"10G<C-y>", 1, 10},
		{"<C-d>", 9, 12},
		{"<C-d><C-d>", 9, 23},
		{"2<C-d>", 3, 3},
		{"30G<C-u>", 1, 19},
		{"<C-f>", 21, 21},
		{"<C-f><C-f>", 30, 30},
		{"30G<C-b>", 1, 22},
		{"15Gzz", 5, 15},
		{"15Gzt", 15, 15},
		{"15Gzb", 1, 15},
		{"M", 1, 11},
		{"L", 1, 22},
		{"3L", 1, 20},
		{"2H", 1, 2},
		{"30GH", 9, 9},
		{"20Gzt5<C-e>", 25, 25},
		{"G<C-d>", 9, 30},
	} {
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if top, cur := line(e, e.Top()), line(e, e.Cursor()); top != c.top || cur != c.line {
			t.Errorf("%q: top %d, cursor line %d, want %d and %d", c.keys, top, cur, c.top, c.line)
		}
	}
}

// B80
func TestB80StartWithFiles(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("one\n  two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("bee\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := editor.Start([]string{"+2", a, b})
	if err != nil {
		t.Fatal(err)
	}
	if text(e) != "one\n  two" || e.Cursor() != 6 {
		t.Fatalf("started on %q at %d, want line 2 of a at 6", text(e), e.Cursor())
	}
	feed(e, ":bn<CR>")
	if text(e) != "bee" {
		t.Fatalf(":bn showed %q, want b", text(e))
	}
}

// change writes text to path as another program would, an hour later.
func change(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
}

// B81
func TestB81WriteAsksWhenDiskChanged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one\n")
	e := open(t, path)
	feed(e, "x")
	change(t, path, "disk\n")
	feed(e, ":w<CR>")
	if !strings.Contains(e.Message(), "changed on disk") || disk(t, path) != "disk\n" {
		t.Fatalf(":w said %q and left %q, want a question and the disk text", e.Message(), disk(t, path))
	}
	feed(e, "n")
	if disk(t, path) != "disk\n" {
		t.Fatalf("n wrote %q", disk(t, path))
	}
	feed(e, ":w<CR>y")
	if disk(t, path) != "ne\n" {
		t.Fatalf("y left %q, want fude's text", disk(t, path))
	}
	change(t, path, "disk\n")
	feed(e, ":w!<CR>")
	if disk(t, path) != "ne\n" {
		t.Errorf(":w! left %q, want fude's text", disk(t, path))
	}
}

// B82
func TestB82ReloadOnFocus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one\n")
	e := open(t, path)
	change(t, path, "two\n")
	e.Focus()
	if text(e) != "two" {
		t.Fatalf("focus left %q, want the disk text", text(e))
	}
	feed(e, "x")
	change(t, path, "three\n")
	e.Focus()
	if text(e) != "wo" || !strings.Contains(e.Message(), "changed on disk and in fude") {
		t.Errorf("focus on a modified buffer left %q and said %q, want fude's text and a warning", text(e), e.Message())
	}
}

// B83
func TestB83LargeFile(t *testing.T) {
	config(t, "large_file = 1\n")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, strings.Repeat("x", 1<<20+1))
	e := start(t, path)
	if !strings.Contains(e.Message(), "over 1MB") {
		t.Errorf("opened with %q, want it to say the file is large", e.Message())
	}
}

// B95
func TestB95WriteAndQuit(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	for _, c := range []struct {
		keys, a, b string
		done       bool
	}{
		{"xZZ", "ne\n", "two\n", true},
		{"xZQ", "one\n", "two\n", true},
		{"x:bn<CR>x:bp<CR>:wq<CR>", "ne\n", "two\n", false},
		{"x:bn<CR>x:wa<CR>:qa<CR>", "ne\n", "wo\n", true},
		{"x:bn<CR>x:wqa<CR>", "ne\n", "wo\n", true},
		{"x:qa<CR>", "one\n", "two\n", false},
		{"x:qa!<CR>", "one\n", "two\n", true},
	} {
		change(t, a, "one\n")
		change(t, b, "two\n")
		e := start(t, a, b)
		feed(e, c.keys)
		if disk(t, a) != c.a || disk(t, b) != c.b || e.Done() != c.done {
			t.Errorf("%s left %q and %q, done %v, want %q and %q, done %v", c.keys, disk(t, a), disk(t, b), e.Done(), c.a, c.b, c.done)
		}
	}
	// :x writes only a modified buffer.
	e := start(t, a)
	change(t, a, "disk\n")
	feed(e, ":x<CR>")
	if disk(t, a) != "disk\n" || !e.Done() {
		t.Errorf(":x on an unmodified buffer left %q, done %v", disk(t, a), e.Done())
	}
}

// B96
func TestB96AlternateBuffer(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	change(t, a, "one\n")
	change(t, b, "two\n")
	e := start(t, a, b)
	feed(e, "<C-^>")
	if e.Message() != "no alternate buffer" {
		t.Errorf("<C-^> with no alternate said %q", e.Message())
	}
	for _, c := range [][2]string{
		{":bn<CR><C-^>", "one"}, {"<C-^>", "two"}, {"1<C-^>", "one"}, {":b #<CR>", "two"}, {":e #<CR>", "one"},
	} {
		if feed(e, c[0]); text(e) != c[1] {
			t.Errorf("%s showed %q, want %q", c[0], text(e), c[1])
		}
	}
}

// B89
func TestB89Buffers(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("aaa"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("bbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, a)
	feed(e, "x:e "+b+"<CR>")
	if text(e) != "bbb" {
		t.Fatalf(":e showed %q", text(e))
	}
	feed(e, ":ls<CR>")
	if want := `1+ "` + a + `"  2% "` + b + `"`; e.Message() != want {
		t.Errorf(":ls said %q, want %q", e.Message(), want)
	}
	feed(e, ":b 1<CR>")
	if text(e) != "aa" {
		t.Fatalf(":b 1 showed %q, want the modified a", text(e))
	}
	feed(e, ":bp<CR>")
	if text(e) != "bbb" {
		t.Fatalf(":bp showed %q", text(e))
	}
	feed(e, ":bd 1<CR>")
	if !strings.Contains(e.Message(), a) {
		t.Errorf(":bd of a modified buffer said %q, want it refused", e.Message())
	}
	feed(e, ":q<CR>")
	if e.Done() || !strings.Contains(e.Message(), a) {
		t.Errorf(":q with a modified hidden buffer: done %v, message %q", e.Done(), e.Message())
	}
	feed(e, ":bd! 1<CR>:ls<CR>")
	if want := `2% "` + b + `"`; e.Message() != want {
		t.Errorf("after :bd! 1, :ls said %q, want %q", e.Message(), want)
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
}

// windows describes each window as x,y wxh and * for the current one.
func windows(e *editor.Session) string {
	var out []string
	for _, v := range e.Views() {
		cur := ""
		if v.Current {
			cur = "*"
		}
		out = append(out, fmt.Sprintf("%s%d,%d %dx%d", cur, v.X, v.Y, v.W, v.H))
	}
	return strings.Join(out, " ")
}

// B23
func TestB23Splits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ keys, want string }{
		// A new window goes below or right, and the cursor goes to it.
		{"<C-w>s", "0,0 80x12 *0,12 80x11"},
		{"<C-w>v", "0,0 40x23 *41,0 39x23"},
		{"<C-w>s<C-w>v", "0,0 80x12 0,12 40x11 *41,12 39x11"},
		{"<C-w>s<C-w>k", "*0,0 80x12 0,12 80x11"},
		{"<C-w>v<C-w>h", "*0,0 40x23 41,0 39x23"},
		{"<C-w>v<C-w>h<C-w>l", "0,0 40x23 *41,0 39x23"},
	} {
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if got := windows(e); got != c.want {
			t.Errorf("%q: windows %q, want %q", c.keys, got, c.want)
		}
	}

	// Both windows show one buffer, so an edit in one moves the cursor in
	// the other, and an edit after it leaves it where it was.
	e := open(t, path)
	e.SetSize(80, 23)
	feed(e, "jjl<C-w>sggdd<C-w>k")
	if text(e) != "two\nthree\nfour" || e.Cursor() != 5 {
		t.Errorf("after dd above it, the other window is at %d in %q, want 5", e.Cursor(), text(e))
	}
	e = open(t, path)
	e.SetSize(80, 23)
	feed(e, "jjl<C-w>sjx<C-w>k")
	if e.Cursor() != 9 {
		t.Errorf("after x below it, the other window is at %d, want 9", e.Cursor())
	}
}

// B91
func TestB91WindowCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ keys, want string }{
		{"<C-w>s<C-w>c", "*0,0 80x23"},
		{"<C-w>s<C-w>s", "0,0 80x8 0,8 80x8 *0,16 80x7"},
		{"<C-w>s<C-w>s<C-w>o", "*0,0 80x23"},
		{"<C-w>s<C-w>s<C-w>=", "0,0 80x8 0,8 80x8 *0,16 80x7"},
		{"<C-w>s:q<CR>", "*0,0 80x23"},
		{"<C-w>v<C-w>q", "*0,0 80x23"},
	} {
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if got := windows(e); got != c.want || e.Done() {
			t.Errorf("%q: windows %q (done %v), want %q", c.keys, got, e.Done(), c.want)
		}
	}
	e := open(t, path)
	e.SetSize(80, 23)
	feed(e, "<C-w>c")
	if e.Message() != "cannot close the last window" {
		t.Errorf("closing the last window said %q", e.Message())
	}
	feed(e, ":q<CR>")
	if !e.Done() {
		t.Error(":q in the last window did not quit")
	}
}

// B21, in every window
func TestB21MatchesInEveryWindow(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a"), "xa\n")
	change(t, filepath.Join(dir, "b"), "ax\n")
	e := open(t, filepath.Join(dir, "a"))
	feed(e, ":vs "+filepath.Join(dir, "b")+"<CR>/a<CR>")
	for _, v := range e.Views() {
		if want := strings.Index(v.Text.Slice(0, v.Text.Len()), "a"); len(v.Matches) != 1 || v.Matches[0] != [2]int{want, want + 1} {
			t.Errorf("%s shows matches %v", v.Name, v.Matches)
		}
	}
}

// B36, in a terminal with no clipboard command
func TestB36TerminalClipboard(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("DISPLAY", "")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab cd\n")
	e := open(t, path)
	feed(e, `"+yiw`)
	if text, ok := e.TerminalClipboard(); !ok || text != "ab" {
		t.Fatalf(`"+yiw gave the terminal %q`, text)
	}
	if feed(e, `$"*p`); text(e) != "ab cdab" {
		t.Fatalf(`"*p put %q`, text(e))
	}
	if _, ok := e.TerminalClipboard(); ok {
		t.Fatal("the terminal was given the copy twice")
	}
}

// B37
func TestB37Wrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, strings.Repeat("a", 20)+"\nb\nc\n")
	e := open(t, path)
	// Three rows of text, ten columns wide: the first line takes two.
	e.SetSize(10, 4)
	if feed(e, "j"); e.Top() != 0 {
		t.Fatalf("on line 2 the window starts at %d, want 0", e.Top())
	}
	if feed(e, "j"); e.Top() != 21 {
		t.Fatalf("on line 3 the window starts at %d, want line 2 at 21", e.Top())
	}
	// Without wrap the view scrolls sideways to the cursor's column.
	feed(e, "gg:set nowrap<CR>$")
	if v := e.Views()[0]; v.Top != 0 || v.Left != 10 {
		t.Fatalf("on the last of 20 columns in 10 the view starts at %d, column %d, want 0 and 10", v.Top, v.Left)
	}
}

// B38
func TestB38Ruler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab\n\tx\n\n4\n5\n6\n")
	e := open(t, path)
	e.SetSize(40, 4)
	for _, c := range [][2]string{{"", "1,1 Top"}, {"j$", "2,2-9 Top"}, {"j", "3,0-1 Top"}, {"j", "4,1 33%"}, {"G", "6,1 Bot"}} {
		feed(e, c[0])
		if got := strings.Join(strings.Fields(e.Views()[0].Ruler), " "); got != c[1] {
			t.Errorf("after %q the ruler is %q, want %q", c[0], got, c[1])
		}
	}
	if feed(e, "2d"); e.Shown() != "2d" {
		t.Errorf("2d shows %q", e.Shown())
	}
	if feed(e, "<Esc>"); e.Shown() != "" {
		t.Errorf("after <Esc> it shows %q", e.Shown())
	}
}

// B39
func TestB39Set(t *testing.T) {
	e := open(t, "")
	feed(e, ":set number relative_number<CR>")
	if v := e.Views()[0]; !v.Number || !v.Relative || v.Gutter != 4 {
		t.Fatalf("after :set number relative_number the view is %+v", v)
	}
	// A window split from it starts the same, and :set changes only the
	// current one.
	feed(e, ":sp<CR>:set nonumber relative_number!<CR>")
	if vs := e.Views(); vs[1].Number || vs[1].Relative || vs[1].Gutter != 0 || !vs[0].Number {
		t.Fatalf("after :set nonumber relative_number! in a split the views are %+v", vs)
	}
	if feed(e, ":set number?<CR>"); e.Message() != "nonumber" {
		t.Errorf(":set number? said %q", e.Message())
	}
	if feed(e, ":set wide<CR>"); e.Message() != "no option named wide" {
		t.Errorf(":set wide said %q", e.Message())
	}
	// config.toml sets what windows start with.
	config(t, "number = true\n")
	if v := start(t).Views()[0]; !v.Number || v.Relative {
		t.Errorf("with number = true a window starts as %+v", v)
	}
}

// B116
func TestB116ScrollOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	var lines []string
	for i := range 20 {
		lines = append(lines, strconv.Itoa(i))
	}
	change(t, path, strings.Join(lines, "\n")+"\n")
	e := open(t, path)
	// Seven rows, and two lines kept below the cursor's.
	e.SetSize(40, 8)
	feed(e, ":set scroll_off=2<CR>")
	if feed(e, "4j"); e.Top() != 0 {
		t.Fatalf("on line 5 the view starts at %d, want 0", e.Top())
	}
	if feed(e, "j"); e.Top() != 2 {
		t.Fatalf("on line 6 the view starts at %d, want line 2 at 2", e.Top())
	}
	if feed(e, ":set scroll_off?<CR>"); e.Message() != "scroll_off=2" {
		t.Errorf(":set scroll_off? said %q", e.Message())
	}
	if feed(e, ":set scroll_off=x<CR>"); !strings.Contains(e.Message(), "takes a number") {
		t.Errorf(":set scroll_off=x said %q", e.Message())
	}
}

// B117
func TestB117CursorLine(t *testing.T) {
	e := open(t, "")
	if feed(e, ":set cursor_line<CR>"); !e.Views()[0].CursorLine {
		t.Fatal(":set cursor_line left the window without it")
	}
}

// B120, through :set
func TestB120SetIgnoreCase(t *testing.T) {
	e := open(t, "")
	feed(e, "ix ABC abc<Esc>0:set ignore_case<CR>/abc<CR>")
	if e.Cursor() != 2 {
		t.Fatalf("/abc with ignore_case went to %d, want 2", e.Cursor())
	}
}

// B121
func TestB121MatchParen(t *testing.T) {
	e := open(t, "")
	feed(e, "if(a, (b))<Esc>")
	for _, c := range []struct {
		keys string
		want []int
	}{
		{"0", nil},
		{"l", []int{1, 8}},
		{"f(", []int{5, 7}},
		{"$", []int{8, 1}},
		// In insert mode the bracket before the cursor counts.
		{"a", []int{8, 1}},
	} {
		if feed(e, c.keys); !slices.Equal(e.Views()[0].Pair, c.want) {
			t.Errorf("after %q the pair is %v, want %v", c.keys, e.Views()[0].Pair, c.want)
		}
	}
}

// B123
func TestB123GitSigns(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	change(t, path, "a\nb\nc\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	e := open(t, path)
	e.SetSize(40, 10)
	// a becomes xa and b goes, a change that also deleted, and new is added.
	feed(e, "ix<Esc>jddGonew<Esc>")
	want := map[int]editor.Sign{0: {Text: "~", Name: "git.change"}, 2: {Text: "┃", Name: "git.add"}}
	settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })

	// Without git installed, the file shows no git signs.
	t.Setenv("PATH", "")
	e = open(t, path)
	if feed(e, "ix<Esc>"); len(e.Views()[0].Signs) != 0 {
		t.Fatalf("without git the signs are %v", e.Views()[0].Signs)
	}
}

// B126
func TestB126ListingGitStatus(t *testing.T) {
	dir := t.TempDir()
	top := filepath.Join(dir, "top")
	if err := os.MkdirAll(filepath.Join(top, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"mod", "same"} {
		change(t, filepath.Join(top, f), f+"\n")
	}
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	change(t, filepath.Join(top, "mod"), "changed\n")
	change(t, filepath.Join(top, "added"), "added\n")
	git("add", "top/added")
	change(t, filepath.Join(top, "sub", "new"), "new\n")
	e := open(t, top)
	e.SetSize(40, 10)
	// A listing below the repository's root, with an untracked file below sub/.
	want := map[int]editor.Sign{0: {Text: "+", Name: "git.add"}, 1: {Text: "✹", Name: "git.change"}, 3: {Text: "?", Name: "git.delete"}}
	settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })

	// Without git installed, the listing shows no git signs.
	t.Setenv("PATH", "")
	if e = open(t, top); len(e.Views()[0].Signs) != 0 {
		t.Fatalf("without git the signs are %v", e.Views()[0].Signs)
	}
}

// B127
func TestB127ListingNotes(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "plain"), "")
	change(t, filepath.Join(dir, "run"), "")
	if err := os.Chmod(filepath.Join(dir, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("plain", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	e := open(t, dir)
	e.SetSize(40, 10)
	want := map[int]string{0: " -> plain", 2: "*"}
	if got := e.Views()[0].Notes; !maps.Equal(got, want) {
		t.Fatalf("the notes are %v, want %v", got, want)
	}
}

// B129
func TestB129Files(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"editor/editor.go", "editor/git.go", "editor/editor_test.go", "prd/decisions.md", "skip.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		change(t, filepath.Join(dir, f), f+"\n")
	}
	change(t, filepath.Join(dir, ".gitignore"), "skip.go\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "prd/decisions.md")
	e.SetSize(80, 24)
	results := func() []string {
		f, _ := e.Float()
		return strings.Fields(strings.SplitN(f.Text, "\n", 2)[1])
	}
	// The finder lists the files git does not ignore.
	feed(e, ":files<CR>")
	settle(t, e, func() bool { return len(results()) == 5 })
	feed(e, "edgo")
	settle(t, e, func() bool {
		return slices.Equal(results(), []string{"editor/editor.go", "editor/git.go", "editor/editor_test.go"})
	})
	if feed(e, "<C-n><CR>"); e.Views()[0].Name != "editor/git.go" || e.Mode() != vim.Normal {
		t.Fatalf("<C-n><CR> opened %q in %v mode", e.Views()[0].Name, e.Mode())
	}
	// <Esc> closes the finder and opens nothing.
	feed(e, ":files<CR>x<Esc>")
	if _, ok := e.Float(); ok || e.Views()[0].Name != "editor/git.go" || text(e) != "editor/git.go" {
		t.Fatalf("after <Esc> the finder shows %v and the window has %q", ok, e.Views()[0].Name)
	}
}

// B130
func TestB130FilesPreview(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a"), "one\ntwo\n")
	change(t, filepath.Join(dir, "b"), "three\n")
	change(t, filepath.Join(dir, "c.go"), "package c\n")
	t.Chdir(dir)
	e := open(t, "a")
	e.SetSize(100, 24)
	feed(e, ":files<CR>")
	doc := func() string { f, _ := e.Float(); return f.Doc }
	settle(t, e, func() bool { return doc() == "one\ntwo" })
	feed(e, "<C-n>")
	settle(t, e, func() bool { return doc() == "three" })
	// A file's preview has its filetype's highlights.
	feed(e, "<C-n>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return f.Doc == "package c" && slices.ContainsFunc(f.DocSpans, func(s syntax.Span) bool { return s.Name == "keyword" })
	})
	// A narrow screen has no room for it.
	if e.SetSize(50, 24); doc() != "" {
		t.Fatalf("on 50 columns the preview is %q", doc())
	}
}

// B131
func TestB131CommandCompletion(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "editor"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "editor", "files.go"), "")
	change(t, filepath.Join(dir, "readme"), "")
	t.Chdir(dir)
	config(t, "autocomplete = true")
	e := start(t, "readme")
	e.SetSize(80, 24)
	for _, c := range []struct{ keys, line string }{
		// <Tab> puts the best match in the line, and <C-e> takes it back.
		{":ti<Tab>", ":timeline"},
		{"<C-e>", ":ti"},
		// A path completes in the directory typed so far.
		{"<Esc>:e ed<Tab>", ":e editor/"},
		{"f<Tab>", ":e editor/files.go"},
		// <S-Tab> goes back from the typed word to the last match.
		{"<Esc>:set nu<S-Tab>", ":set relative_number"},
	} {
		if feed(e, c.keys); e.Line() != c.line {
			t.Fatalf("after %q the line is %q, want %q", c.keys, e.Line(), c.line)
		}
	}
	// With autocomplete, the menu shows as the line is typed.
	feed(e, "<Esc>:bu")
	if f, ok := e.Float(); !ok || !strings.HasPrefix(f.Text, "buffer") {
		t.Fatalf("typing :bu shows the menu %q", f.Text)
	}
	// A path's menu shows once its directory is read.
	feed(e, "<Esc>:e ed")
	settle(t, e, func() bool { f, ok := e.Float(); return ok && strings.HasPrefix(f.Text, "editor/\n") })
}

// B134
func TestB134Grep(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.go"), "one\n  two Needle\n")
	change(t, filepath.Join(dir, "b.txt"), "needles\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "b.txt")
	e.SetSize(100, 24)
	results := func() []string {
		f, _ := e.Float()
		lines := strings.Split(f.Text, "\n")[1:]
		return slices.DeleteFunc(lines, func(l string) bool { return l == "" })
	}
	// A query in lower case ignores case, and one with a capital does not.
	feed(e, ":grep needle<CR>")
	settle(t, e, func() bool { return len(results()) == 2 })
	feed(e, "<BS><BS><BS><BS><BS><BS>Needle")
	settle(t, e, func() bool { return slices.Equal(results(), []string{"a.go:2: two Needle"}) })
	// <CR> opens the file on the match.
	if feed(e, "<CR>"); e.Views()[0].Name != "a.go" || e.Cursor() != 10 {
		t.Fatalf("<CR> opened %q at %d", e.Views()[0].Name, e.Cursor())
	}
}

// B136
func TestB136FormatOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "keep\nx here\n")
	config(t, "format_on_save = true\n[filetype.txt]\nformat = [[\"sed\", \"s/x/y/\"], [\"sed\", \"s/here/there/\"]]")
	e := start(t, path)
	// The commands run in turn, and the cursor on a line they keep stays.
	feed(e, "ll:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "keep\ny there\n" })
	if text(e) != "keep\ny there" || e.Cursor() != 2 || e.Views()[0].Modified {
		t.Fatalf("after :w the buffer has %q, the cursor at %d", text(e), e.Cursor())
	}
	// A formatter that fails leaves the text, which is written anyway.
	config(t, "format_on_save = true\n[filetype.txt]\nformat = [[\"false\"]]")
	e = start(t, path)
	feed(e, "x:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "eep\ny there\n" })
}

// B138
func TestB138Hunks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	change(t, path, "a\nb\nc\nd\ne\n")
	for _, args := range [][]string{{"init", "-q"}, {"add", "f"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	e := open(t, path)
	e.SetSize(40, 10)
	feed(e, "jrBjjrD")
	settle(t, e, func() bool { return len(e.Views()[0].Signs) == 2 })
	line := func() int { return e.Text().LineOf(e.Cursor()) }
	for _, c := range []struct {
		keys string
		line int
	}{{"gg]h", 1}, {"]h", 3}, {"[h", 1}, {"]H", 3}, {"[H", 1}, {"gg2]h", 3}} {
		if feed(e, c.keys); line() != c.line {
			t.Errorf("%s went to line %d, want %d", c.keys, line(), c.line)
		}
	}
	// The preview shows the hunk's old and new lines.
	feed(e, "[h<Plug>(git.preview)")
	if f, ok := e.Float(); !ok || f.Text != "-b\n+B" {
		t.Fatalf("the preview is %q", f.Text)
	}
	// gH puts the hunks in range back as the index has them.
	if feed(e, "<Esc>]hVgH"); text(e) != "a\nB\nc\nd\ne" {
		t.Fatalf("VgH on line 4 left %q", text(e))
	}
}

// B139
func TestB139Flash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab cd\n")
	e := open(t, path)
	flashed := func() []int {
		for _, d := range e.Views()[0].Decorations {
			if d.Name == "flash" {
				return []int{d.Start, d.End}
			}
		}
		return nil
	}
	for _, c := range []struct {
		keys string
		want []int
	}{
		{"yiw", []int{0, 2}},
		{"p", []int{1, 3}},
		{"x", nil},
		{"u", []int{2, 3}},
		{"uu<C-r>", []int{1, 3}},
	} {
		if feed(e, c.keys); !slices.Equal(flashed(), c.want) {
			t.Errorf("after %q the flash is %v, want %v", c.keys, flashed(), c.want)
		}
	}
	// It goes after a moment.
	settle(t, e, func() bool { return flashed() == nil })
}

// B140
func TestB140Navigate(t *testing.T) {
	bin, log := t.TempDir(), filepath.Join(t.TempDir(), "tmux.log")
	script := "#!/bin/sh\necho \"$@\" >> " + log + "\n"
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TMUX_PANE", "%3")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\n")
	e := open(t, path)
	e.SetSize(80, 24)
	// :vsplit leaves the cursor in the right window, so left moves within
	// fude, and left again goes to tmux's pane.
	feed(e, ":vsplit<CR><Plug>(window.left)")
	if v := e.Views(); !v[0].Current || v[1].Current {
		t.Fatal("<Plug>(window.left) did not move to the left window")
	}
	if feed(e, "<Plug>(window.left)"); disk(t, log) != "select-pane -t %3 -L\n" {
		t.Fatalf("tmux heard %q", disk(t, log))
	}
}

// B124
func TestB124Listing(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "a.txt"), "A\n")
	change(t, filepath.Join(dir, "b.txt"), "B\n")
	change(t, filepath.Join(dir, "sub", "c"), "C\n")
	e := open(t, filepath.Join(dir, "a.txt"))
	line := func() string {
		s, c := e.Text(), e.Cursor()
		return s.Slice(s.LineStart(s.LineOf(c)), vim.LineEnd(s, c))
	}
	for _, c := range []struct{ keys, text, line string }{
		// - lists the file's directory with the cursor on the file.
		{"-", "a.txt\nb.txt\nsub/", "a.txt"},
		{"G<CR>", "c", "c"},
		{"<CR>", "C", "C"},
		{"-", "c", "c"},
		{"-", "a.txt\nb.txt\nsub/", "sub/"},
		{"gg<CR>", "A", "A"},
		// A count goes up that many levels from the file.
		{":e " + filepath.Join(dir, "sub", "c") + "<CR>2-", "a.txt\nb.txt\nsub/", "sub/"},
	} {
		if feed(e, c.keys); text(e) != c.text || line() != c.line {
			t.Fatalf("after %q the text is %q on %q, want %q on %q", c.keys, text(e), line(), c.text, c.line)
		}
	}
	// 1- and _ list the working directory.
	t.Chdir(filepath.Join(dir, "sub"))
	if feed(e, "1-"); text(e) != "c" {
		t.Fatalf("1- listed %q", text(e))
	}
}

// B125
func TestB125EditListing(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt", "keep.txt", "old/x", "old/y"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		change(t, filepath.Join(dir, f), f+"\n")
	}
	e := open(t, filepath.Join(dir, "a.txt"))
	// a.txt becomes z.txt, b.txt and old/ go, and new.txt and new/ come.
	feed(e, "-ggcwz<Esc>jddjjddOnew.txt<CR>new/<Esc>:w<CR>")
	if want := "rename a.txt to z.txt, delete b.txt, delete old/ and its 2 files, create new.txt, create new/? (y/n)"; e.Message() != want {
		t.Fatalf(":w asked %q, want %q", e.Message(), want)
	}
	if feed(e, "n"); disk(t, filepath.Join(dir, "a.txt")) != "a.txt\n" {
		t.Fatal("answering n changed the directory")
	}
	feed(e, ":w<CR>y")
	for path, want := range map[string]string{"z.txt": "a.txt\n", "keep.txt": "keep.txt\n", "new.txt": ""} {
		if got := disk(t, filepath.Join(dir, path)); got != want {
			t.Errorf("%s holds %q, want %q", path, got, want)
		}
	}
	for _, gone := range []string{"a.txt", "b.txt", "old"} {
		if _, err := os.Stat(filepath.Join(dir, gone)); err == nil {
			t.Errorf("%s is still there", gone)
		}
	}
	if info, err := os.Stat(filepath.Join(dir, "new")); err != nil || !info.IsDir() {
		t.Errorf("new/ is not a directory: %v", err)
	}
	if text(e) != "keep.txt\nnew/\nnew.txt\nz.txt" || e.Views()[0].Modified {
		t.Errorf("after :w the listing is %q", text(e))
	}
	// The renamed file's buffer follows it.
	if feed(e, ":b 1<CR>"); e.Views()[0].Name != filepath.Join(dir, "z.txt") {
		t.Errorf("the buffer of a.txt is named %q", e.Views()[0].Name)
	}
}

// B118
func TestB118List(t *testing.T) {
	e := open(t, "")
	if feed(e, ":set list<CR>"); !e.Views()[0].List {
		t.Fatal(":set list left the window without it")
	}
}

// B31
func TestB31Timeline(t *testing.T) {
	e := open(t, filepath.Join(t.TempDir(), "f"))
	feed(e, "ione<Esc>uitwo<Esc>:timeline<CR>")
	lines := strings.Split(text(e), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "  0") || !strings.HasPrefix(lines[1], "  1") ||
		!strings.HasPrefix(lines[2], "* 2") || !strings.HasSuffix(lines[2], "(from 0)") {
		t.Fatalf(":timeline listed %q", lines)
	}
	feed(e, ":q<CR>:restore 1<CR>")
	if text(e) != "one" {
		t.Fatalf(":restore 1 left %q, want one", text(e))
	}
	feed(e, ":timeline<CR>")
	lines = strings.Split(text(e), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[3], "* 3") || !strings.HasSuffix(lines[3], "restore 1") {
		t.Errorf("after :restore, :timeline listed %q, want a new state 3 and state 2 kept", lines)
	}
	// In a timeline window, :restore takes the state on the cursor's line.
	feed(e, "3G:restore<CR>:q<CR>")
	if text(e) != "two" {
		t.Errorf(":restore on the line of state 2 left %q, want two", text(e))
	}
	// A command that stands for an operator, as x for dl, shows as typed.
	feed(e, "x:timeline<CR>")
	if lines = strings.Split(text(e), "\n"); !strings.HasSuffix(lines[len(lines)-1], " x") {
		t.Errorf("after x, :timeline listed %q", lines)
	}

	// In the window, ] moves the text a state later, <CR> restores the
	// state on top of the newest, and d diffs it against now.
	path := filepath.Join(t.TempDir(), "g")
	e = open(t, path)
	source := func() string {
		for _, v := range e.Views() {
			if v.Name == path {
				return v.Text.Slice(0, v.Text.Len())
			}
		}
		return ""
	}
	feed(e, "ione<Esc>uitwo<Esc>:timeline<CR>]")
	if source() != "one" {
		t.Fatalf("] left %q, want state 1", source())
	}
	feed(e, "<CR>")
	lines = strings.Split(text(e), "\n")
	if source() != "one" || len(lines) != 4 || !strings.HasPrefix(lines[3], "* 3") {
		t.Fatalf("<CR> left %q and listed %q, want state 1 restored as state 3", source(), lines)
	}
	feed(e, "ggd")
	if want := "@@ -1 +1 @@\n-\n+one"; text(e) != want {
		t.Errorf("d on state 0 showed %q, want %q", text(e), want)
	}
}

// B32
func TestB32Diff(t *testing.T) {
	e := open(t, filepath.Join(t.TempDir(), "f"))
	feed(e, "ia<CR>one<CR>c<Esc>uia<CR>two<CR>c<Esc>:diff 1<CR>")
	if want := "@@ -1 +1 @@\n a\n-one\n+two\n c"; text(e) != want {
		t.Errorf(":diff 1 showed %q, want %q", text(e), want)
	}
}

// B35
func TestB35TimeOrder(t *testing.T) {
	run(t, [][3]string{
		{"x", "A1<Esc>uA2<Esc>g-", "x|1"},
		{"x", "A1<Esc>uA2<Esc>g-g-", "|x"},
		{"x", "A1<Esc>uA2<Esc>g-g-g+", "x|1"},
		{"x", "A1<Esc>uA2<Esc>g-g-g+g+", "x|2"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>", "x|1"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>:later<CR>", "x1|2"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>uA9<Esc>g-", "x12|3"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>uA9<Esc>g-g-", "x1|2"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 1f<CR>", "x|1"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 1f<CR>:earlier 1f<CR>", "|x"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 2f<CR>:later 1f<CR>", "x|1"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 2f<CR>:later 2f<CR>", "x1|2"},
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

// B34
func TestB34Recovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The session ends without :q, as a crash would.
	feed(open(t, path), "A two<Esc>:w<CR>A three<Esc>")

	e := open(t, path)
	if !strings.Contains(e.Message(), ":recover") || text(e) != "one two" {
		t.Fatalf("after a crash, opening said %q with %q", e.Message(), text(e))
	}
	feed(e, ":recover<CR>")
	if text(e) != "one two three" {
		t.Errorf(":recover left %q", text(e))
	}

	// Declining keeps the change on the timeline as a branch.
	e = open(t, path)
	feed(e, ":timeline<CR>")
	if lines := strings.Split(text(e), "\n"); len(lines) != 3 || !strings.HasPrefix(lines[1], "* 1") {
		t.Errorf("without :recover, :timeline listed %q, want the unsaved state kept", lines)
	}
}

// run opens a file holding each case's text, feeds its keys and compares the
// text with | at the cursor.
func run(t *testing.T, cases [][3]string) {
	t.Helper()
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(c[0]), 0o644); err != nil {
			t.Fatal(err)
		}
		e := open(t, path)
		feed(e, c[1])
		got := text(e)
		if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B5
func TestB5QuitRefusesUnsaved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	e := open(t, path)
	feed(e, "ix<Esc>:q<CR>")
	if e.Done() || !strings.Contains(e.Message(), path) {
		t.Fatalf("quit with unsaved changes: done %v, message %q", e.Done(), e.Message())
	}
	feed(e, ":q!<CR>")
	if !e.Done() {
		t.Fatal(":q! did not quit")
	}
}

// captures waits until the current window shows highlights, and returns the
// captures of each highlighted piece of text, joined by spaces.
func captures(t *testing.T, e *editor.Session) map[string]string {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		for _, v := range e.Views() {
			if v.Current && len(v.Highlights) > 0 {
				out := map[string]string{}
				for _, s := range v.Highlights {
					text := v.Text.Slice(s.Start, s.End)
					out[text] = strings.TrimSpace(out[text] + " " + s.Name)
				}
				return out
			}
		}
		select {
		case f := <-e.Queue():
			f()
		case <-deadline:
			t.Fatal("no highlights after 5s")
		}
	}
}

// B46, and B43 for the edit
func TestB46Highlighting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package main\n\nfunc main() {\n\tx := len(\"hi\") // note\n}\n")
	e := start(t, path)
	e.SetSize(80, 10)
	got := captures(t, e)
	// len is a call, a builtin and an identifier, and the identifier pattern
	// comes last in the query.
	for text, name := range map[string]string{"package": "keyword", "func": "keyword", `"hi"`: "string", "// note": "comment", "len": "variable"} {
		if got[text] != name {
			t.Errorf("%s shows as %q, want %q", text, got[text], name)
		}
	}
	// The edit lands before any reparse, and the highlights it shows are the
	// old parse's moved through it.
	feed(e, "Ox<Esc>")
	if got := captures(t, e); got["package"] != "keyword" {
		t.Errorf("after an edit above it, package shows as %q", got["package"])
	}
}

// B40
func TestB40SyntaxObjects(t *testing.T) {
	src := "package p\n\ntype T struct {\n\tA int\n\tB string\n}\n\nfunc F(a int, b string) error {\n\tx := g(a, b)\n\treturn nil\n}\n"
	typ := "type T struct {\n\tA int\n\tB string\n}"
	fn := "func F(a int, b string) error {\n\tx := g(a, b)\n\treturn nil\n}"
	for _, c := range [][2]string{
		{"9G^dif", "package p\n\n" + typ + "\n\nfunc F(a int, b string) error {\n\t\n}"},
		{"9G^vifd", "package p\n\n" + typ + "\n\nfunc F(a int, b string) error {\n\t\n}"},
		{"9Gdaf", "package p\n\n" + typ + "\n\n"},
		{"4G^dic", "package p\n\ntype T struct {\n\t\n}\n\n" + fn},
		{"4Gdac", "package p\n\n\n\n" + fn},
		{"8Gf(ldia", "package p\n\n" + typ + "\n\nfunc F(, b string) error {\n\tx := g(a, b)\n\treturn nil\n}"},
		{"8Gf(ldaa", "package p\n\n" + typ + "\n\nfunc F(b string) error {\n\tx := g(a, b)\n\treturn nil\n}"},
		{"8Gfbdaa", "package p\n\n" + typ + "\n\nfunc F(a int) error {\n\tx := g(a, b)\n\treturn nil\n}"},
	} {
		path := filepath.Join(t.TempDir(), "a.go")
		change(t, path, src)
		e := start(t, path)
		e.SetSize(80, 20)
		captures(t, e)
		if feed(e, c[0]); text(e) != c[1] {
			t.Errorf("%s left %q, want %q", c[0], text(e), c[1])
		}
	}
}

// B41
func TestB41QueryOverrides(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "queries", "go")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "highlights.scm"), []byte("(comment) @keyword\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p // note\n")
	e := start(t, path)
	e.SetSize(80, 10)
	if got := captures(t, e); len(got) != 1 || got["// note"] != "keyword" {
		t.Errorf("with an override, captures are %v, want only the comment as a keyword", got)
	}

	objects := filepath.Join(dir, "textobjects.scm")
	if err := os.WriteFile(objects, []byte("(comment) @x\n((identifier) @y\n  (#foo? @y \"z\"))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e = start(t, path)
	if want := objects + " line 2: "; !strings.Contains(e.Message(), want) {
		t.Errorf("an unknown predicate said %q, want %q", e.Message(), want)
	}
}

// B45
func TestB45SyntaxNodes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc F(a int, b string) error {\n\tx := g(a, b)\n\treturn nil\n}\n")
	// Each case starts in visual mode on the a in g(a, b).
	for _, c := range [][2]string{
		{"any", "(a, b)"},
		{"ananany", "x := g(a, b)"},
		{"2any", "g(a, b)"},
		{"ananiny", "(a, b)"},
		{"ananininy", "a"},
		{"iny", "a"},
		{"]ny", "b"},
		{"[ny", "a"},
		{"an[ny", "g"},
		{"ananan]ny", "return nil"},
		{"anananany", "x := g(a, b)^J\treturn nil^J"},
	} {
		e := start(t, path)
		e.SetSize(80, 20)
		captures(t, e)
		feed(e, "4Gf(lv"+c[0]+`:reg "<CR>`)
		if want := `""   ` + c[1]; e.Message() != want {
			t.Errorf("v%s yanked %q, want %q", c[0], e.Message(), want)
		}
	}
}

// B44
func TestB44Comments(t *testing.T) {
	src := "func f() {\n\tx := 1\n\n\t\ty := 2\n}"
	for _, c := range [][3]string{
		// Each starts after "x :=", as j5l puts it.
		{src, "j5lgcc", "func f() {\n\t|// x := 1\n\n\t\ty := 2\n}"},
		{src, "j5lgcj", "func f() {\n\t// x| := 1\n\t//\n\t\ty := 2\n}"},
		{src, "j5lgc2j", "func f() {\n\t// x| := 1\n\t//\n\t// \ty := 2\n}"},
		{src, "j5lgccgcc", "func f() {\n\t|x := 1\n\n\t\ty := 2\n}"},
		{src, "j5lgc2jgc2j", "func f() {\n\tx :=| 1\n\n\t\ty := 2\n}"},
		{src, "j5lgcc.", "func f() {\n\t|x := 1\n\n\t\ty := 2\n}"},
		{src, "j5lvjgc", "func f() {\n\t// x| := 1\n\t//\n\t\ty := 2\n}"},
		{src, "j5lgcip", "|// func f() {\n// \tx := 1\n\n\t\ty := 2\n}"},
		{src, "j5lgcck.", "|// func f() {\n\t// x := 1\n\n\t\ty := 2\n}"},
		{src, "jjgcc", "func f() {\n\tx := 1\n|\n\t\ty := 2\n}"},
		{"a\n//b\n  //  c\n//", "gc3j", "|// a\n// //b\n//   //  c\n// //"},
		{"//a\n//b\n\n  //  c\n//", "gc4j", "|a\nb\n\n   c\n"},
	} {
		path := filepath.Join(t.TempDir(), "a.go")
		change(t, path, c[0])
		e := open(t, path)
		feed(e, c[1])
		if got := text(e)[:e.Cursor()] + "|" + text(e)[e.Cursor():]; got != c[2] {
			t.Errorf("%s on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B42
func TestB42Select(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc f() {\n\ta := b + c\n}\n")
	for _, c := range [][3]string{
		{"d", "package p\n\nfunc f() {\n\t :=  + \n}", ""},
		{"du", "package p\n\nfunc f() {\n\ta := b + c\n}", ""},
		{"cxy<Esc>", "package p\n\nfunc f() {\n\txy := xy + xy\n}", ""},
		{"cxy<Esc>u", "package p\n\nfunc f() {\n\ta := b + c\n}", ""},
	} {
		e := start(t, path)
		e.SetSize(80, 20)
		captures(t, e)
		feed(e, "4GV:select (identifier) @x<CR>"+c[0])
		if text(e) != c[1] {
			t.Errorf("%s on every identifier left %q, want %q", c[0], text(e), c[1])
		}
	}
	e := start(t, path)
	e.SetSize(80, 20)
	captures(t, e)
	for query, want := range map[string]string{
		"(identifier)":                     "the query captures nothing",
		`((identifier) @x (#foo? @x "y"))`: "query line 1: fude does not evaluate #foo?",
	} {
		feed(e, "4GV:select "+query+"<CR>")
		if e.Message() != want {
			t.Errorf(":select %s said %q, want %q", query, e.Message(), want)
		}
		feed(e, "<Esc>")
	}
}
