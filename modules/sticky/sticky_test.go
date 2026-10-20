package sticky_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/sticky"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B217, B379
func TestB217StickyContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	editortest.Change(t, path, "package p\n\nfunc F() {\n\tg := func() {\n"+strings.Repeat("\t\tx()\n", 30)+"\t}\n}\n")
	e := editortest.Start(t, path)
	e.Use(sticky.New())
	e.SetSize(80, 10)
	pinned := func(want ...int) func() bool {
		return func() bool { return slices.Equal(e.Windows()[0].Context, want) }
	}
	// Scrolled into g in F, both first lines are pinned, with their highlights.
	editortest.Feed(e, "20Gzt3j")
	editortest.Settle(t, e, func() bool { return pinned(11, 22)() && len(e.Windows()[0].ContextHighlights) > 0 })
	// With sticky.lines 1, only the innermost line pins.
	editortest.Feed(e, ":set sticky.lines 1<CR>")
	editortest.Settle(t, e, pinned(22))
	// With the cursor on the top line, nothing covers it.
	editortest.Feed(e, "zt")
	editortest.Settle(t, e, pinned())
	// sticky.enabled false turns it off.
	editortest.Feed(e, "3j")
	editortest.Settle(t, e, pinned(22))
	editortest.Feed(e, ":set sticky.enabled false<CR>")
	editortest.Settle(t, e, pinned())
}
