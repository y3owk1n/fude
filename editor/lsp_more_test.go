package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	explorermod "github.com/y3owk1n/fude/modules/explorer"
	"github.com/y3owk1n/fude/protocol/lsp"
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

// B475
func TestB475FileNotifications(t *testing.T) {
	t.Setenv("FUDE_FAKE_OFFER", "files")
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a.go"), filepath.Join(dir, "b.go"), filepath.Join(dir, "c.go")
	e, log := withServerAt(t, a, "x\n", "utf-8")
	e.Use(explorermod.New())
	// fude answers the server's registration on its loop.
	settle(t, e, func() bool {
		got, _ := os.ReadFile(log)
		return strings.Contains(string(got), `"id":"register"`)
	})
	watched := func(path string, kind int) string {
		return fmt.Sprintf(`{"type":%d,"uri":%q}`, kind, lsp.URI(path))
	}
	// A write of a file the server watches is a change, and of one it does
	// not watch is nothing.
	feed(e, ":w "+filepath.Join(dir, "a.txt")+"<CR>:w<CR>")
	heard(t, log, watched(a, 2), 1)
	if strings.Contains(disk(t, log), "a.txt") {
		t.Errorf("the server heard of a.txt, which it does not watch")
	}
	// The explorer renames a.go to b.go and creates c.go. The server edits
	// a.go first.
	feed(e, "-/a.go<CR>cwb<Esc>oc.go<Esc>:w<CR>y")
	settle(t, e, func() bool {
		_, err := os.Stat(b)
		return err == nil
	})
	heard(t, log, `"method":"workspace/willRenameFiles"`, 1)
	heard(t, log, `"text":"// moved\n"`, 1)
	heard(t, log, fmt.Sprintf(`"method":"workspace/didRenameFiles","params":{"files":[{"newUri":%q,"oldUri":%q}]}`, lsp.URI(b), lsp.URI(a)), 1)
	heard(t, log, fmt.Sprintf(`"method":"workspace/didCreateFiles","params":{"files":[{"uri":%q}]}`, lsp.URI(c)), 1)
	heard(t, log, watched(a, 3), 1)
	heard(t, log, watched(b, 1), 1)
	heard(t, log, watched(c, 1), 1)
	if will, did := strings.Index(disk(t, log), "willRenameFiles"), strings.Index(disk(t, log), "didRenameFiles"); will > did {
		t.Errorf("the server heard of the rename before it was asked for its edits")
	}
	// It deletes c.go.
	feed(e, "/c.go<CR>dd:w<CR>y")
	heard(t, log, fmt.Sprintf(`"method":"workspace/didDeleteFiles","params":{"files":[{"uri":%q}]}`, lsp.URI(c)), 1)
	heard(t, log, watched(c, 3), 1)
	// An open file found changed on disk is a change.
	feed(e, ":e "+b+"<CR>")
	change(t, b, "y\n")
	e.Focus()
	heard(t, log, watched(b, 2), 1)
}
