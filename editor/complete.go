package editor

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// menu is the completions shown while typing, which <C-n> and <C-p> step
// through, each replacing the word before the cursor (B64).
type menu struct {
	items  []string          // what each puts in the text
	labels []string          // what the menu shows of each
	docs   []string          // each one's detail and documentation, as markdown
	raws   []json.RawMessage // each as its server sent it, to resolve
	from   []*server         // the server each came from
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
		e.signature()
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
		e.closeMenu()
		return false
	}
	return true
}

// complete asks the current buffer's servers what may go at the cursor, and
// shows their answers as one menu with its first item in the text, or with
// none when auto. It drops answers for text that changed since (B169).
func (e *Session) complete(auto bool) {
	b := e.win.buf
	if len(b.servers) == 0 {
		e.message = "no language server"
		return
	}
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	start := c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	mark := b.text.SetMark(start, kernel.Left)
	asked := ask(b.servers, "textDocument/completion", func(_ *server, enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc)}
	}, func(rs []reply) {
		start, cur := b.text.Mark(mark), e.Cursor()
		// An answer about text since changed is dropped before it is read.
		if e.vim.Mode() != vim.Insert || e.win.buf != b || cur < start || b.text.Snapshot().Version() != t.Version() {
			b.text.DeleteMark(mark)
			return
		}
		var items []json.RawMessage
		var from []*server
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
			items = append(items, got...)
			for range got {
				from = append(from, r.s)
			}
		}
		if len(items) == 0 {
			b.text.DeleteMark(mark)
			if !auto {
				e.message = "no completions"
			}
			return
		}
		if e.menu != nil {
			e.closeMenu()
		}
		m := &menu{raws: items, from: from, asked: map[int]bool{}, start: mark, typed: b.text.Snapshot().Slice(start, cur), sel: len(items)}
		ft := e.filetype(b)
		for _, raw := range items {
			it := readItem(raw, ft)
			m.items, m.labels, m.docs = append(m.items, it.text), append(m.labels, it.label), append(m.docs, it.doc)
		}
		e.menu = m
		if auto {
			e.showMenu()
			return
		}
		e.choose(0)
	})
	if !asked {
		b.text.DeleteMark(mark)
		if !auto {
			e.message = "no language server offers completion"
		}
	}
}

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

// item is what fude uses of a completion item.
type item struct {
	label, text, doc string
}

// readItem reads a completion item. Its doc is its detail as code of
// filetype, then its documentation.
func readItem(raw json.RawMessage, filetype string) item {
	var it struct {
		Label         string          `json:"label"`
		InsertText    string          `json:"insertText"`
		Detail        string          `json:"detail"`
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
	return item{it.Label, cmp.Or(it.TextEdit.NewText, it.InsertText, it.Label), strings.TrimSpace(doc)}
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
	if m.asked[i] {
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

// autocomplete asks for completions after k typed a word character, or a
// character one of the servers names as a trigger, while autocomplete is on
// (B67).
func (e *Session) autocomplete(k vim.Key) {
	if !e.cfg.Autocomplete || e.vim.Mode() != vim.Insert || utf8.RuneCountInString(string(k)) != 1 || k == "<" {
		return
	}
	if slices.ContainsFunc(e.win.buf.servers, func(s *server) bool {
		p, ok := completionOptions(s)
		return ok && (isWord(k[0]) || slices.Contains(p.TriggerCharacters, string(k)))
	}) {
		e.complete(true)
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

// closeMenu stops completing and keeps what is in the text.
func (e *Session) closeMenu() {
	e.win.buf.text.DeleteMark(e.menu.start)
	e.menu, e.float, e.floatSpans, e.doc, e.docSpans = nil, "", nil, "", nil
}
