// Package guides marks each indent level in a line's leading blanks with a
// faint line (B153).
package guides

import (
	"context"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// ahead is how far a blank line looks for the next line with text, whose
// guides it shows.
const ahead = 200

// Module is guides.
type Module struct{}

// New returns guides.
func New() *Module { return &Module{} }

func (Module) Name() string { return "guides" }

func (Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "window.moved", "window.opened", "option.set", "config.reloaded"}
}

func (Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Option, Name: "enabled", Default: "true", Help: "draws a faint line at each indent level"}}
}

func (Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event puts the guides of each window that shows the buffer it is about,
// from a window's height above it to one below, so a short scroll needs
// no new ones. With guides.enabled off, it takes every window's away.
func (Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	switch event, detail, _ := strings.Cut(name, " "); {
	case event != "option.set":
	case detail == "guides.enabled", detail == "indent_width", detail == "tab_width":
	default:
		return api.Result{}, nil
	}
	on, _ := s.Option("guides.enabled")
	step, _ := strconv.Atoi(s.About["indent"])
	tab, _ := strconv.Atoi(s.About["tab"])
	res := api.Result{Guides: map[int]map[int][]int{}}
	for _, w := range s.Windows() {
		switch {
		case on != "true":
			res.Guides[w.ID] = nil
		case strconv.Itoa(w.Buffer) == s.About["buffer"]:
			at := s.Text.LineOf(w.Top)
			res.Guides[w.ID] = guides(s.Text, at-w.Height, at+2*w.Height, max(step, 1), max(tab, 1))
		}
	}
	return res, nil
}

// guides returns, by line, the guide columns of the lines from first to
// last: one every step columns in a line's leading blanks. A blank line
// shows those of the next line with text, so the walk goes up from below
// last.
func guides(t api.Text, first, last, step, tab int) map[int][]int {
	first, last = max(first, 0), min(last, t.Lines()-1)
	next := 0
	for l := last + 1; l < min(last+1+ahead, t.Lines()); l++ {
		if n, blank := lead(t, l, tab); !blank {
			next = n
			break
		}
	}
	out := map[int][]int{}
	for l := last; l >= first; l-- {
		n, blank := lead(t, l, tab)
		if blank {
			n = next
		}
		next = n
		for col := 0; col < n; col += step {
			out[l] = append(out[l], col)
		}
	}
	return out
}

// lead returns the display columns of line's leading blanks, and whether
// the line has nothing else.
func lead(t api.Text, line, tab int) (int, bool) {
	start, end := t.LineStart(line), t.Len()
	if line+1 < t.Lines() {
		end = t.LineStart(line+1) - 1
	}
	col := 0
	for _, r := range t.Slice(start, end) {
		switch r {
		case ' ':
			col++
		case '\t':
			col += tab - col%tab
		default:
			return col, false
		}
	}
	return col, true
}

func (Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (Module) Close(context.Context) error { return nil }
