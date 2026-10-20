// Package timeline is what lists, moves through and restores the states of
// a buffer's text: :timeline and its window's keys, :earlier, :later,
// :restore, :diff and :recover (B31, B32, B34, B35).
package timeline

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/api/diff"
)

// What timeline registers, by the ids Call runs them by.
const (
	list uint32 = iota + 1
	earlierCmd
	laterCmd
	restoreCmd
	diffCmd
	recoverCmd
	earlierKey
	laterKey
	diffKey
	restoreKey
	relist
)

// title is the timeline window's.
const title = "[timeline]"

// keys are the timeline window's own keys, which [keymap.timeline] changes
// (B142).
var keys = map[string]string{
	"[": "<Plug>(timeline.earlier)", "]": "<Plug>(timeline.later)", "d": "<Plug>(timeline.diff)", "<CR>": "<Plug>(timeline.restore)",
	"q": ":close<CR>",
}

// Module is timeline. It keeps the buffer the timeline window lists.
type Module struct {
	mu  sync.Mutex
	src int
}

// New returns timeline.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "timeline" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Command, Name: "timeline", Help: "lists every state of the buffer's text", ID: list},
		{Kind: api.Command, Name: "earlier", Help: "goes n states back. n may be a time such as 5m, or a count of writes such as 1f", ID: earlierCmd},
		{Kind: api.Command, Name: "later", Help: "goes n states forward. n may be a time such as 5m, or a count of writes such as 1f", ID: laterCmd},
		{Kind: api.Command, Name: "restore", Help: "makes the text of state n the newest. In a timeline, n is the state under the cursor", ID: restoreCmd},
		{Kind: api.Command, Name: "diff", Help: "shows the lines that differ between states n and m, or n and now", ID: diffCmd},
		{Kind: api.Command, Name: "recover", Help: "goes to the newest unsaved change from a session that did not close", ID: recoverCmd},
		{Kind: api.Plug, Name: "earlier", Help: "moves the text a state back", ID: earlierKey},
		{Kind: api.Plug, Name: "later", Help: "moves the text a state forward", ID: laterKey},
		{Kind: api.Plug, Name: "diff", Help: "shows how the state under the cursor differs from now", ID: diffKey},
		{Kind: api.Plug, Name: "restore", Help: "makes the state under the cursor the newest", ID: restoreKey},
		{Kind: api.Plug, Name: "list", Help: "lists the timeline again, with the cursor on the state the text is at", ID: relist},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs a command or a key of the timeline window.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	arg := strings.TrimSpace(s.About["args"])
	b := m.source(s)
	switch id {
	case list:
		m.src = b
		return api.Result{Show: m.panel(s.States(b))}, nil
	case relist:
		states := s.States(m.src)
		at := slices.IndexFunc(states, func(st api.State) bool { return st.Current })
		return api.Result{Show: m.panel(states), Jump: &api.Jump{Line: at + 1}}, nil
	case earlierCmd, laterCmd:
		return travel(s, b, arg, id == laterCmd), nil
	case recoverCmd:
		if i := slices.IndexFunc(s.States(b), func(st api.State) bool { return st.Unsaved }); i >= 0 {
			return api.Result{Travel: &api.Travel{Buffer: b, To: i}}, nil
		}
		return api.Result{Error: "nothing to recover"}, nil
	}
	if id == restoreKey || id == diffKey || id == earlierKey || id == laterKey {
		arg = ""
	}
	i, err := state(s, b, arg, args[0])
	if err != nil {
		return api.Result{Error: err.Error()}, nil
	}
	switch id {
	case restoreCmd:
		return api.Result{Travel: &api.Travel{Buffer: b, To: i, New: true}}, nil
	case restoreKey:
		return api.Result{Travel: &api.Travel{Buffer: b, To: i, New: true}, Keys: "<Plug>(timeline.list)"}, nil
	case earlierKey, laterKey:
		j := i - 1
		if id == laterKey {
			j = i + 1
		}
		if j < 0 || j >= len(s.States(b)) {
			return api.Result{}, nil
		}
		return api.Result{Travel: &api.Travel{Buffer: b, To: j}, Keys: "<Plug>(timeline.list)"}, nil
	}
	// diff: state i against the one the text is at, or the state after it.
	states := s.States(b)
	j := slices.IndexFunc(states, func(st api.State) bool { return st.Current })
	if _, to, ok := strings.Cut(arg, " "); ok && id == diffCmd {
		if j, err = state(s, b, strings.TrimSpace(to), args[0]); err != nil {
			return api.Result{Error: err.Error()}, nil
		}
	}
	x, _ := s.StateText(b, i)
	y, _ := s.StateText(b, j)
	text := diff.Unified(diff.Lines(strings.Split(x, "\n"), strings.Split(y, "\n")), 3)
	return api.Result{Show: &api.Panel{Title: fmt.Sprintf("[diff %d %d]", i, j), Text: text, Spans: colours(text)}}, nil
}

