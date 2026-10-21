package editor_test

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/lens"
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

// B464, B465
func TestB464Hierarchy(t *testing.T) {
	elsewhere := filepath.FromSlash("/elsewhere/b.go")
	for _, c := range []struct{ keys, want string }{
		{"gI", elsewhere + ":2:12: caller"},
		{"gX", "a.go:2:12: callee"},
		// Two types are under the cursor, so a menu offers them.
		{"gS2", elsewhere + ":3:6: Base interface"},
	} {
		e, log := withServer(t, "func f() {}\nfunc g() { f() }\n", "utf-8",
			`keymap.normal.gI = "<Plug>(lsp.incoming_calls)"`, `keymap.normal.gX = "<Plug>(lsp.outgoing_calls)"`, `keymap.normal.gS = "<Plug>(lsp.supertypes)"`)
		withCommands(e)
		heard(t, log, `"method":"textDocument/didOpen"`, 1)
		dir := filepath.Dir(e.Windows()[0].Name)
		feed(e, c.keys[:2])
		if len(c.keys) > 2 {
			settle(t, e, func() bool { _, ok := e.Menu(); return ok })
			feed(e, c.keys[2:])
		}
		settle(t, e, func() bool { return len(e.Windows()) == 2 })
		if got := strings.ReplaceAll(text(e), dir+string(filepath.Separator), ""); got != c.want {
			t.Errorf("%s listed %q, want %q", c.keys, got, c.want)
		}
	}
}

// B462, B463
func TestB462CodeLens(t *testing.T) {
	e, log := withServer(t, "package p\n\tfunc f() {}\n", "utf-8")
	e.Use(lens.New())
	row := func() string {
		for _, v := range e.Windows()[0].Virtual[1] {
			if v.Above && v.Style == "lens" {
				return v.Text
			}
		}
		return ""
	}
	// The lens that came without a title shows once it is resolved, from
	// the first lens's column, and stays on the cursor's line.
	settle(t, e, func() bool { return strings.HasSuffix(row(), " | 2 references") })
	shown := row()
	if !strings.HasPrefix(shown, "\trun ") {
		t.Errorf("the lenses show as %q", shown)
	}
	feed(e, "j")
	editortest.Quiet(t, e)
	if row() != shown {
		t.Errorf("on the cursor's line the lenses show as %q, want %q", row(), shown)
	}
	// grx offers the line's lenses, the second asks for them again, and the
	// first runs its command.
	feed(e, "grx")
	settle(t, e, func() bool { _, ok := e.Menu(); return ok })
	feed(e, "2")
	settle(t, e, func() bool { return row() != shown && strings.HasSuffix(row(), " | 2 references") })
	feed(e, "grx1")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "cmd ") })
	heard(t, log, `"command":"fake.run"`, 1)
	if feed(e, "gggrx"); e.Message() != "no code lens on this line" {
		t.Errorf("grx on a line with no lens said %q", e.Message())
	}
}
