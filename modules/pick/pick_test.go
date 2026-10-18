package pick_test

import (
	"os"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/pick"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B266
func TestB266PickCommands(t *testing.T) {
	e := editortest.Start(t)
	e.Use(pick.New())
	e.SetSize(100, 30)
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	// Typing what a command does finds it, and the one picked runs.
	editortest.Feed(e, ":pick commands<CR>opens a window below with a new empty")
	editortest.Settle(t, e, func() bool { return shows(":new") })
	editortest.Feed(e, "<CR>")
	editortest.Settle(t, e, func() bool { return len(e.Views()) == 2 })
	// One that needs an argument waits on the command line.
	editortest.Feed(e, ":pick commands<CR>:cdo")
	editortest.Settle(t, e, func() bool { return shows(":cdo") })
	editortest.Feed(e, "<CR>")
	editortest.Settle(t, e, func() bool { return e.Line() == ":cdo " })
}

// B215
func TestB215PickLists(t *testing.T) {
	editortest.Config(t, "[keymap.normal]\n\"z9\" = \":set number true<CR>\"\n")
	e := editortest.Start(t)
	e.Use(pick.New())
	e.SetSize(100, 30)
	shows := func(want string) bool { f, _ := e.Float(); return strings.Contains(f.Text, want) }
	// :pick alone lists the sources, and picking one opens its finder.
	editortest.Feed(e, ":pick<CR>")
	editortest.Settle(t, e, func() bool { return shows("keymaps") })
	editortest.Feed(e, "keymaps<CR>z9")
	editortest.Settle(t, e, func() bool { return shows("normal mapping  types :set<Space>number<Space>true<CR>") })
	// A mapping picked runs.
	editortest.Feed(e, "<CR>")
	editortest.Settle(t, e, func() bool { return e.Views()[0].Number })
	// It lists the machine's keys too, each with its kind and help, and a
	// motion picked runs, j here after insert mode's <Down>.
	editortest.Feed(e, "ggix<CR>y<Esc>gg:pick keymaps<CR>moves down a line")
	editortest.Settle(t, e, func() bool { return shows("motion          moves down a line") })
	editortest.Feed(e, "<C-n><CR>")
	editortest.Settle(t, e, func() bool { return e.Text().LineOf(e.Cursor()) == 1 })
	editortest.Feed(e, ":pick help<CR>guide-start<CR>")
	editortest.Settle(t, e, func() bool { return strings.HasPrefix(editortest.Text(e), "# fude help") })
}
