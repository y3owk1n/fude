package editor

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/y3owk1n/fude/vim"
)

// B35, :earlier and :later by time, with a clock the test moves.
func TestB35EarlierLaterByTime(t *testing.T) {
	e, err := Open(filepath.Join(t.TempDir(), "f"))
	if err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(0, 0)
	e.now = func() time.Time { return clock }
	type step struct {
		keys string
		at   time.Duration
	}
	for _, s := range []step{{"Aa<Esc>", 0}, {"Ab<Esc>", time.Minute}, {"Ac<Esc>", 2 * time.Minute}} {
		clock = time.Unix(0, 0).Add(s.at)
		for _, k := range vim.Keys(s.keys) {
			e.Feed(k)
		}
	}
	for _, c := range []struct{ cmd, want string }{
		{":earlier 90s<CR>", "a"},
		{":later 70s<CR>", "abc"},
		{":earlier 1m<CR>", "ab"},
	} {
		for _, k := range vim.Keys(c.cmd) {
			e.Feed(k)
		}
		if s := e.Text(); s.Slice(0, s.Len()) != c.want {
			t.Errorf("%s left %q, want %q", c.cmd, s.Slice(0, s.Len()), c.want)
		}
	}
}
