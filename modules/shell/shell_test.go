package shell_test

import (
	"os"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/shell"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B156
func TestB156Shell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	e := editortest.Open(t, "")
	e.Use(shell.New())
	editortest.Feed(e, "ic<CR>a<CR>b<Esc>")
	// A range goes through the command, and its output takes its place.
	editortest.Feed(e, ":%!sort<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "a\nb\nc" })
	// :r !cmd puts what the command writes below the line.
	editortest.Feed(e, "gg:r !echo out<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "a\nout\nb\nc" })
	// Without a range, the output is a message.
	editortest.Feed(e, ":!echo hi<CR>")
	editortest.Settle(t, e, func() bool { return e.Message() == "hi" })
	// A filter's lines that change while it runs keep the change.
	editortest.Feed(e, ":%!sleep 0.2; cat<CR>ggix<Esc>")
	editortest.Settle(t, e, func() bool { return e.Message() == "the text changed while sleep 0.2; cat ran" })
	if editortest.Text(e) != "xa\nout\nb\nc" {
		t.Errorf("after the text changed under the filter it is %q", editortest.Text(e))
	}
}
