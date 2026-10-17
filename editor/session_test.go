package editor_test

import (
	"testing"

	"github.com/y3owk1n/fude/editor"
)

// B73
func TestB73SessionsConverge(t *testing.T) {
	e := open(t, "")
	agent := e.Attach("agent", editor.Host)
	feed(e, "ione")
	feed(agent, "0itwo <Esc>")
	feed(e, " three<Esc>")
	if got := text(e); got != "two one three" || text(agent) != got {
		t.Fatalf("got %q and %q, want both %q", got, text(agent), "two one three")
	}
}

// B77
func TestB77UndoOwnChanges(t *testing.T) {
	e := open(t, "")
	agent := e.Attach("agent", editor.Host)
	// The agent's change lands while the first session is still typing.
	feed(e, "ihello")
	feed(agent, "A world<Esc>")
	feed(e, "<Esc>u")
	if got := text(e); got != " world" {
		t.Fatalf("after the first session's undo got %q, want %q", got, " world")
	}
	feed(agent, "u")
	if got := text(e); got != "" {
		t.Fatalf("after the agent's undo got %q, want it empty", got)
	}
}
