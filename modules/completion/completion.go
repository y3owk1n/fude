// Package completion offers insert mode completions from the words of the
// open buffers, the entries of a directory a path before the cursor names,
// and the user's snippets (B206, B207).
package completion

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/fuzzy"
	"github.com/y3owk1n/fude/internal/home"
	"github.com/y3owk1n/fude/internal/keep"
	"github.com/y3owk1n/fude/internal/trace"
)

// What completion registers, by the ids Call runs them by.
const (
	buffer uint32 = iota + 1
	path
	snippet
)

// wordWindow is how many bytes around the cursor, and from the start of
// each other buffer, words come from, so a large file does not slow a key.
const wordWindow = 64 << 10

// Module is completion. It keeps each snippets file read and the paths
// being read.
type Module struct {
	mu       sync.Mutex
	snippets map[string]snippetFile // by path
	reads    map[uint32]read        // by job
	jobs     uint32
}

// read is a directory being read for a path's completion: where the part
// typed of its last name starts, and that part.
type read struct {
	start int
	part  string
}

// New returns completion.
func New() *Module {
	return &Module{snippets: map[string]snippetFile{}, reads: map[uint32]read{}}
}

func (m *Module) Name() string { return "completion" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Completer, Name: "buffer", Help: "the words of the open buffers, the nearest the cursor first", ID: buffer},
		{Kind: api.Completer, Name: "path", Help: "the entries of the directory a path before the cursor names", ID: path, Keys: "/"},
		{Kind: api.Completer, Name: "snippet", Help: "the snippets in fude's snippets directory, in VS Code's format", ID: snippet},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call offers what its source has for the text before the cursor.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, c := s.Text, args[0]
	start := c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	keys := s.About["keys"]
	switch id {
	case buffer:
		switch keys {
		case "<C-x><C-l>":
			// The line's text after its indent.
			start = c - len(strings.TrimLeft(t.Slice(t.LineStart(t.LineOf(c)), c), " \t"))
			return api.Result{Completion: &api.Completion{Start: start, Items: searched(s, keys, t.Slice(start, c), c), Only: true}}, nil
		case "<C-n>", "<C-p>", "<C-x><C-n>", "<C-x><C-p>":
			return api.Result{Completion: &api.Completion{Start: start, Items: searched(s, keys, t.Slice(start, c), c)}}, nil
		}
		return api.Result{Completion: &api.Completion{Start: start, Items: words(s, t.Slice(start, c), c)}}, nil
	case path:
		dir, at, ok := pathBefore(t, c, s.About["path"], keys == "<C-x><C-f>")
		if !ok {
			return api.Result{}, nil
		}
		m.jobs++
		m.reads[m.jobs] = read{at, t.Slice(at, c)}
		return api.Result{Completion: &api.Completion{Only: true, Later: true}, Jobs: []api.Job{{ID: m.jobs, Argv: []string{dir}}}}, nil
	}
	items, err := m.userSnippets(s.About["filetype"], t.Slice(start, c))
	res := api.Result{Completion: &api.Completion{Start: start, Items: items}}
	if err != nil {
		res.Error = err.Error()
	}
	return res, nil
}

// Callback offers a directory's entries once read, those that match the
// part typed, the hidden ones only for a part that starts with a dot.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.reads[id]
	delete(m.reads, id)
	if !ok {
		return api.Result{}, nil
	}
	var names []string
	for _, n := range strings.Split(s.About["output"], "\n") {
		if n != "" && (!strings.HasPrefix(n, ".") || strings.HasPrefix(r.part, ".")) {
			names = append(names, n)
		}
	}
	var items []api.CompletionItem
	for _, n := range fuzzy.Match(names, r.part) {
		items = append(items, api.CompletionItem{Label: n, Text: n})
	}
	return api.Result{Completion: &api.Completion{Start: r.start, Items: items, Only: true}}, nil
}

