package editor_test

import (
	"slices"
	"testing"

	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/wire"
)

// B470
func TestB470PulledDiagnostics(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "diagnostic")
	t.Setenv("FUDE_FAKE_SYNC", `{"change":1,"save":{}}`)
	e, _ := withServer(t, "worse\n", "utf-8")
	errors := func(n int) func() bool { return func() bool { return e.Windows()[0].Errors == n } }
	settle(t, e, errors(1))
	// fude asks again after a change.
	feed(e, "dd")
	settle(t, e, errors(0))
	// The server's diagnostics change with each write, and it asks fude to
	// ask again.
	feed(e, ":w<CR>")
	settle(t, e, errors(1))
}

// B471
func TestB471SemanticTokens(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "tokens")
	t.Setenv("FUDE_FAKE_SYNC", "1")
	e, _ := withServer(t, "x call\n", "utf-8")
	function := func(at int) func() bool {
		return func() bool {
			return slices.Contains(e.Windows()[0].Highlights, syntax.Span{Start: at, End: at + 4, Name: "function"})
		}
	}
	settle(t, e, function(2))
	// The token moves with the text, as the server says again.
	feed(e, "Ia<Esc>")
	settle(t, e, function(3))
}

// B472
func TestB472FormatOnType(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "ontype")
	e, log := withServer(t, "\n", "utf-8")
	heard(t, log, `"method":"initialized"`, 1)
	feed(e, "ix;")
	settle(t, e, func() bool { return text(e) == "\tx;" })
	// The edits are part of the insert's undo step.
	feed(e, "y<Esc>u")
	if got := text(e); got != "" {
		t.Errorf("u after the insert left %q, want the text before it", got)
	}
}

// B473
func TestB473LinkedEditing(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "linked")
	e, log := withServer(t, "<div></div>\n", "utf-8")
	heard(t, log, `"method":"initialized"`, 1)
	feed(e, "l")
	settle(t, e, func() bool { return len(e.Windows()[0].Decorations) == 2 })
	feed(e, "ciwspan<Esc>")
	if got := text(e); got != "<span></span>" {
		t.Errorf("typing in a linked range left %q, want <span></span>", got)
	}
	// The edits to the other ranges are part of the insert's undo step.
	if feed(e, "u"); text(e) != "<div></div>" {
		t.Errorf("u left %q, want <div></div>", text(e))
	}
}

// B474
func TestB474InlineCompletion(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "inline")
	e, log := withServer(t, "\n", "utf-8")
	heard(t, log, `"method":"initialized"`, 1)
	ghost := func(at int, text string) func() bool {
		return func() bool {
			return slices.Contains(e.Windows()[0].Ghosts, wire.Ghost{Pos: at, Text: text, Style: "comment"})
		}
	}
	feed(e, "i")
	settle(t, e, ghost(0, "hello world"))
	// Typing what it suggests leaves the rest of it showing.
	feed(e, "he")
	settle(t, e, ghost(2, "llo world"))
	feed(e, "<Tab>")
	if got := text(e); got != "hello world" {
		t.Errorf("<Tab> left %q, want hello world", got)
	}
	heard(t, log, `"command":"fake.accepted"`, 1)
	// Out of insert mode it shows no more.
	feed(e, "<Esc>")
	if g := e.Windows()[0].Ghosts; len(g) > 0 {
		t.Errorf("normal mode shows %v", g)
	}
}
