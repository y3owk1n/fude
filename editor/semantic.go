package editor

import (
	"encoding/json"
	"sort"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/syntax"
)

// tokens are a language server's semantic tokens of a buffer, by where they
// start, and whether they are being asked for, and again after (B471).
type tokens struct {
	spans         []syntax.Span
	asking, again bool
}

// tokenNames are the highlights a semantic token's type shows as, the
// names the syntax highlights give the same kinds of text (B471).
var tokenNames = map[string]string{
	"class": "type", "comment": "comment", "decorator": "attribute", "enum": "type", "enumMember": "constant",
	"event": "type", "function": "function", "interface": "type", "keyword": "keyword", "macro": "constant.macro",
	"method": "function.method", "modifier": "type.qualifier", "namespace": "module", "number": "number",
	"operator": "operator", "parameter": "variable.parameter", "property": "property", "regexp": "string.regexp",
	"string": "string", "struct": "type", "type": "type", "typeParameter": "type.definition", "variable": "variable",
}

// askTokens asks the first of b's servers that offers them for the semantic
// tokens of b's text. One request waits at a time, and an answer for text
// that changed since is dropped for the one asked after it (B471).
func (ed *Editor) askTokens(b *buffer) {
	var s *server
	var types []string
	for _, o := range b.servers {
		var p struct {
			Full   json.RawMessage `json:"full"`
			Legend struct {
				TokenTypes []string `json:"tokenTypes"`
			} `json:"legend"`
		}
		if json.Unmarshal(o.client.Capability("semanticTokensProvider"), &p) == nil && len(p.Full) > 0 && string(p.Full) != "false" {
			s, types = o, p.Legend.TokenTypes
			break
		}
	}
	if s == nil {
		return
	}
	if b.tokens.asking {
		b.tokens.again = true
		return
	}
	b.tokens.asking = true
	uri := lsp.URI(b.file.path)
	s.client.Request("textDocument/semanticTokens/full", func(string) any {
		return map[string]any{"textDocument": map[string]any{"uri": uri}}
	}, func(res json.RawMessage) {
		b.tokens.asking = false
		if b.tokens.again {
			b.tokens.again = false
			ed.askTokens(b)
			return
		}
		var r struct {
			Data []int `json:"data"`
		}
		if json.Unmarshal(res, &r) == nil {
			b.tokens.spans = tokenSpans(b.text.Snapshot(), r.Data, types, s.client.Encoding())
		}
	})
}

// tokenSpans reads data, five numbers a token, each token's line and start
// counted from the one before, as highlights of t named after their types.
func tokenSpans(t kernel.Snapshot, data []int, types []string, enc string) []syntax.Span {
	var out []syntax.Span
	line, char := 0, 0
	for i := 0; i+4 < len(data); i += 5 {
		if data[i] > 0 {
			line, char = line+data[i], 0
		}
		char += data[i+1]
		if data[i+3] >= len(types) || tokenNames[types[data[i+3]]] == "" {
			continue
		}
		start := lsp.Offset(t, lsp.Position{Line: line, Character: char}, enc)
		end := lsp.Offset(t, lsp.Position{Line: line, Character: char + data[i+2]}, enc)
		out = append(out, syntax.Span{Start: start, End: end, Name: tokenNames[types[data[i+3]]]})
	}
	return out
}

// moveTokens moves b's tokens through edits until the server's next
// answer, dropping those an edit made longer or shorter.
func (b *buffer) moveTokens(edits []kernel.Edit) {
	kept := b.tokens.spans[:0]
	for _, sp := range b.tokens.spans {
		start, end := moved(edits, sp.Start), moved(edits, sp.End)
		if end-start == sp.End-sp.Start {
			kept = append(kept, syntax.Span{Start: start, End: end, Name: sp.Name})
		}
	}
	b.tokens.spans = kept
}

// tokensIn returns b's tokens over [from, to), while a server is attached.
func (b *buffer) tokensIn(from, to int) []syntax.Span {
	if len(b.servers) == 0 {
		return nil
	}
	sp := b.tokens.spans
	i := sort.Search(len(sp), func(i int) bool { return sp[i].End > from })
	j := sort.Search(len(sp), func(i int) bool { return sp[i].Start >= to })
	return sp[i:max(i, j)]
}
