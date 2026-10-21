package fold_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/fold"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

// B224
func TestB224Folds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	editortest.Change(t, path, "func a() {\n\tx\n\ty\n}\nfunc b() {\n\tz\n}\n")
	e := editortest.Start(t, path)
	e.Use(fold.New())
	e.SetSize(80, 20)
	folds := func() map[int]int { return e.Windows()[0].Folds }
	// zc in a body closes the fold of the line above it with less indent,
	// and j passes over it.
	if editortest.Feed(e, "jzc"); !maps.Equal(folds(), map[int]int{0: 2}) || e.Cursor() != 0 {
		t.Fatalf("zc made folds %v with the cursor at %d", folds(), e.Cursor())
	}
	if editortest.Feed(e, "j"); e.Text().LineOf(e.Cursor()) != 3 {
		t.Errorf("j over the fold went to line %d", e.Text().LineOf(e.Cursor()))
	}
	// A search into a closed fold opens it.
	if editortest.Feed(e, "gg/y<CR>"); folds() != nil {
		t.Errorf("searching into the fold left %v", folds())
	}
	// zM closes every outermost fold, za opens one, and zR all.
	if editortest.Feed(e, "zM"); !maps.Equal(folds(), map[int]int{0: 2, 4: 1}) {
		t.Errorf("zM made %v", folds())
	}
	if editortest.Feed(e, "za"); !maps.Equal(folds(), map[int]int{4: 1}) {
		t.Errorf("za made %v", folds())
	}
	if editortest.Feed(e, "zR"); folds() != nil {
		t.Errorf("zR left %v", folds())
	}
	// zf closes a fold over the lines it covers.
	if editortest.Feed(e, "ggzfj"); !maps.Equal(folds(), map[int]int{0: 1}) {
		t.Errorf("zfj made %v", folds())
	}
	// zj goes to where the next fold starts and zk to where the one above
	// ends, a closed fold counting as one line.
	for keys, want := range map[string]int{"zRggzj": 4, "zRGzk": 5, "zRG2zk": 2, "zMGzk": 4} {
		if editortest.Feed(e, keys); e.Text().LineOf(e.Cursor()) != want {
			t.Errorf("%s went to line %d, want %d", keys, e.Text().LineOf(e.Cursor()), want)
		}
	}
	// zm and zr fold one level more or less, from every fold open, and zv
	// opens the folds that hide the cursor's line. zA closes every fold the
	// cursor is in or opens them. zi opens every fold, and again closes them
	// as they were.
	editortest.Change(t, path, "a\n\tb\n\t\tc\n\td\ne\n")
	e = editortest.Start(t, path)
	e.Use(fold.New())
	e.SetSize(80, 20)
	for _, c := range []struct {
		keys string
		want map[int]int
	}{
		{"zm", map[int]int{1: 1}},
		{"zm", map[int]int{0: 3}},
		{"zm", map[int]int{0: 3}},
		{"zr", map[int]int{1: 1}},
		{"zr", nil},
		{"2zm", map[int]int{0: 3}},
		{"zv", map[int]int{1: 1}},
		{"jjzA", map[int]int{0: 3}},
		{"zA", nil},
		{"zMzi", nil},
		{"zi", map[int]int{0: 3}},
	} {
		if editortest.Feed(e, c.keys); !maps.Equal(e.Windows()[0].Folds, c.want) {
			t.Errorf("%s made %v, want %v", c.keys, e.Windows()[0].Folds, c.want)
		}
	}
}

// B502
func TestB502FoldLevels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	editortest.Change(t, path, "a\n\tb\n\t\tc\n\t\t\td\n\t\t\td\n\t\te\n\tf\ng\n")
	e := editortest.Start(t, path)
	e.Use(fold.New())
	e.SetSize(80, 20)
	// The folds start on lines 0, 1 and 2, and line 3 is in all three.
	for _, c := range []struct {
		keys string
		want map[int]int
	}{
		{"4Gzc", map[int]int{2: 2}},
		{"zc", map[int]int{1: 4}},
		{"zo", map[int]int{2: 2}},
		{"zR4G2zc", map[int]int{1: 4}},
		{"zR4GzC", map[int]int{0: 6}},
		{"zo", map[int]int{1: 4}},
		{"jza", map[int]int{2: 2}},
		{"za", map[int]int{1: 4}},
		{"zMzO", nil},
		{"4GzA", map[int]int{0: 6}},
		{"zA", nil},
	} {
		if editortest.Feed(e, c.keys); !maps.Equal(e.Windows()[0].Folds, c.want) {
			t.Errorf("%s made %v, want %v", c.keys, e.Windows()[0].Folds, c.want)
		}
	}
}

// B488
func TestB488FoldColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	editortest.Change(t, path, "func a() {\n\tx\n\n\ty\n}\nb\n")
	e := editortest.Start(t, path)
	e.Use(fold.New())
	e.SetSize(80, 20)
	signs := func() map[int]string {
		out := map[int]string{}
		for line, s := range e.Windows()[0].Signs {
			out[line] = s.Text
		}
		return out
	}
	want := map[int]string{0: "-", 1: "│", 2: "│", 3: "│"}
	editortest.Feed(e, ":set fold.column true<CR>")
	editortest.Settle(t, e, func() bool { return maps.Equal(signs(), want) })
	// A closed fold shows its own mark.
	editortest.Feed(e, "zc")
	editortest.Settle(t, e, func() bool { return signs()[0] == "+" })
	editortest.Feed(e, ":set fold.column false<CR>")
	editortest.Settle(t, e, func() bool { return len(signs()) == 0 })
}
