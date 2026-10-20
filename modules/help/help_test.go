package help_test

import (
	"os"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/help"
	"github.com/y3owk1n/fude/vim"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B162
func TestB162Help(t *testing.T) {
	e := editortest.Open(t, "")
	e.Use(help.New())
	e.SetSize(100, 40)
	line := func() string {
		t := e.Text()
		ls := t.LineStart(t.LineOf(e.Cursor()))
		return t.Slice(ls, vim.LineEnd(t, ls))
	}
	// Help opens on its index, and the reference comes from the tables.
	if editortest.Feed(e, ":h<CR>"); !strings.HasPrefix(editortest.Text(e), "# fude help") || !strings.Contains(editortest.Text(e), "`:write [file]  :w`\n  writes the buffer") {
		t.Fatalf(":h opened %q", editortest.Text(e)[:min(200, len(editortest.Text(e)))])
	}
	for topic, want := range map[string]string{":w": "`:write [file]  :w`", "w": "`w`", "theme": "`theme = \"dark\"`", "colors": "`[colors]`", "guide-start": "# Getting started"} {
		if editortest.Feed(e, ":h "+topic+"<CR>"); line() != want {
			t.Errorf(":h %s went to %q, want %q", topic, line(), want)
		}
	}
	// Every command, key, option and setting has a longer explanation after
	// its line.
	for _, section := range []string{"## Motions", "## Operators", "## Text objects", "## Normal mode", "## Visual mode", "## Insert mode", "## Command line", "## Commands", "## Commands for keys", "## Options", "## Settings"} {
		_, rest, _ := strings.Cut(editortest.Text(e), "\n"+section+"\n")
		rest, _, _ = strings.Cut(rest, "\n## ")
		blocks := strings.Split(rest, "\n\n")
		for i, b := range blocks {
			// An entry is its head alone in code, then its line of help.
			head, help, _ := strings.Cut(b, "\n")
			entry := strings.HasPrefix(head, "`") && strings.HasSuffix(head, "`") && strings.HasPrefix(help, "  ")
			if entry && (i+1 == len(blocks) || !strings.HasPrefix(blocks[i+1], "  ")) {
				t.Errorf("%s has no longer explanation", strings.SplitN(b, "\n", 2)[0])
			}
		}
	}
	if editortest.Feed(e, ":h nope<CR>"); e.Message() != "no help for nope" {
		t.Errorf(":h nope said %q", e.Message())
	}
	// <CR> on a name goes to it, from a description or the index.
	if editortest.Feed(e, ":h ;<CR>jff<CR>"); line() != "`f{char}`" {
		t.Errorf("<CR> on f went to %q", line())
	}
	if editortest.Feed(e, ":h<CR>/guide-config<CR><CR>"); line() != "# Configuring fude" {
		t.Errorf("<CR> on guide-config went to %q", line())
	}
}
