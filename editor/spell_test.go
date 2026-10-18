package editor_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/y3owk1n/fude/editor"
	"github.com/y3owk1n/fude/modules/spell"
)

func typosIn(e *editor.Session) []string {
	var out []string
	t := e.Text()
	for _, d := range e.Views()[0].Decorations {
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
	change(t, list, strings.Join([]string{"the", "cat", "sat", "on", "mat", "is", "this", "comment", "has", "a", "string", "typo"}, "\n")+"\n")
	config(t, fmt.Sprintf("spell = true\nspell_words = %q\n", list))
	// Prose checks every word but names and paths, and plurals and
	// contractions of the list's words pass.
	notes := filepath.Join(dir, "notes")
	change(t, notes, "the cats sta on teh mat, isn't fooBar a_b file.go\n")
	e := start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	settle(t, e, func() bool { return len(typosIn(e)) > 0 })
	if got := typosIn(e); !slices.Equal(got, []string{"sta", "teh"}) {
		t.Errorf("spell found %q in prose", got)
	}
	// Code checks only its comments and strings.
	code := filepath.Join(dir, "a.go")
	change(t, code, "package main\n\n// this comment has a tpyo\nvar x = \"a string wth typo\"\n\nfunc mian() {}\n")
	e = start(t, code)
	spelling(t, e)
	e.SetSize(80, 20)
	settle(t, e, func() bool { return len(typosIn(e)) == 2 })
	if got := typosIn(e); !slices.Equal(got, []string{"tpyo", "wth"}) {
		t.Errorf("spell found %q in code", got)
	}
	// spell is off unless set.
	config(t, fmt.Sprintf("spell_words = %q\n", list))
	e = start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	// The loop runs long enough for a word list to be read.
	for end := time.After(300 * time.Millisecond); ; {
		if got := typosIn(e); len(got) > 0 {
			t.Fatalf("with spell off spell found %q", got)
		}
		select {
		case f := <-e.Queue():
			f()
			continue
		case <-time.After(10 * time.Millisecond):
			continue
		case <-end:
		}
		break
	}
	// A list that fails to read says so, and spell then finds nothing.
	config(t, "spell = true\nspell_words = \"/nowhere/words\"\n")
	e = start(t, notes)
	spelling(t, e)
	e.Views()
	settle(t, e, func() bool { return strings.HasPrefix(e.Message(), "spell has no word list") })
	if got := typosIn(e); len(got) > 0 {
		t.Errorf("without a word list spell found %q", got)
	}
}

// B260
func TestB260SpellKeys(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "words")
	change(t, list, "the\ncat\nsat\non\nmat\n")
	config(t, fmt.Sprintf("spell = true\nspell_words = %q\n", list))
	notes := filepath.Join(dir, "notes")
	change(t, notes, "the cta sat on teh mat\nmat sta\n")
	e := start(t, notes)
	spelling(t, e)
	e.SetSize(80, 20)
	settle(t, e, func() bool { return len(typosIn(e)) > 0 })
	// ]s and [s go through the words found, wrapping at the ends.
	for _, c := range []struct {
		keys string
		want int
	}{{"]s", 4}, {"]s", 15}, {"]s", 27}, {"]s", 4}, {"[s", 27}, {"[s", 15}} {
		if feed(e, c.keys); e.Cursor() != c.want {
			t.Errorf("%s left the cursor at %d, want %d", c.keys, e.Cursor(), c.want)
		}
	}
	// z= lists the list's closest words, and the one chosen replaces it.
	feed(e, "0]sz=")
	settle(t, e, func() bool { f, _ := e.Float(); return strings.Contains(f.Text, "cat") })
	// The choice reaches spell off the loop, and its edit follows.
	feed(e, "<CR>")
	settle(t, e, func() bool { return strings.HasPrefix(text(e), "the cat sat") })
	// zg adds the word to words.txt, and spell passes it from then on.
	feed(e, "]szg")
	if !strings.HasPrefix(e.Message(), "added teh to ") {
		t.Errorf("zg said %q", e.Message())
	}
	path := strings.TrimPrefix(e.Message(), "added teh to ")
	if disk(t, path) != "teh\n" || slices.Contains(typosIn(e), "teh") {
		t.Errorf("after zg, words.txt holds %q and spell finds %q", disk(t, path), typosIn(e))
	}
}
