package editor_test

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/modules/blame"
	"github.com/y3owk1n/fude/modules/comment"
	"github.com/y3owk1n/fude/modules/debug"
	"github.com/y3owk1n/fude/modules/fold"
	"github.com/y3owk1n/fude/modules/format"
	gitmod "github.com/y3owk1n/fude/modules/git"
	"github.com/y3owk1n/fude/modules/grep"
	"github.com/y3owk1n/fude/modules/spell"
	"github.com/y3owk1n/fude/modules/task"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/syntax/syntaxtest"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// TestMain keeps every test's timeline logs, config and caches in a
// directory of its own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fude-test")
	if err != nil {
		panic(err)
	}
	// Go's grammar builds once into the user's cache, before the cache
	// directory moves.
	if err := syntaxtest.InstallGo(filepath.Join(dir, "data")); err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	os.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	os.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	// No test fetches a grammar.
	if err := os.MkdirAll(filepath.Join(dir, "config", "fude"), 0o700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config", "fude", "config.toml"), []byte("auto_install = false\n"), 0o600); err != nil {
		panic(err)
	}
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
	quitAtEnd(t, e)
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
	// :bn and :bp go through the buffers being edited, past help, wrapping
	// at either end, and :bd moves the window to one of them.
	e = open(t, a)
	feed(e, ":e "+b+"<CR>:h<CR>:only<CR>")
	name := func() string { return filepath.Base(e.Views()[0].Name) }
	for _, c := range []struct{ keys, want string }{
		{":bn<CR>", "a"}, {":bn<CR>", "b"}, {":bn<CR>", "a"}, {":bp<CR>", "b"}, {":bp<CR>", "a"}, {":bn<CR>", "b"}, {":bd<CR>", "a"},
	} {
		if feed(e, c.keys); name() != c.want {
			t.Errorf("%s showed %s, want %s", c.keys, name(), c.want)
		}
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
	// split_below and split_right off put a new window above or left.
	config(t, "split_below = false\nsplit_right = false\n")
	for _, c := range []struct{ keys, want string }{
		{"<C-w>s", "*0,0 80x12 0,12 80x11"},
		{"<C-w>v", "*0,0 40x23 41,0 39x23"},
	} {
		e := start(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if got := windows(e); got != c.want {
			t.Errorf("%q with split_below and split_right off: windows %q, want %q", c.keys, got, c.want)
		}
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

// B193
func TestB193WindowSizes(t *testing.T) {
	e := open(t, "")
	e.SetSize(80, 20)
	heights := func() []int {
		var out []int
		for _, v := range e.Views() {
			out = append(out, v.H)
		}
		return out
	}
	for _, c := range []struct {
		keys string
		want []int
	}{
		// The new window is below, and is the current one.
		{"<C-w>s", []int{10, 10}},
		{"3<C-w>+", []int{7, 13}},
		{"<C-w>=", []int{10, 10}},
		{":resize 5<CR>", []int{15, 5}},
		{":resize +2<CR>", []int{13, 7}},
		{"2<C-w>-", []int{15, 5}},
		// The other window keeps a row and its status line.
		{"<C-w>_", []int{2, 18}},
	} {
		if feed(e, c.keys); !slices.Equal(heights(), c.want) {
			t.Errorf("%s left the windows %v rows tall, want %v", c.keys, heights(), c.want)
		}
	}
	feed(e, "<C-w>o<C-w>v:vertical resize 30<CR>")
	if vs := e.Views(); len(vs) != 2 || vs[0].W != 49 || vs[1].W != 30 {
		t.Errorf(":vertical resize 30 left the windows %d and %d wide", vs[0].W, vs[1].W)
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

// B241
func TestB241AttachedClipboard(t *testing.T) {
	bin := t.TempDir()
	board := filepath.Join(bin, "board")
	for name, script := range map[string]string{
		"pbcopy": "cat > " + board, "wl-copy": "cat > " + board,
		"pbpaste": "cat " + board, "wl-paste": "cat " + board,
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	t.Setenv("WAYLAND_DISPLAY", "fude-test")
	change(t, board, "host")
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab cd\n")
	e := open(t, path)
	agent := e.Attach("agent", editor.Host)
	// Its copy goes to its client, and leaves the host's clipboard alone.
	feed(agent, `"+yiw`)
	if text, ok := agent.TerminalClipboard(); !ok || text != "ab" || disk(t, board) != "host" {
		t.Fatalf(`"+yiw in an attached session gave its client %q, and the host's clipboard holds %q`, text, disk(t, board))
	}
	// Its put takes its own copy, not the host's clipboard.
	if feed(agent, `$"+p`); text(e) != "ab cdab" {
		t.Fatalf(`"+p in an attached session put %q`, text(e))
	}
}

// B240
func TestB240AttachedLeaves(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "abc\n")
	e := open(t, path)
	// :qa leaves, and the change it made stays, unsaved, for the host.
	agent := e.Attach("agent", editor.Host)
	feed(agent, "x:qa<CR>")
	if !agent.Done() || e.Done() || text(e) != "bc" || !e.Views()[0].Modified {
		t.Fatalf(":qa in an attached session: it done %v, fude done %v, text %q", agent.Done(), e.Done(), text(e))
	}
	for _, keys := range []string{":q<CR>", "ZZ"} {
		agent = e.Attach("agent", editor.Host)
		if feed(agent, keys); !agent.Done() || e.Done() {
			t.Errorf("%s in an attached session: it done %v, fude done %v", keys, agent.Done(), e.Done())
		}
	}
	agent = e.Attach("agent", editor.Host)
	if feed(agent, ":restart<CR>"); agent.Done() || !strings.Contains(agent.Message(), "only the terminal") {
		t.Errorf(":restart in an attached session said %q", agent.Message())
	}
}

// B244
func TestB244GuestRefuses(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	e, log := withServerAt(t, filepath.Join(project, "a.go"), "b\na\n", "utf-8")
	e.Use(debug.New())
	e.Use(task.New())
	e.Use(gitmod.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	guest := e.Attach("guest", editor.Guest)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	theme, _ := e.Theme()
	ran := filepath.Join(t.TempDir(), "ran")
	for _, keys := range []string{
		":!touch " + ran + "<CR>", ":r !touch " + ran + "<CR>", ":%!sort<CR>", ":terminal<CR>", ":task<CR>",
		":pick tasks<CR>", ":restart<CR>", ":cd /<CR>", ":plugin reload<CR>", ":grammar install go<CR>",
		":grammar update<CR>", ":lsp stop<CR>", ":theme light<CR>", ":share<CR>", ":kick x<CR>", ":debug start<CR>", "ZR", "ghj", "gHj", "<Space>db",
	} {
		if feed(guest, keys); !strings.HasPrefix(guest.Message(), "a guest may not") {
			t.Errorf("%s in a guest said %q", keys, guest.Message())
		}
	}
	now, _ := os.Getwd()
	if name, _ := e.Theme(); now != wd || name != theme || text(e) != "b\na" || guest.Done() || len(guest.Views()) != 1 {
		t.Errorf("a guest's refused commands left the directory %s, the theme %s, the text %q, it done %v, %d windows", now, name, text(e), guest.Done(), len(guest.Views()))
	}
	if _, err := os.Stat(ran); err == nil {
		t.Error("a guest ran a program")
	}
	// The guest takes a code action that edits, and not one that runs a
	// command.
	feed(guest, "gra")
	settle(t, guest, func() bool { _, ok := guest.Float(); return ok })
	if feed(guest, "2"); guest.Message() != "a guest may not take a code action that runs a command" || text(e) != "b\na" {
		t.Errorf("a guest's code action that runs a command said %q and left %q", guest.Message(), text(e))
	}
	feed(guest, "gra")
	settle(t, guest, func() bool { _, ok := guest.Float(); return ok })
	feed(guest, "1")
	settle(t, guest, func() bool { return text(e) != "b\na" })
	if text(e) != "hi b\na" {
		t.Errorf("a guest's code action that edits left %q", text(e))
	}
	for _, keys := range []string{":pick files<CR><Esc>", ":lsp<CR>"} {
		if feed(guest, keys); strings.Contains(guest.Message(), "guest") {
			t.Errorf("%s in a guest said %q", keys, guest.Message())
		}
	}
	// A guest shows no terminal, whose keys would run on the host.
	t.Setenv("SHELL", "/bin/sh")
	feed(e, `:terminal cat<CR><C-\><C-n>`)
	views := e.Views()
	term := views[len(views)-1]
	if feed(guest, fmt.Sprintf(":b %d<CR>", term.Buffer)); guest.Message() != "a guest may not use a terminal" || guest.Views()[0].Name == term.Name {
		t.Errorf(":b on the host's terminal in a guest said %q and shows %s", guest.Message(), guest.Views()[0].Name)
	}
}

// B245
func TestB245GuestProject(t *testing.T) {
	project, away := t.TempDir(), t.TempDir()
	t.Chdir(project)
	change(t, "a.txt", "a\n")
	change(t, filepath.Join(away, "out.txt"), "out\n")
	out := filepath.Join(away, "out.txt")
	if err := os.Mkdir(".git", 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(".git", "config"), "")
	for link, to := range map[string]string{"link": away, "outlink": out} {
		if err := os.Symlink(to, link); err != nil {
			t.Fatal(err)
		}
	}
	e := open(t, "a.txt")
	// A guest that joins while the host shows a file outside starts on one
	// inside.
	feed(e, ":e "+out+"<CR>")
	guest := e.Attach("guest", editor.Guest)
	guest.SetSize(80, 24)
	if name := guest.Views()[0].Name; name != "a.txt" {
		t.Fatalf("a guest joined showing %s", name)
	}
	r := editor.Request{Buffer: e.Views()[0].Buffer, Command: editor.Command{Op: "delete", Target: editor.Target{Name: "lines"}}}
	if err := guest.Request(r); err == nil || err.Error() != out+" is outside the project" {
		t.Errorf("a guest's request naming a buffer outside said %v", err)
	}
	feed(e, ":ls<CR>")
	buffers := e.Message()
	for _, keys := range []string{
		":e " + out + "<CR>", ":e link/out.txt<CR>", ":b out<CR>", ":split " + out + "<CR>",
		":r " + out + "<CR>", ":w " + filepath.Join(away, "new.txt") + "<CR>",
		":e .<CR>/^outlink<CR><CR>", ":e .<CR>Go../made.txt<Esc>:w<CR>", "-",
		":e .git/config<CR>", ":w .git/config<CR>",
	} {
		if feed(guest, keys); !strings.HasSuffix(guest.Message(), "is outside the project") {
			t.Errorf("%s in a guest said %q", keys, guest.Message())
		}
	}
	feed(guest, "u:b a.txt<CR>")
	if v := guest.Views(); len(v) != 1 || v[0].Name != "a.txt" || text(guest) != "a" {
		t.Errorf("a guest's paths outside left %d windows, on %q holding %q", len(v), v[0].Name, text(guest))
	}
	// The only buffer the guest's paths opened is the project's listing.
	if feed(e, ":ls<CR>"); !strings.HasPrefix(e.Message(), buffers+"  ") || strings.Count(e.Message(), "  ") != strings.Count(buffers, "  ")+1 {
		t.Errorf("a guest's paths outside left the buffers %s, from %s", e.Message(), buffers)
	}
	for _, path := range []string{filepath.Join(away, "new.txt"), filepath.Join(project, "..", "made.txt")} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("a guest made %s", path)
		}
	}
	// A file not made yet in the project opens.
	if feed(guest, ":e sub/new.txt<CR>"); guest.Views()[0].Name != filepath.Join("sub", "new.txt") {
		t.Errorf(":e sub/new.txt in a guest said %q", guest.Message())
	}
	// The finder previews only files in the project.
	feed(guest, ":pick buffers<CR>out")
	settle(t, guest, func() bool {
		f, _ := guest.Float()
		return strings.HasSuffix(f.Doc, "\n\n"+out+" is outside the project")
	})
	feed(guest, "<Esc>")
	// The finder and grep find only in the project.
	feed(e, ":cd "+away+"<CR>")
	for _, keys := range []string{":pick files<CR>", ":pick grep out<CR>"} {
		if feed(guest, keys); guest.Message() != "the working directory is outside the project" {
			t.Errorf("%s in a guest, with the working directory outside, said %q", keys, guest.Message())
		}
	}
	feed(e, ":cd "+project+"<CR>")
	// A server's rename edits no file outside, even through a link in the
	// project.
	change(t, filepath.Join(away, "other.go"), "package p\n")
	if err := os.Symlink(filepath.Join(away, "other.go"), "other.go"); err != nil {
		t.Fatal(err)
	}
	served, log := withServerAt(t, filepath.Join(project, "a.go"), "a target\n", "utf-8")
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	renamer := served.Attach("guest", editor.Guest)
	feed(renamer, "wgrn<BS><BS><BS><BS><BS><BS>goal<CR>")
	settle(t, renamer, func() bool {
		return strings.Contains(renamer.Message(), "renamed") || strings.Contains(renamer.Message(), "outside")
	})
	feed(served, ":ls<CR>")
	if text(served) != "a goal" || !strings.HasSuffix(renamer.Message(), "other.go is outside the project") || strings.Contains(served.Message(), "other.go") {
		t.Errorf("a guest's rename left %q, said %q, with the buffers %s", text(served), renamer.Message(), served.Message())
	}
}

// B247
func TestB247ReadOnlyGuest(t *testing.T) {
	project := t.TempDir()
	t.Chdir(project)
	e, log := withServerAt(t, filepath.Join(project, "a.go"), `say "hi" target`+"\n", "utf-8")
	e.Use(format.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	agent := e.Attach("agent", editor.Host)
	viewer := e.Attach("viewer", editor.ReadOnly)
	proposeQuote(t, agent, 5, "hello")
	feed(e, "A!<Esc>")
	want := text(e)
	readOnly := func() bool {
		return strings.Contains(viewer.Message(), "cannot make changes, the session is read-only")
	}
	// The server answers formatting and rename off the loop.
	for _, keys := range []string{"gqq", "$bgrn<BS><BS><BS><BS><BS><BS>goal<CR>"} {
		feed(viewer, keys)
		settle(t, viewer, readOnly)
		feed(viewer, ":<Esc>")
	}
	for _, keys := range []string{"x", "<Esc>ihello", "<Esc>dd", ":accept<CR>", ":e!<CR>", ":restore 0<CR>"} {
		if feed(viewer, keys); !readOnly() {
			t.Errorf("%s in a read-only guest said %q", keys, viewer.Message())
		}
	}
	feed(viewer, "<Esc>")
	r := editor.Request{Command: editor.Command{Op: "delete", Target: editor.Target{Name: "lines"}}}
	if err := viewer.Request(r); err == nil || err.Error() != "cannot make changes, the session is read-only" {
		t.Errorf("a read-only guest's request said %v", err)
	}
	if text(e) != want {
		t.Errorf("a read-only guest left %q, want %q", text(e), want)
	}
	// It still moves, searches, yanks and proposes.
	feed(viewer, "0/hi<CR>yiw:reg 0<CR>")
	if viewer.Cursor() != 5 || !strings.Contains(viewer.Message(), "hi") {
		t.Errorf("a read-only guest's search and yank left the cursor at %d and said %q", viewer.Cursor(), viewer.Message())
	}
	proposeQuote(t, viewer, 5, "bye")
	if g := e.Views()[0].Ghosts; len(g) != 2 {
		t.Errorf("a read-only guest's proposal left the ghosts %v", g)
	}
}

// B256
func TestB256Follow(t *testing.T) {
	project, away := t.TempDir(), t.TempDir()
	t.Chdir(project)
	change(t, "a.txt", "a\n")
	change(t, "b.txt", strings.Repeat("b\n", 100))
	change(t, "c.txt", strings.Repeat("c", 300)+"\n")
	change(t, filepath.Join(away, "out.txt"), "out\n")
	config(t, "wrap = false\n")
	e := start(t, "a.txt")
	e.SetName("host")
	e.SetSize(80, 20)
	pat := e.Attach("pat", editor.Host)
	pat.SetSize(80, 20)
	feed(pat, ":e b.txt<CR>50G")
	// The window shows what pat's shows, and keeps up as pat moves.
	same := func() bool {
		v, w := e.Views()[0], pat.Views()[0]
		return v.Name == w.Name && v.Cursor == w.Cursor && v.Top == w.Top
	}
	if feed(e, ":follow pat<CR>"); !same() {
		t.Fatalf(":follow pat shows %s at %d, want pat's %s at %d", e.Views()[0].Name, e.Views()[0].Cursor, pat.Views()[0].Name, pat.Views()[0].Cursor)
	}
	if feed(pat, "90G"); !same() {
		t.Errorf("after pat moved, the follower is at %d, want %d", e.Views()[0].Cursor, pat.Views()[0].Cursor)
	}
	// It copies the layout, which window is current, and each window's left
	// column.
	feed(pat, ":vsplit c.txt<CR>$10h")
	vs, ws := e.Views(), pat.Views()
	if len(vs) != len(ws) {
		t.Fatalf("the follower has %d windows, pat %d", len(vs), len(ws))
	}
	for i := range vs {
		if vs[i].Name != ws[i].Name || vs[i].Current != ws[i].Current || vs[i].Left != ws[i].Left || vs[i].Cursor != ws[i].Cursor {
			t.Errorf("window %d of the follower is %s at %d, left %d, current %v, want pat's %s at %d, left %d, current %v", i, vs[i].Name, vs[i].Cursor, vs[i].Left, vs[i].Current, ws[i].Name, ws[i].Cursor, ws[i].Left, ws[i].Current)
		}
	}
	if ws[1].Left == 0 {
		t.Fatal("pat's window on the long line does not scroll, so the test proves nothing")
	}
	// A window too short for pat's top line scrolls to show the cursor.
	feed(pat, "<C-w>w95G")
	e.SetSize(80, 8)
	for _, v := range e.Views() {
		if v.Name == "b.txt" && (v.Cursor/2 < v.Top/2 || v.Cursor/2 >= v.Top/2+v.H-1) {
			t.Errorf("the follower's short window shows lines %d on, the cursor on %d", v.Top/2, v.Cursor/2)
		}
	}
	e.SetSize(80, 20)
	// A key typed stops following.
	feed(e, "k")
	if feed(pat, "10G"); same() || e.Message() != "stopped following pat" {
		t.Errorf("a key typed left the follower following, saying %q", e.Message())
	}
	// So does a paste.
	feed(e, ":follow pat<CR>")
	if e.Paste("x"); e.Message() != "stopped following pat" {
		t.Errorf("a paste left the follower saying %q", e.Message())
	}
	// pat leaving stops it too.
	feed(e, ":follow pat<CR>")
	feed(pat, ":qa<CR>")
	if e.Views(); e.Message() != "pat left, so following stopped" {
		t.Errorf("when pat left, the follower said %q", e.Message())
	}
	// A key's own message shows over that of following's end.
	reader := e.Attach("reader", editor.ReadOnly)
	feed(reader, ":follow host<CR>")
	if feed(reader, "x"); reader.Message() != "cannot make changes, the session is read-only" {
		t.Errorf("x in a following read-only session said %q", reader.Message())
	}
	// A guest follows only into files it may show.
	guest := e.Attach("guest", editor.Guest)
	feed(guest, ":follow host<CR>")
	feed(e, ":e "+filepath.Join(away, "out.txt")+"<CR>")
	if name := guest.Views()[0].Name; name != "b.txt" {
		t.Errorf("a guest followed the host to %s", name)
	}
}

// typosIn returns the words spell underlines in e's first window.
func typosIn(e *editor.Session) []string {
	var out []string
	t := e.Text()
	for _, d := range e.Views()[0].Decorations {
		if d.Name == "spell.bad" {
			out = append(out, t.Slice(d.Start, d.End))
		}
	}
	return out
}

// spelling adds spell to e, with the user's own words in the config
// directory.
func spelling(t *testing.T, e *editor.Session) {
	t.Helper()
	dir, err := editor.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	e.Use(spell.New(filepath.Join(dir, "words.txt")))
}

// B259
func TestB259Spell(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "words")
	change(t, list, strings.Join([]string{"the", "cat", "sat", "on", "mat", "is", "this", "comment", "has", "a", "string", "typo"}, "\n")+"\n")
	config(t, fmt.Sprintf("spell = true\nspell_words = %q\n", list))
	// Prose checks every word but names and paths, and plurals and
	// contractions of the list's words pass.
	notes := filepath.Join(dir, "notes")
	change(t, notes, "the cats sta on teh mat, isn't fooBar a_b file.go\n")
	e := start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	settle(t, e, func() bool { return len(typosIn(e)) > 0 })
	if got := typosIn(e); !slices.Equal(got, []string{"sta", "teh"}) {
		t.Errorf("spell found %q in prose", got)
	}
	// Code checks only its comments and strings.
	code := filepath.Join(dir, "a.go")
	change(t, code, "package main\n\n// this comment has a tpyo\nvar x = \"a string wth typo\"\n\nfunc mian() {}\n")
	e = start(t, code)
	spelling(t, e)
	e.SetSize(80, 20)
	settle(t, e, func() bool { return len(typosIn(e)) == 2 })
	if got := typosIn(e); !slices.Equal(got, []string{"tpyo", "wth"}) {
		t.Errorf("spell found %q in code", got)
	}
	// spell is off unless set.
	config(t, fmt.Sprintf("spell_words = %q\n", list))
	e = start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	// The loop runs long enough for a word list to be read.
	for end := time.After(300 * time.Millisecond); ; {
		if got := typosIn(e); len(got) > 0 {
			t.Fatalf("with spell off spell found %q", got)
		}
		select {
		case f := <-e.Queue():
			f()
			continue
		case <-time.After(10 * time.Millisecond):
			continue
		case <-end:
		}
		break
	}
	// A list that fails to read says so, and spell then finds nothing.
	config(t, "spell = true\nspell_words = \"/nowhere/words\"\n")
	e = start(t, notes)
	spelling(t, e)
	e.Views()
	settle(t, e, func() bool { return strings.HasPrefix(e.Message(), "spell has no word list") })
	if got := typosIn(e); len(got) > 0 {
		t.Errorf("without a word list spell found %q", got)
	}
}

// B260
func TestB260SpellKeys(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "words")
	change(t, list, "the\ncat\nsat\non\nmat\n")
	config(t, fmt.Sprintf("spell = true\nspell_words = %q\n", list))
	notes := filepath.Join(dir, "notes")
	change(t, notes, "the cta sat on teh mat\nmat sta\n")
	e := start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	settle(t, e, func() bool { return len(typosIn(e)) > 0 })
	// ]s and [s go through the words found, wrapping at the ends.
	for _, c := range []struct {
		keys string
		want int
	}{{"]s", 4}, {"]s", 15}, {"]s", 27}, {"]s", 4}, {"[s", 27}, {"[s", 15}} {
		if feed(e, c.keys); e.Cursor() != c.want {
			t.Errorf("%s left the cursor at %d, want %d", c.keys, e.Cursor(), c.want)
		}
	}
	// z= lists the list's closest words, and the one chosen replaces it.
	feed(e, "0]sz=")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "cat") })
	// The choice reaches spell off the loop, and its edit follows.
	feed(e, "<CR>")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "the cat sat") })
	// zg adds the word to words.txt, and spell passes it from then on.
	feed(e, "]szg")
	if !strings.HasPrefix(e.Message(), "added teh to ") {
		t.Errorf("zg said %q", e.Message())
	}
	path := strings.TrimPrefix(e.Message(), "added teh to ")
	if disk(t, path) != "teh\n" || slices.Contains(typosIn(e), "teh") {
		t.Errorf("after zg, words.txt holds %q and spell finds %q", disk(t, path), typosIn(e))
	}
}

