package review_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/review"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B390, B391
func TestB391Review(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	editortest.Change(t, path, "a\nb\nc\n")
	e := editortest.Open(t, path)
	e.Use(review.New())
	e.SetSize(80, 24)
	agent := e.Attach("agent", editor.Host)
	ids, err := agent.Propose([]editor.ProposedEdit{{Buffer: 1, Start: 0, End: 1, Text: "A"}, {Buffer: 1, Start: 4, End: 5, Text: "C"}})
	if err != nil {
		t.Fatal(err)
	}
	// The window lists each proposal with the lines it changes.
	editortest.Feed(e, ":review<CR>")
	want := fmt.Sprintf("%s\n%d at line 1 by agent\n- a\n+ A\n%d at line 3 by agent\n- c\n+ C", path, ids[0], ids[1])
	if got := editortest.Text(e); got != want {
		t.Fatalf(":review shows %q, want %q", got, want)
	}
	// a on the first accepts it, and the window follows.
	editortest.Feed(e, "2Ga")
	editortest.Settle(t, e, func() bool { return !strings.Contains(editortest.Text(e), "+ A") })
	// <CR> goes to the second, and R in the window rejects it.
	editortest.Feed(e, "2G<CR>")
	if e.Text().Slice(0, e.Text().Len()) != "A\nb\nc" || e.Cursor() != 4 {
		t.Fatalf("after accepting the first, <CR> showed %q with the cursor at %d", e.Text().Slice(0, e.Text().Len()), e.Cursor())
	}
	editortest.Feed(e, ":review<CR>R")
	editortest.Settle(t, e, func() bool { return editortest.Text(e) == "no proposals" })
	if editortest.Feed(e, ":q<CR>:review<CR>"); e.Message() != "no proposals" {
		t.Fatalf(":review with none said %q", e.Message())
	}
}
