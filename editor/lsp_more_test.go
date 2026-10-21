package editor_test

import (
	"slices"
	"testing"

	"github.com/y3owk1n/fude/syntax"
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
