package autopairs_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/autopairs"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B202, B323
func TestB202AutoPairs(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		{"|", "i(", "(|)"},
		{"|)", "i[", "[|])"},
		{"|x", "i(", "(|x"},
		{"|", "i(a)", "(a)|"},
		{"|", `i"`, `"|"`},
		{"|", `i""`, `""|`},
		{"do|n", "a'", "don'|"},
		{"|", "i(<BS>", "|"},
		{"(|)", "i<BS>", "|"},
		{"a|b", "i<BS>", "|b"},
		// Dot repeats what was typed with its pairs.
		{"|", "i(a<Esc>$.", "(a(|a))"},
		// With autopairs off, a bracket comes alone.
		{"|", ":set autopairs false<CR>i(", "(|"},
	} {
		path := filepath.Join(t.TempDir(), "a.txt")
		editortest.Change(t, path, strings.Replace(c.start, "|", "", 1)+"\n")
		e := editortest.Open(t, path)
		e.Use(autopairs.New())
		editortest.Feed(e, fmt.Sprintf("%dgo%s", strings.Index(c.start, "|")+1, c.keys))
		text := strings.TrimSuffix(editortest.Text(e), "\n")
		if got := text[:e.Cursor()] + "|" + text[e.Cursor():]; got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
	// A paste is never paired.
	e := editortest.Open(t, "")
	e.Use(autopairs.New())
	editortest.Feed(e, "i")
	if e.Paste("("); editortest.Text(e) != "(" {
		t.Errorf("pasting ( made %q", editortest.Text(e))
	}
}
