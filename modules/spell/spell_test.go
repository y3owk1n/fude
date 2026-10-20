package spell_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/editor/editortest"
	"github.com/y3owk1n/fude/modules/spell"
)

func TestMain(m *testing.M) { os.Exit(editortest.Main(m)) }

func typosIn(e *editor.Session) []string {
	var out []string
	t := e.Text()
	for _, d := range e.Windows()[0].Decorations {
		if d.Name == "spell.bad" {
			out = append(out, t.Slice(d.Start, d.End))
		}
	}
	return out
}

// spelling adds spell to e, with the user's own words in the config
// directory.
func spelling(t *testing.T, e *editor.Session) {
	t.Helper()
	dir, err := editor.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	e.Use(spell.New(filepath.Join(dir, "words.txt")))
}

// B259
func TestB259Spell(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "words")
	editortest.Change(t, list, strings.Join([]string{"the", "cat", "sat", "on", "mat", "is", "this", "comment", "has", "a", "string", "typo"}, "\n")+"\n")
	editortest.Config(t, fmt.Sprintf("[module.spell]\nenabled = true\nwords = %q\n", list))
	// Prose checks every word but names and paths, and plurals and
	// contractions of the list's words pass.
	notes := filepath.Join(dir, "notes")
	editortest.Change(t, notes, "the cats sta on teh mat, isn't fooBar a_b file.go\n")
	e := editortest.Start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	editortest.Settle(t, e, func() bool { return len(typosIn(e)) > 0 })
	if got := typosIn(e); !slices.Equal(got, []string{"sta", "teh"}) {
		t.Errorf("spell found %q in prose", got)
	}
	// Code checks only its comments and strings. A filetype table turns
	// spell on for its files.
	code := filepath.Join(dir, "a.go")
	editortest.Change(t, code, "package main\n\n// this comment has a tpyo\nvar x = \"a string wth typo\"\n\nfunc mian() {}\n")
	editortest.Config(t, fmt.Sprintf("[module.spell]\nwords = %q\n[filetype.go]\nspell = true\n", list))
	e = editortest.Start(t, code)
	spelling(t, e)
	e.SetSize(80, 20)
	editortest.Settle(t, e, func() bool { return len(typosIn(e)) == 2 })
	if got := typosIn(e); !slices.Equal(got, []string{"tpyo", "wth"}) {
		t.Errorf("spell found %q in code", got)
	}
	// spell is off unless set.
	editortest.Config(t, fmt.Sprintf("[module.spell]\nwords = %q\n", list))
	e = editortest.Start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	editortest.Quiet(t, e)
	if got := typosIn(e); len(got) > 0 {
		t.Fatalf("with spell off spell found %q", got)
	}
	// A list that fails to read says so, and spell then finds nothing.
	editortest.Config(t, "[module.spell]\nenabled = true\nwords = \"/nowhere/words\"\n")
	e = editortest.Start(t, notes)
	spelling(t, e)
	e.Windows()
	editortest.Settle(t, e, func() bool { return strings.HasPrefix(e.Message(), "spell has no word list") })
	if got := typosIn(e); len(got) > 0 {
		t.Errorf("without a word list spell found %q", got)
	}
}

// B260
func TestB260SpellKeys(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "words")
	editortest.Change(t, list, "the\ncat\nsat\non\nmat\n")
	editortest.Config(t, fmt.Sprintf("[module.spell]\nenabled = true\nwords = %q\n", list))
	notes := filepath.Join(dir, "notes")
	editortest.Change(t, notes, "the cta sat on teh mat\nmat sta\n")
	e := editortest.Start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	editortest.Settle(t, e, func() bool { return len(typosIn(e)) > 0 })
	// ]s and [s go through the words found, wrapping at the ends.
	for _, c := range []struct {
		keys string
		want int
	}{{"]s", 4}, {"]s", 15}, {"]s", 27}, {"]s", 4}, {"[s", 27}, {"[s", 15}} {
		if editortest.Feed(e, c.keys); e.Cursor() != c.want {
			t.Errorf("%s left the cursor at %d, want %d", c.keys, e.Cursor(), c.want)
		}
	}
	// z= lists the list's closest words, and the one chosen replaces it.
	editortest.Feed(e, "0]sz=")
	editortest.Settle(t, e, func() bool { f, _ := e.Menu(); return strings.Contains(f.Text, "cat") })
	// The choice reaches spell off the loop, and its edit follows.
	editortest.Feed(e, "<CR>")
	editortest.Settle(t, e, func() bool { return strings.HasPrefix(editortest.Text(e), "the cat sat") })
	// zg adds the word to words.txt, and spell passes it from then on.
	editortest.Feed(e, "]szg")
	if !strings.HasPrefix(e.Message(), "added teh to ") {
		t.Errorf("zg said %q", e.Message())
	}
	path := strings.TrimPrefix(e.Message(), "added teh to ")
	if editortest.Disk(t, path) != "teh\n" || slices.Contains(typosIn(e), "teh") {
		t.Errorf("after zg, words.txt holds %q and spell finds %q", editortest.Disk(t, path), typosIn(e))
	}
}

// B39, B259: :set spell.words reads the list it names.
func TestSetSpellWords(t *testing.T) {
	dir := t.TempDir()
	first, second := filepath.Join(dir, "first"), filepath.Join(dir, "second")
	editortest.Change(t, first, "the\ncat\n")
	editortest.Change(t, second, "the\ncat\nsat\n")
	editortest.Config(t, fmt.Sprintf("[module.spell]\nenabled = true\nwords = %q\n", first))
	notes := filepath.Join(dir, "notes")
	editortest.Change(t, notes, "the cat sat\n")
	e := editortest.Start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	editortest.Settle(t, e, func() bool { return slices.Equal(typosIn(e), []string{"sat"}) })
	// The new list is read when spell next hears of the window, here a move.
	editortest.Feed(e, fmt.Sprintf(":set spell.words %s<CR>w", second))
	editortest.Settle(t, e, func() bool { return len(typosIn(e)) == 0 })
}
