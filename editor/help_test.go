package editor_test

import (
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/vim"
)

// B162
func TestB162Help(t *testing.T) {
	e := open(t, "")
	e.SetSize(100, 40)
	line := func() string {
		t := e.Text()
		ls := t.LineStart(t.LineOf(e.Cursor()))
		return t.Slice(ls, vim.LineEnd(t, ls))
	}
	// Help opens on its index, and the reference comes from the tables.
	if feed(e, ":h<CR>"); !strings.HasPrefix(text(e), "# fude help") || !strings.Contains(text(e), "`:write [file]  :w`\n  writes the buffer") {
		t.Fatalf(":h opened %q", text(e)[:min(200, len(text(e)))])
	}
	for topic, want := range map[string]string{":w": "`:write [file]  :w`", "w": "`w`", "theme": "`theme = \"dark\"`", "colors": "`[colors]`", "guide-start": "# Getting started"} {
		if feed(e, ":h "+topic+"<CR>"); line() != want {
			t.Errorf(":h %s went to %q, want %q", topic, line(), want)
		}
	}
	// Every option and setting has a longer explanation after its line.
	for _, section := range []string{"## Options", "## Settings"} {
		_, rest, _ := strings.Cut(text(e), "\n"+section+"\n")
		rest, _, _ = strings.Cut(rest, "\n## ")
		blocks := strings.Split(rest, "\n\n")
		for i, b := range blocks {
			if strings.HasPrefix(b, "`") && (i+1 == len(blocks) || !strings.HasPrefix(blocks[i+1], "  ")) {
				t.Errorf("%s has no longer explanation", strings.SplitN(b, "\n", 2)[0])
			}
		}
	}
	if feed(e, ":h nope<CR>"); e.Message() != "no help for nope" {
		t.Errorf(":h nope said %q", e.Message())
	}
	// <CR> on a name goes to it, from a description or the index.
	if feed(e, ":h ;<CR>jff<CR>"); line() != "`f{char}`" {
		t.Errorf("<CR> on f went to %q", line())
	}
	if feed(e, ":h<CR>/guide-config<CR><CR>"); line() != "# Configuring fude" {
		t.Errorf("<CR> on guide-config went to %q", line())
	}
}

// B163
func TestB163Man(t *testing.T) {
	// Each page has its own part of help.
	for name, want := range map[string]string{
		"":       ".TP\n\\fB:write [file]  :w\\fR\nwrites the buffer",
		"config": ".TP\n\\fBtheme = \"dark\"\\fR",
		"guide":  ".SH GETTING STARTED",
	} {
		page, err := editor.Man(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(page, ".TH FUDE") || !strings.Contains(page, want) {
			t.Errorf("man page %q is %q...", name, page[:min(300, len(page))])
		}
	}
}