// B268
func TestB268YankMessage(t *testing.T) {
	bin := t.TempDir()
	board := filepath.Join(bin, "board")
	for _, name := range []string{"pbcopy", "wl-copy"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\ncat > "+board+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+":/bin:/usr/bin")
	t.Setenv("WAYLAND_DISPLAY", "fude-test")
	e := open(t, "")
	if feed(e, ":messages yank<CR>"); e.Message() != "no messages" {
		t.Errorf(":messages yank with none said %q", e.Message())
	}
	long := strings.Repeat("word ", 40) + "end"
	feed(e, ":"+long+"<CR>")
	shown := e.Message()
	if feed(e, ":messages yank<CR>"); e.Message() != "copied the last message" || disk(t, board) != shown {
		t.Errorf(":messages yank said %q and put %q on the clipboard, want %q", e.Message(), disk(t, board), shown)
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
	// A :set with a bad argument changes nothing.
	if feed(e, ":set number bogus<CR>"); e.Views()[1].Number || e.Message() != "no option named bogus" {
		t.Errorf(":set number bogus left number %v and said %q", e.Views()[1].Number, e.Message())
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
	e.Use(gitmod.New())
	e.SetSize(40, 10)
	// a becomes xa and b goes, a change that also deleted, and new is added.
	feed(e, "ix<Esc>jddGonew<Esc>")
	want := map[int]wire.Sign{0: {Text: "~", Name: "git.change"}, 2: {Text: "┃", Name: "git.add"}}
	settle(t, e, func() bool { return maps.Equal(e.Views()[0].Signs, want) })

	// Without git installed, the file shows no git signs.
	t.Setenv("PATH", "")
	e = open(t, path)
	e.Use(gitmod.New())
	if feed(e, "ix<Esc>"); len(e.Views()[0].Signs) != 0 {
		t.Fatalf("without git the signs are %v", e.Views()[0].Signs)
	}
}

// B221
func TestB221StageHunk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "a\nb\nc\n")
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
		if err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	e := open(t, path)
	e.Use(gitmod.New())
	e.SetSize(80, 10)
	feed(e, "rAGrC")
	settle(t, e, func() bool { return len(e.Views()[0].Signs) == 2 })
	// ghh stages the first line's hunk, and the last stays as it was.
	feed(e, "ggghh")
	settle(t, e, func() bool { return git("show", ":a.txt") == "A\nb\nc\n" && len(e.Views()[0].Signs) == 1 })
}

// B222
func TestB222GitDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "a\nb\n")
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "init")
	e := open(t, path)
	e.Use(gitmod.New())
	e.SetSize(80, 10)
	settle(t, e, func() bool { feed(e, ":gitdiff<CR>"); return e.Message() == "no changes from git's index" })
	feed(e, "jrB:gitdiff<CR>")
	if got := text(e); !strings.Contains(got, "-b\n+B") {
		t.Errorf(":gitdiff shows %q", got)
	}
	// Removed lines show in the diff.minus colour and added ones in diff.plus.
	if got := decorations(e); !slices.Contains(got, "-b diff.minus") || !slices.Contains(got, "+B diff.plus") {
		t.Errorf(":gitdiff colours %q", got)
	}
}

