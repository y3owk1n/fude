package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/frontend/wire"
	buffersmod "github.com/y3owk1n/fude/modules/buffers"
	placesmod "github.com/y3owk1n/fude/modules/places"
	windowmod "github.com/y3owk1n/fude/modules/window"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// The helpers of editortest, by the short names the tests use.
var (
	open   = editortest.Open
	feed   = editortest.Feed
	text   = editortest.Text
	disk   = editortest.Disk
	change = editortest.Change
	start  = editortest.Start
	config = editortest.Config
	settle = editortest.Settle
)

// withCommands is e with the modules that hold :b, :bn, :bp, :bd, :enew,
// the window commands and the lists of places, as fude has them.
// floatAt returns the float over the layout at anchor, with its text, and
// whether one shows there (D55).
func floatAt(e *editor.Session, anchor string) (wire.Window, string) {
	for _, w := range e.Windows() {
		if w.Float == anchor {
			return w, w.Text.Slice(0, w.Text.Len())
		}
	}
	return wire.Window{}, ""
}

// floated reports whether the current window is a float.
func floated(e *editor.Session) bool {
	return slices.ContainsFunc(e.Windows(), func(w wire.Window) bool { return w.Float != "" && w.Current })
}

func withCommands(e *editor.Session) *editor.Session {
	e.Use(buffersmod.New())
	e.Use(windowmod.New())
	e.Use(placesmod.New())
	return e
}

func windows(e *editor.Session) string {
	var out []string
	for _, v := range e.Windows() {
		cur := ""
		if v.Current {
			cur = "*"
		}
		out = append(out, fmt.Sprintf("%s%d,%d %dx%d", cur, v.X, v.Y, v.W, v.H))
	}
	return strings.Join(out, " ")
}

func run(t *testing.T, cases [][3]string) {
	t.Helper()
	for _, c := range cases {
		path := filepath.Join(t.TempDir(), "f")
		if err := os.WriteFile(path, []byte(c[0]), 0o644); err != nil {
			t.Fatal(err)
		}
		e := open(t, path)
		feed(e, c[1])
		got := text(e)
		if got = got[:e.Cursor()] + "|" + got[e.Cursor():]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

func captures(t *testing.T, e *editor.Session) map[string]string {
	t.Helper()
	deadline := time.After(editortest.Patience)
	for {
		for _, v := range e.Windows() {
			if v.Current && len(v.Highlights) > 0 {
				out := map[string]string{}
				for _, s := range v.Highlights {
					text := v.Text.Slice(s.Start, s.End)
					out[text] = strings.TrimSpace(out[text] + " " + s.Name)
				}
				return out
			}
		}
		select {
		case f := <-e.Queue():
			f()
		case <-deadline:
			t.Fatal("no highlights")
		}
	}
}
