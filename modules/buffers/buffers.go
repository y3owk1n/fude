// Package buffers shows, steps through, writes and closes buffers:
// :buffer, :bnext, :bprevious, :update, :bdelete and :enew (B89, B155).
package buffers

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/api"
)

// What buffers registers, by the ids Call runs them by.
const (
	show uint32 = iota + 1
	next
	prev
	drop
	empty
	update
)

// Module is buffers.
type Module struct{}

// New returns buffers.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "buffers" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "buffer", Short: []string{"b"}, Help: "shows the buffer with that number, or whose name holds it. # is the one shown before", ID: show, Complete: true, Args: "{buffer}"},
		{Kind: api.Command, Name: "bnext", Short: []string{"bn"}, Help: "shows the next buffer", ID: next},
		{Kind: api.Command, Name: "bprevious", Short: []string{"bp", "bN", "bNext"}, Help: "shows the buffer before", ID: prev},
		{Kind: api.Command, Name: "bdelete", Short: []string{"bd"}, Help: "closes the buffer. ! drops its changes", ID: drop, Complete: true, Args: "[buffer]"},
		{Kind: api.Command, Name: "enew", Help: "shows a new empty buffer", ID: empty},
		{Kind: api.Command, Name: "update", Short: []string{"up"}, Help: "writes the buffer if it is modified", ID: update},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call shows, steps to or closes a buffer, or completes a buffer's name.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, _ ...int) (api.Result, error) {
	if _, ok := s.About["complete"]; ok {
		return api.Result{Complete: names(s)}, nil
	}
	arg := strings.TrimSpace(s.About["args"])
	current, _ := strconv.Atoi(s.About["buffer"])
	switch id {
	case next, prev:
		if b, ok := step(s, current, id == next); ok {
			return api.Result{Jump: &api.Jump{Buffer: b}}, nil
		}
		return api.Result{}, nil
	case empty:
		return api.Result{Scratch: &api.Scratch{}}, nil
	case update:
		if !s.Buffers[current].Modified {
			return api.Result{}, nil
		}
		// :w! writes over a file changed on disk, which Result.Write does not.
		write := "write"
		if s.About["bang"] == "true" {
			write += "!"
		}
		return api.Result{Ex: []string{write}}, nil
	}
	b := current
	if arg != "" || id == show {
		var msg string
		if b, msg = find(s, arg); b == 0 {
			return api.Result{Message: msg}, nil
		}
	}
	switch {
	case id == show:
		return api.Result{Jump: &api.Jump{Buffer: b}}, nil
	case s.About["bang"] == "true":
		return api.Result{Drop: []int{b}}, nil
	}
	return api.Result{Close: []int{b}}, nil
}

// step returns the buffer being edited after the current one, or before
// it, wrapping at either end. It passes help, lists, listings, terminals,
// the windows fude writes, and those the session may not show. From a
// buffer not among them, the next is the first and the one before the
// last (B89).
func step(s api.Scope, current int, forward bool) (int, bool) {
	var edited []int
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		if b := s.Buffers[id]; b.Kind == "" && !b.Barred {
			edited = append(edited, id)
		}
	}
	if len(edited) == 0 {
		return 0, false
	}
	at := slices.Index(edited, current)
	n := 1
	if !forward {
		n = -1
	}
	switch {
	case at >= 0:
		at = ((at+n)%len(edited) + len(edited)) % len(edited)
	case forward:
		at = 0
	default:
		at = len(edited) - 1
	}
	return edited[at], true
}

// find returns the buffer arg names: its number, # for the one the current
// window showed before, or text its name holds. It says why for none.
func find(s api.Scope, arg string) (int, string) {
	// A completion names a buffer by its number, then its path.
	if n, _, ok := strings.Cut(arg, " "); ok {
		if _, err := strconv.Atoi(n); err == nil {
			arg = n
		}
	}
	if arg == "#" {
		for _, w := range s.Windows() {
			if w.Current && w.Alt != 0 {
				return w.Alt, ""
			}
		}
		return 0, "no alternate buffer"
	}
	found := 0
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		switch {
		case strconv.Itoa(id) == arg:
			return id, ""
		case arg != "" && strings.Contains(s.Buffers[id].Name, arg):
			if found != 0 {
				return 0, "more than one buffer matches " + arg
			}
			found = id
		}
	}
	if found == 0 {
		return 0, "no buffer matches " + arg
	}
	return found, ""
}

// names are what a buffer's name completes with: each buffer with a file,
// by its number and its path.
func names(s api.Scope) []string {
	var out []string
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		if b := s.Buffers[id]; b.Path != "" {
			out = append(out, strconv.Itoa(id)+" "+short(b.Path, s.About["cwd"]))
		}
	}
	return out
}

// short is path as a buffer's completion shows it: under the working
// directory relative to it, under the home directory with ~, or else whole.
func short(path, wd string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, abs); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.Join("~", rel)
		}
	}
	return abs
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
