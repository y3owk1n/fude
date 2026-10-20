package flash_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/flash"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// flashed returns where the flash is, or nil.
func flashed(e *editor.Session) []int {
	for _, d := range e.Windows()[0].Decorations {
		if d.Name == "flash" {
			return []int{d.Start, d.End}
		}
	}
	return nil
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
	// flash_time = 0 turns it off.
	editortest.Config(t, "flash_time = 0\n")
	e = editortest.Start(t, path)
	e.Use(flash.New())
	editortest.Feed(e, "yiw")
	// The module's answer arrives through the loop.
	for wait := time.After(200 * time.Millisecond); ; {
		select {
		case f := <-e.Queue():
			f()
			continue
		case <-wait:
		}
		break
	}
	if flashed(e) != nil {
		t.Errorf("with flash_time = 0 the flash is %v, want none", flashed(e))
	}
}
