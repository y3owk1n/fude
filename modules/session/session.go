// Package session keeps the working directory's session as fude quits:
// its windows on files, each with its cursor, top line and options, and
// which is current. <Plug>(session.restore) builds them again, as the start
// screen's s does (B226).
package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/keep"
)

// restore is the id of <Plug>(session.restore).
const restore uint32 = 1

// Module is session.
type Module struct{}

// New returns session.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "session" }

func (m *Module) Subscribed() []string { return []string{"editor.quitting"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Plug, Name: "restore", Help: "restores the last session of the working directory", ID: restore}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call restores the session kept for the working directory.
func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	path, err := keep.SessionPath()
	if err != nil {
		return api.Result{Message: err.Error()}, nil
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return api.Result{Message: "no session here"}, nil
	case err != nil:
		return api.Result{Message: fmt.Sprintf("session: %v", err)}, nil
	}
	return api.Result{Restore: string(data)}, nil
}

// Event keeps the session as fude quits. With no window on a file, it
// keeps the last one.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	data := s.Session()
	if data == "" {
		return api.Result{}, nil
	}
	path, err := keep.SessionPath()
	if err == nil {
		err = keep.Write(path, []byte(data))
	}
	if err != nil {
		return api.Result{Log: []string{fmt.Sprintf("session: %v", err)}}, nil
	}
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
