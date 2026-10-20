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
	switch id {
	case buffer:
		return api.Result{Completion: &api.Completion{Start: start, Items: words(s, t.Slice(start, c), c)}}, nil
	case path:
		dir, at, ok := pathBefore(t, c, s.About["path"])
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

// words returns the words longer than prefix that start with it, from the
// buffer the call is about nearest c first, then from the other open
// buffers but terminals (B206).
func words(s api.Scope, prefix string, c int) []api.CompletionItem {
	if prefix == "" {
		return nil
	}
	cur := s.Text
	lo, hi := max(c-wordWindow, 0), min(c+wordWindow, cur.Len())
	type found struct {
		word string
		far  int
	}
	var all []found
	seen := map[string]bool{}
	scan := func(text string, at, base int) {
		for i := 0; i < len(text); {
			if !isWord(text[i]) {
				i++
				continue
			}
			j := i
			for j < len(text) && isWord(text[j]) {
				j++
			}
			if w := text[i:j]; len(w) > len(prefix) && strings.HasPrefix(w, prefix) && !seen[w] {
				seen[w] = true
				all = append(all, found{w, base + max(at-i, i-at)})
			}
			i = j
		}
	}
	scan(cur.Slice(lo, hi), c-lo, 0)
	self, _ := strconv.Atoi(s.About["buffer"])
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		if b := s.Buffers[id]; id != self && b.Kind != "terminal" {
			scan(b.Text.Slice(0, min(b.Text.Len(), wordWindow)), 0, cur.Len())
		}
	}
	slices.SortStableFunc(all, func(a, b found) int { return cmp.Compare(a.far, b.far) })
	out := make([]api.CompletionItem, len(all))
	for i, f := range all {
		out[i] = api.CompletionItem{Label: f.word, Text: f.word}
	}
	return out
}

// pathBefore reports whether the text before c, back to a blank or a
// quote, is a path, and returns the directory it names and where its last
// part starts. A relative path is from file's directory, and ~/ is the
// home directory.
func pathBefore(t api.Text, c int, file string) (string, int, bool) {
	ls := t.LineStart(t.LineOf(c))
	line := t.Slice(ls, c)
	p := line[strings.LastIndexAny(line, " \t\"'`(<=,;")+1:]
	// Windows takes \ between folders as well as /.
	sep := string(filepath.Separator)
	slash := strings.LastIndexAny(p, "/"+sep)
	if slash < 0 {
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
