// Package bufferline says what the top row lists: the open buffers that
// have files, each by its file's name, with its folder too when two names
// are the same, while bufferline.enabled is on (B258). It is also :bmove,
// which moves a buffer along the row (B554).
package bufferline

import (
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Module is bufferline.
type Module struct {
	order []int // the buffers :bmove placed, by id, in the row's order
}

// New returns bufferline.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "bufferline" }

// Subscribed are what opens, closes or renames a buffer, and what may
// change bufferline.enabled.
func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.closed", "buffer.renamed", "explorer.read", "config.reloaded", "option.set"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Option, Name: "enabled", Default: "true", Help: "the top row lists the open buffers that have files"},
		{Kind: api.Command, Name: "bmove", Short: []string{"bm"}, Args: "[n]", Help: "moves the buffer after buffer n of the top row, first with 0, by n with +n or -n, or to the end", ID: 1},
	}
}

// Init lists the buffers open at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	return api.Result{Tabs: m.tabs(s)}, nil
}

// Call moves the current buffer along the row, as :tabmove moves a tab
// page (B554).
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	row := *m.tabs(s)
	current, _ := strconv.Atoi(s.About["buffer"])
	at := slices.IndexFunc(row, func(t api.Tab) bool { return t.Buffer == current })
	if at < 0 {
		return api.Result{Error: "the buffer is not on the top row"}, nil
	}
	arg := strings.TrimSpace(s.About["args"])
	to := len(row) - 1
	if arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil {
			return api.Result{Error: "not a place on the top row: " + arg}, nil
		}
		switch {
		case arg[0] == '+' || arg[0] == '-':
			to = at + n
		case n > at:
			to = n - 1
		default:
			to = n
		}
		if to < 0 || to >= len(row) || arg[0] != '+' && arg[0] != '-' && n > len(row) {
			return api.Result{Error: fmt.Sprintf("cannot move the buffer to %s", arg)}, nil
		}
	}
	t := row[at]
	row = slices.Insert(slices.Delete(row, at, at+1), to, t)
	m.order = m.order[:0]
	for _, t := range row {
		m.order = append(m.order, t.Buffer)
	}
	return api.Result{Tabs: &row}, nil
}

// Event lists the buffers again.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	return api.Result{Tabs: m.tabs(s)}, nil
}

// tabs are the buffers a user edits that have files, in the order :bmove
// left them and then the order they opened, or none while
// bufferline.enabled is off.
func (m *Module) tabs(s api.Scope) *[]api.Tab {
	out := []api.Tab{}
	if on, _ := s.Option("bufferline.enabled"); on != "true" {
		return &out
	}
	bases := map[string]int{}
	ids := slices.Sorted(maps.Keys(s.Buffers))
	ids = slices.DeleteFunc(ids, func(id int) bool { b := s.Buffers[id]; return b.Kind != "" || b.Path == "" })
	// place is where :bmove put a buffer, after all it placed for one it
	// did not.
	place := func(id int) int {
		if i := slices.Index(m.order, id); i >= 0 {
			return i
		}
		return len(m.order)
	}
	slices.SortStableFunc(ids, func(a, b int) int { return place(a) - place(b) })
	for _, id := range ids {
		bases[filepath.Base(s.Buffers[id].Path)]++
	}
	for _, id := range ids {
		path := s.Buffers[id].Path
		name := filepath.Base(path)
		if bases[name] > 1 {
			name = filepath.Join(filepath.Base(filepath.Dir(path)), name)
		}
		out = append(out, api.Tab{Buffer: id, Name: name})
	}
	return &out
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
