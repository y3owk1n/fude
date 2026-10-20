package format_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/format"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B136
func TestB136FormatOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "keep\nx here\n")
	editortest.Config(t, "format_on_save = true\n[filetype.txt]\nformat = [\"y\", \"there\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n[formatter.there]\ncommand = [\"sed\", \"s/here/there/\"]\n")
	e := editortest.Start(t, path)
	e.Use(format.New())
	// The commands run in turn, and the cursor on a line they keep stays.
	editortest.Feed(e, "ll:w<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Disk(t, path) == "keep\ny there\n" })
	if editortest.Text(e) != "keep\ny there" || e.Cursor() != 2 || e.Windows()[0].Modified {
		t.Fatalf("after :w the buffer has %q, the cursor at %d", editortest.Text(e), e.Cursor())
	}
	// A formatter that fails leaves the text, which is written anyway.
	editortest.Config(t, "format_on_save = true\n[filetype.txt]\nformat = [\"false\"]")
	e = editortest.Start(t, path)
	e.Use(format.New())
	editortest.Feed(e, "x:w<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Disk(t, path) == "eep\ny there\n" })
	// A filetype's format_on_save wins over the one at the top.
	editortest.Config(t, "format_on_save = true\n[filetype.txt]\nformat_on_save = false\nformat = [\"y\"]\n[formatter.y]\ncommand = [\"sed\", \"s/y/z/\"]\n")
	e = editortest.Start(t, path)
	e.Use(format.New())
	editortest.Feed(e, "x:w<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Disk(t, path) == "ep\ny there\n" })
}

// B234
func TestB234Format(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "keep\nx here\n")
	editortest.Config(t, "[filetype.txt]\nformat = [\"y\", \"fude-no-such-formatter\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n")
	e := editortest.Start(t, path)
	e.Use(format.New())
	editortest.Feed(e, "j:format<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "keep\ny here" })
	if !e.Windows()[0].Modified || editortest.Disk(t, path) != "keep\nx here\n" {
		t.Fatalf("after :format the file has %q, want it unwritten", editortest.Disk(t, path))
	}
	if m := e.Message(); m != "fude-no-such-formatter not installed" {
		t.Errorf("after :format the message is %q, want the formatter not installed", m)
	}
	if editortest.Feed(e, "u"); editortest.Text(e) != "keep\nx here" {
		t.Errorf("u after :format left %q, want it undone in one step", editortest.Text(e))
	}
}

// B171
func TestB171FirstFormatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "ab\n")
	// The first is not installed and the second needs a root, so the third,
	// and only it, runs.
	editortest.Config(t, `format_on_save = true
[filetype.txt]
format = ["fude-no-such-formatter", "rooted", "upper", "lower"]
format_first = true
[formatter.rooted]
command = ["sed", "s/a/x/"]
root = ["no-such-root"]
workspace_required = true
[formatter.upper]
command = ["tr", "a-z", "A-Z"]
[formatter.lower]
command = ["tr", "A-Z", "a-z"]
`)
	e := editortest.Start(t, path)
	e.Use(format.New())
	editortest.Feed(e, ":w<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Disk(t, path) == "AB\n" })
}
