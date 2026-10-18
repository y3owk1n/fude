// Package surround puts brackets or quotes around text, as vim-surround
// does: ys{motion}{char}, yss{char} and S{char} in visual mode add them,
// ds{char} deletes them and cs{old}{new} changes them (B219).
package surround

import (
	"context"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// What surround registers, by the ids Call runs them by.
const (
	add uint32 = iota + 1
	addVisual
	del
	change
)

// Module is surround.
type Module struct{}

// New returns surround.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "surround" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Operator, Name: "ys", Help: "puts the brackets or quotes the character names around the text", ID: add, Chars: 1},
		{Kind: api.Key, Name: "S", Help: "puts the brackets or quotes the character names around the selection", ID: addVisual, Chars: 1, Visual: true},
		{Kind: api.Key, Name: "ds", Help: "deletes the brackets or quotes the character names around the cursor", ID: del, Chars: 1},
		{Kind: api.Key, Name: "cs", Help: "changes the brackets or quotes the first character names around the cursor to those the second names", ID: change, Chars: 2},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call adds, deletes or changes the pair the characters typed name.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	chars := strings.Split(s.About["chars"], "")
	switch id {
	case add, addVisual:
		return api.Result{Edits: wrap(s.Text, args[0], args[1], args[2] == 1, chars[0])}, nil
	case del:
		return api.Result{Edits: rewrap(s, args[0], chars[0], "")}, nil
	}
	return api.Result{Edits: rewrap(s, args[0], chars[0], chars[1])}, nil
}

// pair returns the text that goes before and after what ch names: a pair of
// brackets, with a blank inside each when ch is the opening one, or ch
// itself on both sides.
func pair(ch string) (string, string) {
	switch ch {
	case ")", "b":
		return "(", ")"
	case "(":
		return "( ", " )"
	case "]", "r":
		return "[", "]"
	case "[":
		return "[ ", " ]"
	case "}", "B":
		return "{", "}"
	case "{":
		return "{ ", " }"
	case ">", "<", "a":
		return "<", ">"
	}
	return ch, ch
}

// wrap puts what ch names around the text from start to end. A linewise
// range is its lines' text from the first non-blank, as yss surrounds the
// line.
func wrap(t api.Text, start, end int, lines bool, ch string) []api.Edit {
	if lines {
		line := t.Slice(start, lineEnd(t, start))
		start, end = start+len(line)-len(strings.TrimLeft(line, " \t")), lineEnd(t, max(end-1, start))
	}
	open, close := pair(ch)
	return []api.Edit{{Start: start, End: start, Text: open}, {Start: end, End: end, Text: close}}
}

// rewrap deletes the brackets or quotes that old names around c, with the
// blanks inside them when old is an opening bracket, and puts what new
// names in their place, or nothing.
func rewrap(s api.Scope, c int, old, new string) []api.Edit {
	open, close := pair(old)
	o, cl := strings.TrimSpace(open), strings.TrimSpace(close)
	if o == "" || cl == "" {
		return nil
	}
	// Two of a quote keep the quotes, and a( is the brackets with what is
	// inside.
	object := "a" + o
	if o == cl {
		object = "2i" + o
	}
	start, end, ok := s.Object(object, c)
	if !ok {
		return nil
	}
	t := s.Text
	in, out := start+1, end-1
	if open != o {
		for in < out && strings.Contains(" \t", t.Slice(in, in+1)) {
			in++
		}
		for out > in && strings.Contains(" \t", t.Slice(out-1, out)) {
			out--
		}
	}
	before, after := "", ""
	if new != "" {
		before, after = pair(new)
	}
	return []api.Edit{{Start: start, End: in, Text: before}, {Start: out, End: end, Text: after}}
}

// lineEnd is where the line holding pos ends, before its newline.
func lineEnd(t api.Text, pos int) int {
	if l := t.LineOf(pos); l+1 < t.Lines() {
		return t.LineStart(l+1) - 1
	}
	return t.Len()
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
