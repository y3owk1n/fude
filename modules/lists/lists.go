// Package lists lists what the editor keeps: :buffers, :marks, :jumps,
// :changes, :registers and :messages (B157, B268).
package lists

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// What lists registers, by the ids Call runs them by.
const (
	buffers uint32 = iota + 1
	marks
	jumps
	changes
	registers
	messages
)

// Module is lists.
type Module struct{}

// New returns lists.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "lists" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "buffers", Short: []string{"ls"}, Help: "lists the buffers", ID: buffers},
		{Kind: api.Command, Name: "marks", Help: "lists the buffer's marks", ID: marks},
		{Kind: api.Command, Name: "jumps", Short: []string{"ju"}, Help: "lists the window's jumps", ID: jumps},
		{Kind: api.Command, Name: "changes", Help: "lists where the buffer changed", ID: changes},
		{Kind: api.Command, Name: "registers", Short: []string{"reg"}, Help: "lists the registers, or those named", ID: registers},
		{Kind: api.Command, Name: "messages", Short: []string{"mes"}, Help: "lists the messages shown, oldest first, or with yank puts the last on the clipboard", ID: messages, Complete: true},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call lists what the command names.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	arg := strings.TrimSpace(s.About["args"])
	switch id {
	case buffers:
		var shown []string
		for _, bid := range slices.Sorted(maps.Keys(s.Buffers)) {
			b, flags := s.Buffers[bid], ""
			if strconv.Itoa(bid) == s.About["buffer"] {
				flags += "%"
			}
			if b.Modified {
				flags += "+"
			}
			shown = append(shown, fmt.Sprintf("%d%s \"%s\"", bid, flags, b.Name))
		}
		return api.Result{Message: strings.Join(shown, "  ")}, nil
	case marks:
		return places(s, "marks", s.Marks(), func(_ int, mk api.Mark) string { return mk.Name + "  " }), nil
	case jumps:
		js := s.Jumps()
		return places(s, "jumps", js, func(i int, mk api.Mark) string {
			return fmt.Sprintf("%d  %s  ", len(js)-i, s.Buffers[mk.Buffer].Name)
		}), nil
	case changes:
		cs := s.Changes()
		return places(s, "changes", cs, func(i int, _ api.Mark) string { return fmt.Sprintf("%d  ", len(cs)-i) }), nil
	case registers:
		var shown []string
		if arg == "" {
			for _, r := range s.Registers() {
				shown = append(shown, fmt.Sprintf("\"%s   %s", r.Name, strings.ReplaceAll(r.Text, "\n", "^J")))
			}
			return api.Result{Message: strings.Join(shown, "  ")}, nil
		}
		for _, r := range arg {
			text, ok, err := s.Register(r)
			switch {
			case err != nil:
				shown = append(shown, fmt.Sprintf("\"%c   %v", r, err))
			case ok:
				shown = append(shown, fmt.Sprintf("\"%c   %s", r, strings.ReplaceAll(text, "\n", "^J")))
			}
		}
		return api.Result{Message: strings.Join(shown, "  ")}, nil
	}
	// messages
	if _, ok := s.About["complete"]; ok {
		return api.Result{Complete: []string{"yank"}}, nil
	}
	history := s.Messages()
	switch {
	case len(history) == 0:
		return api.Result{Message: "no messages"}, nil
	case arg == "yank":
		// The clipboard is the client's for a session other than the first
		// (B241).
		return api.Result{Copy: history[len(history)-1], Message: "copied the last message"}, nil
	case arg != "":
		return api.Result{Message: "messages takes yank or nothing"}, nil
	}
	return api.Result{Show: &api.Panel{Title: "[messages]", Text: strings.Join(history, "\n")}}, nil
}

// places lists ms in a list window titled what, each after what label
// says, as its line and column and the text of its line. <CR> goes to it.
func places(s api.Scope, what string, ms []api.Mark, label func(int, api.Mark) string) api.Result {
	if len(ms) == 0 {
		return api.Result{Message: "no " + what}
	}
	pl := &api.Places{Title: what, Show: true}
	for i, mk := range ms {
		b := s.Buffers[mk.Buffer]
		t := b.Text
		pos := min(mk.Pos, t.Len())
		line := t.LineOf(pos)
		ls := t.LineStart(line)
		end := t.Len()
		if line+1 < t.Lines() {
			end = t.LineStart(line+1) - 1
		}
		text := strings.TrimSpace(t.Slice(ls, end))
		pl.Items = append(pl.Items, fmt.Sprintf("%s:%d:%d:%s", b.Path, line+1, pos-ls+1, text))
		pl.Lines = append(pl.Lines, fmt.Sprintf("%s%d:%d  %s", label(i, mk), line+1, pos-ls+1, text))
	}
	return api.Result{Places: pl}
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
