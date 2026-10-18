package editor_test

import (
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/modules/format"
)

// B136
func TestB136FormatOnSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "keep\nx here\n")
	config(t, "format_on_save = true\n[filetype.txt]\nformat = [\"y\", \"there\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n[formatter.there]\ncommand = [\"sed\", \"s/here/there/\"]\n")
	e := start(t, path)
	e.Use(format.New())
	// The commands run in turn, and the cursor on a line they keep stays.
	feed(e, "ll:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "keep\ny there\n" })
	if text(e) != "keep\ny there" || e.Cursor() != 2 || e.Views()[0].Modified {
		t.Fatalf("after :w the buffer has %q, the cursor at %d", text(e), e.Cursor())
	}
	// A formatter that fails leaves the text, which is written anyway.
	config(t, "format_on_save = true\n[filetype.txt]\nformat = [\"false\"]")
	e = start(t, path)
	e.Use(format.New())
	feed(e, "x:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "eep\ny there\n" })
	// A filetype's format_on_save wins over the one at the top.
	config(t, "format_on_save = true\n[filetype.txt]\nformat_on_save = false\nformat = [\"y\"]\n[formatter.y]\ncommand = [\"sed\", \"s/y/z/\"]\n")
	e = start(t, path)
	e.Use(format.New())
	feed(e, "x:w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "ep\ny there\n" })
}

// B234
func TestB234Format(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "keep\nx here\n")
	config(t, "[filetype.txt]\nformat = [\"y\", \"fude-no-such-formatter\"]\n[formatter.y]\ncommand = [\"sed\", \"s/x/y/\"]\n")
	e := start(t, path)
	e.Use(format.New())
	feed(e, "j:format<CR>")
	settle(t, e, func() bool { return text(e) == "keep\ny here" })
	if !e.Views()[0].Modified || disk(t, path) != "keep\nx here\n" {
		t.Fatalf("after :format the file has %q, want it unwritten", disk(t, path))
	}
	if m := e.Message(); m != "fude-no-such-formatter not installed" {
		t.Errorf("after :format the message is %q, want the formatter not installed", m)
	}
	if feed(e, "u"); text(e) != "keep\nx here" {
		t.Errorf("u after :format left %q, want it undone in one step", text(e))
	}
}

// B171
func TestB171FirstFormatter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	change(t, path, "ab\n")
	// The first is not installed and the second needs a root, so the third,
	// and only it, runs.
	config(t, `format_on_save = true
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
	e := start(t, path)
	e.Use(format.New())
	feed(e, ":w<CR>")
	settle(t, e, func() bool { return disk(t, path) == "AB\n" })
}
