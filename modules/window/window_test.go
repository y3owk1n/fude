package window_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/modules/window"
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

// B430
func TestB430NewAndVertical(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	editortest.Change(t, path, "a\n")
	e := editortest.Open(t, path)
	e.Use(window.New())
	e.SetSize(80, 24)
	current := func() wire.Window {
		i := slices.IndexFunc(e.Windows(), func(w wire.Window) bool { return w.Current })
		return e.Windows()[i]
	}
	// <C-w>n opens a new empty buffer below.
	if editortest.Feed(e, "<C-w>n"); len(e.Windows()) != 2 || current().Y == 0 || editortest.Text(e) != "" {
		t.Fatalf("<C-w>n made %d windows, the current at row %d with %q", len(e.Windows()), current().Y, editortest.Text(e))
	}
	// A count is the new window's rows of text, as :resize sets them.
	if editortest.Feed(e, "3<C-w>n"); len(e.Windows()) != 3 || current().H != 3 {
		t.Errorf("3<C-w>n made %d windows, the current %d rows tall", len(e.Windows()), current().H)
	}
	// :vertical opens the window of the command after it to the right.
	for _, cmd := range []string{"new", "split"} {
		editortest.Feed(e, ":only<CR>:vertical "+cmd+"<CR>")
		if w := current(); len(e.Windows()) != 2 || w.X == 0 || w.H != e.Windows()[0].H {
			t.Errorf(":vertical %s made %d windows, the current at column %d", cmd, len(e.Windows()), w.X)
		}
	}
	if editortest.Feed(e, ":vertical<CR>"); e.Message() != "usage: :vertical {cmd}, or :vertical resize [+-]{n}" {
		t.Errorf(":vertical alone said %q", e.Message())
	}
}

// B370
func TestB370Zoom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	editortest.Change(t, path, "a\n")
	e := editortest.Open(t, path)
	e.Use(window.New())
	e.SetSize(80, 24)
	rects := func() [][4]int {
		var out [][4]int
		for _, w := range e.Windows() {
			out = append(out, [4]int{w.X, w.Y, w.W, w.H})
		}
		return out
	}
	editortest.Feed(e, ":vsplit<CR>:split<CR>")
	before := rects()
	// The window shows alone across the screen, and again brings back every
	// window as it was.
	if editortest.Feed(e, "<C-w>z"); len(e.Windows()) != 1 || !e.Windows()[0].Zoomed || e.Windows()[0].W != 80 {
		t.Fatalf("<C-w>z left the windows %v", rects())
	}
	if editortest.Feed(e, ":window zoom<CR>"); !slices.Equal(rects(), before) {
		t.Fatalf(":window zoom left the windows %v, want %v", rects(), before)
	}
	// Moving to another window shows every window again.
	if editortest.Feed(e, "<C-w>z<C-w>h"); !slices.Equal(rects(), before) {
		t.Errorf("<C-w>h from a zoomed window left the windows %v, want %v", rects(), before)
	}
}