// B220
func TestB220Blame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "one\ntwo\n")
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Ann", "-c", "user.email=a@a"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "first words")
	e := open(t, path)
	e.Use(blame.New())
	e.SetSize(80, 10)
	note := func(want string) func() bool {
		return func() bool { return strings.Contains(e.Views()[0].Notes[e.Text().LineOf(e.Cursor())], want) }
	}
	// :blame shows who changed the cursor's line, and a line changed since
	// shows it is not committed.
	feed(e, "j:blame<CR>")
	settle(t, e, note("Ann, just now · first words"))
	feed(e, "Ax<Esc>")
	settle(t, e, note("not committed yet"))
	if feed(e, ":blame<CR>"); e.Views()[0].Notes[1] != "" {
		t.Errorf("after :blame again the line shows %q", e.Views()[0].Notes[1])
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
	want := map[int]wire.Sign{0: {Text: "+", Name: "git.add"}, 1: {Text: "✹", Name: "git.change"}, 3: {Text: "?", Name: "git.delete"}}
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
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
	}
	// The finder lists the files git does not ignore, each by its name and
	// then its directory.
	feed(e, ":pick files<CR>")
	settle(t, e, func() bool { return len(results()) == 5 })
	feed(e, "edgo")
	settle(t, e, func() bool {
		return slices.Equal(results(), []string{"editor.go  editor", "git.go  editor", "editor_test.go  editor"})
	})
	if feed(e, "<C-n><CR>"); e.Views()[0].Name != "editor/git.go" || e.Mode() != vim.Normal {
		t.Fatalf("<C-n><CR> opened %q in %v mode", e.Views()[0].Name, e.Mode())
	}
	// A screen too short for a row of matches still shows the query.
	e.SetSize(80, 5)
	feed(e, ":pick files<CR>zzz")
	if f, _ := e.Float(); !strings.HasPrefix(f.Text, "› zzz") {
		t.Errorf("on a short screen the finder shows %q", f.Text)
	}
	feed(e, "<Esc>")
	e.SetSize(80, 24)
	// <Esc> closes the finder and opens nothing.
	feed(e, ":pick files<CR>x<Esc>")
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
	change(t, filepath.Join(dir, "d"), strings.Repeat("x", 10<<10)+"\x00")
	change(t, filepath.Join(dir, "e"), strings.Repeat("y", 1<<20)+"\n")
	t.Chdir(dir)
	e := open(t, "a")
	e.SetSize(100, 24)
	feed(e, ":pick files<CR>")
	doc := func() string { f, _ := e.Float(); return f.Doc }
	settle(t, e, func() bool { return doc() == "a\n\none\ntwo" })
	feed(e, "<C-n>")
	settle(t, e, func() bool { return doc() == "b\n\nthree" })
	// A file's preview has its filetype's highlights.
	feed(e, "<C-n>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return f.Doc == "c.go\n\npackage c" && slices.ContainsFunc(f.DocSpans, func(s syntax.Span) bool { return s.Name == "keyword" })
	})
	// A zero byte in the first 16KB makes a binary file, and a long line
	// shows its start.
	feed(e, "<C-n>")
	settle(t, e, func() bool { return doc() == "d\n\nbinary file" })
	feed(e, "<C-n>")
	settle(t, e, func() bool { return strings.HasPrefix(doc(), "e\n\nyyyy") && len(doc()) < 1000 })
	// A narrow screen has no room for it.
	if e.SetSize(50, 24); doc() != "" {
		t.Fatalf("on 50 columns the preview is %q", doc())
	}
}

