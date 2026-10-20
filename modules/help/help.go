// Package help shows fude's help in its window, at the entry a name has,
// and goes to the entry of the name under the cursor there (B162).
package help

import (
	"context"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// What help registers, by the ids Call runs them by.
const (
	open uint32 = iota + 1
	follow
)

// keys are help's own keys, which [keymap.help] changes (B162).
var keys = map[string]string{"<CR>": "<Plug>(help.follow)", "<C-]>": "<Plug>(help.follow)", "q": ":close<CR>"}

// Module is help.
type Module struct{}

// New returns help.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "help" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "help", Short: []string{"h"}, Help: "opens help, at the entry for a command, key, option or setting", ID: open, Complete: true},
		{Kind: api.Plug, Name: "follow", Help: "goes to the help entry of the name under the cursor", ID: follow},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call shows help at the entry the name typed after :help names, or the
// name under the cursor names.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if _, ok := s.About["complete"]; ok {
		return api.Result{Complete: s.HelpTags()}, nil
	}
	topic := strings.TrimSpace(s.About["args"])
	if id == follow {
		if topic = word(s.Text, args[0]); topic == "" {
			return api.Result{}, nil
		}
	}
	text, line, ok := s.HelpAt(topic)
	if !ok {
		return api.Result{Message: "no help for " + topic}, nil
	}
	// Help is markdown, coloured as such once its grammar is built, with
	// the entry at the top of the window and its text below it.
	return api.Result{Show: &api.Panel{Title: "[help]", Filetype: "markdown", Text: text, Keys: keys, Line: line + 1}}, nil
}

// word is the name under c: the run of text between blanks, without the
// punctuation around it.
func word(t api.Text, c int) string {
	line := t.LineOf(c)
	ls, le := t.LineStart(line), t.Len()
	if line+1 < t.Lines() {
		le = t.LineStart(line+1) - 1
	}
	text, at := t.Slice(ls, le), c-ls
	start := strings.LastIndexAny(text[:at], " \t") + 1
	end := len(text)
	if i := strings.IndexAny(text[at:], " \t"); i >= 0 {
		end = at + i
	}
	w := strings.Trim(text[start:end], ".,`()")
	if strings.HasPrefix(w, "<Plug>(") {
		w += ")" // the one Trim took
	}
	return w
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
