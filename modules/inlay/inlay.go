// Package inlay shows a language server's inlay hints among a buffer's
// text, such as parameter names and types (B216).
package inlay

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// Module is inlay.
type Module struct {
	mu      sync.Mutex
	shown   map[string][]int  // by buffer, the ids of the hints showing
	asked   map[string]uint32 // by buffer, the request waiting for the server
	again   map[string]bool   // by buffer, the text changed while a request waited
	without map[string]bool   // by buffer, no server gives hints, until one attaches
	bufs    map[uint32]string // by request, the buffer it is about
	ids     int
	reqs    uint32
}

// New returns inlay.
func New() *Module {
	return &Module{shown: map[string][]int{}, asked: map[string]uint32{}, again: map[string]bool{}, without: map[string]bool{}, bufs: map[uint32]string{}}
}

func (m *Module) Name() string { return "inlay" }

func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "buffer.closed", "lsp.attached", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration { return nil }

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event asks the server again after each change, once a server attaches
// and once inlay_hints turns on. Turning it off takes the hints away.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	switch event, detail, _ := strings.Cut(name, " "); {
	case event == "option.set" && detail != "inlay_hints":
		return api.Result{}, nil
	case event == "lsp.attached":
		delete(m.without, buf)
	case event == "buffer.closed":
		delete(m.bufs, m.asked[buf])
		delete(m.shown, buf)
		delete(m.asked, buf)
		delete(m.again, buf)
		delete(m.without, buf)
		return api.Result{}, nil
	}
	if on, _ := s.Option("inlay_hints"); on != "true" {
		res := api.Result{Undecorate: m.shown[buf]}
		delete(m.shown, buf)
		return res, nil
	}
	if m.without[buf] {
		return api.Result{}, nil
	}
	if _, waiting := m.asked[buf]; waiting {
		m.again[buf] = true
		return api.Result{}, nil
	}
	return m.ask(s), nil
}

// Callback shows the hints the server sent, in place of those showing, and
// asks again when the text changed meanwhile.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := m.bufs[id]
	delete(m.bufs, id)
	if m.asked[buf] != id {
		return api.Result{}, nil
	}
	delete(m.asked, buf)
	res := api.Result{Undecorate: m.shown[buf]}
	m.shown[buf] = nil
	if m.again[buf] {
		delete(m.again, buf)
		res.Requests = m.ask(s).Requests
	}
	switch err := s.About["error"]; {
	case strings.HasPrefix(err, "no language server offers"):
		m.without[buf] = true
		return res, nil
	case strings.HasPrefix(err, "no language server answered"):
		// A server answers null for a text with no hints.
		return res, nil
	case err != "":
		res.Error = err
		return res, nil
	}
	var found []struct {
		Position     lsp.Position    `json:"position"`
		Label        json.RawMessage `json:"label"`
		PaddingLeft  bool            `json:"paddingLeft"`
		PaddingRight bool            `json:"paddingRight"`
	}
	if err := json.Unmarshal([]byte(s.About["output"]), &found); err != nil {
		return api.Result{Error: "inlay hints from " + s.About["server"] + ": " + err.Error()}, nil
	}
	for _, h := range found {
		label := hintLabel(h.Label)
		if h.PaddingLeft {
			label = " " + label
		}
		if h.PaddingRight {
			label += " "
		}
		m.ids++
		at := lsp.Offset(s.Text, h.Position, s.About["encoding"])
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: at, End: at, Style: "inlay", Note: label, Insert: true})
		m.shown[buf] = append(m.shown[buf], m.ids)
	}
	return res, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// ask asks the servers of the buffer s is about for the hints of its
// whole text.
func (m *Module) ask(s api.Scope) api.Result {
	buf := s.About["buffer"]
	m.reqs++
	m.asked[buf], m.bufs[m.reqs] = m.reqs, buf
	return api.Result{Requests: []api.Request{{ID: m.reqs, Method: "textDocument/inlayHint", Pos: 0, End: s.Text.Len()}}}
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
