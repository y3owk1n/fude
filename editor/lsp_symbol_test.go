package editor_test

import (
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/references"
)

// B460
func TestB460Declaration(t *testing.T) {
	e, log := withServer(t, "a target\nb target\n", "utf-8", `keymap.normal.gD = "<Plug>(lsp.declaration)"`)
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	feed(e, "gD")
	settle(t, e, func() bool { return e.Cursor() == 11 })
	if feed(e, "<C-o>"); e.Cursor() != 0 {
		t.Errorf("<C-o> after the declaration went to %d, want back to 0", e.Cursor())
	}
}

// B461
func TestB461References(t *testing.T) {
	e, log := withServer(t, "a target\nb target\n", "utf-8")
	e.Use(references.New())
	heard(t, log, `"method":"textDocument/didOpen"`, 1)
	lit := []string{"target reference", "target reference"}
	feed(e, "w")
	settle(t, e, func() bool { return slices.Equal(decorations(e), lit) })
	// Moving on the symbol keeps them, and moving off it takes them away.
	feed(e, "l")
	editortest.Quiet(t, e)
	if !slices.Equal(decorations(e), lit) {
		t.Errorf("moving on the symbol left %q", decorations(e))
	}
	feed(e, "0")
	settle(t, e, func() bool { return len(decorations(e)) == 0 })
	// In insert mode the cursor rests without them.
	feed(e, "wi")
	editortest.Quiet(t, e)
	if len(decorations(e)) != 0 {
		t.Errorf("resting in insert mode lit %q", decorations(e))
	}
}
