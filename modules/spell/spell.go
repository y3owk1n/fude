// Package spell underlines the words its word list lacks, and goes to,
// fixes and adds them (B259, B260).
package spell

import (
	"bufio"
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
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

// rows is more lines than a window shows. Spell underlines that many from
// the top of the view.
const rows = 200

// read is the id of the job that reads the word list.
const read = 1

// What spell registers, by the ids its calls run them by.
const (
	next uint32 = iota + 1
	prev
	suggest
	good
)

// Module is spell.
type Module struct {
	mu    sync.Mutex
	own   string           // the file of the user's own words
	known map[string]bool  // the words of the list and the user's, nil until read
	list  string           // the word list read, being read, or failed to be
	tops  map[string]int   // by buffer, where the view starts, as last heard
	shown map[string][]int // by buffer, the ids of the words underlined
	ids   int
	asked question // the word z= asked about
	picks uint32
}

// question is a word z= lists others for, where it was and what it listed.
type question struct {
	id         uint32
	start, end int
	word       string
	items      []string
}

// New returns spell, which reads its word list once spell is on. own is
// the file of the user's own words, which zg adds to.
func New(own string) *Module {
	return &Module{own: own, tops: map[string]int{}, shown: map[string][]int{}}
}

func (m *Module) Name() string { return "spell" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "view.moved", "buffer.changed"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Key, Name: "]s", Help: "goes to the next misspelled word", ID: next},
		{Kind: api.Key, Name: "[s", Help: "goes to the misspelled word before", ID: prev},
		{Kind: api.Key, Name: "z=", Help: "lists words for the one under the cursor, and the one chosen replaces it", ID: suggest},
		{Kind: api.Key, Name: "zg", Help: "adds the word under the cursor to your words", ID: good},
	}
}

// Init starts reading the word list when spell is on, and its callback
// underlines what the view shows.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on, _ := s.Option("spell"); on != "true" {
		return api.Result{}, nil
	}
	return m.read(s), nil
}

// Call runs ]s, [s, z= or zg at the cursor.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if !m.mu.TryLock() {
		return api.Result{}, fmt.Errorf("spell %w", api.ErrBusy)
	}
	defer m.mu.Unlock()
	if on, _ := s.Option("spell"); on != "true" {
		return api.Result{Message: "spell is off, and :set spell turns it on"}, nil
	}
	if m.known == nil {
		res := m.read(s)
		res.Message = "spell is reading its word list"
		return res, nil
	}
	switch id {
	case next, prev:
		return m.step(s, args[0], id == prev), nil
	case suggest:
		return m.suggest(s, args[0]), nil
	}
	return m.add(s, args[0]), nil
}

// Event underlines the typos the view shows when it moves or the text
// changes.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	if on, _ := s.Option("spell"); on != "true" {
		return m.clear(buf), nil
	}
	// still is a view.moved that did not scroll, which underlines nothing new.
	still := false
	if event, detail, _ := strings.Cut(name, " "); event == "view.moved" {
		_, top, _ := strings.Cut(detail, " ")
		at, _ := strconv.Atoi(top)
		still = at == m.tops[buf] && m.shown[buf] != nil
		m.tops[buf] = at
	}
	switch path, _ := s.Option("spell_words"); {
	case m.known == nil || path != m.list:
		return m.read(s), nil
	case still:
		return api.Result{}, nil
	}
	return m.underline(s), nil
}

// Callback takes the word list once it is read, or the word z= chose.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case id == read && s.About["error"] != "":
		// A list that fails to read says so once, and spell then finds
		// nothing.
		return api.Result{Message: s.About["error"]}, nil
	case id == read:
		return m.underline(s), nil
	case id != m.asked.id || s.About["ok"] != "true":
		return api.Result{}, nil
	}
	q := m.asked
	i, _ := strconv.Atoi(s.About["picked"])
	if q.end > s.Text.Len() || s.Text.Slice(q.start, q.end) != q.word || i < 0 || i >= len(q.items) {
		return api.Result{Message: "the text changed, so " + q.word + " stays"}, nil
	}
	return api.Result{Edits: []api.Edit{{Start: q.start, End: q.end, Text: q.items[i]}}}, nil
}

// Work reads the word list, the only job spell asks for.
func (m *Module) Work(_ context.Context, j api.Job) (string, error) {
	known, err := words(j.Argv[1], m.own)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.known = known
	m.mu.Unlock()
	return "", nil
}

func (m *Module) Close(context.Context) error { return nil }

// read starts reading the word list spell_words names, once for each
// list it names, so :set spell_words reads the new one (B39).
func (m *Module) read(s api.Scope) api.Result {
	path, _ := s.Option("spell_words")
	if path == m.list {
		return api.Result{}
	}
	m.list = path
	return api.Result{Jobs: []api.Job{{ID: read, Argv: []string{"read", path}}}}
}

// clear takes the underlines off buffer buf.
func (m *Module) clear(buf string) api.Result {
	res := api.Result{Undecorate: m.shown[buf]}
	delete(m.shown, buf)
	return res
}

