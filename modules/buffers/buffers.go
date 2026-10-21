// Package buffers shows, steps through, writes and closes buffers:
// :buffer, :bnext, :bprevious, :update, :bdelete, :enew, and [b, ]b, [B
// and ]B (B89, B155, B428).
package buffers

import (
	"context"
	"maps"
	"math"
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
	nextKey
	prevKey
	first
	last
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
		{Kind: api.Plug, Name: "next", Keys: "]b", Help: "shows the next buffer, n buffers on with a count", ID: nextKey},
		{Kind: api.Plug, Name: "prev", Keys: "[b", Help: "shows the buffer before, n buffers back with a count", ID: prevKey},
		{Kind: api.Plug, Name: "first", Keys: "[B", Help: "shows the first buffer", ID: first},
		{Kind: api.Plug, Name: "last", Keys: "]B", Help: "shows the last buffer", ID: last},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call shows, steps to or closes a buffer, or completes a buffer's name.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	if _, ok := s.About["complete"]; ok {
		return api.Result{Complete: names(s)}, nil
	}
	arg := strings.TrimSpace(s.About["args"])
	current, _ := strconv.Atoi(s.About["buffer"])
	// A count steps that many buffers, and [B and ]B as far as they go.
	n := max(args[1], 1)
	switch id {
	case prev, prevKey:
		n = -n
	case first:
		n = math.MinInt
	case last:
		n = math.MaxInt
	}
	switch id {
	case next, prev, nextKey, prevKey, first, last:
		if b, ok := step(s, current, n); ok {
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
	if arg == "" && id == show {
		return api.Result{Error: "which buffer? :buffer n"}, nil
	}
	b := current
	if arg != "" || id == show {
		var msg string
		if b, msg = find(s, arg); b == 0 {
			return api.Result{Error: msg}, nil
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

// step returns the buffer being edited n after the current one, or before
// it for a negative n, wrapping at either end, or the first or last for
// the least or largest n. It passes help, lists, explorers, terminals, the
// windows fude writes, and those the session may not show. From a buffer
// not among them, the next is the first and the one before the last (B89,
// B428).
func step(s api.Scope, current, n int) (int, bool) {
	var edited []int
	for _, id := range slices.Sorted(maps.Keys(s.Buffers)) {
		if b := s.Buffers[id]; b.Kind == "" && !b.Barred && !b.Unnumbered {
			edited = append(edited, id)
		}
	}
	if len(edited) == 0 {
		return 0, false
	}
	at := slices.Index(edited, current)
	switch {
	case n == math.MinInt:
		at = 0
	case n == math.MaxInt:
		at = len(edited) - 1
	case at >= 0:
		at = ((at+n)%len(edited) + len(edited)) % len(edited)
	case n > 0:
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
		case s.Buffers[id].Unnumbered:
			// A buffer with no number is no buffer :b names (B373).
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
		if b := s.Buffers[id]; b.Path != "" && !b.Unnumbered {
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
