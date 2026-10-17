package editor

import (
	"bufio"
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// prose are the filetypes spell checks whole, but for their code and links
// (B259). A file of no filetype is prose.
var prose = map[string]bool{"": true, "markdown": true, "text": true, "gitcommit": true}

// endings are what a word may end in past a word of the list, as plurals and
// contractions, which lists such as macOS's leave out (B259).
var endings = []struct{ cut, add string }{
	{"'s", ""},
	{"s", ""},
	{"es", ""},
	{"ies", "y"},
	{"n't", ""},
	{"'t", ""},
	{"'re", ""},
	{"'ll", ""},
	{"'ve", ""},
	{"'d", ""},
	{"'m", ""},
}

// knows says whether known has word, as it is or in lower case, or without
// one of its endings.
func knows(known map[string]bool, word string) bool {
	for _, w := range []string{word, strings.ToLower(word)} {
		if known[w] {
			return true
		}
		for _, end := range endings {
			if base, ok := strings.CutSuffix(w, end.cut); ok && base != "" && known[base+end.add] {
				return true
			}
		}
	}
	return false
}

// words returns where the words of s that spell checks are, as [start,
// end) in s: runs of letters with apostrophes inside them. A word with a
// capital after its first letter, or a digit or one of _ . / @ \ beside it,
// is a name or a path, and is not checked, nor is one letter alone.
func words(s string) [][2]int {
	var out [][2]int
	name := func(r rune) bool { return unicode.IsDigit(r) || strings.ContainsRune("_./@\\", r) }
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsLetter(r) {
			i += n
			continue
		}
		start, capital := i, false
		for i < len(s) {
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == '\'' && i+n < len(s) {
				if next, _ := utf8.DecodeRuneInString(s[i+n:]); unicode.IsLetter(next) {
					i += n
					continue
				}
			}
			if !unicode.IsLetter(r) {
				break
			}
			capital = capital || i > start && unicode.IsUpper(r)
			i += n
		}
		before, _ := utf8.DecodeLastRuneInString(s[:start])
		after, _ := utf8.DecodeRuneInString(s[i:])
		if !capital && utf8.RuneCountInString(s[start:i]) > 1 && !name(before) && !name(after) {
			out = append(out, [2]int{start, i})
		}
	}
	return out
}

// typos returns, as spans named spell.bad, the words of t in [from, to) that
// known lacks. In prose it checks all but the code and links hl colours, and
// elsewhere only the comments and strings (B259).
func typos(t kernel.Snapshot, from, to int, ft string, hl []syntax.Span, known map[string]bool) []syntax.Span {
	covered := func(a, b int, names ...string) bool {
		return slices.ContainsFunc(hl, func(s syntax.Span) bool {
			return s.Start <= a && s.End >= b && slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(s.Name, n) })
		})
	}
	text := t.Slice(from, to)
	var out []syntax.Span
	for _, w := range words(text) {
		a, b := from+w[0], from+w[1]
		checked := covered(a, b, "comment", "string")
		if prose[ft] {
			checked = !covered(a, b, "markup.raw", "markup.link.url")
		}
		if checked && !knows(known, text[w[0]:w[1]]) {
			out = append(out, syntax.Span{Start: a, End: b, Name: "spell.bad"})
		}
	}
	return out
}

// ownWords is the file of words zg adds, in the config directory.
func ownWords() (string, error) {
	dir, err := xdgDir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fude", "words.txt"), nil
}

