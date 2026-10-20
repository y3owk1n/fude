// Package sessions lists the sessions sharing this fude and acts on them:
// :who lists the guests, :kick ends a guest's session and :follow keeps the
// windows on what another session shows (B250, B256).
package sessions

import (
	"context"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// What sessions registers, by the ids Call runs them by.
const (
	who uint32 = iota + 1
	kick
	follow
)

// Module is sessions.
type Module struct{}

// New returns sessions.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "sessions" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "who", Help: "lists the guests", ID: who},
		{Kind: api.Command, Name: "kick", Help: "ends the session of the guest called name", ID: kick, Complete: true},
		{Kind: api.Command, Name: "follow", Help: "keeps the window on what the session called name shows, until a key is typed", ID: follow, Complete: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call lists the guests, ends one's session, or follows a session, and
// completes the names each takes.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	// guests are the guests' names, and others the other sessions'.
	var guests, others []string
	for _, p := range s.Sessions() {
		if p.Power != "host" {
			guests = append(guests, p.Name)
		}
		if !p.You {
			others = append(others, p.Name)
		}
	}
	if _, ok := s.About["complete"]; ok {
		if id == kick {
			return api.Result{Complete: guests}, nil
		}
		return api.Result{Complete: others}, nil
	}
	name := strings.TrimSpace(s.About["args"])
	switch id {
	case who:
		if len(guests) == 0 {
			return api.Result{Message: "no guests"}, nil
		}
		return api.Result{Message: strings.Join(guests, "  ")}, nil
	case kick:
		if name == "" {
			return api.Result{Error: "kick whom? :kick name"}, nil
		}
		return api.Result{Kick: name}, nil
	}
	if name == "" {
		return api.Result{Error: "follow whom? :follow name"}, nil
	}
	return api.Result{Follow: name}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
