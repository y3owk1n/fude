// Package theme switches the colours every client draws in, with :theme,
// and centers the windows on a wide screen, with :center and
// <Plug>(theme.center) (B158, B199).
package theme

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// What theme registers, by the ids Call runs them by.
const (
	theme uint32 = iota + 1
	center
)

// Module is theme.
type Module struct{}

// New returns theme.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "theme" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "theme", Args: "[dark|light|{file}]", Help: "switches the theme of every client, or shows the one in use", ID: theme, Complete: true},
		{Kind: api.Command, Name: "center", Help: "centers the windows on a wide screen, as wide as the colour column, or stops", ID: center},
		{Kind: api.Plug, Name: "center", Help: "centers the windows on a wide screen, or stops", ID: center},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call switches or shows the theme, or toggles the windows centered, as
// setting theme or center does, which a guest may not (B39).
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	if id == center {
		on, _ := s.Option("center")
		return api.Result{Set: map[string]string{"center": strconv.FormatBool(on != "true")}}, nil
	}
	if _, ok := s.About["complete"]; ok {
		return api.Result{Complete: themes(s)}, nil
	}
	if arg := strings.TrimSpace(s.About["args"]); arg != "" {
		return api.Result{Set: map[string]string{"theme": arg}}, nil
	}
	now, _ := s.Option("theme")
	return api.Result{Message: now}, nil
}

// themes are what :theme completes with: dark, light, and the scheme
// config.toml names, to switch back to it.
func themes(s api.Scope) []string {
	names := []string{"dark", "light"}
	var named string
	if raw, ok := s.Config("theme"); ok && json.Unmarshal([]byte(raw), &named) == nil && !slices.Contains(names, named) {
		names = append(names, named)
	}
	return names
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
