package editor

import (
	"encoding/json"
	"regexp"
	"slices"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// links are the ranges a language server linked around the cursor, such as
// an HTML tag's name and its closing tag's, so that typing in one types in
// every one (B473).
type links struct {
	b      *buffer
	ranges [][2]kernel.MarkID
	asked  linkAsk // where they were last asked for
}

// linkAsk is a cursor in a buffer's text at one version.
type linkAsk struct {
	b       *buffer
	cursor  int
	version kernel.Version
}

// linkWord is what a linked range may hold. A range that stops matching
// it, as when a space is typed after a tag's name, ends the link.
var linkWord = regexp.MustCompile(`^[\w-]*$`)

// linkedAfter follows a key. In insert mode every other linked range takes
// the text of the one the cursor is in, and the ranges are asked for again
// whenever the cursor or the text changed (B473).
func (e *Session) linkedAfter() {
	b, l := e.win.buf, &e.links
	if l.b != b {
		e.unlink()
	}
	if e.vim.Mode() == vim.Insert && len(l.ranges) > 1 {
		e.typeLinked()
	}
	i := slices.IndexFunc(b.servers, func(s *server) bool { return s.client.Supports("linkedEditingRangeProvider") })
	at := linkAsk{b, e.Cursor(), b.text.Snapshot().Version()}
	if i < 0 || at == l.asked {
		return
	}
	l.asked = at
	s, t, uri := b.servers[i], b.text.Snapshot(), lsp.URI(b.file.path)
	s.client.Request("textDocument/linkedEditingRange", func(enc string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, at.cursor, enc)}
	}, func(res json.RawMessage) {
		if b.text.Snapshot().Version() != t.Version() || e.win.buf != b {
			return
		}
		var r struct {
			Ranges []lsp.Range `json:"ranges"`
		}
		json.Unmarshal(res, &r)
		e.unlink()
		l.b = b
		for _, rg := range r.Ranges {
			start, end := lsp.Offset(t, rg.Start, s.client.Encoding()), lsp.Offset(t, rg.End, s.client.Encoding())
			l.ranges = append(l.ranges, [2]kernel.MarkID{b.text.SetMark(start, kernel.Left), b.text.SetMark(end, kernel.Right)})
		}
	})
}

// typeLinked gives every other linked range the text of the one the cursor is
// in, or ends the link when that text is no longer a word.
func (e *Session) typeLinked() {
	l, c := &e.links, e.Cursor()
	t := l.b.text.Snapshot()
	at := func(r [2]kernel.MarkID) (int, int) { return l.b.text.Mark(r[0]), l.b.text.Mark(r[1]) }
	i := slices.IndexFunc(l.ranges, func(r [2]kernel.MarkID) bool {
		start, end := at(r)
		return start <= c && c <= end
	})
	if i < 0 {
		return
	}
	text := t.Slice(at(l.ranges[i]))
	if !linkWord.MatchString(text) {
		e.unlink()
		return
	}
	var edits []vim.Edit
	for j, r := range l.ranges {
		if start, end := at(r); j != i && t.Slice(start, end) != text {
			edits = append(edits, vim.Edit{Start: start, End: end, Text: text})
		}
	}
	if len(edits) == 0 {
		return
	}
	slices.SortFunc(edits, func(x, y vim.Edit) int { return x.Start - y.Start })
	if err := (host{e}).Apply(edits, "linked edit"); err != nil {
		e.message = err.Error()
	}
}

// unlink forgets the linked ranges and lets their marks go.
func (e *Session) unlink() {
	l := &e.links
	for _, r := range l.ranges {
		l.b.text.DeleteMark(r[0])
		l.b.text.DeleteMark(r[1])
	}
	l.b, l.ranges = nil, nil
}

// linkedSpans are the linked ranges in b, drawn as linked.
func (e *Session) linkedSpans(b *buffer) []syntax.Span {
	l := &e.links
	if l.b != b {
		return nil
	}
	var out []syntax.Span
	for _, r := range l.ranges {
		out = append(out, syntax.Span{Start: b.text.Mark(r[0]), End: b.text.Mark(r[1]), Name: "lsp.linked"})
	}
	return out
}
