package jump_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/jump"
	"github.com/y3owk1n/fude/wire"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// labels returns the labels the window shows.
func labels(e *editor.Session) []wire.Ghost {
	return slices.DeleteFunc(slices.Clone(e.Windows()[0].Ghosts), func(g wire.Ghost) bool { return g.Style != "jump.label" })
}

// B218, B315, B316
func TestB218Jump(t *testing.T) {
	e := editortest.Open(t, "")
	e.Use(jump.New())
	e.SetSize(80, 10)
	editortest.Feed(e, "ifoo bar baz bat<Esc>0")
	// After two characters each match has a label, never a character that
	// would continue a match. A label covers the character after its match,
	// on the cursor's line too, and typing it goes to the match.
	editortest.Feed(e, "sba")
	l := labels(e)
	if len(l) != 3 || slices.ContainsFunc(l, func(g wire.Ghost) bool { return strings.Contains("rzt", g.Text) }) {
		t.Fatalf("sba labels %+v", l)
	}
	if segs := e.Windows()[0].Segments; !slices.Contains(segs, "sba") {
		t.Errorf("the status line shows %q while sba is typed", segs)
	}
	i := slices.IndexFunc(l, func(g wire.Ghost) bool { return g.Pos == 10 && g.End == 11 })
	if i < 0 {
		t.Fatalf("no label covers the z of baz in %+v", l)
	}
	if editortest.Feed(e, l[i].Text); e.Cursor() != 8 || len(labels(e)) != 0 {
		t.Errorf("the label of baz went to %d", e.Cursor())
	}
	// The jump lets the keys go. <CR> goes to the nearest, and <Esc> leaves
	// the cursor where it was.
	if editortest.Feed(e, "0sb<CR>"); e.Cursor() != 4 {
		t.Errorf("sb<CR> went to %d, want 4", e.Cursor())
	}
	if editortest.Feed(e, "sfo<Esc>"); e.Cursor() != 4 || len(labels(e)) != 0 {
		t.Errorf("sfo<Esc> moved the cursor to %d", e.Cursor())
	}
	// In visual mode a jump moves the selection's end.
	editortest.Feed(e, "0vsbat<CR>")
	if sel, ok := e.Selection(); !ok || sel.End != 13 {
		t.Errorf("vsbat<CR> made the selection %+v", sel)
	}
}
