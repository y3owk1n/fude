// Package recent keeps the files windows show, newest first, in fude's
// state directory, and lists them for :pick recent, those under the
// working directory first (B213).
package recent

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/internal/keep"
)

// Module is recent. It keeps the files shown.
type Module struct {
	mu     sync.Mutex
	store  *keep.Recent
	failed []string // what failed to write, for the next result's log
}

// New returns recent.
func New() *Module {
	m := &Module{}
	m.store = keep.NewRecent(keep.RecentPath, func(msg string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.failed = append(m.failed, msg)
	})
	return m
}

func (m *Module) Name() string { return "recent" }

func (m *Module) Subscribed() []string { return []string{"buffer.shown"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Source, Name: "recent", Help: "lists the files windows showed last, those under the working directory first", ID: 1}}
}

// Init keeps the file shown at the start.
func (m *Module) Init(_ context.Context, s api.Scope) (api.Result, error) {
	m.store.Note(s.About["path"])
	return api.Result{}, nil
}

// Call lists the recent files that still exist, but the one shown.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := m.logged()
	wd, err := os.Getwd()
	if err != nil {
		res.Error = fmt.Sprintf("recent files: %v", err)
		return res, nil
	}
	here, elsewhere, err := m.store.Files(s.About["path"], wd)
	if err != nil {
		res.Error = err.Error()
		return res, nil
	}
	res.Pick = &api.Pick{ID: 1, Title: "recent files", Items: append(here, elsewhere...), Files: true}
	return res, nil
}

// Event keeps the file a window shows.
func (m *Module) Event(_ context.Context, _ string, s api.Scope) (api.Result, error) {
	m.store.Note(s.About["path"])
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.logged(), nil
}

// Callback has nothing to do, as the finder opens the file picked.
func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

// logged returns a result that logs what failed to write since.
func (m *Module) logged() api.Result {
	res := api.Result{Log: m.failed}
	m.failed = nil
	return res
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

// Close writes the files noted, as fude quits.
func (m *Module) Close(context.Context) error {
	m.store.Flush()
	return nil
}
