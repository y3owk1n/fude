// Package terminal is :terminal, which runs a command, or the shell, in a
// terminal window below (B205).
package terminal

import (
	"context"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Module is terminal.
type Module struct{}

// New returns terminal.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "terminal" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "terminal", Short: []string{"ter", "term"}, Help: "runs cmd, or the shell, in a terminal window below. <C-\\><C-n> or <Esc><Esc> leaves terminal mode, and i goes back", ID: 1, Host: true}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs the command typed after :terminal, or the shell.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	return api.Result{Terminal: &api.Terminal{Command: strings.TrimSpace(s.About["args"])}}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
