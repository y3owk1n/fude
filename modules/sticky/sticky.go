// Package sticky pins the first lines of the functions and classes a
// window's top is in over its top rows (B217).
package sticky

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// Module is sticky.
type Module struct{}

// New returns sticky.
func New() *Module { return &Module{} }

func (Module) Name() string { return "sticky" }

func (Module) Subscribed() []string {
	return []string{"window.moved", "window.opened", "buffer.changed", "syntax.updated", "option.set", "config.reloaded"}
}

func (Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Option, Name: "enabled", Default: "true", Help: "the first lines of the functions and classes the top of a window is in stay pinned over it"},
		{Kind: api.Option, Name: "lines", Default: "3", Help: "the most lines sticky pins"},
	}
}

func (Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event pins the lines of each window that shows the buffer it is about.
// With sticky.enabled off, it unpins every window.
func (Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	if event, detail, _ := strings.Cut(name, " "); event == "option.set" && !strings.HasPrefix(detail, "sticky.") {
		return api.Result{}, nil
	}
	pins := map[int][]int{}
	on, _ := s.Option("sticky.enabled")
	n, _ := s.Option("sticky.lines")
	lines, _ := strconv.Atoi(n)
	buf := s.About["buffer"]
	for _, w := range s.Windows() {
		switch {
		case on != "true":
			pins[w.ID] = nil
		case strconv.Itoa(w.Buffer) == buf && s.Context != nil:
			pins[w.ID] = pinned(s.Text, s.Context, w, lines)
		}
	}
	return api.Result{Pins: pins}, nil
}

// pinned returns the starts of the lines to pin over w's first rows: those
// of the functions and classes that hold the first line they leave in view
// and start above it, the outermost first. They never cover the cursor's
// line, and are at most lines.
func pinned(t api.Text, holding func(pos int) []int, w api.Window, lines int) []int {
	var starts []int
	first := t.LineOf(w.Top)
	// A cursor above the window, as while a search shows its match (B21),
	// has no line here to keep uncovered.
	below := t.LineOf(w.Cursor) - first
	if below < 0 {
		below = lines
	}
	room := min(lines, below, max(w.Height-2, 0))
	// Each pinned line covers one more line of text, so the loop asks again
	// about the line under them until the answer stops changing.
	for range lines {
		under := first + len(starts)
		var found []int
		for _, s := range holding(t.LineStart(min(under, t.Lines()-1))) {
			if ls := t.LineStart(t.LineOf(s)); t.LineOf(s) < under && !slices.Contains(found, ls) {
				found = append(found, ls)
			}
		}
		found = found[max(len(found)-room, 0):]
		if slices.Equal(found, starts) {
			break
		}
		starts = found
	}
	return starts
}

func (Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (Module) Close(context.Context) error { return nil }
