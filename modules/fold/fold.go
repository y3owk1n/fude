// Package fold folds a buffer by indent, through zc, zo, za, zR and zM
// (B224, N34).
package fold

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/api"
)

// The keys fold registers, by the ids Call runs them by.
const (
	toggle uint32 = iota + 1
	closeOne
	open
	openAll
	closeAll
)

// Module is fold.
type Module struct {
	mu  sync.Mutex
	ids int
}

// New returns fold.
func New() *Module { return &Module{} }

func (m *Module) Name() string         { return "fold" }
func (m *Module) Subscribed() []string { return nil }

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Key, Name: "za", Help: "opens the closed fold at the cursor, or closes the one it is in", ID: toggle},
		{Kind: api.Key, Name: "zc", Help: "closes the fold the cursor's line starts, or the one it is in", ID: closeOne},
		{Kind: api.Key, Name: "zo", Help: "opens the closed fold at the cursor", ID: open},
		{Kind: api.Key, Name: "zR", Help: "opens every fold", ID: openAll},
		{Kind: api.Key, Name: "zM", Help: "closes every outermost fold", ID: closeAll},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs a fold key at the cursor. zc closes the fold the cursor's
// line starts, or else the one it is in, zo opens the closed fold it is
// in, and za does one or the other. zR opens every fold, and zM closes
// every outermost one.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := s.Text
	line := t.LineOf(args[0])
	tab, _ := strconv.Atoi(s.About["tab"])
	folds := closed(s)
	var res api.Result
	switch id {
	case open:
		res = opened(t, folds, line)
	case toggle:
		if res = opened(t, folds, line); res.Undecorate == nil {
			res = m.closeAround(t, folds, line, tab)
		}
	case closeOne:
		res = m.closeAround(t, folds, line, tab)
	case openAll:
		for _, f := range folds {
			res.Undecorate = append(res.Undecorate, f.ID)
		}
	case closeAll:
		for l := 0; l < t.Lines(); l++ {
			if last, ok := foldEnd(t, l, tab); ok {
				res.Decorate = append(res.Decorate, m.fold(t, l, last))
				l = last
			}
		}
	}
	// The cursor lands on the first line of the fold it is in.
	if first := firstLine(t, folds, res, line); first != line {
		res.Jump = &api.Jump{Pos: t.LineStart(first)}
	}
	return res, nil
}

func (m *Module) Event(context.Context, string, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// fold returns the decoration that closes the fold from line first to line
// last.
func (m *Module) fold(t api.Text, first, last int) api.Decoration {
	m.ids++
	return api.Decoration{ID: m.ids, Start: t.LineStart(first), End: t.LineStart(last), Fold: true}
}

// closed returns the closed folds on the buffer.
func closed(s api.Scope) []api.Decoration {
	id, _ := strconv.Atoi(s.About["buffer"])
	var out []api.Decoration
	for _, d := range s.Decorations() {
		if d.Fold && d.Buffer == id {
			out = append(out, d)
		}
	}
	return out
}

// firstOf returns the first line of the outermost of folds that holds
// line, or line.
func firstOf(t api.Text, folds []api.Decoration, line int) int {
	first := line
	for _, f := range folds {
		if s, e := t.LineOf(f.Start), t.LineOf(f.End); e > s && line >= s && line <= e {
			first = min(first, s)
		}
	}
	return first
}

// opened returns what opens the closed folds that hold line, or nothing.
func opened(t api.Text, folds []api.Decoration, line int) api.Result {
	var res api.Result
	for _, f := range folds {
		if s, e := t.LineOf(f.Start), t.LineOf(f.End); line >= s && line <= e {
			res.Undecorate = append(res.Undecorate, f.ID)
		}
	}
	return res
}

// closeAround closes the fold line starts, or else the innermost one that
// holds it, which the nearest line above with less indent starts.
func (m *Module) closeAround(t api.Text, folds []api.Decoration, line, tab int) api.Result {
	if res, ok := m.closeAt(t, folds, line, tab); ok {
		return res
	}
	ind, ok := indentCol(t, line, tab)
	for l := line - 1; l >= 0; l-- {
		if col, text := indentCol(t, l, tab); text && (!ok || col < ind) {
			if res, ok := m.closeAt(t, folds, l, tab); ok {
				return res
			}
		}
	}
	return api.Result{Message: "no fold here"}
}

// closeAt closes the fold that starts on line, when there is one, opening
// the closed folds it holds first.
func (m *Module) closeAt(t api.Text, folds []api.Decoration, line, tab int) (api.Result, bool) {
	last, ok := foldEnd(t, line, tab)
	if !ok {
		return api.Result{}, false
	}
	var res api.Result
	for _, f := range folds {
		if s := t.LineOf(f.Start); s >= line && s <= last {
			res.Undecorate = append(res.Undecorate, f.ID)
		}
	}
	res.Decorate = []api.Decoration{m.fold(t, line, last)}
	return res, true
}

// firstLine returns the first line of the fold holding line once res
// applies, or line.
func firstLine(t api.Text, folds []api.Decoration, res api.Result, line int) int {
	var after []api.Decoration
	for _, f := range folds {
		if !slices.Contains(res.Undecorate, f.ID) {
			after = append(after, f)
		}
	}
	after = append(after, res.Decorate...)
	return firstOf(t, after, line)
}

// indentCol returns the column of line's first non-blank, or false for a
// line of blanks.
func indentCol(t api.Text, line, tab int) (int, bool) {
	ls := t.LineStart(line)
	le := t.Len()
	if line+1 < t.Lines() {
		le = t.LineStart(line+1) - 1
	}
	text := t.Slice(ls, le)
	rest := strings.TrimLeft(text, " \t")
	if rest == "" {
		return 0, false
	}
	col := 0
	for _, c := range text[:len(text)-len(rest)] {
		if c == '\t' {
			col += tab - col%tab
		} else {
			col++
		}
	}
	return col, true
}

// foldEnd returns the last line of the fold line starts. The fold holds
// the lines after it that are blank or indented more, without the blank
// ones at its end. It is false when no line after it is indented more.
func foldEnd(t api.Text, line, tab int) (int, bool) {
	ind, ok := indentCol(t, line, tab)
	if !ok {
		return 0, false
	}
	last := line
	for l := line + 1; l < t.Lines(); l++ {
		col, ok := indentCol(t, l, tab)
		if !ok {
			continue
		}
		if col <= ind {
			break
		}
		last = l
	}
	return last, last > line
}
