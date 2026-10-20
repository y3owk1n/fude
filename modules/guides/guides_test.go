package guides_test

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/guides"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B153, B380
func TestB153IndentGuides(t *testing.T) {
	editortest.Config(t, "indent_width = 4\n")
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "a\n\tb\n\n        c\n  d\n")
	e := editortest.Start(t, path)
	e.Use(guides.New())
	e.SetSize(80, 10)
	at := func(want map[int][]int) func() bool {
		return func() bool { return maps.EqualFunc(e.Windows()[0].Guides, want, slices.Equal) }
	}
	// A guide every indent_width columns of the leading blanks, a tab as
	// wide as it shows, and the blank line takes the next line's.
	editortest.Settle(t, e, at(map[int][]int{1: {0, 4}, 2: {0, 4}, 3: {0, 4}, 4: {0}}))
	editortest.Feed(e, ":set guides.enabled false<CR>")
	editortest.Settle(t, e, at(nil))
}
