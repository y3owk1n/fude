package editor_test

import (
	"testing"

	"github.com/y3owk1n/fude/editor"
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
	agent := e.Attach("agent")
	proposeQuote(t, agent, 5, "hello")
	v := e.Views()[0]
	if text(e) != `say "hi" now` || e.Cursor() != 0 || len(v.Ghosts) != 1 || v.Ghosts[0] != (editor.Ghost{Pos: 7, Text: "ello"}) {
		t.Fatalf("got %q, cursor %d and ghosts %v, want the text unchanged and ello after hi", text(e), e.Cursor(), v.Ghosts)
	}
}

// B72
func TestB72AcceptIsOneNode(t *testing.T) {
	e := open(t, "")
	feed(e, `isay "hi" now<Esc>`)
	agent := e.Attach("agent")
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
	agent := e.Attach("agent")
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
