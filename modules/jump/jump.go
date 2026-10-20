// Package jump moves the cursor to any text in view in a few keys. s and
// the characters to find label each match, nearest the cursor first, and
// typing a label goes to its match (B218).
package jump

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
)

// labels are the labels a jump's matches get, nearest first.
const labels = "asdfghjklqwertyuiopzxcvbnm"

// Module is jump. It keeps the jump being typed.
type Module struct {
	mu      sync.Mutex
	typing  bool
	query   string
	matches [][2]int          // the matches in view, nearest the cursor first
	labeled map[string][2]int // the match each label goes to
	shown   []int             // the ids of the decorations that show them
	ids     int
}

// New returns jump.
func New() *Module { return &Module{} }

func (m *Module) Name() string { return "jump" }

func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{{Kind: api.Plug, Name: "start", Help: "jumps to a match of the characters typed next, by its label", ID: 1}}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call starts a jump, then takes each key typed. A label goes to its
// match, <CR> to the nearest, <BS> drops the last character typed and
// <Esc> cancels. Any other character adds to the text to find.
func (m *Module) Call(_ context.Context, _ uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := s.About["key"]
	if !ok {
		m.typing, m.query = true, ""
		return m.find(s, args[0]), nil
	}
	if !m.typing {
		return api.Result{}, nil
	}
	switch {
	case k == "<Esc>":
		return m.end(nil), nil
	case k == "<CR>":
		if len(m.matches) > 0 {
			return m.end(&m.matches[0]), nil
		}
		return m.end(nil), nil
	case k == "<BS>":
		if m.query == "" {
			return m.end(nil), nil
		}
		_, size := utf8.DecodeLastRuneInString(m.query)
		m.query = m.query[:len(m.query)-size]
	case strings.HasPrefix(k, "<") && k != "<":
		return api.Result{Capture: true}, nil
	default:
		if at, ok := m.labeled[k]; ok {
			return m.end(&at), nil
		}
		m.query += k
	}
	return m.find(s, args[0]), nil
}

// find finds the text typed in what the current window shows, ignoring case
// unless it has a capital, and labels the matches nearest the cursor
// first. A label is never the character after a match, so typing more of
// the text never jumps.
func (m *Module) find(s api.Scope, cursor int) api.Result {
	m.matches, m.labeled = nil, map[string][2]int{}
	res := api.Result{Capture: true, Undecorate: m.shown, Status: new("s" + m.query)}
	m.shown = nil
	if m.query == "" {
		return res
	}
	t := s.Text
	top, _ := strconv.Atoi(s.About["top"])
	bottom, _ := strconv.Atoi(s.About["bottom"])
	hay, q := t.Slice(top, bottom), m.query
	if q == strings.ToLower(q) {
		hay = strings.ToLower(hay)
	}
	next := map[string]bool{}
	for at := 0; ; {
		i := strings.Index(hay[at:], q)
		if i < 0 {
			break
		}
		start := top + at + i
		m.matches = append(m.matches, [2]int{start, start + len(q)})
		if r, _ := utf8.DecodeRuneInString(hay[at+i+len(q):]); r != utf8.RuneError {
			next[strings.ToLower(string(r))] = true
		}
		at += i + 1
	}
	dist := func(a [2]int) int { return max(a[0]-cursor, cursor-a[0]) }
	slices.SortStableFunc(m.matches, func(a, b [2]int) int { return cmp.Compare(dist(a), dist(b)) })
	free := []rune(labels)
	for _, at := range m.matches {
		res.Decorate = append(res.Decorate, m.decoration(api.Decoration{Start: at[0], End: at[1], Style: "jump.match"}))
		for len(free) > 0 && next[string(free[0])] {
			free = free[1:]
		}
		if len(free) == 0 {
			continue
		}
		l := string(free[0])
		free = free[1:]
		m.labeled[l] = at
		// The label covers the character after the match, or shows after
		// the line's end.
		end := at[1]
		if r, size := utf8.DecodeRuneInString(t.Slice(end, min(end+utf8.UTFMax, t.Len()))); r != '\n' && r != utf8.RuneError {
			end += size
		}
		res.Decorate = append(res.Decorate, m.decoration(api.Decoration{Start: at[1], End: end, Note: l, Style: "jump.label", Cover: true}))
	}
	return res
}

// decoration gives d an id of its own and keeps it to take off later.
func (m *Module) decoration(d api.Decoration) api.Decoration {
	m.ids++
	d.ID = m.ids
	m.shown = append(m.shown, m.ids)
	return d
}

// end ends the jump. With at set, the cursor goes to that match's start,
// and without it the cursor stays.
func (m *Module) end(at *[2]int) api.Result {
	res := api.Result{Undecorate: m.shown, Status: new("")}
	if at != nil {
		res.Jump = &api.Jump{Pos: at[0]}
	}
	m.typing, m.query, m.matches, m.labeled, m.shown = false, "", nil, nil, nil
	return res
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }

func (m *Module) Close(context.Context) error { return nil }
