package surround_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/surround"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B219, B322
func TestB219Surround(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		{"say |hi there", "ysiw)", "say |(hi) there"},
		{"say |hi there", "ysiw(", "say |( hi ) there"},
		{"say |hi there", `ysiw"`, `say |"hi" there`},
		{"  |say hi", "yss]", "  |[say hi]"},
		{"f(|a, b)", "ds(", "f|a, b"},
		{"f( |a )", "ds(", "f|a"},
		{`x = "|a"`, `cs"'`, "x = |'a'"},
		{"f(|a)", "cs)}", "f|{a}"},
		{"|ab cd", "vlS]", "|[ab] cd"},
		// Dot repeats a surround with its character.
		{"|a b", "ysiw)W.", "(a) |(b)"},
		{"f(|a) g(b)", "ds)fb.", "fa g|b"},
		// <Esc> cancels one waiting for its character.
		{"|ab", "ysiw<Esc>", "|ab"},
		// A blank names no pair.
		{"(|a)", "ds<Space>", "(|a)"},
		{"(|a)", "cs<Space>x", "(|a)"},
		// d and c work as they do without it.
		{"|ab cd", "dw", "|cd"},
	} {
		path := filepath.Join(t.TempDir(), "a.txt")
		editortest.Change(t, path, strings.Replace(c.start, "|", "", 1)+"\n")
		e := editortest.Open(t, path)
		e.Use(surround.New())
		editortest.Feed(e, fmt.Sprintf("%dgo%s", strings.Index(c.start, "|")+1, c.keys))
		text := strings.TrimSuffix(editortest.Text(e), "\n")
		if got := text[:e.Cursor()] + "|" + text[e.Cursor():]; got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}