// readWords reads the words of the list at path and of the user's own file,
// one a line. A missing own file has no words.
func readWords(path string) (map[string]bool, error) {
	known := map[string]bool{}
	read := func(path string) error {
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		lines := bufio.NewScanner(f)
		for lines.Scan() {
			if w := strings.TrimSpace(lines.Text()); w != "" {
				known[w] = true
			}
		}
		return lines.Err()
	}
	if err := read(path); err != nil {
		return nil, fmt.Errorf("spell has no word list, so spell_words should name one: %w", err)
	}
	own, err := ownWords()
	if err == nil {
		if err = read(own); os.IsNotExist(err) {
			err = nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("read your words: %w", err)
	}
	return known, nil
}

// dictionary returns the words spell knows, or nil until they are read
// off the loop, which the first call starts. A list that fails to read
// says so once, and spell then finds nothing (B259).
func (e *Session) dictionary() map[string]bool {
	if e.dict != nil || e.reading {
		return e.dict
	}
	e.reading = true
	go func() {
		known, err := readWords(e.cfg.SpellWords)
		e.queue <- func() {
			if err != nil {
				e.message = err.Error()
				return
			}
			e.dict = known
		}
	}()
	return nil
}

// stepTypo moves to the next word spell finds after the cursor, or with
// back the one before, wrapping at the ends of the text (B260).
func (e *Session) stepTypo(back bool) {
	known, ok := e.spelling()
	if !ok {
		return
	}
	b := e.win.buf
	t, ft := b.text.Snapshot(), e.filetype(b)
	var hl []syntax.Span
	if b.syntax != nil {
		var err error
		if hl, err = syntax.Highlight(ft, e.queries, t.Slice(0, t.Len())); err != nil {
			e.message = err.Error()
			return
		}
	}
	found := typos(t, 0, t.Len(), ft, hl, known)
	if len(found) == 0 {
		e.message = "no misspelled words"
		return
	}
	// With none after the cursor, or before it with back, the search wraps
	// to the other end.
	c, i := e.Cursor(), 0
	if back {
		i = len(found) - 1
		for k := len(found) - 1; k >= 0; k-- {
			if found[k].Start < c {
				i = k
				break
			}
		}
	} else if k := slices.IndexFunc(found, func(s syntax.Span) bool { return s.Start > c }); k >= 0 {
		i = k
	}
	e.setCursor(found[i].Start)
	e.reveal()
}

// spelling returns the words spell knows, or says why spell cannot check
// the current window yet.
func (e *Session) spelling() (map[string]bool, bool) {
	if !e.win.opts.Spell {
		e.message = "spell is off, and :set spell turns it on"
		return nil, false
	}
	known := e.dictionary()
	if known == nil {
		e.message = cmp.Or(e.message, "spell is reading its word list")
		return nil, false
	}
	return known, true
}

// wordAt returns where the word at pos of t is, as words finds words, or
// false when pos is in none.
func wordAt(t kernel.Snapshot, pos int) (int, int, bool) {
	start, end := vim.LineStart(t, pos), vim.LineEnd(t, pos)
	line := t.Slice(start, end)
	i := slices.IndexFunc(words(line), func(w [2]int) bool { return start+w[0] <= pos && pos < start+w[1] })
	if i < 0 {
		return 0, 0, false
	}
	w := words(line)[i]
	return start + w[0], start + w[1], true
}

// suggest lists the words of the list closest to the one under the cursor,
// and the one chosen takes its place, as z= does (B260).
func (e *Session) suggest() {
	known, ok := e.spelling()
	if !ok {
		return
	}
	t := e.Text()
	start, end, ok := wordAt(t, e.Cursor())
	if !ok {
		e.message = "no word under the cursor"
		return
	}
	word := t.Slice(start, end)
	lower := strings.ToLower(word)
	type near struct {
		word string
		d    int
	}
	var nears []near
	for w := range known {
		if d := distance(lower, strings.ToLower(w), 2); d <= 2 && w != word {
			nears = append(nears, near{w, d})
		}
	}
	slices.SortFunc(nears, func(a, b near) int { return cmp.Or(a.d-b.d, strings.Compare(a.word, b.word)) })
	var items []string
	for _, n := range nears[:min(len(nears), 9)] {
		w := n.word
		// A word that starts with a capital keeps it.
		if r, _ := utf8.DecodeRuneInString(word); unicode.IsUpper(r) {
			w = strings.ToUpper(w[:1]) + w[1:]
		}
		if !slices.Contains(items, w) {
			items = append(items, w)
		}
	}
	if len(items) == 0 {
		e.message = "no words close to " + word
		return
	}
	b := e.win.buf
	e.pickFrom("spelling of "+word, items, func(i int) {
		if e.win.buf != b || t.Version() != b.text.Snapshot().Version() {
			e.message = "the text changed, so " + word + " stays"
			return
		}
		if err := (host{e}).Apply([]vim.Edit{{Start: start, End: end, Text: items[i]}}, "z="); err != nil {
			e.message = err.Error()
		}
	})
}

// distance returns how many rune insertions, deletions and changes turn a
// into b, or most+1 once that is past most.
func distance(a, b string, most int) int {
	ra, rb := []rune(a), []rune(b)
	if d := len(ra) - len(rb); d > most || -d > most {
		return most + 1
	}
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		low := cur[0]
		for j := 1; j <= len(rb); j++ {
			change := 1
			if ra[i-1] == rb[j-1] {
				change = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+change)
			low = min(low, cur[j])
		}
		if low > most {
			return most + 1
		}
		prev = cur
	}
	return prev[len(rb)]
}

// good adds the word under the cursor to the user's own words, so spell
// no longer finds it, as zg does (B260).
func (e *Session) good() {
	known, ok := e.spelling()
	if !ok {
		return
	}
	t := e.Text()
	start, end, ok := wordAt(t, e.Cursor())
	if !ok {
		e.message = "no word under the cursor"
		return
	}
	word := t.Slice(start, end)
	path, err := ownWords()
	if err == nil {
		err = os.MkdirAll(filepath.Dir(path), 0o755)
	}
	var f *os.File
	if err == nil {
		f, err = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	}
	if err == nil {
		_, err = fmt.Fprintln(f, word)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		e.message = fmt.Sprintf("add %s to your words: %v", word, err)
		return
	}
	known[word] = true
	e.message = "added " + word + " to " + path
}
