package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// B110: the vim module's grammar cases give the same result here as with
// its minimal host, with s unmapped, as fude maps it to a jump (N32).
func TestB110GrammarCases(t *testing.T) {
	src, err := os.ReadFile("../vim/testdata/grammar.txt")
	if err != nil {
		t.Fatal(err)
	}
	config(t, "[keymap.normal]\n\"s\" = \"\"\n[keymap.visual]\n\"s\" = \"\"\n")
	for _, l := range strings.Split(strings.TrimRight(string(src), "\n"), "\n") {
		if l != "" && !strings.HasPrefix(l, "# ") {
			grammarCase(t, l)
		}
	}
}

// B162: every example in help's reference gives what it shows.
func TestB162Examples(t *testing.T) {
	files, err := filepath.Glob("reference/*.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, block := range strings.Split(string(src), "```keys\n")[1:] {
				block, _, _ = strings.Cut(block, "```")
				for _, l := range strings.Split(strings.TrimSpace(block), "\n") {
					grammarCase(t, l)
				}
			}
		})
	}
}

// grammarCase checks a case written as the text with | at the cursor, the
// keys and the text after them, each quoted, in a file of the editor's own.
func grammarCase(t *testing.T, l string) {
	t.Helper()
	var c [3]string
	if _, err := fmt.Sscanf(l, "%q %q %q", &c[0], &c[1], &c[2]); err != nil {
		t.Fatalf("%s: %v", l, err)
	}
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, strings.Replace(c[0], "|", "", 1)+"\n")
	e := start(t, path)
	e.SetSize(80, 23)
	feed(e, fmt.Sprintf("%dgo%s", strings.Index(c[0], "|")+1, c[1]))
	got := text(e)
	if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
		t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
	}
}
