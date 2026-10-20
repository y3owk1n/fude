// Package terminal is :terminal, which shows and hides the session's
// shells, or runs a command, in a terminal window below, and :pick
// terminals, which shows a terminal again (B205, B364).
package terminal

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

const (
	run    = 1 + iota // :terminal
	pick              // :pick terminals
	picked            // the terminal picked
)

// Module is terminal.
type Module struct {
	mu    sync.Mutex
	shown []int // the terminal buffers the list shows, by id
}

// New returns terminal.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "terminal" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "terminal", Short: []string{"ter", "term"}, Args: "[n|cmd]", Help: "shows or hides shell n, 1 without it, or runs cmd in a terminal window below. <C-\\><C-n> or <Esc><Esc> leaves terminal mode, and i goes back", ID: run, Host: true},
		{Kind: api.Source, Name: "terminals", Help: "the terminals, and shows the one picked", ID: pick, Host: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call shows or hides the shell numbered after :terminal, or runs the
// command typed after it, or lists the terminals to pick from.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	if id == pick {
		return m.list(s), nil
	}
	args := strings.TrimSpace(s.About["args"])
	if n, err := strconv.Atoi(args); err == nil && n > 0 {
		return api.Result{Terminal: &api.Terminal{Shell: n}}, nil
	}
	return api.Result{Terminal: &api.Terminal{Command: args}}, nil
}

// list lists the terminals the session may show, by their names.
func (m *Module) list(s api.Scope) api.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shown = m.shown[:0]
	var items []string
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		if b := s.Buffers[id]; b.Kind == "terminal" && !b.Barred {
			m.shown = append(m.shown, id)
			items = append(items, strings.Trim(b.Name, "[]"))
		}
	}
	if len(items) == 0 {
		return api.Result{Message: "no terminals"}
	}
	return api.Result{Pick: &api.Pick{ID: picked, Title: "terminals", Items: items}}
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// Callback shows the terminal picked.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, _ := strconv.Atoi(s.About["picked"])
	if id != picked || s.About["ok"] != "true" || i < 0 || i >= len(m.shown) {
		return api.Result{}, nil
	}
	return api.Result{Terminal: &api.Terminal{Buffer: m.shown[i]}}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
