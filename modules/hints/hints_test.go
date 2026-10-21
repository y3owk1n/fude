package hints_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/hints"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B172, B326
func TestB172KeyHints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "ab\n")
	editortest.Config(t, "[module.hints]\ndelay = 1\n[keymap.normal]\n\"<Leader>w\" = \":w<CR>\"\n\"<Leader>sg\" = \":pick grep<CR>\"\n[keymap.groups]\n\"<Leader>s\" = \"search\"\n")
	e := editortest.Start(t, path)
	e.Use(hints.New())
	e.SetSize(100, 30)
	shows := func(want string) bool { f, ok := e.Menu(); return ok && strings.Contains(f.Text, want) }
	// After a mapping's first key, the hints say what may follow, and the key
	// waits for the next one instead of key_timeout.
	editortest.Feed(e, "<Space>")
	editortest.Settle(t, e, func() bool { return shows("w  :w<CR>") })
	// A key that starts longer ones shows its group's name.
	if f, _ := e.Menu(); f.Title != "<Space>" || !strings.Contains(f.Text, "s  +search 1") {
		t.Fatalf("the hints show %q titled %q", f.Text, f.Title)
	}
	if _, waiting := e.Wait(); waiting {
		t.Error("key_timeout runs while the hints show")
	}
	// The hints follow the command as it goes on, and <BS> goes back a key.
	editortest.Feed(e, "s")
	editortest.Settle(t, e, func() bool { return shows("g  :pick<Space>grep<CR>") })
	editortest.Feed(e, "<BS>")
	editortest.Settle(t, e, func() bool { return shows("w  :w<CR>") })
	// <Esc> drops the held key, so the cursor stays, and the hints close.
	if editortest.Feed(e, "<Esc>"); e.Cursor() != 0 {
		t.Errorf("<Esc> ran the held <Space>, the cursor is at %d", e.Cursor())
	}
	if _, ok := e.Menu(); ok {
		t.Error("the hints stay after <Esc>")
	}
	// The machine's own keys follow g, in a column as wide as the widest.
	editortest.Feed(e, "g")
	row := regexp.MustCompile(`(?m)(^|  )0 +goes to the start of the screen row`)
	editortest.Settle(t, e, func() bool { f, ok := e.Menu(); return ok && row.MatchString(f.Text) })
}
