package explorer_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/buffers"
	"github.com/y3owk1n/fude/modules/explorer"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// disk is the text of the file at path.
func disk(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// B124
func TestB124Explorer(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	editortest.Change(t, filepath.Join(dir, "a.txt"), "A\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "B\n")
	editortest.Change(t, filepath.Join(dir, "sub", "c"), "C\n")
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(explorer.New())
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
		if editortest.Feed(e, c.keys); editortest.Text(e) != c.text || line() != c.line {
			t.Fatalf("after %q the text is %q on %q, want %q on %q", c.keys, editortest.Text(e), line(), c.text, c.line)
		}
	}
	// 1- and _ list the working directory, with the cursor on the entry
	// that leads to the file.
	t.Chdir(filepath.Join(dir, "sub"))
	if editortest.Feed(e, "1-"); editortest.Text(e) != "c" {
		t.Fatalf("1- listed %q", editortest.Text(e))
	}
	t.Chdir(dir)
	if editortest.Feed(e, "_"); editortest.Text(e) != "a.txt\nb.txt\nsub/" || line() != "sub/" {
		t.Fatalf("_ from the explorer of sub/ listed %q on %q", editortest.Text(e), line())
	}
	if editortest.Feed(e, "G<CR><CR>1-"); line() != "sub/" {
		t.Fatalf("1- from sub/c left the cursor on %q", line())
	}
}

// B125
func TestB125EditExplorer(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt", "keep.txt", "old/x", "old/y"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		editortest.Change(t, filepath.Join(dir, f), f+"\n")
	}
	e := editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(explorer.New())
	e.Use(buffers.New())
	// a.txt becomes z.txt, b.txt and old/ go, and new.txt and new/ come.
	editortest.Feed(e, "-ggcwz<Esc>jddjjddOnew.txt<CR>new/<Esc>:w<CR>")
	if want := "rename a.txt to z.txt, delete b.txt, delete old/ and its 2 files, create new.txt, create new/?"; e.Confirm() != want {
		t.Fatalf(":w asked %q, want %q", e.Confirm(), want)
	}
	if editortest.Feed(e, "n"); disk(t, filepath.Join(dir, "a.txt")) != "a.txt\n" {
		t.Fatal("answering n changed the directory")
	}
	editortest.Feed(e, ":w<CR>y")
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
	if editortest.Text(e) != "keep.txt\nnew/\nnew.txt\nz.txt" || e.Views()[0].Modified {
		t.Errorf("after :w the explorer is %q", editortest.Text(e))
	}
	// The renamed file's buffer follows it.
	if editortest.Feed(e, ":b 1<CR>"); e.Views()[0].Name != filepath.Join(dir, "z.txt") {
		t.Errorf("the buffer of a.txt is named %q", e.Views()[0].Name)
	}
	// A rename that fails, into a folder that is a file, leaves the name.
	dir = t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a"), "a\n")
	editortest.Change(t, filepath.Join(dir, "sub"), "sub\n")
	e = editortest.Open(t, filepath.Join(dir, "a"))
	e.Use(explorer.New())
	editortest.Feed(e, "-Csub/a<Esc>:w<CR>y")
	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || disk(t, filepath.Join(dir, "a")) != "a\n" || !strings.Contains(e.Message(), "rename a") {
		t.Errorf("a failed rename left %v and said %q", names, e.Message())
	}
	// A rename of only a name's case, which a file system that ignores case
	// finds already there, renames it.
	dir = t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\n")
	e = editortest.Open(t, filepath.Join(dir, "a.txt"))
	e.Use(explorer.New())
	editortest.Feed(e, "-ggcwA<Esc>:w<CR>y")
	if names, err = os.ReadDir(dir); err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0].Name() != "A.txt" {
		t.Errorf("renaming a.txt to A.txt left %v and said %q", names, e.Message())
	}
}

// B127
func TestB127ExplorerNotes(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "plain"), "")
	editortest.Change(t, filepath.Join(dir, "run"), "")
	if err := os.Chmod(filepath.Join(dir, "run"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("plain", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	e := editortest.Open(t, dir)
	e.Use(explorer.New())
	e.SetSize(40, 10)
	want := map[int]wire.Note{0: {Text: " -> plain"}, 2: {Text: "*"}}
	editortest.Settle(t, e, func() bool { return maps.Equal(e.Views()[0].Notes, want) })
}
