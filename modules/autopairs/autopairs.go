// Package autopairs types the closing bracket or quote with an opening one
// typed in insert mode, steps over a closing one that comes next, and
// deletes an empty pair with <BS>, while autopairs is on (B202).
package autopairs

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
)

// pairs are the brackets and quotes it closes, by their opening one.
var pairs = map[string]string{"(": ")", "[": "]", "{": "}", `"`: `"`, "'": "'", "`": "`"}

// keys are the keys it reads, by the ids Call runs them by.
var keys = []string{"(", "[", "{", `"`, "'", "`", ")", "]", "}", "<BS>"}

// Module is autopairs.
type Module struct{}

// New returns autopairs.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "autopairs" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	regs := make([]api.Registration, len(keys))
	for i, k := range keys {
		help := "types its closing one too, or steps over it when it comes next"
		if k == "<BS>" {
			help = "deletes an empty pair"
		}
		regs[i] = api.Registration{Kind: api.Typed, Name: k, Help: help, ID: uint32(i + 1)}
	}
	return regs
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call pairs the key typed at the cursor, or lets it type as it is.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if on, _ := s.Option("autopairs"); on != "true" || int(id) > len(keys) {
		return api.Result{}, nil
	}
	return api.Result{Edits: pair(s.Text, args[0], keys[id-1])}, nil
}

// pair returns what typing k at c does: an opening bracket or quote and its
// closing one, before a blank, a line end or a closing bracket, stepping
// over a closing one that comes next, or deleting an empty pair with <BS>.
// A quote after a word's character stays single, so don't keeps one quote.
func pair(t api.Text, c int, k string) []api.Edit {
	before, after := t.Slice(max(c-1, 0), c), t.Slice(c, min(c+1, t.Len()))
	if k == "<BS>" {
		if closing, ok := pairs[before]; ok && c > 0 && after == closing {
			return []api.Edit{{Start: c - 1, End: c + 1}}
		}
		return nil
	}
	if after == k && strings.Contains(")]}\"'`", k) {
		return []api.Edit{{Start: c, End: c + 1, Text: k}}
	}
	closing, ok := pairs[k]
	if !ok || after != "" && !strings.Contains(" \t\n)]}", after) {
		return nil
	}
	if r, _ := utf8.DecodeLastRuneInString(t.Slice(max(c-utf8.UTFMax, 0), c)); k == closing && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
		return nil
	}
	return []api.Edit{{Start: c, End: c, Text: k}, {Start: c, End: c, Text: closing}}
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
