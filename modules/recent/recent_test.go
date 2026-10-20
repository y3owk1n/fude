package recent_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/recent"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// rows returns the finder's items.
func rows(e *editor.Session) []string {
	f, _ := e.Menu()
	return slices.DeleteFunc(strings.Split(f.Text, "\n")[1:], func(l string) bool { return l == "" })
}

// B213, B314
func TestB213Recent(t *testing.T) {
	dir := t.TempDir()
	editortest.Change(t, filepath.Join(dir, "a.txt"), "a\n")
	editortest.Change(t, filepath.Join(dir, "b.txt"), "b\n")
	t.Chdir(dir)
	e := editortest.Open(t, "a.txt")
	e.Use(recent.New())
	e.SetSize(100, 30)
	editortest.Feed(e, ":e b.txt<CR>")
	// kept waits for the recent files to name each of paths.
	kept := func(paths ...string) {
		editortest.Settle(t, e, func() bool {
			r, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_STATE_HOME"), "fude", "recent"))
			for _, p := range paths {
				if abs, _ := filepath.Abs(p); !strings.Contains(string(r), abs) {
					return false
				}
			}
			return true
		})
	}
	kept("a.txt", "b.txt")
	// It lists the files but the current one, and <CR> opens the one
	// picked.
	editortest.Feed(e, ":pick recent<CR>")
	editortest.Settle(t, e, func() bool { return slices.Contains(rows(e), "a.txt") && !slices.Contains(rows(e), "b.txt") })
	editortest.Feed(e, "a.txt<CR>")
	editortest.Settle(t, e, func() bool { return e.Windows()[0].Name == "a.txt" })
	editortest.Feed(e, ":e b.txt<CR>")
	// A recent file outside the working directory comes after its own, even
	// when it is newer.
	out := filepath.Join(t.TempDir(), "o.txt")
	editortest.Change(t, out, "o\n")
	editortest.Feed(e, ":e "+out+"<CR>:e b.txt<CR>")
	kept(out)
	editortest.Feed(e, ":pick recent<CR>")
	editortest.Settle(t, e, func() bool {
		r := rows(e)
		at := func(name string) int {
			return slices.IndexFunc(r, func(l string) bool { return strings.HasPrefix(l, name) })
		}
		return at("a.txt") >= 0 && at("a.txt") < at("o.txt")
	})
	editortest.Feed(e, "<Esc>")
	// A name that starts with .. is still under the working directory.
	editortest.Change(t, filepath.Join(dir, "..dots"), "d\n")
	editortest.Feed(e, ":e ..dots<CR>:e b.txt<CR>")
	kept("..dots")
	editortest.Feed(e, ":pick recent<CR>")
	editortest.Settle(t, e, func() bool { return slices.Contains(rows(e), "..dots") })
}
