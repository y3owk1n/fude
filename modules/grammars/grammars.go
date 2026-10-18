// Package grammars is :grammar: it lists the grammars fude builds and
// whether each is built, builds one again, or builds again those built at
// an older pin (B165).
package grammars

import (
	"context"
	"fmt"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Module is grammars.
type Module struct{}

// New returns grammars.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "grammars" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Command, Name: "grammar", Help: "lists the grammars and whether each is built, builds one again with install {filetype}, or builds again with update those built at an older pin", ID: 1, Complete: true}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call lists the grammars, or builds one or the outdated ones.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, _ ...int) (api.Result, error) {
	grammars := s.Grammars()
	if _, ok := s.About["complete"]; ok {
		names := []string{"update"}
		for _, g := range grammars {
			names = append(names, "install "+g.Filetype)
		}
		return api.Result{Complete: names}, nil
	}
	arg := strings.TrimSpace(s.About["args"])
	// A guest lists the grammars and builds none (B244).
	if arg != "" && s.About["power"] != "host" {
		return api.Result{Message: "a guest may not build grammars"}, nil
	}
	if ft, ok := strings.CutPrefix(arg, "install "); ok {
		return api.Result{Build: []string{strings.TrimSpace(ft)}}, nil
	}
	switch arg {
	case "update":
		var outdated []string
		for _, g := range grammars {
			if g.State == "outdated" {
				outdated = append(outdated, g.Filetype)
			}
		}
		if len(outdated) == 0 {
			return api.Result{Message: "every grammar built is up to date"}, nil
		}
		return api.Result{Build: outdated}, nil
	case "":
	default:
		return api.Result{Message: "not a grammar command: " + arg}, nil
	}
	lines := make([]string, len(grammars))
	for i, g := range grammars {
		lines[i] = fmt.Sprintf("%-12s %s", g.Filetype, g.State)
	}
	return api.Result{Show: &api.Panel{Title: "[grammars]", Text: strings.Join(lines, "\n")}}, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
