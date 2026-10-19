package bufferline_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/bufferline"
	"github.com/y3owk1n/fude/modules/buffers"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B258, B331
func TestB331BufferLineFollows(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "one/x.txt", "two/x.txt"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		editortest.Change(t, filepath.Join(dir, f), f+"\n")
	}
	t.Chdir(dir)
	e := editortest.Open(t, "a.txt")
	e.Use(bufferline.New())
	e.Use(buffers.New())
	names := func() []string {
		var out []string
		for _, tab := range e.Buffers() {
			out = append(out, tab.Name)
		}
		return out
	}
	// Files opened add to the line, with folders for two of one name, and
	// closing one names the other by its file alone.
	editortest.Feed(e, ":e one/x.txt<CR>:e two/x.txt<CR>")
	editortest.Settle(t, e, func() bool {
		return slices.Equal(names(), []string{"a.txt", filepath.Join("one", "x.txt"), filepath.Join("two", "x.txt")})
	})
	editortest.Feed(e, ":bd 2<CR>")
	editortest.Settle(t, e, func() bool { return slices.Equal(names(), []string{"a.txt", "x.txt"}) })
	// :set buffer_line turns the row off and on at once.
	editortest.Feed(e, ":set buffer_line false<CR>")
	editortest.Settle(t, e, func() bool { return len(names()) == 0 })
	editortest.Feed(e, ":set buffer_line true<CR>")
	editortest.Settle(t, e, func() bool { return len(names()) == 2 })
}