// B131, B290
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
	e.Use(task.New())
	e.Use(debug.New())
	e.SetSize(80, 24)
	for _, c := range []struct{ keys, line string }{
		// <Tab> puts the best match in the line, and <C-e> takes it back.
		{":ti<Tab>", ":timeline"},
		{"<C-e>", ":ti"},
		// A path completes in the directory typed so far.
		{"<Esc>:e ed<Tab>", ":e editor/"},
		{"f<Tab>", ":e editor/files.go"},
		// <S-Tab> goes back from the lit first match to the last.
		{"<Esc>:set case<S-Tab>", ":set ignore_case"},
	} {
		if feed(e, c.keys); e.Line() != c.line {
			t.Fatalf("after %q the line is %q, want %q", c.keys, e.Line(), c.line)
		}
	}
	lit := func(want string) bool {
		f, _ := e.Float()
		return slices.ContainsFunc(f.Spans, func(s syntax.Span) bool { return s.Name == "menu.selected" && f.Text[s.Start:s.End] == want })
	}
	// <CR> on a name that is no command runs the menu's first, and on one
	// that is a command runs it as typed.
	if feed(e, "<Esc>:timel<CR>"); len(e.Views()) != 2 {
		t.Fatalf(":timel<CR> left %d windows, want the timeline's too", len(e.Views()))
	}
	feed(e, ":q<CR>")
	if feed(e, ":ls<CR>"); !strings.Contains(e.Message(), `"readme"`) {
		t.Errorf(":ls<CR> said %q, want :ls to list the buffers, not :lsp", e.Message())
	}
	// A command typed whole, or by a short name, shows its own item lit.
	if feed(e, ":w"); !lit("write") {
		t.Errorf(":w lights none of the menu, want write")
	}
	// An argument takes the menu's lit item too, and <C-e> keeps what is
	// typed, as for a new file.
	if feed(e, "<Esc>:debug sta<CR>"); !strings.HasPrefix(e.Message(), "no launch settings") {
		t.Errorf(":debug sta<CR> said %q, want :debug start to run", e.Message())
	}
	feed(e, ":e rea")
	settle(t, e, func() bool { return lit("readme") })
	if feed(e, "<CR>"); e.Views()[0].Name != "readme" {
		t.Errorf(":e rea<CR> opened %s, want readme", e.Views()[0].Name)
	}
	feed(e, ":e rea")
	settle(t, e, func() bool { return lit("readme") })
	if feed(e, "<C-e><CR>"); e.Views()[0].Name != "rea" {
		t.Errorf(":e rea<C-e><CR> opened %s, want the new file rea", e.Views()[0].Name)
	}
	feed(e, ":b readme<CR>")
	// A short name matches as its command, which shows its short names.
	feed(e, "<Esc>:ls")
	if f, _ := e.Float(); !regexp.MustCompile(`(?m)^buffers +ls$`).MatchString(f.Text) || !lit("buffers") {
		t.Errorf(":ls shows %q, want buffers lit with ls beside it", f.Text)
	}
	feed(e, "<Esc>")
	// Other sessions complete for :follow and guests for :kick.
	e.Attach("pat", editor.Host)
	e.Attach("guest", editor.Guest).SetName("sam")
	for _, c := range []struct{ keys, line string }{
		{"<Esc>:follow p<Tab>", ":follow pat"},
		{"<Esc>:kick <Tab>", ":kick sam"},
	} {
		if feed(e, c.keys); e.Line() != c.line {
			t.Errorf("after %q the line is %q, want %q", c.keys, e.Line(), c.line)
		}
	}
	// Tasks complete off the loop as the line is typed.
	if err := os.MkdirAll(".fude", 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(".fude", "tasks.toml"), "[task.build]\ncommand = \"true\"\n")
	feed(e, "<Esc>:task b")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.HasPrefix(f.Text, "build") })
	// A menu that comes later lights its item too.
	if !lit("build") {
		t.Error(":task b lights none of its menu, want build")
	}

	// With autocomplete, the menu shows as the line is typed.
	feed(e, "<Esc>:bu")
	if f, ok := e.Float(); !ok || !strings.HasPrefix(f.Text, "buffer") {
		t.Fatalf("typing :bu shows the menu %q", f.Text)
	}
	// A long menu shows its best matches first, lighting the first, and
	// <C-n> goes on to the second.
	feed(e, "<Esc>:h plug")
	f, _ := e.Float()
	items := strings.Split(f.Text, "\n")
	if !lit(items[0]) {
		t.Errorf("the menu %q lights none of its first", items[:2])
	}
	if feed(e, "<C-n>"); e.Line() != ":h "+items[1] {
		t.Errorf("the menu starts with %q, and <C-n> put %q in the line", items[:2], e.Line())
	}
	// A path's menu shows once its directory is read.
	feed(e, "<Esc>:e ed")
	settle(t, e, func() bool { f, ok := e.Float(); return ok && strings.HasPrefix(f.Text, "editor/\n") })
}

// B257
func TestB257Cdo(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "foo one\nfoo two\nkeep\n")
	change(t, filepath.Join(dir, "b.txt"), "foo three\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "a.txt")
	e.Use(grep.New())
	e.SetSize(100, 24)
	feed(e, ":pick grep foo<CR>")
	// The query and three matches.
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Count(f.Text, "foo") == 4 })
	feed(e, "<C-q>:cclose<CR>")
	texts := func() (out []string) {
		for _, name := range []string{"a.txt", "b.txt"} {
			feed(e, ":b "+name+"<CR>")
			out = append(out, text(e))
		}
		return out
	}
	// :cfdo runs once in each file, at its first place.
	if feed(e, ":cfdo normal Ix<CR>"); e.Message() != "ran :normal Ix in 2 files" {
		t.Errorf(":cfdo said %q", e.Message())
	}
	if got := texts(); !slices.Equal(got, []string{"xfoo one\nfoo two\nkeep", "xfoo three"}) {
		t.Errorf(":cfdo left %q", got)
	}
	// A command that fails stops there and says why.
	if feed(e, ":cdo s/nowhere/x/<CR>"); strings.HasPrefix(e.Message(), "ran") {
		t.Errorf(":cdo with a failing command said %q", e.Message())
	}
	// :cdo runs at each place, which stays on its text as the lines before
	// it go.
	if feed(e, ":cdo d<CR>"); e.Message() != "ran :d at 3 places in 2 files" {
		t.Errorf(":cdo said %q", e.Message())
	}
	if got := texts(); !slices.Equal(got, []string{"keep", ""}) {
		t.Errorf(":cdo d left %q", got)
	}
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
	e.Use(grep.New())
	e.SetSize(100, 24)
	results := func() []string {
		f, _ := e.Float()
		lines := strings.Split(f.Text, "\n")[1:]
		return slices.DeleteFunc(lines, func(l string) bool { return l == "" })
	}
	// A query in lower case ignores case, and one with a capital does not.
	// Each match shows under its file, as its line number and its line.
	feed(e, ":pick grep needle<CR>")
	settle(t, e, func() bool { return len(results()) == 4 })
	feed(e, "<BS><BS><BS><BS><BS><BS>Needle")
	settle(t, e, func() bool { return slices.Equal(results(), []string{"a.go", "2  two Needle"}) })
	// <CR> opens the file on the match.
	if feed(e, "<CR>"); e.Views()[0].Name != "a.go" || e.Cursor() != 10 {
		t.Fatalf("<CR> opened %q at %d", e.Views()[0].Name, e.Cursor())
	}
	// A match on a line of megabytes shows with the others.
	change(t, filepath.Join(dir, "c.txt"), "needle "+strings.Repeat("x", 2<<20)+"\n")
	feed(e, ":pick grep needle<CR>")
	settle(t, e, func() bool { r := results(); return slices.Contains(r, "b.txt") && slices.Contains(r, "c.txt") })
}

