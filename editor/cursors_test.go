package editor_test

import (
	"path/filepath"
	"testing"
)

// B397, as the window shows another buffer (N43)
func TestB397CursorsEndWithTheirBuffer(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	change(t, a, "ab\ncd\n")
	change(t, b, "ef\ngh\n")
	e := withCommands(start(t, a, b))
	if feed(e, "Qj"); len(e.Cursors()) != 1 {
		t.Fatalf("Qj placed %v", e.Cursors())
	}
	if feed(e, ":bn<CR>"); len(e.Cursors()) != 0 {
		t.Errorf("the other buffer shows the multicursors %v", e.Cursors())
	}
	// A key there removes them for good.
	if feed(e, "x:bp<CR>gQ"); len(e.Cursors()) != 0 || text(e) != "ab\ncd" {
		t.Errorf("back in their buffer, gQ put back %v, and the text is %q", e.Cursors(), text(e))
	}
}
