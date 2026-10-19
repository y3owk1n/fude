package lists_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/internal/clipboard"
	"github.com/y3owk1n/fude/modules/lists"
	"github.com/y3owk1n/fude/modules/places"
	"github.com/y3owk1n/fude/modules/window"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// disk is the text of the file at path.
func disk(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// B157
func TestB157InfoViews(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	editortest.Change(t, path, "one\ntwo\nthree\n")
	e := editortest.Open(t, path)
	e.Use(lists.New())
	e.Use(places.New())
	e.Use(window.New())
	e.SetSize(80, 24)
	// :messages lists the messages shown so far.
	for _, keys := range []string{":set number<CR>", ":set wrap<CR>"} {
		editortest.Feed(e, keys)
		e.Message()
	}
	if editortest.Feed(e, ":messages<CR>"); editortest.Text(e) != "number false\nwrap true" {
		t.Fatalf(":messages listed %q", editortest.Text(e))
	}
	// :marks lists the marks, and <CR> jumps to one.
	// The list shows once modules/places hears of it.
	editortest.Feed(e, ":q<CR>majjmb:marks<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "a  1:1  one\nb  3:1  three" })
	if editortest.Text(e) != "a  1:1  one\nb  3:1  three" {
		t.Fatalf(":marks listed %q", editortest.Text(e))
	}
	if editortest.Feed(e, "j<CR>"); e.Cursor() != 8 {
		t.Fatalf("<CR> on mark b went to %d", e.Cursor())
	}
	// :jumps and :changes list where the cursor jumped and the text changed.
	editortest.Feed(e, ":only<CR>ggGx:jumps<CR>")
	editortest.Settle(t, e, func() bool { return strings.Contains(editortest.Text(e), "1:1  one") })
	if !strings.Contains(editortest.Text(e), "1:1  one") {
		t.Fatalf(":jumps listed %q", editortest.Text(e))
	}
	editortest.Feed(e, ":q<CR>:changes<CR>")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "1  3:1  hree" })
	if editortest.Text(e) != "1  3:1  hree" {
		t.Fatalf(":changes listed %q", editortest.Text(e))
	}
	// :registers with no name lists every register that holds text.
	if editortest.Feed(e, ":q<CR>ggyiw:registers<CR>"); e.Message() != `""   one  "-   t  "0   one` {
		t.Fatalf(":registers listed %q", e.Message())
	}
}

// B268
func TestB268YankMessage(t *testing.T) {
	board := filepath.Join(t.TempDir(), "board")
	t.Cleanup(clipboard.Fake(board))
	e := editortest.Open(t, "")
	e.Use(lists.New())
	if editortest.Feed(e, ":messages yank<CR>"); e.Message() != "no messages" {
		t.Errorf(":messages yank with none said %q", e.Message())
	}
	long := strings.Repeat("word ", 40) + "end"
	editortest.Feed(e, ":"+long+"<CR>")
	shown := e.Message()
	if editortest.Feed(e, ":messages yank<CR>"); e.Message() != "copied the last message" || disk(t, board) != shown {
		t.Errorf(":messages yank said %q and put %q on the clipboard, want %q", e.Message(), disk(t, board), shown)
	}
}
