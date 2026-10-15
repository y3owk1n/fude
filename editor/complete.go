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
	items  []string // what each puts in the text
	labels []string // what the menu shows of each
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
	if k == "<C-s>" && e.win.buf.server != nil {
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

// complete asks the current buffer's server what may go at the cursor, and
// shows the answer as a menu with its first item in the text, or with none
// when auto. It drops an answer for text that changed since.
func (e *Session) complete(auto bool) {
	b := e.win.buf
	s := b.server
	if s == nil {
		e.message = "no language server"
		return
	}
	t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
	start := c
	for start > 0 && isWord(t.Slice(start-1, start)[0]) {
		start--
	}
	mark := b.text.SetMark(start, kernel.Left)
	s.client.Request("textDocument/completion", func(enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc)}
	}, func(res json.RawMessage) {
		// The answer is a list, or a list inside an object.
		type item struct {
			Label      string `json:"label"`
			InsertText string `json:"insertText"`
			TextEdit   struct {
				NewText string `json:"newText"`
			} `json:"textEdit"`
		}
		var items []item
		if json.Unmarshal(res, &items) != nil {
			var list struct {
				Items []item `json:"items"`
			}
			json.Unmarshal(res, &list)
			items = list.Items
		}
		start := b.text.Mark(mark)
		cur := e.Cursor()
		if e.vim.Mode() != vim.Insert || e.win.buf != b || cur < start || b.text.Snapshot().Version() != t.Version() || len(items) == 0 {
			b.text.DeleteMark(mark)
			if len(items) == 0 && !auto {
				e.message = "no completions"
			}
			return
		}
		if e.menu != nil {
			e.closeMenu()
		}
		m := &menu{start: mark, typed: b.text.Snapshot().Slice(start, cur), sel: len(items)}
		for _, it := range items {
			text := cmp.Or(it.TextEdit.NewText, it.InsertText, it.Label)
			m.items, m.labels = append(m.items, text), append(m.labels, it.Label)
		}
		e.menu = m
		if auto {
			e.showMenu()
			return
		}
		e.choose(0)
	})
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

// showMenu shows the menu's labels in a float, the item in the text
// selected.
func (e *Session) showMenu() {
	m := e.menu
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

// autocomplete asks for completions after k typed a word character, or a
// character the server names as a trigger, while autocomplete is on (B67).
func (e *Session) autocomplete(k vim.Key) {
	s := e.win.buf.server
	if !e.cfg.Autocomplete || s == nil || e.vim.Mode() != vim.Insert || utf8.RuneCountInString(string(k)) != 1 || k == "<" {
		return
	}
	var p struct {
		TriggerCharacters []string `json:"triggerCharacters"`
	}
	// A server that does not offer completion has no options to read.
	if json.Unmarshal(s.client.Capability("completionProvider"), &p) != nil {
		return
	}
	if isWord(k[0]) || slices.Contains(p.TriggerCharacters, string(k)) {
		e.complete(true)
	}
}

// closeMenu stops completing and keeps what is in the text.
func (e *Session) closeMenu() {
	e.win.buf.text.DeleteMark(e.menu.start)
	e.menu, e.float, e.floatSpans = nil, "", nil
}
