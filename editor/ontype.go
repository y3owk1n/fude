package editor

import (
	"encoding/json"
	"slices"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
	"github.com/y3owk1n/fude/vim"
)

// formatTyped asks the first server that formats as text is typed to format
// after k, when k is one of its trigger characters, and applies its edits
// to the text it saw. While insert mode lasts they join its undo step
// (B472).
func (e *Session) formatTyped(k vim.Key) {
	if e.vim.Mode() != vim.Insert {
		return
	}
	ch := string(k)
	if k == "<CR>" {
		ch = "\n"
	}
	b := e.win.buf
	for _, s := range b.servers {
		var p struct {
			First string   `json:"firstTriggerCharacter"`
			More  []string `json:"moreTriggerCharacter"`
		}
		if json.Unmarshal(s.client.Capability("documentOnTypeFormattingProvider"), &p) != nil {
			continue
		}
		if ch != p.First && !slices.Contains(p.More, ch) {
			return
		}
		t, c, uri := b.text.Snapshot(), e.Cursor(), lsp.URI(b.file.path)
		options := map[string]any{"tabSize": e.indentWidth(b), "insertSpaces": e.win.opts.ExpandTab}
		s.client.Request("textDocument/onTypeFormatting", func(enc string) any {
			return map[string]any{"textDocument": map[string]any{"uri": uri}, "position": lsp.Point(t, c, enc), "ch": ch, "options": options}
		}, func(res json.RawMessage) {
			var edits []textEdit
			if json.Unmarshal(res, &edits) != nil || len(edits) == 0 || b.text.Snapshot().Version() != t.Version() {
				return
			}
			tx := kernel.Transaction{Base: t.Version(), Origin: e.origin, Label: "format", Time: e.now(), Edits: kernelEdits(t, edits, s.client.Encoding())}
			if err := b.text.Apply(tx); err != nil {
				e.message = err.Error()
				return
			}
			b.head = b.text.Head()
		})
		return
	}
}
