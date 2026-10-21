package flash_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/flash"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// flashed returns where the flash is, the start and end of each place in
// order, or nil.
func flashed(e *editor.Session) []int {
	var out []int
	for _, d := range e.Windows()[0].Decorations {
		if d.Name == "flash" {
			out = append(out, d.Start, d.End)
		}
	}
	return out
}

// B139, B313
func TestB139Flash(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	editortest.Change(t, path, "ab cd\n")
	e := editortest.Open(t, path)
	e.Use(flash.New())
	for _, c := range []struct {
		keys string
		want []int
	}{
		{"yiw", []int{0, 2}},
		{"p", []int{1, 3}},
		{"x", nil},
		{"u", []int{2, 3}},
		{"uu<C-r>", []int{1, 3}},
	} {
		editortest.Feed(e, c.keys)
		editortest.Settle(t, e, func() bool { return slices.Equal(flashed(e), c.want) })
	}
	// It goes after a moment.
	editortest.Settle(t, e, func() bool { return flashed(e) == nil })
	// With multicursors it shows at each.
	editortest.Change(t, path, "ab\ncd\n")
	e = editortest.Open(t, path)
	e.Use(flash.New())
	e.SetSize(80, 20)
	editortest.Feed(e, "Qjyiw")
	editortest.Settle(t, e, func() bool { return slices.Equal(flashed(e), []int{0, 2, 3, 5}) })
	// flash.enabled false turns it off.
	editortest.Config(t, "[module.flash]\nenabled = false\n")
	e = editortest.Start(t, path)
	e.Use(flash.New())
	editortest.Feed(e, "yiw")
	editortest.Quiet(t, e)
	if flashed(e) != nil {
		t.Errorf("with flash.enabled false the flash is %v, want none", flashed(e))
	}
}