// B195
func TestB195FinderLists(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.go"), "one\n  two Needle\n")
	change(t, filepath.Join(dir, "b.txt"), "needles\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "b.txt")
	e.Use(grep.New())
	e.SetSize(100, 24)
	shown := func() int {
		f, _ := e.Float()
		return len(slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" }))
	}
	visited := func() []string {
		feed(e, ":cclose<CR>:cfirst<CR>")
		names := []string{e.Views()[0].Name}
		feed(e, ":cnext<CR>")
		return append(names, e.Views()[0].Name)
	}
	// Grep shows each match under a row for its file.
	for find, rows := range map[string]int{":pick grep needle<CR>": 4, ":pick files<CR>": 2} {
		feed(e, find)
		settle(t, e, func() bool { return shown() == rows })
		// <C-q> sends both matches to a list, which is the last list.
		if feed(e, "<C-q>"); len(e.Views()) != 2 {
			t.Fatalf("<C-q> after %s left %d windows", find, len(e.Views()))
		}
		if got := visited(); !slices.Contains(got, "a.go") || !slices.Contains(got, "b.txt") {
			t.Errorf("the list of %s went to %q", find, got)
		}
	}
}

// B202, for the finder's query
func TestB202FinderQuery(t *testing.T) {
	e := open(t, "")
	e.Use(grep.New())
	e.SetSize(100, 24)
	feed(e, ":pick grep<CR>f(")
	if f, _ := e.Float(); !strings.HasPrefix(f.Text, "› f( ") {
		t.Fatalf("typing f( in the finder makes the line %q", strings.SplitN(f.Text, "\n", 2)[0])
	}
}

// B213
func TestB213Pick(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := open(t, "a.txt")
	e.Use(grep.New())
	e.SetSize(100, 30)
	feed(e, ":e b.txt<CR>")
	rows := func() []string {
		f, _ := e.Float()
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
	}
	// recent waits for the recent files to name each of paths.
	recent := func(paths ...string) {
		settle(t, e, func() bool {
			r, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fude", "recent"))
			for _, p := range paths {
				if abs, _ := filepath.Abs(p); !strings.Contains(string(r), abs) {
					return false
				}
			}
			return true
		})
	}
	recent("a.txt", "b.txt")
	// Buffers and recent files list the files but the current one, and
	// <CR> opens the one picked.
	for _, source := range []string{"buffers", "recent"} {
		feed(e, ":pick "+source+"<CR>")
		settle(t, e, func() bool { return slices.Contains(rows(), "a.txt") && !slices.Contains(rows(), "b.txt") })
		if feed(e, "a.txt<CR>"); e.Views()[0].Name != "a.txt" {
			t.Fatalf("<CR> in :pick %s opened %q", source, e.Views()[0].Name)
		}
		feed(e, ":e b.txt<CR>")
	}
	// A recent file outside the working directory comes after its own, even
	// when it is newer.
	out := filepath.Join(t.TempDir(), "o.txt")
	change(t, out, "o\n")
	feed(e, ":e "+out+"<CR>:e b.txt<CR>")
	recent(out)
	feed(e, ":pick recent<CR>")
	settle(t, e, func() bool {
		r := rows()
		at := func(name string) int {
			return slices.IndexFunc(r, func(l string) bool { return strings.HasPrefix(l, name) })
		}
		return at("a.txt") >= 0 && at("a.txt") < at("o.txt")
	})
	feed(e, "<Esc>")
	// A name that starts with .. is still under the working directory.
	change(t, filepath.Join(dir, "..dots"), "d\n")
	feed(e, ":e ..dots<CR>:e b.txt<CR>")
	recent("..dots")
	feed(e, ":pick recent<CR>")
	settle(t, e, func() bool { return slices.Contains(rows(), "..dots") })
	feed(e, "<Esc>")
	// Files and grep are sources too.
	feed(e, ":pick grep b<CR>")
	settle(t, e, func() bool { return slices.Contains(rows(), "b.txt") })
}

// B266
func TestB266PickCommands(t *testing.T) {
	e := start(t)
	e.SetSize(100, 30)
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	// Typing what a command does finds it, and the one picked runs.
	feed(e, ":pick commands<CR>opens a window below with a new empty")
	settle(t, e, func() bool { return shows(":new") })
	if feed(e, "<CR>"); len(e.Views()) != 2 {
		t.Fatalf("picking :new left %d windows", len(e.Views()))
	}
	// One that needs an argument waits on the command line.
	feed(e, ":pick commands<CR>:cdo")
	settle(t, e, func() bool { return shows(":cdo") })
	if feed(e, "<CR>"); e.Line() != ":cdo " {
		t.Errorf("picking :cdo left the line %q", e.Line())
	}
}

// B271
func TestB271Plug(t *testing.T) {
	e := withKit(t)
	e.SetSize(100, 30)
	// A plugin's key runs with no key typed.
	feed(e, ":plug kit.gS<CR>")
	settle(t, e, func() bool { return text(e) == "!ab cd\nef" })
	// A name that is no plug says so.
	if feed(e, ":plug nope<CR>"); e.Message() != "no plug is called nope" {
		t.Errorf(":plug nope said %q", e.Message())
	}
	// :pick commands finds a plug by what it does.
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	feed(e, ":pick commands<CR>lists the directory of the file")
	settle(t, e, func() bool { return shows(":plug listing.up") })
	feed(e, "<Esc><Esc>")
	// Names complete, and fude's plugs run.
	if feed(e, ":plug listing.u<Tab>"); e.Line() != ":plug listing.up" {
		t.Errorf("<Tab> left the line %q", e.Line())
	}
	feed(e, "<CR>")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "f") })
}

// B215
func TestB215PickLists(t *testing.T) {
	config(t, "[keymap.normal]\n\"zq\" = \":set number<CR>\"\n")
	e := start(t)
	e.SetSize(100, 30)
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	// :pick alone lists the sources, and picking one opens its finder.
	feed(e, ":pick<CR>")
	settle(t, e, func() bool { return shows("keymaps") })
	feed(e, "keymaps<CR>zq")
	settle(t, e, func() bool { return shows("normal mapping  types :set<Space>number<CR>") })
	// A mapping picked runs.
	if feed(e, "<CR>"); !e.Views()[0].Number {
		t.Error("picking zq did not run it")
	}
	// It lists the machine's keys too, each with its kind and help, and a
	// motion picked runs, j here after insert mode's <Down>.
	feed(e, "ggix<CR>y<Esc>gg:pick keymaps<CR>moves down a line")
	settle(t, e, func() bool { return shows("motion          moves down a line") })
	if feed(e, "<C-n><CR>"); e.Text().LineOf(e.Cursor()) != 1 {
		t.Errorf("picking j left the cursor on line %d", e.Text().LineOf(e.Cursor()))
	}
	if feed(e, ":pick help<CR>guide-start<CR>"); !strings.HasPrefix(text(e), "# fude help") {
		t.Errorf("picking guide-start opened %q", e.Views()[len(e.Views())-1].Name)
	}
}

// B212
func TestB212QCloses(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\n")
	t.Chdir(dir)
	e := open(t, "a.txt")
	e.SetSize(100, 30)
	feed(e, "ix<Esc>")
	// A list of places, a timeline and a window fude wrote each close.
	feed(e, ":pick files<CR>")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "a.txt") })
	for _, keys := range []string{"<C-q>", ":timeline<CR>", ":grammar<CR>"} {
		if feed(e, keys); len(e.Views()) != 2 {
			t.Fatalf("%s made %d windows", keys, len(e.Views()))
		}
		if feed(e, "q"); len(e.Views()) != 1 || e.Recording() != 0 {
			t.Errorf("q after %s left %d windows, recording %q", keys, len(e.Views()), e.Recording())
		}
	}
}

// B210
func TestB210FinderIgnored(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.go"), "needle\n")
	change(t, filepath.Join(dir, "skip.go"), "needle\n")
	change(t, filepath.Join(dir, ".gitignore"), "skip.go\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := open(t, "a.go")
	e.Use(grep.New())
	e.SetSize(100, 24)
	// shown is the finder's rows, sorted, as the search's order may vary.
	shown := func() []string {
		f, _ := e.Float()
		return slices.Sorted(slices.Values(slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })))
	}
	for _, c := range []struct {
		find          string
		without, with []string
	}{
		{":pick grep needle<CR>", []string{"1  needle", "a.go"}, []string{"1  needle", "1  needle", "a.go", "skip.go"}},
		{":pick files<CR>", []string{".gitignore", "a.go"}, []string{".gitignore", "a.go", "skip.go"}},
	} {
		feed(e, c.find)
		settle(t, e, func() bool { return slices.Equal(shown(), c.without) })
		feed(e, "<C-g>")
		settle(t, e, func() bool { return slices.Equal(shown(), c.with) })
		feed(e, "<C-g>")
		settle(t, e, func() bool { return slices.Equal(shown(), c.without) })
		feed(e, "<Esc>")
	}
}

// B136
func TestB136FormatOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "keep\nx here\n")
	config(t, "format_on_save = true\n[filetype.txt]\nformat = [\"y\", \"there\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n[formatter.there]\ncommand = [\"sed\", \"s/here/there/\"]\n")
	e := start(t, path)
	e.Use(format.New())
	// The commands run in turn, and the cursor on a line they keep stays.
	feed(e, "ll:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "keep\ny there\n" })
	if text(e) != "keep\ny there" || e.Cursor() != 2 || e.Views()[0].Modified {
		t.Fatalf("after :w the buffer has %q, the cursor at %d", text(e), e.Cursor())
	}
	// A formatter that fails leaves the text, which is written anyway.
	config(t, "format_on_save = true\n[filetype.txt]\nformat = [\"false\"]")
	e = start(t, path)
	e.Use(format.New())
	feed(e, "x:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "eep\ny there\n" })
	// A filetype's format_on_save wins over the one at the top.
	config(t, "format_on_save = true\n[filetype.txt]\nformat_on_save = false\nformat = [\"y\"]\n[formatter.y]\ncommand = [\"sed\", \"s/y/z/\"]\n")
	e = start(t, path)
	e.Use(format.New())
	feed(e, "x:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "ep\ny there\n" })
}

// B234
func TestB234Format(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "keep\nx here\n")
	config(t, "[filetype.txt]\nformat = [\"y\", \"fude-no-such-formatter\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n")
	e := start(t, path)
	e.Use(format.New())
	feed(e, "j:format<CR>")
	settle(t, e, func() bool { return text(e) == "keep\ny here" })
	if !e.Views()[0].Modified || disk(t, path) != "keep\nx here\n" {
		t.Fatalf("after :format the file has %q, want it unwritten", disk(t, path))
	}
	if m := e.Message(); m != "fude-no-such-formatter not installed" {
		t.Errorf("after :format the message is %q, want the formatter not installed", m)
	}
	if feed(e, "u"); text(e) != "keep\nx here" {
		t.Errorf("u after :format left %q, want it undone in one step", text(e))
	}
}

// B171
func TestB171FirstFormatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "ab\n")
	// The first is not installed and the second needs a root, so the third,
	// and only it, runs.
	config(t, `format_on_save = true
[filetype.txt]
format = ["fude-no-such-formatter", "rooted", "upper", "lower"]
format_first = true
[formatter.rooted]
command = ["sed", "s/a/x/"]
root = ["no-such-root"]
workspace_required = true
[formatter.upper]
command = ["tr", "a-z", "A-Z"]
[formatter.lower]
command = ["tr", "A-Z", "a-z"]
`)
	e := start(t, path)
	e.Use(format.New())
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "AB\n" })
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
	e.Use(gitmod.New())
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
	// flash_time = 0 turns it off.
	config(t, "flash_time = 0\n")
	e = start(t, path)
	if feed(e, "yiw"); flashed() != nil {
		t.Errorf("with flash_time = 0 the flash is %v, want none", flashed())
	}
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

