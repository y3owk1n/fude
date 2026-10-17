package editor

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// hinted names the inlay hints a server sent for a buffer.
type hinted struct {
	s *server
	b *buffer
}

// askHints asks s for the inlay hints of b, when inlay_hints is on and s
// gives them. It drops an answer for text that changed since, as the
// change asks again (B216).
func (e *Editor) askHints(s *server, b *buffer) {
	if !e.cfg.InlayHints || !s.client.Supports("inlayHintProvider") {
		return
	}
	t := b.text.Snapshot()
	uri := lsp.URI(b.file.path)
	s.client.Request("textDocument/inlayHint", func(enc string) any {
		whole := lsp.Range{End: lsp.Point(t, t.Len(), enc)}
		return map[string]any{"textDocument": map[string]any{"uri": uri}, "range": whole}
	}, func(res json.RawMessage) {
		if b.text.Snapshot().Version() != t.Version() {
			return
		}
		var found []struct {
			Position     lsp.Position    `json:"position"`
			Label        json.RawMessage `json:"label"`
			PaddingLeft  bool            `json:"paddingLeft"`
			PaddingRight bool            `json:"paddingRight"`
		}
		if err := json.Unmarshal(res, &found); err != nil {
			logLine(fmt.Sprintf("inlay hints from %s: %v", s.name, err))
			return
		}
		for _, d := range e.decos[hinted{s, b}] {
			d.remove()
		}
		enc := s.client.Encoding()
		var ds []decoration
		for _, h := range found {
			label := hintLabel(h.Label)
			if h.PaddingLeft {
				label = " " + label
			}
			if h.PaddingRight {
				label += " "
			}
			at := lsp.Offset(t, h.Position, enc)
			ds = append(ds, decoration{buf: b, start: b.text.SetMark(at, kernel.Right), end: b.text.SetMark(at, kernel.Right), style: "inlay", note: label})
		}
		e.decos[hinted{s, b}] = ds
	})
}

// hintLabel returns an inlay hint's label, which is text or a list of
// parts with text.
func hintLabel(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Value string `json:"value"`
	}
	json.Unmarshal(raw, &parts)
	var s strings.Builder
	for _, p := range parts {
		s.WriteString(p.Value)
	}
	return s.String()
}
