package editor_test

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	buffersmod "github.com/y3owk1n/fude/modules/buffers"
	explorermod "github.com/y3owk1n/fude/modules/explorer"
	shellmod "github.com/y3owk1n/fude/modules/shell"
	"github.com/y3owk1n/fude/vim"
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
	// Windows gives files no mode, only a read-only flag.
	if fi, _ := os.Stat(path); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("write changed the mode to %v", fi.Mode().Perm())
	}

	release := refuseWrite(t, path)
	feed(e, "ix<Esc>:w<CR>")
	release()
	if got := disk(t, path); got != "new old\n" {
		t.Fatalf("failed write changed the file to %q", got)
	}
	if !strings.Contains(e.Message(), path) {
		t.Fatalf("message %q does not name the path", e.Message())
	}
	// A write into a folder that does not exist says so.
	missing := filepath.Join(t.TempDir(), "gone", "f")
	if feed(e, ":w "+missing+"<CR>"); e.Message() != "write "+missing+": the folder "+filepath.Dir(missing)+" does not exist" {
		t.Fatalf(":w into a missing folder said %q", e.Message())
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

// B302
func TestB302ReadOnlyTakesNoTyping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bin")
	if err := os.WriteFile(path, []byte("alpha beta\xff\nalways\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	// No key that would type starts insert or replace mode, nor moves the
	// cursor or opens a menu.
	for _, keys := range []string{"i", "a", "A", "I", "o", "O", "cw", "cc", "R", "<C-v>I"} {
		feed(e, "<Esc>gg")
		if feed(e, keys); e.Mode() != vim.Normal && e.Mode() != vim.Visual || e.Cursor() != 0 || e.Message() != "cannot make changes, the file is read-only" {
			t.Errorf("%s left mode %v, the cursor at %d, saying %q", keys, e.Mode(), e.Cursor(), e.Message())
		}
		if _, ok := e.Menu(); ok {
			t.Errorf("%s opened a float", keys)
		}
	}
}

// B304
func TestB304ImageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 32))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	e := open(t, path)
	e.SetSize(80, 20)
	e.SetCell(8, 16)
	// The file shows as what it is, with the image under it, and takes no
	// typing.
	if text(e) != "PNG, 80 by 32 pixels" || e.Message() != "" {
		t.Fatalf("the image opened as %q, saying %q", text(e), e.Message())
	}
	settle(t, e, func() bool { return len(e.Windows()[0].Virtual[0]) == 2 })
	if feed(e, "i"); e.Mode() != vim.Normal {
		t.Error("i in the image started insert mode")
	}
}

// B307
func TestB307ReloadImage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.png")
	write := func(w int, when time.Time) {
		var b bytes.Buffer
		if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, 32))); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, when, when); err != nil {
			t.Fatal(err)
		}
	}
	write(80, time.Now().Add(-time.Hour))
	e := open(t, path)
	e.SetSize(80, 20)
	e.SetCell(8, 16)
	settle(t, e, func() bool { return len(e.Windows()[0].Virtual[0]) == 2 })
	old := e.Windows()[0].Virtual[0][0].Image
	// Coming back to fude after the file grew shows the new image, under a
	// line that says so, and frees the old one.
	write(160, time.Now())
	e.Focus()
	settle(t, e, func() bool {
		v := e.Windows()[0].Virtual[0]
		return text(e) == "PNG, 160 by 32 pixels" && len(v) > 0 && v[0].Image != old && v[0].Cols == 20
	})
	if !slices.Contains(e.Freed(), old) {
		t.Error("the old image was not freed")
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
	e.Use(buffersmod.New())
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
	if !strings.Contains(e.Confirm(), "changed on disk") || disk(t, path) != "disk\n" {
		t.Fatalf(":w asked %q and left %q, want a question and the disk text", e.Confirm(), disk(t, path))
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
		e := withCommands(start(t, a, b))
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
	e := withCommands(start(t, a, b))
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
	// Windows finds the home folder in USERPROFILE.
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	b, c := filepath.Join(dir, "b.go"), filepath.Join(dir, "c.txt")
	change(t, b, "B\n")
	change(t, c, "C\n")
	change(t, filepath.Join(dir, "d.txt"), "D\n")
	e := withCommands(open(t, b))
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
	// Completion keeps the / typed after the folder, on Windows too.
	feed(e, ":e %:h/c.t<Tab>")
	settle(t, e, func() bool { return e.Line() == ":e "+dir+"/c.txt" })
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
	e := withCommands(open(t, "a"))
	e.Use(explorermod.New())
	// :w with a name writes a copy, and the buffer stays on its own file.
	feed(e, "x:w b<CR>")
	if disk(t, filepath.Join(dir, "b")) != "ne\n" || disk(t, path) != "one\n" || e.Windows()[0].Name != "a" {
		t.Fatalf(":w b wrote %q, left a %q, and the window shows %q", disk(t, filepath.Join(dir, "b")), disk(t, path), e.Windows()[0].Name)
	}
	if feed(e, ":w b<CR>"); !strings.Contains(e.Message(), "exists") {
		t.Fatalf(":w over a file that exists said %q", e.Message())
	}
	// :e keeps changes, and :e! drops them.
	if feed(e, ":e<CR>"); text(e) != "ne" {
		t.Fatalf(":e with changes left %q", text(e))
	}
	if feed(e, ":e!<CR>"); text(e) != "one" || e.Windows()[0].Modified {
		t.Fatalf(":e! left %q", text(e))
	}
	// :update writes only a buffer with changes.
	change(t, path, "two\n")
	if feed(e, ":update<CR>"); disk(t, path) != "two\n" {
		t.Fatal(":update wrote a buffer with no changes")
	}
	// :new splits to a buffer with no file.
	if feed(e, ":new<CR>"); len(e.Windows()) != 2 || e.Windows()[1].Name != "[No Name]" {
		t.Fatalf(":new left windows %v", e.Windows())
	}
	// :cd moves the working directory, and the buffers keep their files.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	feed(e, ":cd sub<CR><C-w>k")
	if wd, _ := os.Getwd(); wd != sub && !strings.HasSuffix(wd, string(filepath.Separator)+"sub") {
		t.Fatalf(":cd sub left the working directory %s", wd)
	}
	if feed(e, ":pwd<CR>"); !strings.HasSuffix(e.Message(), string(filepath.Separator)+"sub") {
		t.Fatalf(":pwd said %q", e.Message())
	}
	// a changed on disk above, so :w! writes over it.
	if feed(e, "0x:w!<CR>"); disk(t, path) != "ne\n" {
		t.Fatalf("after :cd, :w! wrote %q to a", disk(t, path))
	}
}

// B156, for the commands that run a shell
func TestB156Read(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "in.txt"), "read\n")
	t.Chdir(dir)
	e := open(t, "")
	e.Use(shellmod.New())
	feed(e, "ia<CR>b<Esc>")
	// :r puts a file's text below the line.
	if feed(e, "gg:r in.txt<CR>"); text(e) != "a\nread\nb" {
		t.Fatalf(":r in.txt made %q", text(e))
	}
}