// underline underlines the typos in the lines of the view, from its top.
func (m *Module) underline(s api.Scope) api.Result {
	buf, t := s.About["buffer"], s.Text
	res := m.clear(buf)
	from := min(m.tops[buf], t.Len())
	last := min(t.LineOf(from)+rows, t.Lines()-1)
	to := t.Len()
	if last+1 < t.Lines() {
		to = t.LineStart(last + 1)
	}
	m.shown[buf] = []int{}
	for _, w := range typos(s, from, to, m.known) {
		m.ids++
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: w[0], End: w[1], Style: "spell.bad"})
		m.shown[buf] = append(m.shown[buf], m.ids)
	}
	return res
}

// step goes to the next typo after the cursor, or with back the one
// before, wrapping at the ends of the text (B260).
func (m *Module) step(s api.Scope, cursor int, back bool) api.Result {
	found := typos(s, 0, s.Text.Len(), m.known)
	if len(found) == 0 {
		return api.Result{Message: "no misspelled words"}
	}
	i := 0
	if back {
		i = len(found) - 1
		for k := len(found) - 1; k >= 0; k-- {
			if found[k][0] < cursor {
				i = k
				break
			}
		}
	} else if k := slices.IndexFunc(found, func(w [2]int) bool { return w[0] > cursor }); k >= 0 {
		i = k
	}
	return api.Result{Jump: &api.Jump{Pos: found[i][0]}}
}

// suggest lists the words of the list closest to the one under the cursor,
// and the one chosen takes its place, as z= does (B260).
func (m *Module) suggest(s api.Scope, cursor int) api.Result {
	start, end, ok := wordAt(s.Text, cursor)
	if !ok {
		return api.Result{Message: "no word under the cursor"}
	}
	word := s.Text.Slice(start, end)
	lower := strings.ToLower(word)
	type near struct {
		word string
		d    int
	}
	var nears []near
	for w := range m.known {
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
		return api.Result{Message: "no words close to " + word}
	}
	m.picks++
	m.asked = question{read + m.picks, start, end, word, items}
	return api.Result{Pick: &api.Pick{ID: m.asked.id, Prompt: "spelling of " + word, Items: items}}
}

// add adds the word under the cursor to the user's own words, so spell no
// longer finds it, as zg does (B260).
func (m *Module) add(s api.Scope, cursor int) api.Result {
	start, end, ok := wordAt(s.Text, cursor)
	if !ok {
		return api.Result{Message: "no word under the cursor"}
	}
	word, path := s.Text.Slice(start, end), m.own
	err := os.MkdirAll(filepath.Dir(path), 0o755)
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
		return api.Result{Message: fmt.Sprintf("add %s to your words: %v", word, err)}
	}
	m.known[word] = true
	res := m.underline(s)
	res.Message = "added " + word + " to " + path
	return res
}

// typos returns, as [start, end), the words of s's text in [from, to) that
// known lacks. In prose it checks all but the code and links the
// highlights mark, and elsewhere only the comments and strings (B259).
func typos(s api.Scope, from, to int, known map[string]bool) [][2]int {
	var hl []api.Span
	if s.Spans != nil {
		hl = s.Spans(from, to)
	}
	covered := func(a, b int, names ...string) bool {
		return slices.ContainsFunc(hl, func(sp api.Span) bool {
			return sp.Start <= a && sp.End >= b && slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(sp.Name, n) })
		})
	}
	text := s.Text.Slice(from, to)
	var out [][2]int
	for _, w := range find(text) {
		a, b := from+w[0], from+w[1]
		checked := covered(a, b, "comment", "string")
		if prose[s.About["filetype"]] {
			checked = !covered(a, b, "markup.raw", "markup.link.url")
		}
		if checked && !knows(known, text[w[0]:w[1]]) {
			out = append(out, [2]int{a, b})
		}
	}
	return out
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

// find returns where the words of s that spell checks are, as [start, end)
// in s: runs of letters with apostrophes inside them. A word with a capital
// after its first letter, or a digit or one of _ . / @ \ beside it, is a
// name or a path, and is not checked, nor is one letter alone.
func find(s string) [][2]int {
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

// wordAt returns where the word at pos of t is, as find finds words, or
// false when pos is in none.
func wordAt(t api.Text, pos int) (int, int, bool) {
	n := t.LineOf(pos)
	start, end := t.LineStart(n), t.Len()
	if n+1 < t.Lines() {
		end = t.LineStart(n+1) - 1
	}
	for _, w := range find(t.Slice(start, end)) {
		if start+w[0] <= pos && pos < start+w[1] {
			return start + w[0], start + w[1], true
		}
	}
	return 0, 0, false
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

// words reads the words of the list at path and of the user's own file
// own, one a line. A missing own file has no words.
func words(path, own string) (map[string]bool, error) {
	known := map[string]bool{}
	load := func(path string) error {
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
	if err := load(path); err != nil {
		return nil, fmt.Errorf("spell has no word list, so spell_words should name one: %w", err)
	}
	if err := load(own); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read your words: %w", err)
	}
	return known, nil
}