// Work reads a directory's entries, a directory's with a trailing slash.
func (m *Module) Work(_ context.Context, j api.Job) (string, error) {
	list, _ := os.ReadDir(cmp.Or(j.Argv[0], "."))
	names := make([]string, len(list))
	for i, en := range list {
		names[i] = en.Name()
		if en.IsDir() {
			names[i] += "/"
		}
	}
	return strings.Join(names, "\n"), nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Close(context.Context) error { return nil }

// isWord reports whether c is a byte of a word.
func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// unit is a word, or a line after its indent, and where it starts.
type unit struct {
	text string
	at   int
}

// units returns the words of text, or with lines its lines after their
// indent, that start with prefix and are longer, each where it starts
// plus base.
func units(text string, base int, lines bool, prefix string) []unit {
	var out []unit
	keep := func(s string, at int) {
		if len(s) > len(prefix) && strings.HasPrefix(s, prefix) {
			out = append(out, unit{s, base + at})
		}
	}
	if lines {
		at := 0
		for l := range strings.Lines(text) {
			keep(strings.TrimLeft(strings.TrimSuffix(l, "\n"), " \t"), at)
			at += len(l)
		}
		return out
	}
	for i := 0; i < len(text); {
		if !isWord(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && isWord(text[j]) {
			j++
		}
		keep(text[i:j], i)
		i = j
	}
	return out
}

// elsewhere returns the units of the open buffers except the call's and
// the terminals, from the start of each, by buffer id.
func elsewhere(s api.Scope, lines bool, prefix string) []unit {
	var out []unit
	self, _ := strconv.Atoi(s.About["buffer"])
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		if b := s.Buffers[id]; id != self && b.Kind != "terminal" {
			out = append(out, units(b.Text.Slice(0, min(b.Text.Len(), wordWindow)), 0, lines, prefix)...)
		}
	}
	return out
}

// unique returns the texts of us as items, each once where it first
// comes, or with last where it last comes.
func unique(us []unit, last bool) []api.CompletionItem {
	if last {
		slices.Reverse(us)
	}
	seen := map[string]bool{}
	var out []api.CompletionItem
	for _, u := range us {
		if !seen[u.text] {
			seen[u.text] = true
			out = append(out, api.CompletionItem{Label: u.text, Text: u.text})
		}
	}
	if last {
		slices.Reverse(out)
	}
	return out
}

// words returns the words longer than prefix that start with it, from the
// buffer the call is about nearest c first, then from the other open
// buffers but terminals (B206).
func words(s api.Scope, prefix string, c int) []api.CompletionItem {
	if prefix == "" {
		return nil
	}
	t, lo := s.Text, max(c-wordWindow, 0)
	near := units(t.Slice(lo, min(c+wordWindow, t.Len())), lo, false, prefix)
	slices.SortStableFunc(near, func(a, b unit) int { return cmp.Compare(max(a.at-c, c-a.at), max(b.at-c, c-b.at)) })
	return unique(slices.Concat(near, elsewhere(s, false, prefix)), false)
}

// searched returns the words that start with prefix, or for <C-x><C-l>
// the lines that do after their indent, in the order keys search: the
// buffer the call is about from c to its end and then from its start,
// then the other open buffers. A key that searches back puts the other
// buffers first, so the last item is the nearest before c. <C-x><C-n> and
// <C-x><C-p> search the buffer alone (B440, B441, B442).
func searched(s api.Scope, keys, prefix string, c int) []api.CompletionItem {
	t, lines := s.Text, keys == "<C-x><C-l>"
	// Lines go from the one after the cursor's, which is left out.
	pivot := c
	if lines {
		pivot = t.LineStart(t.LineOf(c))
	}
	lo := t.LineStart(t.LineOf(max(c-wordWindow, 0)))
	var after, before, others []unit
	for _, u := range units(t.Slice(lo, min(c+wordWindow, t.Len())), lo, lines, prefix) {
		switch {
		case lines && u.at == pivot:
		case u.at < pivot:
			before = append(before, u)
		default:
			after = append(after, u)
		}
	}
	if keys != "<C-x><C-n>" && keys != "<C-x><C-p>" {
		others = elsewhere(s, lines, prefix)
	}
	if keys == "<C-p>" || keys == "<C-x><C-p>" || lines {
		return unique(slices.Concat(others, after, before), true)
	}
	return unique(slices.Concat(after, before, others), false)
}

// pathBefore reports whether the text before c, back to a blank or a
// quote, is a path, and returns the directory it names and where its last
// part starts. A relative path is from file's directory, and ~/ is the
// home directory. With bare, a name with no / is a path in file's
// directory too (B443).
func pathBefore(t api.Text, c int, file string, bare bool) (string, int, bool) {
	ls := t.LineStart(t.LineOf(c))
	line := t.Slice(ls, c)
	p := line[strings.LastIndexAny(line, " \t\"'`(<=,;")+1:]
	// Windows takes \ between folders as well as /.
	sep := string(filepath.Separator)
	slash := strings.LastIndexAny(p, "/"+sep)
	if slash < 0 && !bare {
		return "", 0, false
	}
	dir := p[:slash+1]
	full, err := home.Expand(dir)
	switch {
	case err != nil:
		return "", 0, false
	case full != dir:
		dir = full + sep
	case !filepath.IsAbs(dir) && file != "":
		dir = filepath.Join(filepath.Dir(file), dir) + sep
	}
	return dir, c - (len(p) - slash - 1), true
}

// snippetFile is a parsed snippets file with the modification time and
// size it had, so it is read again only after it changes.
type snippetFile struct {
	mod      time.Time
	size     int64
	snippets map[string]struct {
		Prefix      json.RawMessage `json:"prefix"`
		Body        json.RawMessage `json:"body"`
		Description string          `json:"description"`
	}
}

// userSnippets returns the snippets of filetype ft and of all filetypes
// in fude's snippets directory, in VS Code's format, whose prefix starts
// with typed, each as an item of its body (B207).
func (m *Module) userSnippets(ft, typed string) ([]api.CompletionItem, error) {
	dir, err := keep.Dir("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return nil, fmt.Errorf("find snippets: %w", err)
	}
	var out []api.CompletionItem
	for _, name := range []string{ft, "all"} {
		if name == "" {
			continue
		}
		path := filepath.Join(dir, "fude", "snippets", name+".json")
		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read snippets: %w", err)
		}
		f, ok := m.snippets[path]
		if !ok || !f.mod.Equal(info.ModTime()) || f.size != info.Size() {
			at := time.Now()
			src, err := os.ReadFile(path)
			trace.Since("file", fmt.Sprintf("read %q, %d bytes", path, len(src)), at, err)
			if err != nil {
				return nil, fmt.Errorf("read snippets: %w", err)
			}
			f = snippetFile{mod: info.ModTime(), size: info.Size()}
			if err := json.Unmarshal(src, &f.snippets); err != nil {
				return nil, fmt.Errorf("read snippets %s: %w", path, err)
			}
			m.snippets[path] = f
		}
		for _, title := range slices.Sorted(maps.Keys(f.snippets)) {
			s := f.snippets[title]
			body := strings.Join(oneOrMany(s.Body), "\n")
			for _, prefix := range oneOrMany(s.Prefix) {
				if typed != "" && strings.HasPrefix(prefix, typed) {
					doc := strings.TrimSpace(s.Description + "\n```" + ft + "\n" + body + "\n```")
					out = append(out, api.CompletionItem{Label: prefix, Text: body, Doc: doc, Snippet: true})
				}
			}
		}
	}
	return out, nil
}

// oneOrMany reads a string or a list of strings, as a snippet's prefix and
// body may be.
func oneOrMany(raw json.RawMessage) []string {
	var one string
	if json.Unmarshal(raw, &one) == nil {
		return []string{one}
	}
	var many []string
	json.Unmarshal(raw, &many)
	return many
}
