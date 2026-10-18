package grep_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/grep"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B134
func TestB134Grep(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.go"), "one\n  two Needle\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "needles\n")
	if out, err := exec.Command("git", "-C", dir, "init", "-q").CombinedOutput(); err != nil {
		t.Skipf("git init: %v: %s", err, out)
	}
	t.Chdir(dir)
	e := editortest.Open(t, "b.txt")
	e.Use(grep.New())
	e.SetSize(100, 24)
	results := func() []string {
		f, _ := e.Float()
		lines := strings.Split(f.Text, "\n")[1:]
		return slices.DeleteFunc(lines, func(l string) bool { return l == "" })
	}
	// A query in lower case ignores case, and one with a capital does not.
	// Each match shows under its file, as its line number and its line.
	editortest.Feed(e, ":pick grep needle<CR>")
	editortest.Settle(t, e, func() bool { return len(results()) == 4 })
	editortest.Feed(e, "<BS><BS><BS><BS><BS><BS>Needle")
	editortest.Settle(t, e, func() bool { return slices.Equal(results(), []string{"a.go", "2  two Needle"}) })
	// <CR> opens the file on the match.
	if editortest.Feed(e, "<CR>"); e.Views()[0].Name != "a.go" || e.Cursor() != 10 {
		t.Fatalf("<CR> opened %q at %d", e.Views()[0].Name, e.Cursor())
	}
	// A match on a line of megabytes shows with the others.
	editortest.Change(t, filepath.Join(dir, "c.txt"), "needle "+strings.Repeat("x", 2<<20)+"\n")
	editortest.Feed(e, ":pick grep needle<CR>")
	editortest.Settle(t, e, func() bool { r := results(); return slices.Contains(r, "b.txt") && slices.Contains(r, "c.txt") })
}
