package editor

import (
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// menu is the completions shown while typing, which <C-n> and <C-p> step
// through, each replacing the word before the cursor (B64).
type menu struct {
	items  []string          // what each puts in the text
	labels []string          // what the menu shows of each
	docs   []string          // each one's detail and documentation, as markdown
	stops  [][]lsp.Stop      // each one's snippet stops, nil for plain text
	raws   []json.RawMessage // each as its server sent it, to resolve
	from   []*server         // the server each came from, nil for the other sources
	asked  map[int]bool      // the items resolve was asked for
	start  kernel.MarkID
	typed  string // the word it replaced, the menu's last stop
	sel    int    // the item in the text, or len(items) for typed
}

// completing handles a key while completing in insert mode, and reports
// whether it took it. <C-x><C-o> asks the server, and while the menu shows,
// <C-n>, <C-p>, <C-y> and <C-e> work it. Any other key keeps the item and
// goes on to type.
func (e *Session) completing(k vim.Key) bool {
	if e.vim.Mode() != vim.Insert {
		e.ctrlX = false
		if e.menu != nil {
			e.closeMenu()
		}
		return false
	}
	if k == "<C-s>" && len(e.win.buf.servers) > 0 {
		if e.sig != nil {
			e.sig = nil
		} else {
			e.signature(false)
		}
		return true
	}
	if e.ctrlX {
		e.ctrlX = false
		if k == "<C-o>" {
			e.complete(false)
			return true
		}
	}
	if k == "<C-x>" {
		e.ctrlX = true
		return true
	}
	m := e.menu
	if m == nil {
		return false
	}
	switch k {
	case "<C-n>":
		e.choose((m.sel + 1) % (len(m.items) + 1))
	case "<C-p>":
		e.choose((m.sel + len(m.items)) % (len(m.items) + 1))
	case "<C-e>":
		e.choose(len(m.items))
		e.closeMenu()
	case "<C-y>":
		e.closeMenu()
	default:
		// <Tab> on a snippet starts it, at its first stop.
		return e.closeMenu() && k == "\t"
	}
	return true
}

// complete lists completions for the word before the cursor as one menu,
// with its first item in the text, from the sources completion_sources
// names, in its order, each text once: the current buffer's servers, the
// entries of the directory a path before the cursor names, snippets and
// the words of the open buffers. With auto it only shows them, and says
// nothing when there are none (B64, B206).
func (e *Session) complete(auto bool) {
	b := e.win.buf
	t, c := b.text.Snapshot(), e.Cursor()
	if dir, start, ok := e.pathBefore(t, c); ok && slices.Contains(e.cfg.Sources, "path") {
		go func() {
			names := readEntries(dir)
			e.queue <- func() { e.offer(auto, t, start, map[string][]item{"path": pathItems(names, t.Slice(start, c))}) }
		}()
		return
	}
	start := c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	local := map[string][]item{}
	if slices.Contains(e.cfg.Sources, "buffer") {
		local["buffer"] = e.words(t.Slice(start, c), start)
	}
	if slices.Contains(e.cfg.Sources, "snippet") {
		snips, err := e.userSnippets(e.filetype(b), t.Slice(start, c))
		if err != nil {
			e.message = err.Error()
		}
		local["snippet"] = snips
	}
	if !slices.Contains(e.cfg.Sources, "lsp") || len(b.servers) == 0 {
		e.offer(auto, t, start, local)
		return
	}
	uri := lsp.URI(b.file.path)
	asked := ask(b.servers, "textDocument/completion", func(_ *server, enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc)}
	}, func(rs []reply) {
		ft := e.filetype(b)
		for _, r := range rs {
			// The answer is a list, or a list inside an object.
			var got []json.RawMessage
			if json.Unmarshal(r.res, &got) != nil {
				var list struct {
					Items []json.RawMessage `json:"items"`
				}
				json.Unmarshal(r.res, &list)
				got = list.Items
			}
			for _, raw := range got {
				it := readItem(raw, ft)
				it.raw, it.from = raw, r.s
				local["lsp"] = append(local["lsp"], it)
			}
		}
		e.offer(auto, t, start, local)
	})
	if !asked {
		e.offer(auto, t, start, local)
	}
}

