package editor

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
)

// suggestion is a language server's inline completion: text it suggests
// for the cursor in insert mode, shown dim after it until <Tab> takes it
// (B474).
type suggestion struct {
	b          *buffer
	s          *server
	start, end kernel.MarkID // what the text replaces, from where it goes
	text       string        // empty for no suggestion
	stops      []lsp.Stop    // a snippet's stops in text
	command    json.RawMessage
	shown      bool
	asked      linkAsk // where it was last asked for
	asking     bool
	again      bool // the cursor or the text changed while it was asked for
}

// suggestOwner owns the decoration that shows a session's suggestion.
type suggestOwner struct{ e *Session }

// suggestAfter follows a key. In insert mode, once the cursor or the text
// changed, the suggestion shows what is left of it past what was typed and
// the first server that offers inline completions is asked again. Out of
// insert mode no suggestion shows (B474).
func (e *Session) suggestAfter() {
	g, b := &e.suggest, e.win.buf
	i := slices.IndexFunc(b.servers, func(s *server) bool { return s.client.Supports("inlineCompletionProvider") })
	if e.vim.Mode() != vim.Insert || i < 0 {
		e.unsuggest()
		g.asked = linkAsk{}
		return
	}
	at := linkAsk{b, e.Cursor(), b.text.Snapshot().Version()}
	if at == g.asked {
		return
	}
	g.asked = at
	if g.b != b {
		e.unsuggest()
	}
	e.showSuggestion()
	e.askSuggestion(b.servers[i])
}

// askSuggestion asks s for inline completions at the cursor. One request
// waits at a time, and an answer for a cursor or text that changed since is
// dropped for one asked where they are now.
func (e *Session) askSuggestion(s *server) {
	g := &e.suggest
	if g.asking {
		g.again = true
		return
	}
	g.asking = true
	b, t, c, uri := e.win.buf, e.win.buf.text.Snapshot(), e.Cursor(), lsp.URI(e.win.buf.file.path)
	s.client.Request("textDocument/inlineCompletion", func(enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc), "context": map[string]any{"triggerKind": 2}}
	}, func(res json.RawMessage) {
		g.asking = false
		if e.vim.Mode() != vim.Insert || e.win.buf != b {
			g.again = false
			return
		}
		if g.again || b.text.Snapshot().Version() != t.Version() || e.Cursor() != c {
			g.again = false
			e.askSuggestion(s)
			return
		}
		// The answer is a list of items, or holds one.
		var list struct {
			Items []json.RawMessage `json:"items"`
		}
		if json.Unmarshal(res, &list.Items) != nil {
			json.Unmarshal(res, &list)
		}
		var item struct {
			InsertText json.RawMessage `json:"insertText"`
			Range      *lsp.Range      `json:"range"`
			Command    json.RawMessage `json:"command"`
		}
		e.unsuggest()
		if len(list.Items) == 0 || json.Unmarshal(list.Items[0], &item) != nil {
			return
		}
		start, end := c, c
		if r := item.Range; r != nil {
			start, end = lsp.Offset(t, r.Start, s.client.Encoding()), lsp.Offset(t, r.End, s.client.Encoding())
		}
		// The text is plain, or a snippet with its stops.
		var snip struct {
			Value string `json:"value"`
		}
		var text string
		var stops []lsp.Stop
		if json.Unmarshal(item.InsertText, &text) != nil && json.Unmarshal(item.InsertText, &snip) == nil {
			indent, unit := e.snippetIndent(b, t, start)
			text, stops = lsp.ExpandSnippet(snip.Value, indent, unit, e.snippetVars(b, start))
		}
		if text == "" {
			return
		}
		*g = suggestion{b: b, s: s, start: b.text.SetMark(start, kernel.Left), end: b.text.SetMark(end, kernel.Right), text: text, stops: stops, command: item.Command, asked: g.asked}
		e.showSuggestion()
	})
}

// showSuggestion shows what is left of the suggestion past the text typed
// since its start, dim after the cursor, its first line among the text and
// the others below, or over the rest of the line when its first line holds
// that. A suggestion the typed text no longer starts is dropped.
func (e *Session) showSuggestion() {
	g := &e.suggest
	e.hideSuggestion()
	if g.text == "" {
		return
	}
	t, c, start := g.b.text.Snapshot(), e.Cursor(), g.b.text.Mark(g.start)
	if c < start || t.LineOf(c) != t.LineOf(start) || !strings.HasPrefix(g.text, t.Slice(start, c)) || c-start == len(g.text) {
		e.unsuggest()
		return
	}
	typed := t.Slice(start, c)
	first, more, multi := strings.Cut(g.text[len(typed):], "\n")
	d := decoration{buf: g.b, style: "comment", note: first, insert: true}
	if multi {
		d.lines = strings.Split(more, "\n")
	}
	end := c
	if le := vim.LineEnd(t, c); le > c && strings.Contains(first, t.Slice(c, le)) {
		d.insert, d.cover, end = false, true, le
	}
	d.start, d.end = g.b.text.SetMark(c, kernel.Right), g.b.text.SetMark(end, kernel.Left)
	g.b.inlines++
	e.decos[suggestOwner{e}] = []decoration{d}
	g.shown = true
}

// hideSuggestion takes the suggestion off the screen.
func (e *Session) hideSuggestion() {
	// A buffer that closed took the decoration with it.
	for _, d := range e.decos[suggestOwner{e}] {
		d.remove()
	}
	delete(e.decos, suggestOwner{e})
	e.suggest.shown = false
}

// unsuggest drops the suggestion, keeping where it was last asked for.
func (e *Session) unsuggest() {
	g := &e.suggest
	e.hideSuggestion()
	if g.text != "" {
		g.b.text.DeleteMark(g.start)
		g.b.text.DeleteMark(g.end)
	}
	asked, asking, again := g.asked, g.asking, g.again
	*g = suggestion{asked: asked, asking: asking, again: again}
}

// key takes <Tab> while a suggestion shows. Its text replaces what it
// replaces and what was typed since, a snippet starts at its first stop,
// and the server runs the command that comes with it.
func (g *suggestion) key(e *Session, k vim.Key) bool {
	if k != "\t" || !g.shown || e.vim.Mode() != vim.Insert || e.win.buf != g.b {
		return false
	}
	b, s, text, stops, command := g.b, g.s, g.text, g.stops, g.command
	start, end := b.text.Mark(g.start), max(b.text.Mark(g.end), e.Cursor())
	e.unsuggest()
	if err := (host{e}).Apply([]vim.Edit{{Start: start, End: end, Text: text}}, "inline completion"); err != nil {
		e.message = err.Error()
		return true
	}
	e.setCursor(start + len(text))
	if len(stops) > 0 {
		e.startSnippet(start, text, stops)
	}
	var cmd struct {
		Command   string          `json:"command"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if json.Unmarshal(command, &cmd) == nil && cmd.Command != "" && !e.clientCommand(s, cmd.Command, cmd.Arguments) {
		params := map[string]any{"command": cmd.Command, "arguments": cmd.Arguments}
		s.client.Request("workspace/executeCommand", func(string) any { return params }, func(json.RawMessage) {})
	}
	e.reveal()
	return true
}
