package editor_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
)

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