// offer shows the menu of items, by source, that replace the text from
// start to the cursor in t, in the order completion_sources gives the
// sources and without a second item of the same text. An answer about
// text since changed is dropped.
func (e *Session) offer(auto bool, t kernel.Snapshot, start int, items map[string][]item) {
	b, cur := e.win.buf, e.Cursor()
	if e.vim.Mode() != vim.Insert || b.text.Snapshot().Version() != t.Version() || cur < start {
		return
	}
	m := &menu{asked: map[int]bool{}, typed: t.Slice(start, cur)}
	seen := map[string]bool{}
	// A snippet's lines take the indent of the line it goes on.
	before := t.Slice(vim.LineStart(t, start), start)
	indent := before[:len(before)-len(strings.TrimLeft(before, " \t"))]
	unit := "\t"
	if e.win.opts.ExpandTab {
		unit = strings.Repeat(" ", e.indentWidth(b))
	}
	vars := e.snippetVars(b, start)
	for _, source := range e.cfg.Sources {
		for _, it := range items[source] {
			var stops []lsp.Stop
			if it.snippet {
				it.text, stops = lsp.ExpandSnippet(it.text, indent, unit, vars)
			}
			if seen[it.text] || it.text == m.typed {
				continue
			}
			seen[it.text] = true
			label := it.label
			if source != "lsp" {
				label += "  [" + source + "]"
			}
			m.items, m.labels, m.docs = append(m.items, it.text), append(m.labels, label), append(m.docs, it.doc)
			m.raws, m.from, m.stops = append(m.raws, it.raw), append(m.from, it.from), append(m.stops, stops)
		}
	}
	if len(m.items) == 0 {
		if !auto {
			e.message = "no completions"
		}
		return
	}
	if e.menu != nil {
		e.closeMenu()
	}
	m.start, m.sel = b.text.SetMark(start, kernel.Left), len(m.items)
	e.menu = m
	if auto {
		e.showMenu()
		return
	}
	e.choose(0)
}

// pathBefore reports whether the text before c, back to a blank or a
// quote, is a path, and returns the directory it names and where its last
// part starts. A relative path is from the current file's directory, and ~/
// is the home directory.
func (e *Session) pathBefore(t kernel.Snapshot, c int) (string, int, bool) {
	ls := vim.LineStart(t, c)
	line := t.Slice(ls, c)
	path := line[strings.LastIndexAny(line, " \t\"'`(<=,;")+1:]
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 {
		return "", 0, false
	}
	dir := path[:slash+1]
	switch {
	case strings.HasPrefix(dir, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", 0, false
		}
		dir = filepath.Join(home, dir[2:]) + "/"
	case !filepath.IsAbs(dir) && e.win.buf.file.path != "":
		dir = filepath.Join(filepath.Dir(e.win.buf.file.path), dir) + "/"
	}
	return dir, c - (len(path) - slash - 1), true
}

// pathItems returns the entries of a directory that start with part, the
// hidden ones only for a part that starts with a dot.
func pathItems(names []string, part string) []item {
	var out []item
	for _, n := range pathMatches(part, names) {
		out = append(out, item{label: n, text: n})
	}
	return out
}

// wordWindow is how many bytes around the cursor, and from the start of
// each other buffer, words come from, so a large file does not slow a key.
const wordWindow = 64 << 10

// words returns the words longer than prefix that start with it, from the
// current buffer nearest c first, then from the other open buffers (B206).
func (e *Session) words(prefix string, c int) []item {
	if prefix == "" {
		return nil
	}
	cur := e.win.buf.text.Snapshot()
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
				all = append(all, found{w, base + abs(at-i)})
			}
			i = j
		}
	}
	scan(cur.Slice(lo, hi), c-lo, 0)
	for _, b := range e.bufs {
		if b != e.win.buf && b.term == nil {
			t := b.text.Snapshot()
			scan(t.Slice(0, min(t.Len(), wordWindow)), 0, cur.Len())
		}
	}
	slices.SortStableFunc(all, func(a, b found) int { return cmp.Compare(a.far, b.far) })
	out := make([]item, len(all))
	for i, f := range all {
		out[i] = item{label: f.word, text: f.word}
	}
	return out
}

func abs(n int) int { return max(n, -n) }

// choose puts item i of the menu in the text, or the typed word for the
// menu's last stop, and shows the menu with it selected.
func (e *Session) choose(i int) {
	m := e.menu
	b := e.win.buf
	start := b.text.Mark(m.start)
	old := m.typed
	if m.sel < len(m.items) {
		old = m.items[m.sel]
	}
	text := m.typed
	if i < len(m.items) {
		text = m.items[i]
	}
	if err := (host{e}).Apply([]vim.Edit{{Start: start, End: start + len(old), Text: text}}, "completion"); err != nil {
		e.message = err.Error()
		return
	}
	m.sel = i
	e.setCursor(start + len(text))
	e.showMenu()
}

// item is what fude uses of a completion item, and for a server's, the
// item as it came and the server, to resolve it.
type item struct {
	label, text, doc string
	snippet          bool // text is a snippet's body (B207)
	raw              json.RawMessage
	from             *server
}

