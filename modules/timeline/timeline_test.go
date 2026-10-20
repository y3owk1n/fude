package timeline_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/timeline"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B31
func TestB31Timeline(t *testing.T) {
	e := editortest.Open(t, filepath.Join(t.TempDir(), "f"))
	e.Use(timeline.New())
	editortest.Feed(e, "ione<Esc>uitwo<Esc>:timeline<CR>")
	lines := strings.Split(editortest.Text(e), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "  0") || !strings.HasPrefix(lines[1], "  1") ||
		!strings.HasPrefix(lines[2], "* 2") || !strings.HasSuffix(lines[2], "(from 0)") {
		t.Fatalf(":timeline listed %q", lines)
	}
	editortest.Feed(e, ":q<CR>:restore 1<CR>")
	if editortest.Text(e) != "one" {
		t.Fatalf(":restore 1 left %q, want one", editortest.Text(e))
	}
	editortest.Feed(e, ":timeline<CR>")
	lines = strings.Split(editortest.Text(e), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[3], "* 3") || !strings.HasSuffix(lines[3], "restore 1") {
		t.Errorf("after :restore, :timeline listed %q, want a new state 3 and state 2 kept", lines)
	}
	// Outside one, :restore with no number says so, as the window's keys do.
	if editortest.Feed(e, ":q<CR>:restore<CR>"); e.Message() != "not in a timeline" {
		t.Errorf(":restore outside a timeline said %q", e.Message())
	}
	editortest.Feed(e, ":timeline<CR>")
	// In a timeline window, :restore takes the state on the cursor's line.
	editortest.Feed(e, "3G:restore<CR>:q<CR>")
	if editortest.Text(e) != "two" {
		t.Errorf(":restore on the line of state 2 left %q, want two", editortest.Text(e))
	}
	// A command that stands for an operator, as x for dl, shows as typed.
	editortest.Feed(e, "x:timeline<CR>")
	if lines = strings.Split(editortest.Text(e), "\n"); !strings.HasSuffix(lines[len(lines)-1], " x") {
		t.Errorf("after x, :timeline listed %q", lines)
	}

	// In the window, ] moves the text a state later, <CR> restores the
	// state on top of the newest, and d diffs it against now.
	path := filepath.Join(t.TempDir(), "g")
	e = editortest.Open(t, path)
	e.Use(timeline.New())
	source := func() string {
		for _, v := range e.Windows() {
			if v.Name == path {
				return v.Text.Slice(0, v.Text.Len())
			}
		}
		return ""
	}
	editortest.Feed(e, "ione<Esc>uitwo<Esc>:timeline<CR>]")
	if source() != "one" {
		t.Fatalf("] left %q, want state 1", source())
	}
	editortest.Feed(e, "<CR>")
	lines = strings.Split(editortest.Text(e), "\n")
	if source() != "one" || len(lines) != 4 || !strings.HasPrefix(lines[3], "* 3") {
		t.Fatalf("<CR> left %q and listed %q, want state 1 restored as state 3", source(), lines)
	}
	editortest.Feed(e, "ggd")
	if want := "@@ -1 +1 @@\n-\n+one"; editortest.Text(e) != want {
		t.Errorf("d on state 0 showed %q, want %q", editortest.Text(e), want)
	}
}

// B32
func TestB32Diff(t *testing.T) {
	e := editortest.Open(t, filepath.Join(t.TempDir(), "f"))
	e.Use(timeline.New())
	editortest.Feed(e, "ia<CR>one<CR>c<Esc>uia<CR>two<CR>c<Esc>:diff 1<CR>")
	if want := "@@ -1 +1 @@\n a\n-one\n+two\n c"; editortest.Text(e) != want {
		t.Errorf(":diff 1 showed %q, want %q", editortest.Text(e), want)
	}
}

// B35
func TestB35EarlierLater(t *testing.T) {
	for _, c := range [][3]string{
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>", "x|1"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>:later<CR>", "x1|2"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>uA9<Esc>g-", "x12|3"},
		{"x", "A1<Esc>A2<Esc>A3<Esc>:earlier 2<CR>uA9<Esc>g-g-", "x1|2"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 1f<CR>", "x|1"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 1f<CR>:earlier 1f<CR>", "|x"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 2f<CR>:later 1f<CR>", "x|1"},
		{"x", "A1<Esc>:w<CR>A2<Esc>:earlier 2f<CR>:later 2f<CR>", "x1|2"},
	} {
		path := filepath.Join(t.TempDir(), "f")
		editortest.Change(t, path, c[0])
		e := editortest.Open(t, path)
		e.Use(timeline.New())
		editortest.Feed(e, c[1])
		got := editortest.Text(e)
		if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}
