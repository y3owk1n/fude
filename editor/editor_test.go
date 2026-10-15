package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// TestMain keeps every test's timeline logs and config in a directory of
// its own.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fude-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func open(t *testing.T, path string) *editor.Editor {
	t.Helper()
	e, err := editor.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func feed(e *editor.Editor, keys string) {
	for _, k := range vim.Keys(keys) {
		e.Feed(k)
	}
}

func text(e *editor.Editor) string {
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
	line := func(e *editor.Editor, pos int) int { return strings.Count(text(e)[:pos], "\n") + 1 }
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
func windows(e *editor.Editor) string {
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
		{"<C-w>s", "*0,0 80x12 0,12 80x11"},
		{"<C-w>v", "*0,0 40x23 41,0 39x23"},
		{"<C-w>s<C-w>v", "*0,0 40x12 41,0 39x12 0,12 80x11"},
		{"<C-w>s<C-w>j", "0,0 80x12 *0,12 80x11"},
		{"<C-w>v<C-w>l", "0,0 40x23 *41,0 39x23"},
		{"<C-w>v<C-w>l<C-w>h", "*0,0 40x23 41,0 39x23"},
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
	feed(e, "jjl<C-w>sggdd<C-w>j")
	if text(e) != "two\nthree\nfour" || e.Cursor() != 5 {
		t.Errorf("after dd above it, the other window is at %d in %q, want 5", e.Cursor(), text(e))
	}
	e = open(t, path)
	e.SetSize(80, 23)
	feed(e, "jjl<C-w>sjx<C-w>j")
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
		{"<C-w>s<C-w>s", "*0,0 80x8 0,8 80x8 0,16 80x7"},
		{"<C-w>s<C-w>s<C-w>o", "*0,0 80x23"},
		{"<C-w>s<C-w>s<C-w>=", "*0,0 80x8 0,8 80x8 0,16 80x7"},
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
