// Package references lights up the references to the symbol under the
// cursor, as a language server finds them, once the cursor rests on it in
// normal mode. They go when the cursor moves off them or the text changes
// (B461).
package references

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/protocol/lsp"
)

// rest is how long the cursor stays put before its references light up.
const rest = 300 * time.Millisecond

// The callbacks references asks for.
const (
	rested uint32 = iota + 1 // a wait for the cursor to rest
	found                    // the server's answer
)

// Module is references.
type Module struct {
	mu      sync.Mutex
	waits   int             // the waits under way, the last of which to end asks
	at      string          // the buffer and cursor asked about
	shown   []int           // the ids of the references lit
	without map[string]bool // by buffer, no server finds references, until one attaches
	ids     int
}

// New returns references.
func New() *Module { return &Module{without: map[string]bool{}} }

func (m *Module) Name() string { return "references" }

func (m *Module) Subscribed() []string {
	return []string{"window.moved", "buffer.changed", "mode.changed", "buffer.closed", "lsp.attached", "option.set"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Option, Name: "enabled", Default: "true", Help: "the references to the symbol under the cursor light up once the cursor rests on it"}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event takes the references away when the cursor leaves them or the text
// changes, and waits for the cursor to rest in normal mode.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	switch event, detail, _ := strings.Cut(name, " "); event {
	case "option.set":
		if detail != "references.enabled" {
			return api.Result{}, nil
		}
	case "lsp.attached", "buffer.closed":
		delete(m.without, buf)
		return api.Result{}, nil
	case "window.moved":
		if m.on(s) {
			return api.Result{}, nil
		}
	}
	res := api.Result{Undecorate: m.shown}
	m.shown, m.at = nil, ""
	// Only a buffer a server may answer for waits, so a scroll in one with
	// none settles at once.
	if on, _ := s.Option("references.enabled"); on == "true" && s.About["mode"] == "normal" && s.About["servers"] != "" && !m.without[buf] {
		m.waits++
		res.Timers = []api.Timer{{ID: rested, After: rest}}
	}
	return res, nil
}

// Callback asks the server once the last wait ends with the cursor still
// off any reference lit, and lights up what it finds while the cursor is
// where it asked.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf := s.About["buffer"]
	if id == rested {
		cursor, err := strconv.Atoi(s.About["cursor"])
		if m.waits--; m.waits > 0 || err != nil || s.About["mode"] != "normal" || m.on(s) {
			return api.Result{}, nil
		}
		m.at = buf + " " + s.About["cursor"]
		return api.Result{Requests: []api.Request{{ID: found, Method: "textDocument/documentHighlight", Pos: cursor, End: -1}}}, nil
	}
	if m.at != buf+" "+s.About["cursor"] {
		return api.Result{}, nil
	}
	if strings.HasPrefix(s.About["error"], "no language server offers") {
		m.without[buf] = true
	}
	var refs []struct {
		Range lsp.Range `json:"range"`
	}
	json.Unmarshal([]byte(s.About["output"]), &refs)
	res := api.Result{Undecorate: m.shown}
	m.shown = nil
	enc := s.About["encoding"]
	for _, r := range refs {
		m.ids++
		m.shown = append(m.shown, m.ids)
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: lsp.Offset(s.Text, r.Range.Start, enc), End: lsp.Offset(s.Text, r.Range.End, enc), Style: "reference"})
	}
	return res, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// on reports whether the cursor is on a reference lit in its buffer.
func (m *Module) on(s api.Scope) bool {
	cursor, err := strconv.Atoi(s.About["cursor"])
	if err != nil {
		return false
	}
	for _, d := range s.Decorations() {
		if strconv.Itoa(d.Buffer) == s.About["buffer"] && d.Start <= cursor && cursor < d.End {
			return true
		}
	}
	return false
}
