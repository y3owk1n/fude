package editor_test

import "testing"

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
