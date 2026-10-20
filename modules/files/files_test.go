package files_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/files"
	"github.com/y3owk1n/fude/vim"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B129
func TestB129Files(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"editor/editor.go", "editor/git.go", "editor/editor_test.go", "prd/decisions.md", "skip.go"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		editortest.Change(t, filepath.Join(dir, f), f+"\n")
	}
	editortest.Change(t, filepath.Join(dir, ".gitignore"), "skip.go\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := editortest.Open(t, "prd/decisions.md")
	e.Use(files.New())
	e.SetSize(80, 24)
	results := func() []string {
		f, _ := e.Float()
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
	}
	// The finder lists the files git does not ignore, each by its name and
	// then its directory.
	editortest.Feed(e, ":pick files<CR>")
	editortest.Settle(t, e, func() bool { return len(results()) == 5 })
	editortest.Feed(e, "edgo")
	editortest.Settle(t, e, func() bool {
		return slices.Equal(results(), []string{"editor.go  editor", "git.go  editor", "editor_test.go  editor"})
	})
	if editortest.Feed(e, "<C-n><CR>"); e.Windows()[0].Name != filepath.Join("editor", "git.go") || e.Mode() != vim.Normal {
		t.Fatalf("<C-n><CR> opened %q in %v mode", e.Windows()[0].Name, e.Mode())
	}
	// A screen too short for a row of matches still shows the query.
	e.SetSize(80, 5)
	editortest.Feed(e, ":pick files<CR>zzz")
	if f, _ := e.Float(); !strings.HasPrefix(f.Text, "› zzz") {
		t.Errorf("on a short screen the finder shows %q", f.Text)
	}
	editortest.Feed(e, "<Esc>")
	e.SetSize(80, 24)
	// <Esc> closes the finder and opens nothing.
	editortest.Feed(e, ":pick files<CR>x<Esc>")
	if _, ok := e.Float(); ok || e.Windows()[0].Name != filepath.Join("editor", "git.go") || editortest.Text(e) != "editor/git.go" {
		t.Fatalf("after <Esc> the finder shows %v and the window has %q", ok, e.Windows()[0].Name)
	}
}

// B213
func TestB213Buffers(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := editortest.Open(t, "a.txt")
	e.Use(files.New())
	e.SetSize(100, 30)
	editortest.Feed(e, ":e b.txt<CR>")
	rows := func() []string {
		f, _ := e.Float()
		return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
	}
	// Buffers lists the files but the current one, and <CR> opens the one
	// picked.
	editortest.Feed(e, ":pick buffers<CR>")
	editortest.Settle(t, e, func() bool { return slices.Contains(rows(), "a.txt") && !slices.Contains(rows(), "b.txt") })
	if editortest.Feed(e, "a.txt<CR>"); e.Windows()[0].Name != "a.txt" {
		t.Fatalf("<CR> in :pick buffers opened %q", e.Windows()[0].Name)
	}
}
