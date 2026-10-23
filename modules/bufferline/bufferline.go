// Package bufferline says what the top row lists: the open buffers that
// have files, each by its file's name, with its folder too when two names
// are the same, while bufferline.enabled is on (B258).
package bufferline

import (
	"context"
	"maps"
	"path/filepath"
	"slices"

	"github.com/y3owk1n/fude/api"
)

// Module is bufferline.
type Module struct{}

// New returns bufferline.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "bufferline" }

// Subscribed are what opens, closes or renames a buffer, and what may
// change bufferline.enabled.
func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.closed", "buffer.renamed", "explorer.read", "config.reloaded", "option.set"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Option, Name: "enabled", Default: "true", Help: "the top row lists the open buffers that have files"}}
}

// Init lists the buffers open at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	return api.Result{Tabs: tabs(s)}, nil
}

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event lists the buffers again.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	return api.Result{Tabs: tabs(s)}, nil
}

// tabs are the buffers a user edits that have files, in the order they
// opened, or none while bufferline.enabled is off.
func tabs(s api.Scope) *[]api.Tab {
	out := []api.Tab{}
	if on, _ := s.Option("bufferline.enabled"); on != "true" {
		return &out
	}
	bases := map[string]int{}
	ids := slices.Sorted(maps.Keys(s.Buffers))
	ids = slices.DeleteFunc(ids, func(id int) bool { b := s.Buffers[id]; return b.Kind != "" || b.Path == "" })
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
