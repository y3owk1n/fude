package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/wire"
)

// proposeQuote has agent propose text for the quote around at.
func proposeQuote(t *testing.T, agent *editor.Session, at int, text string) {
	t.Helper()
	r := editor.Request{At: &at, Base: agent.Text().Version(), Proposal: true, Command: editor.Command{Op: "change", Target: editor.Target{Name: "inner_quote"}, Text: text}}
	if err := agent.Request(r); err != nil {
		t.Fatal(err)
	}
}

// B71
func TestB71ProposalIsGhostText(t *testing.T) {
	e := open(t, "")
	feed(e, `isay "hi" now<Esc>0`)
	agent := e.Attach("agent", editor.Host)
	proposeQuote(t, agent, 5, "hello")
	v := e.Windows()[0]
	if text(e) != `say "hi" now` || e.Cursor() != 0 || len(v.Ghosts) != 1 || v.Ghosts[0] != (wire.Ghost{Pos: 7, Text: "ello"}) {
		t.Fatalf("got %q, cursor %d and ghosts %v, want the text unchanged and ello after hi", text(e), e.Cursor(), v.Ghosts)
	}
}

// B72
func TestB72AcceptIsOneNode(t *testing.T) {
	e := open(t, "")
	feed(e, `isay "hi" now<Esc>`)
	agent := e.Attach("agent", editor.Host)
	proposeQuote(t, agent, 5, "hello")
	feed(e, ":accept<CR>")
	if got := text(e); got != `say "hello" now` {
		t.Fatalf("accepting made %q", got)
	}
	proposeQuote(t, agent, 5, "bye")
	feed(e, ":reject<CR>")
	// The agent's one undo reverts the accepted proposal whole, and the
	// rejected one left nothing to undo.
	feed(agent, "u")
	if got := text(e); got != `say "hi" now` {
		t.Fatalf("undoing the accepted proposal left %q", got)
	}
	feed(agent, "u")
	if agent.Message() != "already at the oldest change" {
		t.Fatalf("a second undo said %q", agent.Message())
	}
}

// B74
func TestB74ProposalFollowsEdits(t *testing.T) {
	e := open(t, "")
	feed(e, `isay "hi" now<Esc>`)
	agent := e.Attach("agent", editor.Host)
	proposeQuote(t, agent, 5, "hello")
	feed(e, "0iwe <Esc>:accept<CR>")
	if got := text(e); got != `we say "hello" now` {
		t.Fatalf("accepting after an edit before it made %q", got)
	}
	proposeQuote(t, agent, 8, "bye")
	feed(e, `0f"lx:accept<CR>`)
	if got := text(e); got != `we say "ello" now` || e.Message() != "proposal 2 is stale" {
		t.Fatalf("accepting after an edit inside it made %q and said %q", got, e.Message())
	}
}

// B387
func TestB387ProposeChange(t *testing.T) {
	dir := t.TempDir()
	other, fresh := filepath.Join(dir, "other.txt"), filepath.Join(dir, "new.txt")
	change(t, other, "one\n")
	e := open(t, "")
	feed(e, "ihello world<Esc>")
	agent := e.Attach("agent", editor.Host)
	v := agent.Text().Version()
	old := v - 1
	// One edit at an old version, or two that overlap, propose nothing.
	for _, edits := range [][]editor.ProposedEdit{
		{{Buffer: 1, Start: 6, End: 11, Text: "there"}, {Buffer: 1, Version: &old, Start: 0, End: 5, Text: "bye"}},
		{{Buffer: 1, Start: 0, End: 5, Text: "bye"}, {Buffer: 1, Start: 3, End: 8, Text: "x"}},
	} {
		if _, err := agent.Propose(edits); err == nil || len(e.Windows()[0].Ghosts) != 0 {
			t.Fatalf("%v proposed %v with error %v", edits, e.Windows()[0].Ghosts, err)
		}
	}
	ids, err := agent.Propose([]editor.ProposedEdit{
		{Buffer: 1, Version: &v, Start: 0, End: 5, Text: "goodbye"},
		{Path: other, Start: 0, End: 3, Text: "two"},
		{Path: fresh, Text: "fresh"},
	})
	if err != nil || len(ids) != 3 {
		t.Fatalf("proposing three edits gave %v, %v", ids, err)
	}
	if text(e) != "hello world" || len(e.Windows()[0].Ghosts) != 1 {
		t.Fatalf("the change made the text %q with ghosts %v", text(e), e.Windows()[0].Ghosts)
	}
	// The files a change names open with no window, and accepting changes
	// their buffers, not the files.
	feed(e, fmt.Sprintf(":accept %d %d<CR>:e %s<CR>", ids[1], ids[2], other))
	if text(e) != "two" || disk(t, other) != "one\n" {
		t.Fatalf("accepting the edit of other.txt made its buffer %q and its file %q", text(e), disk(t, other))
	}
	if feed(e, ":e "+fresh+"<CR>"); text(e) != "fresh" {
		t.Fatalf("accepting the edit of a new file made %q", text(e))
	}
	if _, err := os.Stat(fresh); err == nil {
		t.Fatal("accepting the edit of a new file wrote it")
	}
}

// B388
func TestB388AcceptSeveral(t *testing.T) {
	e := open(t, "")
	feed(e, "ione two three<Esc>")
	agent := e.Attach("agent", editor.Host)
	ids, err := agent.Propose([]editor.ProposedEdit{{Buffer: 1, Start: 0, End: 3, Text: "1"}, {Buffer: 1, Start: 4, End: 7, Text: "2"}, {Buffer: 1, Start: 8, End: 13, Text: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	// An edit inside the third makes it stale, and it stays as the others
	// apply.
	feed(e, "$x")
	feed(e, fmt.Sprintf(":accept %d %d %d<CR>", ids[0], ids[1], ids[2]))
	if text(e) != "1 2 thre" || e.Message() != fmt.Sprintf("proposal %d is stale", ids[2]) {
		t.Fatalf("accepting three, one stale, made %q and said %q", text(e), e.Message())
	}
	// The agent's one undo reverts both.
	if feed(agent, "u"); text(e) != "one two thre" {
		t.Fatalf("one undo of the agent's left %q", text(e))
	}
}
