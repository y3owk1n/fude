package tabline_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/tabline"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B545
func TestB545TabLineFollows(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "one\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "two\n")
	t.Chdir(dir)
	e := editortest.Open(t, "a.txt")
	e.Use(tabline.New())
	names := func() []string {
		var out []string
		for _, p := range e.Pages() {
			out = append(out, p.Name)
		}
		return out
	}
	// One page lists none, and two or more list each by its buffer.
	editortest.Feed(e, ":tabedit b.txt<CR>")
	editortest.Settle(t, e, func() bool { return slices.Equal(names(), []string{"a.txt", "b.txt"}) })
	// A page shows another buffer, and the line follows.
	editortest.Feed(e, ":e a.txt<CR>")
	editortest.Settle(t, e, func() bool { return slices.Equal(names(), []string{"a.txt", "a.txt"}) })
	editortest.Feed(e, ":tabclose<CR>")
	editortest.Settle(t, e, func() bool { return len(names()) == 0 })
	// :set tabline.enabled turns the pages off and on at once.
	editortest.Feed(e, ":tabnew<CR>:set tabline.enabled false<CR>")
	editortest.Settle(t, e, func() bool { return len(names()) == 0 })
	editortest.Feed(e, ":set tabline.enabled true<CR>")
	editortest.Settle(t, e, func() bool { return slices.Equal(names(), []string{"a.txt", "[No Name]"}) })
}
