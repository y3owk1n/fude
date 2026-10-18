package editor_test

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

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