// B423
func TestB423Cquit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "one\n")
	for keys, code := range map[string]int{"x:cq<CR>": 1, "x:cquit 3<CR>": 3, ":cq 0<CR>": 0} {
		e := open(t, path)
		if feed(e, keys); !e.Done() || e.ExitCode() != code || disk(t, path) != "one\n" {
			t.Errorf("%s: done %v, exit code %d and the file %q, want done, %d and the file as it was", keys, e.Done(), e.ExitCode(), disk(t, path), code)
		}
	}
}

// B426
func TestB426FileInfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	change(t, path, "one\ntwo\nthree\nfour\n")
	e := open(t, path)
	for _, c := range [][2]string{
		{"j<C-g>", `"` + path + `" 4 lines, 50% down`},
		{"Gx:file<CR>", `"` + path + `" modified, 4 lines, 100% down`},
		{"u2<C-g>", `buf 1: "` + path + `" 4 lines, 100% down`},
	} {
		if feed(e, c[0]); e.Message() != c[1] {
			t.Errorf("%s said %q, want %q", c[0], e.Message(), c[1])
		}
	}
	// :file names the buffer's file, which :w then writes.
	renamed := filepath.Join(dir, "g")
	if feed(e, ":file "+renamed+"<CR>"); e.Message() != `"`+renamed+`" new, 4 lines, 100% down` {
		t.Errorf(":file %s said %q", renamed, e.Message())
	}
	if feed(e, ":w<CR>"); disk(t, renamed) != "one\ntwo\nthree\nfour\n" {
		t.Errorf(":w after :file wrote %q", disk(t, renamed))
	}
	// The old name is the alternate file.
	if feed(e, ":e #<CR>"); e.Windows()[0].Name != path {
		t.Errorf(":e # after :file showed %q, want %q", e.Windows()[0].Name, path)
	}
	empty := open(t, "")
	if feed(empty, "<C-g>"); empty.Message() != `"[No Name]" has no lines` {
		t.Errorf("<C-g> in an empty buffer said %q", empty.Message())
	}
	// The buffer takes the grammar of its new name.
	text := filepath.Join(dir, "a.txt")
	change(t, text, "package main\n")
	e = start(t, text)
	e.SetSize(80, 10)
	if feed(e, ":file "+filepath.Join(dir, "a.go")+"<CR>"); captures(t, e)["package"] != "keyword" {
		t.Errorf("after :file a.go, package shows as %q", captures(t, e)["package"])
	}
}

// B427
func TestB427SaveAs(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	change(t, a, "one\n")
	change(t, b, "two\n")
	e := open(t, a)
	if feed(e, "x:saveas "+b+"<CR>"); e.Message() != `"`+b+`" exists, :saveas! writes over it` || disk(t, b) != "two\n" {
		t.Fatalf(":saveas over a file said %q and left %q", e.Message(), disk(t, b))
	}
	// The buffer writes to its new file from then on, and the old one is
	// left as it was.
	if feed(e, ":saveas! "+b+"<CR>x:w<CR>"); disk(t, b) != "e\n" || disk(t, a) != "one\n" || e.Windows()[0].Name != b {
		t.Errorf(":saveas! then :w left %q in b and %q in a, the buffer named %q", disk(t, b), disk(t, a), e.Windows()[0].Name)
	}
	// The old name is the alternate file, with the text on disk.
	if feed(e, ":e #<CR>"); e.Windows()[0].Name != a || text(e) != "one" {
		t.Errorf(":e # after :saveas showed %q with %q", e.Windows()[0].Name, text(e))
	}
}
