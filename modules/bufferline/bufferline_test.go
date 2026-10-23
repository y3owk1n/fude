package bufferline_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	// :set bufferline.enabled turns the row off and on at once.
	editortest.Feed(e, ":set bufferline.enabled false<CR>")
	editortest.Settle(t, e, func() bool { return len(names()) == 0 })
	editortest.Feed(e, ":set bufferline.enabled true<CR>")
	editortest.Settle(t, e, func() bool { return len(names()) == 2 })
}

// B554
func TestB554MoveBuffer(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"a.txt", "b.txt", "c.txt"} {
		editortest.Change(t, filepath.Join(dir, f), f+"\n")
	}
	t.Chdir(dir)
	e := editortest.Open(t, "a.txt")
	e.Use(bufferline.New())
	e.Use(buffers.New())
	editortest.Feed(e, ":e b.txt<CR>:e c.txt<CR>")
	names := func() string {
		var out []string
		for _, tab := range e.Buffers() {
			out = append(out, strings.TrimSuffix(tab.Name, ".txt"))
		}
		return strings.Join(out, " ")
	}
	for _, c := range []struct{ keys, want string }{
		{":bmove 0<CR>", "c a b"},
		{":bmove +1<CR>", "a c b"},
		{":bmove<CR>", "a b c"},
		{":b a.txt<CR>:bmove 2<CR>", "b a c"},
		{":bmove -1<CR>", "a b c"},
	} {
		editortest.Feed(e, c.keys)
		editortest.Settle(t, e, func() bool { return names() == c.want })
	}
	// A place past the row's ends is refused.
	if editortest.Feed(e, ":bmove 9<CR>"); e.Message() != "cannot move the buffer to 9" || names() != "a b c" {
		t.Errorf(":bmove 9 said %q and left %q", e.Message(), names())
	}
	// A buffer opened after a move goes last.
	editortest.Change(t, filepath.Join(dir, "d.txt"), "d\n")
	editortest.Feed(e, ":bmove 0<CR>:e d.txt<CR>")
	editortest.Settle(t, e, func() bool { return names() == "a b c d" })
}