// source is the buffer a call is about, or the one the timeline lists for
// a call from a window fude wrote, such as the timeline's or a diff's.
func (m *Module) source(s api.Scope) int {
	if s.About["title"] != "" && m.src != 0 {
		return m.src
	}
	b, _ := strconv.Atoi(s.About["buffer"])
	return b
}

// state resolves a state's number from arg, or from the line under the
// cursor in the timeline window when arg is "".
func state(s api.Scope, b int, arg string, cursor int) (int, error) {
	first, _, _ := strings.Cut(arg, " ")
	if first == "" {
		if s.About["title"] != title {
			return 0, errors.New("not in a timeline")
		}
		return s.Text.LineOf(cursor), nil
	}
	i, err := strconv.Atoi(first)
	if err != nil || i < 0 || i >= len(s.States(b)) {
		return 0, fmt.Errorf("no such state: %s", first)
	}
	return i, nil
}

// panel lists every state of the text, one a line with its number, time
// and the command that made it. * marks the state the text is at, and
// (from N) a state that does not follow the one before it.
func (m *Module) panel(states []api.State) *api.Panel {
	lines := make([]string, len(states))
	for i, st := range states {
		mark := " "
		if st.Current {
			mark = "*"
		}
		if i == 0 {
			lines[i] = mark + " 0            the text before any change"
			continue
		}
		lines[i] = fmt.Sprintf("%s %d  %s  %s", mark, i, st.Time.Format("15:04:05"), st.Label)
		if st.Parent != i-1 {
			lines[i] += fmt.Sprintf("  (from %d)", st.Parent)
		}
	}
	return &api.Panel{Title: title, Text: strings.Join(lines, "\n"), Keys: keys}
}

// travel runs :earlier or :later. The argument is a count of states, a time
// such as 10s, 5m, 2h or 1d, or a count of writes such as 1f.
func travel(s api.Scope, b int, arg string, later bool) api.Result {
	states := s.States(b)
	cur := slices.IndexFunc(states, func(st api.State) bool { return st.Current })
	if arg == "" {
		arg = "1"
	}
	unit := arg[len(arg)-1]
	n, err := strconv.Atoi(strings.TrimRight(arg, "smhdf"))
	if err != nil || n < 0 {
		return api.Result{Error: "bad count: " + arg}
	}
	target := cur
	switch unit {
	case 's', 'm', 'h', 'd':
		d := time.Duration(n) * map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[unit]
		when := states[cur].Time
		if later {
			// The first state made at least d after this one, or the newest.
			target = len(states) - 1
			for i := cur + 1; i < len(states); i++ {
				if !states[i].Time.Before(when.Add(d)) {
					target = i
					break
				}
			}
		} else {
			// The last state made at least d before this one, or the first.
			target = 0
			for i := 1; i <= cur; i++ {
				if !states[i].Time.After(when.Add(-d)) {
					target = i
				}
			}
		}
	case 'f':
		target = writes(states, cur, n, later)
	default:
		target = cur - n
		if later {
			target = cur + n
		}
	}
	return api.Result{Travel: &api.Travel{Buffer: b, To: max(0, min(target, len(states)-1))}}
}

// writes returns the state n writes back from cur, or forward when later
// is set. Going back from changes made since the last write counts that
// write as the first. Past the newest write it is the newest state.
func writes(states []api.State, cur, n int, later bool) int {
	var saved []int
	for i, st := range states {
		if st.Saved {
			saved = append(saved, i)
		}
	}
	if later {
		i, _ := slices.BinarySearch(saved, cur+1)
		if i+n-1 < len(saved) {
			return saved[i+n-1]
		}
		return len(states) - 1
	}
	i, _ := slices.BinarySearch(saved, cur)
	if i-n < 0 {
		return 0
	}
	return saved[i-n]
}

// colours colours a diff's lines: a removed line in the diff.minus style,
// an added one in diff.plus and a hunk's header dim (B222).
func colours(text string) []api.Span {
	var spans []api.Span
	at := 0
	for line := range strings.Lines(text) {
		body := strings.TrimSuffix(line, "\n")
		style := ""
		switch {
		case strings.HasPrefix(body, "@@"):
			style = "comment"
		case strings.HasPrefix(body, "-"):
			style = "diff.minus"
		case strings.HasPrefix(body, "+"):
			style = "diff.plus"
		}
		if style != "" && body != "" {
			spans = append(spans, api.Span{Start: at, End: at + len(body), Name: style})
		}
		at += len(line)
	}
	return spans
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
