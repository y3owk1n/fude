// Package tabline says which tab pages the top row lists and what it calls
// each (B545). While tabline.enabled is on and there are two pages or more,
// it lists each by the name of the buffer its current window shows.
package tabline

import (
	"context"
	"path/filepath"

	"github.com/y3owk1n/fude/api"
)

// Module is tabline.
type Module struct{}

// New returns tabline.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "tabline" }

// Subscribed are what opens, closes, moves or shows a tab page, what shows
// or renames a page's buffer, and what may change tabline.enabled.
func (m *Module) Subscribed() []string {
	return []string{"tabpage.changed", "buffer.shown", "buffer.renamed", "buffer.closed", "config.reloaded", "option.set"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Option, Name: "enabled", Default: "true", Help: "the top row lists the tab pages once there are two or more"}}
}

// Init lists the pages at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	return api.Result{TabLine: names(s)}, nil
}

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event lists the pages again.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	return api.Result{TabLine: names(s)}, nil
}

// names are the pages and their names, or none with one page or while
// tabline.enabled is off.
func names(s api.Scope) *[]api.TabName {
	out := []api.TabName{}
	pages := s.Pages()
	if on, _ := s.Option("tabline.enabled"); on != "true" || len(pages) < 2 {
		return &out
	}
	for _, p := range pages {
		name := "[No Name]"
		if b, ok := s.Buffers[p.Buffer]; ok && b.Path != "" {
			name = filepath.Base(b.Path)
		} else if ok && b.Name != "" {
			name = b.Name
		}
		out = append(out, api.TabName{Page: p.ID, Name: name})
	}
	return &out
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
