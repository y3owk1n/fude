package blame_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/blame"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B220
func TestB220Blame(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	editortest.Change(t, path, "one\ntwo\n")
	git := func(args ...string) {
		if out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=Ann", "-c", "user.email=a@a"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "first words")
	e := editortest.Open(t, path)
	e.Use(blame.New())
	e.SetSize(80, 10)
	note := func(want string) func() bool {
		return func() bool { return strings.Contains(e.Windows()[0].Notes[e.Text().LineOf(e.Cursor())].Text, want) }
	}
	// :blame shows who changed the cursor's line, and a line changed since
	// shows it is not committed.
	editortest.Feed(e, "j:blame<CR>")
	editortest.Settle(t, e, note("Ann, just now · first words"))
	editortest.Feed(e, "Ax<Esc>")
	editortest.Settle(t, e, note("not committed yet"))
	if editortest.Feed(e, ":blame<CR>"); e.Windows()[0].Notes[1].Text != "" {
		t.Errorf("after :blame again the line shows %q", e.Windows()[0].Notes[1].Text)
	}
}