// B142
func TestB142LocalKeymaps(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a"), "A\n")
	change(t, filepath.Join(dir, "b"), "B\n")
	config(t, `[keymap.normal]
"<C-h>" = "<Plug>(window.left)"

[keymap.listing]
"<C-h>" = ""
"<C-v>" = "<Plug>(listing.vsplit)"
x = "<Nop>"

[keymap.finder]
"<C-j>" = "<Plug>(finder.next)"
`)
	t.Chdir(dir)
	e := start(t, filepath.Join(dir, "a"))
	e.SetSize(80, 24)
	feed(e, ":vsplit<CR>-")
	// A key the listing's table drops falls through to [keymap.normal].
	if feed(e, "<C-h>"); !e.Views()[0].Current {
		t.Fatal("<C-h> in a listing did not move to the left window")
	}
	// A key mapped in it runs the listing's action, and <Nop> does nothing.
	if feed(e, "<C-w>ljx<C-v>"); len(e.Views()) != 3 || text(e) != "B" {
		t.Fatalf("<C-v> on b left %d windows showing %q", len(e.Views()), text(e))
	}
	// The finder's keys are an insert mode keymap.
	feed(e, "<C-w>h:pick files<CR>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return strings.Count(f.Text, "\n") > 0 && strings.Contains(f.Text, "\nb")
	})
	if feed(e, "<C-j><CR>"); text(e) != "B" {
		t.Fatalf("<C-j><CR> in the finder opened %q", text(e))
	}
}

// B143
func TestB143SignColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\n")
	config(t, "sign_column = true")
	e := start(t, path)
	if v := e.Views()[0]; v.SignCols != 2 || v.Gutter != 2 {
		t.Fatalf("with sign_column a file with no signs has %d sign columns", v.SignCols)
	}
	if feed(e, ":set nosign_column<CR>"); e.Views()[0].SignCols != 0 {
		t.Fatal(":set nosign_column kept the sign column")
	}
}

// B144
func TestB144Leader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	for _, c := range []struct{ config, keys string }{
		// <Space> by default, and leader changes it.
		{"[keymap.normal]\n\"<Leader>x\" = \"dd\"", " x"},
		{"leader = \",\"\n[keymap.normal]\n\"<leader>x\" = \"dd\"", ",x"},
	} {
		change(t, path, "a\nb\n")
		config(t, c.config)
		e := start(t, path)
		if feed(e, c.keys); text(e) != "b" {
			t.Errorf("%q with %q left %q", c.keys, c.config, text(e))
		}
	}
}

// B147
func TestB147Branch(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init", "-q", "-b", "trunk").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	path := filepath.Join(dir, "f")
	change(t, path, "a\n")
	e := open(t, path)
	settle(t, e, func() bool { return e.Views()[0].Branch == "trunk" })
}

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

// B154
func TestB154SubstitutePreview(t *testing.T) {
	e := open(t, "")
	e.SetSize(80, 24)
	feed(e, "ifoo bar foo<CR>foo<Esc>gg")
	// While the pattern is typed, its match on the line shows.
	if feed(e, ":s/foo"); !slices.Equal(e.Views()[0].Matches, [][2]int{{0, 3}}) {
		t.Fatalf("typing :s/foo shows matches %v", e.Views()[0].Matches)
	}
	// With a replacement, each match is struck out before what replaces it,
	// and the text stays as it is.
	feed(e, "/x")
	v := e.Views()[0]
	if len(v.Ghosts) != 1 || v.Ghosts[0] != (wire.Ghost{Pos: 3, Text: "x", Style: "sub.new"}) || text(e) != "foo bar foo\nfoo" {
		t.Fatalf("typing :s/foo/x shows %v over %q", v.Ghosts, text(e))
	}
	if feed(e, "<Esc>:%s/foo/y/g"); len(e.Views()[0].Ghosts) != 3 {
		t.Fatalf(":%%s/foo/y/g previews %d replacements, want 3", len(e.Views()[0].Ghosts))
	}
	if feed(e, "<Esc>"); len(e.Views()[0].Ghosts) != 0 || text(e) != "foo bar foo\nfoo" {
		t.Fatalf("after <Esc> the preview left %v over %q", e.Views()[0].Ghosts, text(e))
	}
}

// B201
func TestB201StartInDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(dir, "a.txt"), "A\n")
	e := start(t, dir)
	if wd, _ := os.Getwd(); wd != dir {
		t.Fatalf("fude %s works in %s", dir, wd)
	}
	if feed(e, ":e a.txt<CR>"); text(e) != "A" {
		t.Errorf(":e a.txt opened %q, want the file in %s", text(e), dir)
	}
}

// B204
func TestB204PathExpansion(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	b, c := filepath.Join(dir, "b.go"), filepath.Join(dir, "c.txt")
	change(t, b, "B\n")
	change(t, c, "C\n")
	change(t, filepath.Join(dir, "d.txt"), "D\n")
	e := open(t, b)
	for _, step := range []struct{ keys, want string }{
		{":e %:h/c.txt<CR>", "C"},
		{":e #<CR>", "B"},
		{":e ~/d.txt<CR>", "D"},
		{":e %:p:h/c.%:e<CR>", "C"},
	} {
		if feed(e, step.keys); text(e) != step.want {
			t.Errorf("%s opened %q, want %q", step.keys, text(e), step.want)
		}
	}
	feed(e, `:w %:r.bak<CR>:w %:h/x\%y<CR>`)
	if disk(t, filepath.Join(dir, "c.bak")) != "C\n" || disk(t, filepath.Join(dir, "x%y")) != "C\n" {
		t.Errorf(":w %%:r.bak and :w %%:h/x\\%%y did not write c.bak and x%%y")
	}
	feed(e, ":e %:h/c.t<Tab>")
	settle(t, e, func() bool { return e.Line() == ":e "+c })
	feed(e, "<Esc>:enew<CR>:e %<CR>")
	if !strings.Contains(e.Message(), "no file for %") {
		t.Errorf("an unnamed buffer's :e %% said %q", e.Message())
	}
}

// B206
func TestB206CompletionSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "apple apricot\n")
	change(t, filepath.Join(dir, "notes.md"), "")
	e := open(t, path)
	// The nearest word comes first, and the menu names where it came from.
	feed(e, "o ap<C-x><C-o>")
	if f, _ := e.Float(); text(e) != "apple apricot\n apricot" || !strings.Contains(f.Text, "apple  [buffer]") {
		t.Fatalf("<C-x><C-o> after ap made %q with the menu %q", text(e), f.Text)
	}
	feed(e, "<Esc>o./no<C-x><C-o>")
	settle(t, e, func() bool { return strings.HasSuffix(text(e), "./notes.md") })

	config(t, `completion_sources = ["buffer"]`)
	e = start(t, path)
	if feed(e, "o./no<C-x><C-o>"); e.Message() != "no completions" {
		t.Errorf("with only buffer words, a path completed to %q", text(e))
	}
	config(t, `completion_sources = ["nope"]`)
	if e = start(t, path); !strings.Contains(e.Message(), `no source "nope"`) {
		t.Errorf("an unknown source said %q", e.Message())
	}
}

// B207
func TestB207Snippets(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "snippets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	snippets := `{"for loop": {"prefix": "for", "body": ["for ${1:i} := 0; $1 < ${2:n}; $1++ {", "\t$0", "}"]}}`
	change(t, filepath.Join(dir, "all.json"), snippets)
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "x\n")
	e := start(t, path)
	feed(e, "ofor<C-x><C-o>")
	if text(e) != "x\nfor i := 0; i < n; i++ {\n\t\n}" {
		t.Fatalf("the snippet went in as %q", text(e))
	}
	// Typing replaces a stop's placeholder everywhere it repeats as it is
	// typed. <Tab> goes on to the next stop, <S-Tab> back, and the last
	// <Tab> to the end.
	for _, step := range []struct{ keys, want string }{
		{"<C-y>j", "x\nfor j := 0; j < n; j++ {\n\t\n}"},
		{"<Tab>m<S-Tab>k", "x\nfor k := 0; k < m; k++ {\n\t\n}"},
		{"<Tab><Tab>x", "x\nfor k := 0; k < m; k++ {\n\tx\n}"},
	} {
		if feed(e, step.keys); text(e) != step.want {
			t.Errorf("%s made %q, want %q", step.keys, text(e), step.want)
		}
	}
	_, log := withServer(t, "x\n", "utf-8")
	heard(t, log, `"snippetSupport":true`, 1)
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
	// A rename that fails, into a folder that is a file, leaves the name.
	dir = t.TempDir()
	change(t, filepath.Join(dir, "a"), "a\n")
	change(t, filepath.Join(dir, "sub"), "sub\n")
	e = open(t, filepath.Join(dir, "a"))
	feed(e, "-Csub/a<Esc>:w<CR>y")
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || disk(t, filepath.Join(dir, "a")) != "a\n" || !strings.Contains(e.Message(), "rename a") {
		t.Errorf("a failed rename left %v and said %q", names, e.Message())
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

	// A formatter that answers after its buffer closed changes nothing, so
	// there is nothing to recover.
	dir := t.TempDir()
	path, done := filepath.Join(dir, "g.txt"), filepath.Join(dir, "done")
	change(t, path, "one\n")
	config(t, fmt.Sprintf("[filetype.txt]\nformat = [\"up\"]\n[formatter.up]\ncommand = [\"sh\", \"-c\", \"sed s/one/ONE/; touch %s\"]\n", done))
	e = start(t, path)
	e.Use(format.New())
	feed(e, ":format<CR>:bd!<CR>")
	settle(t, e, func() bool { _, err := os.Stat(done); return err == nil })
	for end := time.After(100 * time.Millisecond); ; {
		select {
		case f := <-e.Queue():
			f()
			continue
		case <-end:
		}
		break
	}
	if e = start(t, path); strings.Contains(e.Message(), ":recover") {
		t.Errorf("after a formatter answered a closed buffer, opening said %q", e.Message())
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
// B226
func TestB226ProjectSession(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a.txt"), "a\nline two\n")
	change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	feed(e, "j$:vsplit<CR>:e b.txt<CR>:qa<CR>")
	// fude started again with no file offers the session, and s restores
	// its windows, the current one and the cursors.
	e = start(t)
	e.SetSize(80, 24)
	e.Welcome()
	settle(t, e, func() bool { return strings.Contains(text(e), "s  restore the last session here") })
	feed(e, "s")
	vs := e.Views()
	if len(vs) != 2 || vs[0].Name != "a.txt" || vs[1].Name != "b.txt" || !vs[1].Current || vs[0].Cursor != 9 {
		t.Fatalf("s restored %d windows, the first %q at %d", len(vs), vs[0].Name, vs[0].Cursor)
	}
}

// B225
func TestB225StartScreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	change(t, path, "a\n")
	t.Chdir(dir)
	e := start(t, "a.txt")
	e.SetSize(80, 24)
	abs, err := filepath.Abs("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	settle(t, e, func() bool {
		b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fude", "recent"))
		return strings.Contains(string(b), abs)
	})
	// The start screen lists the actions, then the recent files once read.
	e.Welcome()
	if got := text(e); !strings.Contains(got, "f  find a file") || !strings.Contains(got, "q  quit") {
		t.Fatalf("the start screen shows %q", got)
	}
	settle(t, e, func() bool { return strings.Contains(text(e), "1  a.txt") })
	// A digit opens its recent file, and the window shows numbers again.
	if feed(e, "1"); e.Views()[0].Name != "a.txt" {
		t.Errorf("1 opened %q", e.Views()[0].Name)
	}
	// <CR> runs the line's key, as f opens the finder.
	e.Welcome()
	if feed(e, "<CR>"); func() bool { _, ok := e.Float(); return !ok }() {
		t.Error("<CR> on f did not open the finder")
	}
}

