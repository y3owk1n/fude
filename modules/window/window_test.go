package window_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/window"
	"github.com/y3owk1n/fude/wire"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B140, B330
func TestB140Navigate(t *testing.T) {
	log := editortest.FakeProgram(t, "tmux")
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("TMUX_PANE", "%3")
	path := filepath.Join(t.TempDir(), "f")
	editortest.Change(t, path, "a\n")
	e := editortest.Open(t, path)
	e.Use(window.New())
	e.SetSize(80, 24)
	// :vsplit leaves the cursor in the right window, so left moves within
	// fude, and left again goes to tmux's pane.
	editortest.Feed(e, ":vsplit<CR><Plug>(window.left)")
	if v := e.Windows(); !v[0].Current || v[1].Current {
		t.Fatal("<Plug>(window.left) did not move to the left window")
	}
	editortest.Feed(e, "<Plug>(window.left)")
	editortest.Settle(t, e, func() bool {
		b, _ := os.ReadFile(log)
		return string(b) == "select-pane -t %3 -L\n"
	})
}

// B365
func TestB365Place(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	editortest.Change(t, path, "a\n")
	e := editortest.Open(t, path)
	e.Use(window.New())
	e.SetSize(80, 24)
	floated := func() bool {
		return slices.ContainsFunc(e.Windows(), func(w wire.Window) bool { return w.Float != "" && w.Current })
	}
	// :window with a command opens the window it opens that way.
	if editortest.Feed(e, ":window right split<CR>"); len(e.Windows()) != 2 || e.Windows()[1].X == 0 || e.Windows()[1].H != e.Windows()[0].H {
		t.Fatalf(":window right split made %d windows, the last at column %d", len(e.Windows()), e.Windows()[len(e.Windows())-1].X)
	}
	// Alone, it moves the current window, into a float and back to the
	// bottom, below every window and as wide as them.
	if editortest.Feed(e, ":window float<CR>"); !floated() || len(e.Windows()) != 2 {
		t.Fatalf(":window float left %d windows, a float current %v", len(e.Windows()), floated())
	}
	if editortest.Feed(e, ":window below<CR>"); floated() || len(e.Windows()) != 2 || e.Windows()[1].W != 80 || e.Windows()[1].Y == 0 {
		t.Fatalf(":window below left %d windows, the last %d wide at row %d", len(e.Windows()), e.Windows()[1].W, e.Windows()[1].Y)
	}
	// :hide takes the window out of sight, and refuses for the last one.
	if editortest.Feed(e, ":hide<CR>"); len(e.Windows()) != 1 {
		t.Fatalf(":hide left %d windows", len(e.Windows()))
	}
	if editortest.Feed(e, ":hide<CR>"); len(e.Windows()) != 1 || e.Message() != "cannot hide the last window" {
		t.Errorf(":hide in the last window left %d windows saying %q", len(e.Windows()), e.Message())
	}
}
