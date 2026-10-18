// Package plug is :plug, which runs <Plug>(name) as a key mapped to it does
// (B271).
package plug

import (
	"context"
	"slices"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Module is plug.
type Module struct{}

// New returns plug.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "plug" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "plug", Args: "name", Help: "runs <Plug>(name) as a key mapped to it does", ID: 1, Complete: true}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs the plug named, or completes a plug's name.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	var names []string
	for _, c := range s.Commands() {
		if name, ok := strings.CutPrefix(c.Name, "plug "); ok {
			names = append(names, name)
		}
	}
	if _, ok := s.About["complete"]; ok {
		return api.Result{Complete: names}, nil
	}
	name := strings.TrimSpace(s.About["args"])
	if !slices.Contains(names, name) {
		return api.Result{Message: "no plug is called " + name}, nil
	}
	return api.Result{Keys: "<Plug>(" + name + ")"}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
