// Package hints shows the keys that may follow the command being typed,
// once it waits key_hints milliseconds for its next key, as which-key.nvim
// does. They stay while the command goes on, a page at a time (B172).
package hints

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
)

// width is the fewest columns a key and what it does take. Cells widen to
// fill the width the windows have.
const width = 34

// Module is hints. It keeps the command being typed and whether its hints
// show.
type Module struct {
	mu      sync.Mutex
	pending string
	showing bool
	timer   uint32 // the newest timer, which shows the hints when it ends
}

// New returns hints.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "hints" }

func (m *Module) Subscribed() []string { return []string{"keys.pending"} }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Option, Name: "enabled", Default: "true", Help: "the keys that may follow a command being typed show in a panel"},
		{Kind: api.Option, Name: "delay", Default: "300", Help: "the milliseconds a command being typed waits before the keys that may follow it show"},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

func (m *Module) Call(context.Context, uint32, api.Scope, ...int) (api.Result, error) {
	return api.Result{}, nil
}

// Event waits hints.delay for a command being typed, or shows the hints of
// one that goes on while they show.
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, m.pending, _ = strings.Cut(name, " ")
	switch {
	case m.pending == "":
		m.showing = false
		return api.Result{}, nil
	case m.showing:
		return m.show(s), nil
	}
	if on, _ := s.Option("hints.enabled"); on != "true" {
		return api.Result{}, nil
	}
	ms, _ := s.Option("hints.delay")
	wait, _ := strconv.Atoi(ms)
	m.timer++
	return api.Result{Timers: []api.Timer{{ID: m.timer, After: time.Duration(wait) * time.Millisecond}}}, nil
}

// Callback shows the hints once the newest wait ends, while the command it
// waited for is still being typed.
func (m *Module) Callback(_ context.Context, id uint32, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != m.timer || m.pending == "" || s.About["pending"] != m.pending {
		return api.Result{}, nil
	}
	return m.show(s), nil
}

// show lays the hints out in columns, groups first, in the windows' width,
// a page at a time of no more rows than a third of the screen. A form feed
// starts each page after the first. A command with no hints shows none.
func (m *Module) show(s api.Scope) api.Result {
	hints := s.Hints()
	if m.showing = len(hints) > 0; !m.showing {
		return api.Result{Float: &api.Float{Keys: true}}
	}
	area, _ := strconv.Atoi(s.About["area"])
	lines, _ := strconv.Atoi(s.About["lines"])
	inner := max(area-4, width)
	cols := max((inner+2)/(width+2), 1)
	cellW := (inner+2)/cols - 2
	group := func(h api.Hint) bool { return h.More > 0 && h.Help == "" }
	slices.SortStableFunc(hints, func(a, b api.Hint) int {
		switch {
		case group(a) && !group(b):
			return -1
		case group(b) && !group(a):
			return 1
		}
		return 0
	})
	rows := min((len(hints)+cols-1)/cols, max(lines/3, 2))
	keyW := 1
	for _, h := range hints {
		keyW = max(keyW, utf8.RuneCountInString(h.Key))
	}
	keyW = min(keyW, cellW/3)
	var b strings.Builder
	var spans []api.Span
	for first := 0; first < len(hints); first += rows * cols {
		if first > 0 {
			b.WriteString("\f")
		}
		page := hints[first:min(first+rows*cols, len(hints))]
		// Every page has the first's rows, so the panel keeps its height.
		for r := range rows {
			if r > 0 {
				b.WriteString("\n")
			}
			for c := range cols {
				i := c*rows + r
				if i >= len(page) {
					break
				}
				if c > 0 {
					b.WriteString("  ")
				}
				h := page[i]
				cell := append([]piece{{fmt.Sprintf("%-*s", keyW, h.Key), "hints.key"}, {"  ", ""}}, pieces(h)...)
				// A cell longer than its width ends in an ellipsis.
				room := cellW
				for _, p := range cell {
					text := []rune(p.text)
					if len(text) > room {
						text = append(text[:max(room-1, 0)], '…')
					}
					if len(text) == 0 {
						break
					}
					start := b.Len()
					b.WriteString(string(text))
					if p.style != "" {
						spans = append(spans, api.Span{Start: start, End: b.Len(), Name: p.style})
					}
					if room -= len(text); room <= 0 {
						break
					}
				}
				if c < cols-1 {
					b.WriteString(strings.Repeat(" ", max(room, 0)))
				}
			}
		}
	}
	return api.Result{Float: &api.Float{Text: strings.TrimRight(b.String(), " "), Spans: spans, Keys: true}}
}

// piece is text of one style in a cell.
type piece struct{ text, style string }

// pieces are what a hint's cell shows after its key: a group's name and
// count, a key's help, or the keys a mapping types.
func pieces(h api.Hint) []piece {
	switch {
	case h.Typed:
		return []piece{{h.Help, "hints.typed"}}
	case h.More > 0 && h.Help == "":
		return []piece{{"+" + cmp.Or(h.Group, "keys"), "hints.group"}, {fmt.Sprintf(" %d", h.More), "hints.muted"}}
	case h.More > 0:
		return []piece{{h.Help, ""}, {fmt.Sprintf(" +%d", h.More), "hints.muted"}}
	}
	return []piece{{h.Help, ""}}
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