// B224
func TestB224Folds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "func a() {\n\tx\n\ty\n}\nfunc b() {\n\tz\n}\n")
	e := start(t, path)
	e.Use(fold.New())
	e.SetSize(80, 20)
	folds := func() map[int]int { return e.Views()[0].Folds }
	// zc in a body closes the fold of the line above it with less indent,
	// and j passes over it.
	if feed(e, "jzc"); !maps.Equal(folds(), map[int]int{0: 2}) || e.Cursor() != 0 {
		t.Fatalf("zc made folds %v with the cursor at %d", folds(), e.Cursor())
	}
	if feed(e, "j"); e.Text().LineOf(e.Cursor()) != 3 {
		t.Errorf("j over the fold went to line %d", e.Text().LineOf(e.Cursor()))
	}
	// A search into a closed fold opens it.
	if feed(e, "gg/y<CR>"); folds() != nil {
		t.Errorf("searching into the fold left %v", folds())
	}
	// zM closes every outermost fold, za opens one, and zR all.
	if feed(e, "zM"); !maps.Equal(folds(), map[int]int{0: 2, 4: 1}) {
		t.Errorf("zM made %v", folds())
	}
	if feed(e, "za"); !maps.Equal(folds(), map[int]int{4: 1}) {
		t.Errorf("za made %v", folds())
	}
	if feed(e, "zR"); folds() != nil {
		t.Errorf("zR left %v", folds())
	}
}

// B218
func TestB218Jump(t *testing.T) {
	e := open(t, "")
	e.SetSize(80, 10)
	feed(e, "ifoo bar baz bat<Esc>0")
	// Two characters label each match, never with a character that would
	// continue one, and the label goes to its match.
	feed(e, "sba")
	labels := e.Views()[0].Labels
	if len(labels) != 3 || slices.ContainsFunc(labels, func(l wire.Label) bool { return strings.Contains("rzt", l.Text) }) {
		t.Fatalf("sba labels %v", labels)
	}
	i := slices.IndexFunc(labels, func(l wire.Label) bool { return l.Pos == 10 })
	if feed(e, labels[i].Text); e.Cursor() != 8 || e.Views()[0].Labels != nil {
		t.Errorf("the label of baz went to %d", e.Cursor())
	}
	// <CR> goes to the nearest, and <Esc> leaves the cursor where it was.
	if feed(e, "0sb<CR>"); e.Cursor() != 4 {
		t.Errorf("sb<CR> went to %d, want 4", e.Cursor())
	}
	if feed(e, "sfo<Esc>"); e.Cursor() != 4 {
		t.Errorf("sfo<Esc> moved the cursor to %d", e.Cursor())
	}
	// In visual mode a jump moves the selection's end.
	feed(e, "0vsbat<CR>")
	if sel, ok := e.Selection(); !ok || sel.End != 13 {
		t.Errorf("vsbat<CR> made the selection %+v", sel)
	}
}

// B217
func TestB217StickyContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc F() {\n"+strings.Repeat("\tx()\n", 30)+"}\n")
	e := start(t, path)
	e.SetSize(80, 10)
	// Scrolled into F, its first line is pinned, with its highlights.
	feed(e, "20Gzt3j")
	settle(t, e, func() bool {
		v := e.Views()[0]
		return slices.Equal(v.Context, []int{11}) && len(v.ContextHighlights) > 0
	})
	// With the cursor on the top line, nothing covers it.
	if feed(e, "zt"); len(e.Views()[0].Context) != 0 {
		t.Errorf("with the cursor on the top line, %v is pinned", e.Views()[0].Context)
	}
}

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
	// A plain #lua-match? is a #match?, and #set! "priority" and @spell
	// change nothing. fude leaves out a pattern with a predicate it does not
	// evaluate (B46).
	src := "((comment) @keyword @spell (#set! \"priority\" 95))\n" +
		"((package_identifier) @type (#lua-match? @type \"^p$\"))\n((package_identifier) @string (#lua-match? @string \"^q\"))\n" +
		"((package_identifier) @function (#has-ancestor? @function source_file))\n"
	if err := os.WriteFile(filepath.Join(dir, "highlights.scm"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p // note\n")
	e := start(t, path)
	e.SetSize(80, 10)
	if got := captures(t, e); len(got) != 2 || got["// note"] != "keyword" || got["p"] != "type" {
		t.Errorf("with an override, captures are %v, want the comment as a keyword and p as a type", got)
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
		e.Use(comment.New())
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

// B155
func TestB155WritingAndFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a")
	change(t, path, "one\n")
	t.Chdir(dir)
	e := open(t, "a")
	// :w with a name writes a copy, and the buffer stays on its own file.
	feed(e, "x:w b<CR>")
	if disk(t, filepath.Join(dir, "b")) != "ne\n" || disk(t, path) != "one\n" || e.Views()[0].Name != "a" {
		t.Fatalf(":w b wrote %q, left a %q, and the window shows %q", disk(t, filepath.Join(dir, "b")), disk(t, path), e.Views()[0].Name)
	}
	if feed(e, ":w b<CR>"); !strings.Contains(e.Message(), "exists") {
		t.Fatalf(":w over a file that exists said %q", e.Message())
	}
	// :e keeps changes, and :e! drops them.
	if feed(e, ":e<CR>"); text(e) != "ne" {
		t.Fatalf(":e with changes left %q", text(e))
	}
	if feed(e, ":e!<CR>"); text(e) != "one" || e.Views()[0].Modified {
		t.Fatalf(":e! left %q", text(e))
	}
	// :update writes only a buffer with changes.
	change(t, path, "two\n")
	if feed(e, ":update<CR>"); disk(t, path) != "two\n" {
		t.Fatal(":update wrote a buffer with no changes")
	}
	// :new splits to a buffer with no file.
	if feed(e, ":new<CR>"); len(e.Views()) != 2 || e.Views()[1].Name != "[No Name]" {
		t.Fatalf(":new left windows %v", e.Views())
	}
	// :cd moves the working directory, and the buffers keep their files.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	feed(e, ":cd sub<CR><C-w>k")
	if wd, _ := os.Getwd(); wd != sub && !strings.HasSuffix(wd, "/sub") {
		t.Fatalf(":cd sub left the working directory %s", wd)
	}
	if feed(e, ":pwd<CR>"); !strings.HasSuffix(e.Message(), "/sub") {
		t.Fatalf(":pwd said %q", e.Message())
	}
	// a changed on disk above, so :w! writes over it.
	if feed(e, "0x:w!<CR>"); disk(t, path) != "ne\n" {
		t.Fatalf("after :cd, :w! wrote %q to a", disk(t, path))
	}
}

// B156, for the commands that run a shell
func TestB156Shell(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "in.txt"), "read\n")
	t.Chdir(dir)
	t.Setenv("SHELL", "/bin/sh")
	e := open(t, "")
	feed(e, "ic<CR>a<CR>b<Esc>")
	// A range goes through the command, and its output takes its place.
	feed(e, ":%!sort<CR>")
	settle(t, e, func() bool { return text(e) == "a\nb\nc" })
	// :r puts a file's text, or a command's output, below the line.
	feed(e, "gg:r in.txt<CR>")
	if text(e) != "a\nread\nb\nc" {
		t.Fatalf(":r in.txt made %q", text(e))
	}
	feed(e, "G:r !echo out<CR>")
	settle(t, e, func() bool { return text(e) == "a\nread\nb\nc\nout" })
	// Without a range, the output is a message.
	feed(e, ":!echo hi<CR>")
	settle(t, e, func() bool { return e.Message() == "hi" })
}

