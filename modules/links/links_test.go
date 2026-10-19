package links_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/links"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m, "markdown", "markdown_inline")) }

// B301, gx
func TestB301Open(t *testing.T) {
	// A fake opener of each system logs what it is asked to open.
	got := editortest.FakeProgram(t, "open", "xdg-open", "rundll32")
	dir := t.TempDir()
	for _, c := range []struct{ name, text, keys, want string }{
		// A markdown link opens its destination from its text.
		{"notes.md", "see [the site](https://x.y/z) now\n", "fs", "https://x.y/z"},
		// A link in any file opens without the marks around it.
		{"a.txt", "go to (https://a.b/c), then\n", "fh", "https://a.b/c"},
	} {
		path := filepath.Join(dir, c.name)
		editortest.Change(t, path, c.text)
		e := editortest.Open(t, path)
		e.Use(links.New())
		os.Remove(got)
		editortest.Feed(e, c.keys+"gx")
		editortest.Settle(t, e, func() bool { return e.Message() == "opened "+c.want })
		// Its target comes last, after what Windows' rundll32 takes first.
		b, err := os.ReadFile(got)
		if words := strings.Fields(string(b)); err != nil || len(words) == 0 || words[len(words)-1] != c.want {
			t.Errorf("gx in %s opened %q, %v, want %q", c.name, b, err, c.want)
		}
	}
}

// B301, gf
func TestB301File(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "other.txt"), "one\ntwo\nthree\n")
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	editortest.Change(t, filepath.Join(dir, "sub", "d.md"), "doc\n")
	path := filepath.Join(dir, "notes.md")
	editortest.Change(t, path, "see other.txt:2 and [doc](sub/d.md) or none\n")
	e := editortest.Open(t, path)
	e.Use(links.New())
	// A path opens beside the file, at the line after its colon.
	editortest.Feed(e, "fogf")
	editortest.Settle(t, e, func() bool { return strings.HasSuffix(e.Views()[0].Name, "other.txt") })
	if line := e.Text().LineOf(e.Cursor()); line != 1 {
		t.Errorf("gf went to line %d of other.txt, want 2", line+1)
	}
	// A markdown link opens its destination.
	editortest.Feed(e, "<C-o>f[gf")
	editortest.Settle(t, e, func() bool { return strings.HasSuffix(e.Views()[0].Name, filepath.Join("sub", "d.md")) })
	// A word that names no file says so.
	editortest.Feed(e, "<C-o>$gf")
	editortest.Settle(t, e, func() bool { return e.Message() == "no file named none" })
}
