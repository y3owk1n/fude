package history_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/history"
	"github.com/y3owk1n/fude/modules/plug"
	"github.com/y3owk1n/fude/modules/window"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B371
func TestB371History(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "x x x\n<b>\n")
	e := editortest.Start(t, path)
	e.Use(history.New())
	e.Use(window.New())
	e.Use(plug.New())
	e.SetSize(80, 24)
	editortest.Feed(e, ":s/x/y/<CR>/<lt>b<CR>gg")
	// q: lists the command lines, seven lines tall, with the cursor on an
	// empty line after the newest.
	editortest.Feed(e, "q:")
	if vs := e.Windows(); len(vs) != 2 || vs[1].H != 7 || editortest.Text(e) != "s/x/y/\n" || e.Cursor() != len("s/x/y/\n") {
		t.Fatalf("q: showed %q in %d windows, the cursor at %d", editortest.Text(e), len(e.Windows()), e.Cursor())
	}
	// <CR> closes it and runs the line under the cursor, edited first, in
	// the window it opened from.
	if editortest.Feed(e, "<C-w>k:vsplit<CR><C-w>jkAg<Esc><CR>"); len(e.Windows()) != 2 || editortest.Text(e) != "y y y\n<b>" {
		t.Fatalf("<CR> on s/x/y/g left %d windows and %q", len(e.Windows()), editortest.Text(e))
	}
	// q/ lists the searches, and <CR> searches for one, < and all.
	if editortest.Feed(e, "ggq/k<CR>"); len(e.Windows()) != 2 || e.Cursor() != len("y y y\n") {
		t.Fatalf("<CR> on a search left %d windows and the cursor at %d", len(e.Windows()), e.Cursor())
	}
	// q and another key still records a macro.
	if editortest.Feed(e, "ggqaA!<Esc>q@a"); editortest.Text(e) != "y y y!!\n<b>" {
		t.Errorf("recording and running a macro made %q", editortest.Text(e))
	}
	// Its <CR> does nothing outside its window.
	if editortest.Feed(e, ":plug history.run<CR>"); len(e.Windows()) != 2 || editortest.Text(e) != "y y y!!\n<b>" || e.Message() != "not in a history window" {
		t.Errorf("history.run in a file left %d windows and %q, and said %q", len(e.Windows()), editortest.Text(e), e.Message())
	}
}