// B157
func TestB157InfoViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one\ntwo\nthree\n")
	e := open(t, path)
	e.SetSize(80, 24)
	// :messages lists the messages shown so far.
	for _, keys := range []string{":set number?<CR>", ":set wrap?<CR>"} {
		feed(e, keys)
		e.Message()
	}
	if feed(e, ":messages<CR>"); text(e) != "nonumber\nwrap" {
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

// B162
func TestB162Help(t *testing.T) {
	e := open(t, "")
	e.SetSize(100, 40)
	line := func() string {
		t := e.Text()
		ls := t.LineStart(t.LineOf(e.Cursor()))
		return t.Slice(ls, vim.LineEnd(t, ls))
	}
	// Help opens on its index, and the reference comes from the tables.
	if feed(e, ":h<CR>"); !strings.HasPrefix(text(e), "# fude help") || !strings.Contains(text(e), "`:write [file]  :w`\n  writes the buffer") {
		t.Fatalf(":h opened %q", text(e)[:min(200, len(text(e)))])
	}
	for topic, want := range map[string]string{":w": "`:write [file]  :w`", "w": "`w`", "theme": "`theme = \"dark\"`", "guide-start": "# Getting started"} {
		if feed(e, ":h "+topic+"<CR>"); line() != want {
			t.Errorf(":h %s went to %q, want %q", topic, line(), want)
		}
	}
	// Every option and setting has a longer explanation after its line.
	for _, section := range []string{"## Options", "## Settings"} {
		_, rest, _ := strings.Cut(text(e), "\n"+section+"\n")
		rest, _, _ = strings.Cut(rest, "\n## ")
		blocks := strings.Split(rest, "\n\n")
		for i, b := range blocks {
			if strings.HasPrefix(b, "`") && (i+1 == len(blocks) || !strings.HasPrefix(blocks[i+1], "  ")) {
				t.Errorf("%s has no longer explanation", strings.SplitN(b, "\n", 2)[0])
			}
		}
	}
	if feed(e, ":h nope<CR>"); e.Message() != "no help for nope" {
		t.Errorf(":h nope said %q", e.Message())
	}
	// <CR> on a name goes to it, from a description or the index.
	if feed(e, ":h ;<CR>jff<CR>"); line() != "`f{char}`" {
		t.Errorf("<CR> on f went to %q", line())
	}
	if feed(e, ":h<CR>/guide-config<CR><CR>"); line() != "# Configuring fude" {
		t.Errorf("<CR> on guide-config went to %q", line())
	}
}

// B163
func TestB163Man(t *testing.T) {
	// Each page has its own part of help.
	for name, want := range map[string]string{
		"":       ".TP\n\\fB:write [file]  :w\\fR\nwrites the buffer",
		"config": ".TP\n\\fBtheme = \"dark\"\\fR",
		"guide":  ".SH GETTING STARTED",
	} {
		page, err := editor.Man(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(page, ".TH FUDE") || !strings.Contains(page, want) {
			t.Errorf("man page %q is %q...", name, page[:min(300, len(page))])
		}
	}
}

// B164
func TestB164Filetypes(t *testing.T) {
	config(t, "[filetype.toml]\nfiles = [\"*.toml.tmpl\"]\n")
	dir := t.TempDir()
	for name, want := range map[string]string{"a.toml.tmpl": "toml", "a.yml": "yaml", "a.rs": "rust", ".bashrc": "bash", "a.nix": "nix"} {
		path := filepath.Join(dir, name)
		change(t, path, "x\n")
		if got := start(t, path).Views()[0].Filetype; got != want {
			t.Errorf("%s has filetype %q, want %q", name, got, want)
		}
	}
	// A table named after an extension applies to that extension's filetype.
	config(t, "[filetype.yml]\nindent_width = 3\n")
	path := filepath.Join(dir, "a.yaml")
	change(t, path, "x\n")
	e := start(t, path)
	if feed(e, ">>"); text(e) != "   x" {
		t.Errorf("[filetype.yml] gave >> in a.yaml %q, want three spaces", text(e))
	}
}

// B165, through a grammar built by hand: Go's, as the filetype gotest.
func TestB165BuiltGrammar(t *testing.T) {
	mod, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/tree-sitter/tree-sitter-go").Output()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "src")
	if err := os.CopyFS(src, os.DirFS(filepath.Join(strings.TrimSpace(string(mod)), "src"))); err != nil {
		t.Fatal(err)
	}
	for name, from := range map[string]string{"parser.c": "tree_sitter_go(", "grammar.json": `"name": "go"`} {
		b, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatal(err)
		}
		b = []byte(strings.Replace(string(b), from, strings.Replace(from, "go", "gotest", 1), 1))
		if err := os.WriteFile(filepath.Join(src, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	out := filepath.Join(data, "fude", "grammars", "gotest")
	if err := os.CopyFS(filepath.Join(out, "queries"), os.DirFS(filepath.Join(strings.TrimSpace(string(mod)), "queries"))); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command("tree-sitter", "build", "-o", filepath.Join(out, "parser.so"), filepath.Dir(src)).CombinedOutput(); err != nil {
		t.Fatalf("tree-sitter build: %v: %s", err, b)
	}
	path := filepath.Join(t.TempDir(), "a.gotest")
	change(t, path, "package p\n")
	e := start(t, path)
	e.SetSize(80, 20)
	settle(t, e, func() bool {
		return slices.ContainsFunc(e.Views()[0].Highlights, func(s syntax.Span) bool { return s.Name == "keyword" })
	})

	// A grammar built at an older pin is outdated, and :grammar update
	// builds it again. Without git on the PATH the build fails at once.
	old := filepath.Join(data, "fude", "grammars", "toml")
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	change(t, filepath.Join(old, "0000000-1.so"), "")
	if feed(e, ":grammar<CR>"); !strings.Contains(text(e), "toml         outdated") {
		t.Fatalf(":grammar listed %q, want toml outdated", text(e))
	}
	t.Setenv("PATH", "")
	if feed(e, ":q<CR>:grammar update<CR>"); e.Message() != "updating the toml grammars" {
		t.Fatalf(":grammar update said %q", e.Message())
	}
	settle(t, e, func() bool { return strings.Contains(e.Message(), "git is not installed") })
}

// B166, through an injection a query override makes: Go inside a Go raw
// string.
func TestB166Injections(t *testing.T) {
	config(t, "")
	dir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "fude", "queries", "go")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The second pattern's predicate is one fude does not evaluate, which
	// leaves that pattern out but keeps the query.
	src := "((raw_string_literal_content) @injection.content (#set! injection.language \"go\"))\n" +
		"((comment) @injection.content (#offset! @injection.content 0 1 0 0) (#set! injection.language \"go\"))\n"
	if err := os.WriteFile(filepath.Join(dir, "injections.scm"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	text := "package p\n\nvar s = `func f() {}`\n"
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, text)
	e := start(t, path)
	e.SetSize(80, 10)
	at := strings.Index(text, "`func") + 1
	settle(t, e, func() bool {
		return slices.ContainsFunc(e.Views()[0].Highlights, func(s syntax.Span) bool { return s.Name == "keyword" && s.Start == at })
	})
	if e.Message() != "" {
		t.Errorf("the injections said %q", e.Message())
	}
}

// B170
func TestB170Restart(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.txt"), filepath.Join(dir, "b.txt")
	change(t, a, "one\ntwo\n")
	change(t, b, "three\n")
	e := start(t, a, b)
	e.SetSize(80, 20)
	feed(e, "j:vsplit<CR>:b 2<CR>ix<Esc>:set number<CR>:restart<CR>")
	if !e.Done() || e.Restart() == "" {
		t.Fatal(":restart did not quit with a saved session")
	}
	t.Setenv(editor.RestartEnv, e.Restart())
	e = start(t)
	e.SetSize(80, 20)
	vs := e.Views()
	if len(vs) != 2 || vs[0].Name != a || vs[1].Name != b {
		t.Fatalf("the restarted session shows %d windows", len(vs))
	}
	// The unsaved change, the cursor, the option and the current window come back.
	if got := vs[1].Text.Slice(0, vs[1].Text.Len()); !vs[1].Current || got != "xthree" || !vs[1].Number || !vs[1].Modified {
		t.Errorf("the current window shows %q, current %v, number %v", got, vs[1].Current, vs[1].Number)
	}
	if vs[0].Cursor != 4 {
		t.Errorf("the other window's cursor is at %d, want 4", vs[0].Cursor)
	}
	e = start(t, a)
	if feed(e, "ZR"); !e.Done() || e.Restart() == "" {
		t.Error("ZR did not quit with a saved session")
	}
}

// B172
func TestB172KeyHints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "ab\n")
	config(t, "[keymap.normal]\n\"<Leader>w\" = \":w<CR>\"\n\"<Leader>sg\" = \":pick grep<CR>\"\n[keymap.groups]\n\"<Leader>s\" = \"search\"\n")
	e := start(t, path)
	e.SetSize(100, 30)
	// After a mapping's first key, the hints say what may follow, and the key
	// waits for the next one instead of key_timeout.
	feed(e, "<Space>")
	if _, due := e.HintsIn(); !due {
		t.Fatal("no hints are due after <Space>")
	}
	e.ShowHints()
	// A key that starts longer ones shows its group's name.
	if f, ok := e.Float(); !ok || f.Title != "<Space>" || !strings.Contains(f.Text, "w  :w<CR>") || !strings.Contains(f.Text, "s  +search 1") {
		t.Fatalf("the hints show %q titled %q", f.Text, f.Title)
	}
	if _, waiting := e.Wait(); waiting {
		t.Error("key_timeout runs while the hints show")
	}
	// <BS> goes back a key.
	feed(e, "s<BS>")
	if f, _ := e.Float(); f.Title != "<Space>" {
		t.Errorf("after s<BS> the hints are titled %q", f.Title)
	}
	// <Esc> drops the held key, so the cursor stays.
	if feed(e, "<Esc>"); e.Cursor() != 0 {
		t.Errorf("<Esc> ran the held <Space>, the cursor is at %d", e.Cursor())
	}
	if _, ok := e.Float(); ok {
		t.Error("the hints stay after <Esc>")
	}
	// The machine's own keys follow g.
	feed(e, "g")
	e.ShowHints()
	if f, _ := e.Float(); !strings.Contains(f.Text, "g  goes to the first line") {
		t.Errorf("the hints after g show %q", f.Text)
	}
}
