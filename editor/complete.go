package editor

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// completion is what completes in insert mode: the menu showing, whether
// <C-x> began a completion key, and the source whose items come later.
type completion struct {
	menu    *menu
	ctrlX   bool
	waiting *waiting // the source whose items come later (B336)
}

// waiting is a Completer whose items come in a callback, and the keys
// that asked for the menu (B336).
type waiting struct {
	p          api.Module
	name, keys string
}

// completer is what a module registered as a source of completion (B336).
type completer struct {
	p    api.Module
	id   uint32
	keys string
}

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

// key handles a key while completing in insert mode, and reports whether
// it took it. <C-x> and a key after it ask for completions, and so do
// <C-n> and <C-p> with no menu. While the menu shows, <C-n>, <C-p>, <C-y>
// and <C-e> work it. Any other key keeps the item and goes on to type.
func (c *completion) key(e *Session, k vim.Key) bool {
	if e.vim.Mode() != vim.Insert {
		c.ctrlX = false
		if c.menu != nil {
			e.closeMenu()
		}
		return false
	}
	if k == "<C-s>" && len(e.win.buf.servers) > 0 {
		if f := e.floatAt(aboveCursor); f != nil {
			e.closeFloat(f)
		} else {
			e.signature(false)
		}
		return true
	}
	if c.ctrlX {
		c.ctrlX = false
		switch k {
		case "<C-o>", "<C-n>", "<C-p>", "<C-l>", "<C-f>":
			e.complete("<C-x>" + string(k))
			return true
		}
	}
	if k == "<C-x>" {
		c.ctrlX = true
		return true
	}
	m := c.menu
	if m == nil {
		if k == "<C-n>" || k == "<C-p>" {
			e.complete(string(k))
			return true
		}
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
// from the sources completion_sources names, in its order, each text
// once: the current buffer's servers, the entries of the directory a path
// before the cursor names, snippets and the words of the open buffers.
// keys are what asked, which the sources hear, none for autocomplete.
// <C-n>, <C-p> and <C-x> with <C-n>, <C-p> or <C-l> ask the buffer
// source alone, and <C-x><C-f> the path source (B64, B206, B440).
func (e *Session) complete(keys string) {
	sources := e.cfg.Sources
	switch keys {
	case "<C-n>", "<C-p>", "<C-x><C-n>", "<C-x><C-p>", "<C-x><C-l>":
		sources = []string{"buffer"}
	case "<C-x><C-f>":
		sources = []string{"path"}
	}
	b := e.win.buf
	t, c := b.text.Snapshot(), e.Cursor()
	start := c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	e.completion.waiting = nil
	local := map[string][]item{}
	for _, name := range sources {
		cp, ok := e.completionSources[name]
		if !ok {
			continue
		}
		scope := e.scope(cp.p, b, t)
		scope.About["keys"] = keys
		_, res, err := e.run(cp.p, cp.id, scope, c)
		got := res.Completion
		switch {
		case err != nil || got == nil:
		case got.Only && got.Later:
			e.completion.waiting = &waiting{cp.p, name, keys}
			return
		case got.Only:
			e.offer(keys, t, got.Start, []string{name}, map[string][]item{name: items(got.Items)})
			return
		default:
			local[name] = items(got.Items)
		}
	}
	if !slices.Contains(sources, "lsp") || len(b.servers) == 0 {
		e.offer(keys, t, start, sources, local)
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
		e.offer(keys, t, start, sources, local)
	})
	if !asked {
		e.offer(keys, t, start, sources, local)
	}
}

// offer shows the menu of items, by source, that replace the text from
// start to the cursor in t, in the order of sources and without a second
// item of the same text. An answer about text since changed is dropped.
// It puts the first item in the text, or the last for keys that search
// back. For autocomplete, with no keys, it only shows them, and says
// nothing when there are none.
func (e *Session) offer(keys string, t kernel.Snapshot, start int, sources []string, items map[string][]item) {
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
	for _, source := range sources {
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
		if keys != "" {
			e.message = "no completions"
		}
		return
	}
	if e.completion.menu != nil {
		e.closeMenu()
	}
	m.start, m.sel = b.text.SetMark(start, kernel.Left), len(m.items)
	e.completion.menu = m
	switch keys {
	case "":
		e.showMenu()
	case "<C-p>", "<C-x><C-p>", "<C-x><C-l>":
		e.choose(len(m.items) - 1)
	default:
		e.choose(0)
	}
}

// items are a Completer's items as the menu takes them (B336).
func items(in []api.CompletionItem) []item {
	out := make([]item, len(in))
	for i, it := range in {
		out[i] = item{label: it.Label, text: it.Text, doc: it.Doc, snippet: it.Snippet}
	}
	return out
}

// choose puts item i of the menu in the text, or the typed word for the
// menu's last stop, and shows the menu with it selected.
func (e *Session) choose(i int) {
	m := e.completion.menu
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
	// The cursor is after old, and the multicursors after the same (B393).
	e.setCursor(start + len(old))
	if err := e.vim.Complete(len(old), text); err != nil {
		e.message = err.Error()
		return
	}
	m.sel = i
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

// showMenu shows the menu's labels at the cursor, the item in the text
// selected, with its documentation beside it (B68).
func (e *Session) showMenu() {
	m := e.completion.menu
	e.menu.doc, e.menu.docSpans = "", nil
	if i := m.sel; i < len(m.items) && m.docs[i] != "" {
		e.menu.doc, e.menu.docSpans = e.markdown(m.docs[i])
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
	e.menu.text, e.menu.spans = strings.TrimSuffix(shown.String(), "\n"), spans
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
		if m.docs[i] = readItem(res, ft).doc; e.completion.menu == m && m.sel == i {
			e.showMenu()
		}
	})
}

// autocomplete asks for completions after k typed a word character, a
// character a Completer names in its keys, as / for paths, or one a server
// names as a trigger, while autocomplete is on (B67, B206, B336).
func (e *Session) autocomplete(k vim.Key) {
	if !e.cfg.Autocomplete || e.vim.Mode() != vim.Insert || utf8.RuneCountInString(string(k)) != 1 || k == "<" {
		return
	}
	server := slices.ContainsFunc(e.win.buf.servers, func(s *server) bool {
		p, ok := completionOptions(s)
		return ok && (isWord(k[0]) || slices.Contains(p.TriggerCharacters, string(k)))
	})
	local := false
	for _, name := range e.cfg.Sources {
		if cp, ok := e.completionSources[name]; ok && (isWord(k[0]) || strings.Contains(cp.keys, string(k))) {
			local = true
		}
	}
	if server || local {
		e.complete("")
	}
}

// signatureHelp asks for the signature after k is typed in insert mode,
// when k is one of a server's trigger characters or a signature already
// shows. Out of insert mode, no signature shows (B203).
func (e *Session) signatureHelp(k vim.Key) {
	if e.vim.Mode() != vim.Insert {
		e.showSignature("", nil)
		return
	}
	if e.floatAt(aboveCursor) != nil || slices.ContainsFunc(e.win.buf.servers, func(s *server) bool {
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
type completionOpts struct {
	TriggerCharacters []string `json:"triggerCharacters"`
	ResolveProvider   bool     `json:"resolveProvider"`
}

// completionOptions returns what s said of its completion, or false when it
// offers none.
func completionOptions(s *server) (completionOpts, bool) {
	var p completionOpts
	return p, json.Unmarshal(s.client.Capability("completionProvider"), &p) == nil
}

// closeMenu stops completing and keeps what is in the text. A snippet
// kept starts, and closeMenu reports that it did (B207).
func (e *Session) closeMenu() bool {
	m := e.completion.menu
	start := e.win.buf.text.Mark(m.start)
	e.win.buf.text.DeleteMark(m.start)
	e.completion.menu, e.menu.text, e.menu.spans, e.menu.doc, e.menu.docSpans = nil, "", nil, "", nil
	if m.sel < len(m.items) && m.stops[m.sel] != nil {
		e.startSnippet(start, m.items[m.sel], m.stops[m.sel])
		return true
	}
	return false
}
