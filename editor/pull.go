package editor

import (
	"encoding/json"
	"slices"

	"github.com/y3owk1n/fude/protocol/lsp"
)

// askDiagnostics asks s for b's diagnostics when s offers them to be asked
// for, and shows them as those a server publishes show. One request waits at
// a time, and a change while it waits asks again once it is answered (B470).
func (ed *Editor) askDiagnostics(s *server, b *buffer) {
	if !s.client.Supports("diagnosticProvider") {
		return
	}
	if _, waiting := s.pulls[b]; waiting {
		s.pulls[b] = true
		return
	}
	s.pulls[b] = false
	var p struct {
		Identifier string `json:"identifier"`
	}
	json.Unmarshal(s.client.Capability("diagnosticProvider"), &p)
	uri := lsp.URI(b.file.path)
	params := map[string]any{"textDocument": map[string]any{"uri": uri}}
	if p.Identifier != "" {
		params["identifier"] = p.Identifier
	}
	s.client.Request("textDocument/diagnostic", func(string) any { return params }, func(res json.RawMessage) {
		again := s.pulls[b]
		delete(s.pulls, b)
		if !slices.Contains(s.bufs, b) {
			return
		}
		// An unchanged report keeps the diagnostics showing.
		var r struct {
			Kind  string          `json:"kind"`
			Items json.RawMessage `json:"items"`
		}
		if json.Unmarshal(res, &r) == nil && r.Kind == "full" {
			params, _ := json.Marshal(map[string]any{"uri": uri, "diagnostics": r.Items})
			ed.published(s, b, params)
		}
		if again {
			ed.askDiagnostics(s, b)
		}
	})
}