// readItem reads a completion item. Its doc is its detail as code of
// filetype, then its documentation.
func readItem(raw json.RawMessage, filetype string) item {
	var it struct {
		Label         string          `json:"label"`
		InsertText    string          `json:"insertText"`
		Detail        string          `json:"detail"`
		Format        int             `json:"insertTextFormat"`
		Documentation json.RawMessage `json:"documentation"`
		TextEdit      struct {
			NewText string `json:"newText"`
		} `json:"textEdit"`
	}
	json.Unmarshal(raw, &it)
	doc := markupText(it.Documentation)
	if it.Detail != "" {
		doc = "```" + filetype + "\n" + it.Detail + "\n```\n" + doc
	}
	// Format 2 is a snippet.
	return item{label: it.Label, text: cmp.Or(it.TextEdit.NewText, it.InsertText, it.Label), doc: strings.TrimSpace(doc), snippet: it.Format == 2}
}

// showMenu shows the menu's labels in a float, the item in the text
// selected, with its documentation beside it (B68).
func (e *Session) showMenu() {
	m := e.menu
	e.doc, e.docSpans = "", nil
	if i := m.sel; i < len(m.items) && m.docs[i] != "" {
		e.doc, e.docSpans = e.markdown(m.docs[i])
	} else if i < len(m.items) {
		e.resolve(m, i)
	}
	var spans []syntax.Span
	var shown strings.Builder
	for j, l := range m.labels {
		if j == m.sel {
			spans = append(spans, syntax.Span{Start: shown.Len(), End: shown.Len() + len(l), Name: "menu.selected"})
		}
		shown.WriteString(l + "\n")
	}
	e.float, e.floatSpans = strings.TrimSuffix(shown.String(), "\n"), spans
}

// resolve asks the server once for the documentation of item i of m, which
// came without any, and shows it if the item is still selected.
func (e *Session) resolve(m *menu, i int) {
	s := m.from[i]
	if s == nil || m.asked[i] {
		return
	}
	m.asked[i] = true
	if p, ok := completionOptions(s); !ok || !p.ResolveProvider {
		return
	}
	ft := e.filetype(e.win.buf)
	s.client.Request("completionItem/resolve", func(string) any { return m.raws[i] }, func(res json.RawMessage) {
		if res == nil {
			return
		}
		if m.docs[i] = readItem(res, ft).doc; e.menu == m && m.sel == i {
			e.showMenu()
		}
	})
}

// autocomplete asks for completions after k typed a word character, a /
// for paths, or a character one of the servers names as a trigger, while
// autocomplete is on (B67, B206).
func (e *Session) autocomplete(k vim.Key) {
	if !e.cfg.Autocomplete || e.vim.Mode() != vim.Insert || utf8.RuneCountInString(string(k)) != 1 || k == "<" {
		return
	}
	server := slices.ContainsFunc(e.win.buf.servers, func(s *server) bool {
		p, ok := completionOptions(s)
		return ok && (isWord(k[0]) || slices.Contains(p.TriggerCharacters, string(k)))
	})
	local := isWord(k[0]) && slices.Contains(e.cfg.Sources, "buffer") || k == "/" && slices.Contains(e.cfg.Sources, "path")
	if server || local {
		e.complete(true)
	}
}

// signatureHelp asks for the signature after k is typed in insert mode,
// when k is one of a server's trigger characters or a signature already
// shows. Out of insert mode, no signature shows (B203).
func (e *Session) signatureHelp(k vim.Key) {
	if e.vim.Mode() != vim.Insert {
		e.sig = nil
		return
	}
	if e.sig != nil || slices.ContainsFunc(e.win.buf.servers, func(s *server) bool {
		var p struct {
			TriggerCharacters   []string `json:"triggerCharacters"`
			RetriggerCharacters []string `json:"retriggerCharacters"`
		}
		json.Unmarshal(s.client.Capability("signatureHelpProvider"), &p)
		return slices.Contains(p.TriggerCharacters, string(k)) || slices.Contains(p.RetriggerCharacters, string(k))
	}) {
		e.signature(true)
	}
}

// completion is what a server says of its completion.
type completion struct {
	TriggerCharacters []string `json:"triggerCharacters"`
	ResolveProvider   bool     `json:"resolveProvider"`
}

// completionOptions returns what s said of its completion, or false when it
// offers none.
func completionOptions(s *server) (completion, bool) {
	var p completion
	return p, json.Unmarshal(s.client.Capability("completionProvider"), &p) == nil
}

// closeMenu stops completing and keeps what is in the text. A snippet
// kept starts, and closeMenu reports that it did (B207).
func (e *Session) closeMenu() bool {
	m := e.menu
	start := e.win.buf.text.Mark(m.start)
	e.win.buf.text.DeleteMark(m.start)
	e.menu, e.float, e.floatSpans, e.doc, e.docSpans = nil, "", nil, "", nil
	if m.sel < len(m.items) && m.stops[m.sel] != nil {
		e.startSnippet(start, m.items[m.sel], m.stops[m.sel])
		return true
	}
	return false
}
