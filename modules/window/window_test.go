package window_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
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
	if v := e.Views(); !v[0].Current || v[1].Current {
		t.Fatal("<Plug>(window.left) did not move to the left window")
	}
	editortest.Feed(e, "<Plug>(window.left)")
	editortest.Settle(t, e, func() bool {
		b, _ := os.ReadFile(log)
		return string(b) == "select-pane -t %3 -L\n"
	})
}
