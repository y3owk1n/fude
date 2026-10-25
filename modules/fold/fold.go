// Package fold folds a buffer by indent, or as its grammar's folds query
// says, through zc, zC, zo, zO, za, zA, zv, zR, zM, zm, zr and zi, folds the
// lines zf covers, moves between folds with zj and zk, and cycles folds
// (B224, B502, B591, B592, N34).
package fold

import (
	"cmp"
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
	create
	nextFold
	prevFold
	toggleAll
	view
	more
	less
	enable
	openDeep
	closeDeep
	cycle
	cycleAll
)

// Module is fold.
type Module struct {
	mu    sync.Mutex
	ids   int
	signs map[string][]int // by buffer, the ids of the fold column's signs (B488)
}

// New returns fold.
func New() *Module { return &Module{signs: map[string][]int{}} }

func (m *Module) Name() string { return "fold" }
func (m *Module) Subscribed() []string {
	return []string{"buffer.opened", "buffer.changed", "buffer.closed", "window.moved", "window.opened", "option.set", "config.reloaded"}
}

func (m *Module) Registered() []api.Registration {
	return []api.Registration{
		{Kind: api.Plug, Name: "toggle", Keys: "za", Help: "opens the closed fold at the cursor as zo does, or closes one as zc does", ID: toggle},
		{Kind: api.Plug, Name: "close", Keys: "zc", Help: "closes one level of the folds at the cursor, or n levels", ID: closeOne},
		{Kind: api.Plug, Name: "close_deep", Keys: "zC", Help: "closes every fold at the cursor", ID: closeDeep},
		{Kind: api.Plug, Name: "open", Keys: "zo", Help: "opens one level of the closed folds at the cursor, or n levels", ID: open},
		{Kind: api.Plug, Name: "open_deep", Keys: "zO", Help: "opens the closed fold at the cursor and every fold in it", ID: openDeep},
		{Kind: api.Plug, Name: "open_all", Keys: "zR", Help: "opens every fold", ID: openAll},
		{Kind: api.Plug, Name: "close_all", Keys: "zM", Help: "closes every fold", ID: closeAll},
		{Kind: api.Operator, Name: "zf", Help: "closes a fold over the lines a motion or object covers", ID: create},
		{Kind: api.Plug, Name: "next", Keys: "zj", Help: "goes to the start of the n-th fold below", ID: nextFold},
		{Kind: api.Plug, Name: "prev", Keys: "zk", Help: "goes to the end of the n-th fold above", ID: prevFold},
		{Kind: api.Plug, Name: "toggle_all", Keys: "zA", Help: "opens the closed fold at the cursor as zO does, or closes every fold at the cursor", ID: toggleAll},
		{Kind: api.Plug, Name: "view", Keys: "zv", Help: "opens the folds that hide the cursor's line", ID: view},
		{Kind: api.Plug, Name: "more", Keys: "zm", Help: "folds one level more, or n levels", ID: more},
		{Kind: api.Plug, Name: "less", Keys: "zr", Help: "folds one level less, or n levels", ID: less},
		{Kind: api.Plug, Name: "enable", Keys: "zi", Help: "opens every closed fold, or closes again those it opened", ID: enable},
		{Kind: api.Plug, Name: "cycle", Help: "on a fold's first line, opens it with the folds inside it closed, then every fold inside it, then closes it", ID: cycle},
		{Kind: api.Plug, Name: "cycle_all", Help: "closes the outermost folds, then the folds that hold no other, then opens every fold", ID: cycleAll},
		{Kind: api.Option, Name: "column", Default: "false", Help: "the sign column shows where folds start and the lines they hold"},
	}
}

func (m *Module) Init(context.Context, api.Scope) (api.Result, error) { return api.Result{}, nil }

// Call runs a fold key at the cursor. The folds at the cursor are those
// that hold its line, outermost first. zo opens the first closed one, zc
// closes the last open one before it, and za does one or the other. zO
// opens the first closed one and every fold in it, zC closes them all,
// and zA does one or the other. zR opens every fold, and zM closes them.
func (m *Module) Call(_ context.Context, id uint32, s api.Scope, args ...int) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := s.Text
	line := t.LineOf(args[0])
	sh, err := shapeOf(s)
	if err != nil {
		return api.Result{Error: err.Error()}, nil
	}
	folds := closed(s)
	var res api.Result
	switch id {
	case nextFold, prevFold:
		to, ok := moveTo(sh, folds, line, max(args[1], 1), id == nextFold)
		if !ok {
			return api.Result{Error: "no fold there"}, nil
		}
		return api.Result{Jump: &api.Jump{Pos: t.LineStart(to)}}, nil
	case create:
		if last := t.LineOf(args[1]); last > line {
			res = opened(t, folds, line)
			res.Undecorate = append(res.Undecorate, opened(t, folds, last).Undecorate...)
			res.Decorate = []api.Decoration{m.fold(t, line, last)}
		}
	case view:
		res = opened(t, folds, line)
	case open, closeOne, toggle, openDeep, closeDeep, toggleAll:
		res = m.levels(t, folds, spans(sh, folds, line), id, max(args[1], 1))
	case more, less:
		n := max(args[1], 1)
		if id == more {
			n = -n
		}
		res = m.level(sh, folds, n)
	case enable:
		res = m.enable(s, folds)
	case openAll:
		for _, f := range folds {
			res.Undecorate = append(res.Undecorate, f.ID)
		}
	case closeAll:
		for _, sp := range sh.all() {
			if !isClosed(t, folds, sp) {
				res.Decorate = append(res.Decorate, m.fold(t, sp.first, sp.last))
			}
		}
	case cycle:
		res = m.cycle(sh, folds, line)
	case cycleAll:
		res = m.cycleAll(sh, folds)
	}
	// The cursor lands on the first line of the fold it is in.
	if first := firstLine(t, folds, res, line); first != line {
		res.Jump = &api.Jump{Pos: t.LineStart(first)}
	}
	return m.column(s, sh, res), nil
}

// Event draws the fold column again on the buffer it is about, and
// forgets a buffer that closed (B488).
func (m *Module) Event(_ context.Context, name string, s api.Scope) (api.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch event, detail, _ := strings.Cut(name, " "); {
	case event == "buffer.closed":
		delete(m.signs, s.About["buffer"])
		return api.Result{}, nil
	case event == "option.set" && detail != "fold.column" && !strings.HasPrefix(detail, "fill_chars."):
		return api.Result{}, nil
	}
	// The folds query parses the buffer, so it runs only for the column.
	var sh shape
	if on, _ := s.Option("fold.column"); on == "true" {
		var err error
		if sh, err = shapeOf(s); err != nil {
			return api.Result{Error: err.Error()}, nil
		}
	}
	return m.column(s, sh, api.Result{}), nil
}

func (m *Module) Callback(context.Context, uint32, api.Scope) (api.Result, error) {
	return api.Result{}, nil
}

func (m *Module) Work(context.Context, api.Job) (string, error) { return "", nil }
func (m *Module) Close(context.Context) error                   { return nil }

// span is a fold that holds a line: its first and last lines, and its
// decoration when it is closed.
type span struct {
	first, last int
	closed      *api.Decoration
}

// spans returns the folds that hold line, from sh or closed, outermost
// first.
func spans(sh shape, folds []api.Decoration, line int) []span {
	t, out := sh.t, sh.holding(line)
	for _, f := range folds {
		first, last := t.LineOf(f.Start), t.LineOf(f.End)
		if line < first || line > last {
			continue
		}
		if i := slices.IndexFunc(out, func(sp span) bool { return sp.first == first && sp.last == last }); i >= 0 {
			out[i].closed = &f
		} else {
			out = append(out, span{first, last, &f})
		}
	}
	slices.SortFunc(out, func(a, b span) int { return cmp.Or(a.first-b.first, b.last-a.last) })
	return out
}

// indentHolding returns the folds by indent that hold line.
func indentHolding(t api.Text, line, tab int) []span {
	var out []span
	if last, ok := foldEnd(t, line, tab); ok {
		out = append(out, span{first: line, last: last})
	}
	ind, ok := indentCol(t, line, tab)
	for l := line - 1; l >= 0; l-- {
		if col, text := indentCol(t, l, tab); text && (!ok || col < ind) {
			if last, _ := foldEnd(t, l, tab); last >= line {
				out = append(out, span{first: l, last: last})
			}
			ind, ok = col, true
		}
	}
	return out
}

// levels opens or closes the folds at the cursor, path, as the key id says,
// n levels for zo, zc and za.
func (m *Module) levels(t api.Text, folds []api.Decoration, path []span, id uint32, n int) api.Result {
	if len(path) == 0 {
		return api.Result{Error: "no fold here"}
	}
	first := slices.IndexFunc(path, func(sp span) bool { return sp.closed != nil })
	switch {
	case id == toggle && first >= 0:
		id = open
	case id == toggle:
		id = closeOne
	case id == toggleAll && first >= 0:
		id = openDeep
	case id == toggleAll:
		id = closeDeep
	}
	var res api.Result
	switch id {
	case open:
		for _, sp := range path {
			if sp.closed != nil && n > 0 {
				res.Undecorate = append(res.Undecorate, sp.closed.ID)
				n--
			}
		}
	case openDeep:
		if first < 0 {
			break
		}
		for _, f := range folds {
			if l := t.LineOf(f.Start); l >= path[first].first && l <= path[first].last {
				res.Undecorate = append(res.Undecorate, f.ID)
			}
		}
	case closeDeep:
		for _, sp := range path {
			if sp.closed == nil {
				res.Decorate = append(res.Decorate, m.fold(t, sp.first, sp.last))
			}
		}
	case closeOne:
		// Each level closes the last open fold before the first closed one.
		for range n {
			if first = slices.IndexFunc(path, func(sp span) bool { return sp.closed != nil }); first < 0 {
				first = len(path)
			}
			if first == 0 {
				break
			}
			d := m.fold(t, path[first-1].first, path[first-1].last)
			path[first-1].closed = &d
			res.Decorate = append(res.Decorate, d)
		}
	}
	return res
}

// level folds n levels less, or more for a negative n, from the level the
// closed folds are at. Folds deeper than the level close and the rest
// open. With no fold closed the level is the deepest, as after zR.
func (m *Module) level(sh shape, folds []api.Decoration, n int) api.Result {
	t, depth, all := sh.t, sh.depth(), sh.all()
	deepest := 0
	for _, sp := range all {
		deepest = max(deepest, depth(sp.first))
	}
	at := deepest
	for _, f := range folds {
		at = min(at, depth(t.LineOf(f.Start))-1)
	}
	to := min(max(at+n, 0), deepest)
	var res api.Result
	for _, f := range folds {
		res.Undecorate = append(res.Undecorate, f.ID)
	}
	for _, sp := range all {
		if depth(sp.first) > to {
			res.Decorate = append(res.Decorate, m.fold(t, sp.first, sp.last))
		}
	}
	return res
}

// cycle steps the fold that starts on line from closed, to open with the
// folds inside it closed, to open with every fold inside it open, and back
// to closed. Off a fold's first line it does nothing (B592).
func (m *Module) cycle(sh shape, folds []api.Decoration, line int) api.Result {
	t := sh.t
	last, ok := sh.end(line)
	var shut *api.Decoration
	for _, f := range folds {
		if t.LineOf(f.Start) == line {
			shut, last, ok = &f, max(last, t.LineOf(f.End)), true
		}
	}
	if !ok {
		return api.Result{}
	}
	inside := func(first, end int) bool { return first > line && end <= last }
	var res api.Result
	switch {
	case shut != nil:
		res.Undecorate = append(res.Undecorate, shut.ID)
		for _, sp := range sh.all() {
			if inside(sp.first, sp.last) && !isClosed(t, folds, sp) {
				res.Decorate = append(res.Decorate, m.fold(t, sp.first, sp.last))
			}
		}
	case slices.ContainsFunc(folds, func(f api.Decoration) bool { return inside(t.LineOf(f.Start), t.LineOf(f.End)) }):
		for _, f := range folds {
			if inside(t.LineOf(f.Start), t.LineOf(f.End)) {
				res.Undecorate = append(res.Undecorate, f.ID)
			}
		}
	default:
		res.Decorate = append(res.Decorate, m.fold(t, line, last))
	}
	return res
}

// cycleAll steps the buffer from every outermost fold closed, to every
// fold closed that holds no other, to every fold open, and back. It skips
// a step that would change nothing (B592).
func (m *Module) cycleAll(sh shape, folds []api.Decoration) api.Result {
	t := sh.t
	all := sh.all()
	within := func(a, b span) bool {
		return (a.first != b.first || a.last != b.last) && b.first <= a.first && a.last <= b.last
	}
	var outer, leaves []span
	for _, a := range all {
		if !slices.ContainsFunc(all, func(b span) bool { return within(a, b) }) {
			outer = append(outer, a)
		}
		if !slices.ContainsFunc(all, func(b span) bool { return within(b, a) }) {
			leaves = append(leaves, a)
		}
	}
	shut := func(sp span) bool { return isClosed(t, folds, sp) }
	same := func(step []span) bool {
		return len(step) == len(folds) && !slices.ContainsFunc(step, func(sp span) bool { return !shut(sp) })
	}
	steps := [][]span{outer, leaves, nil}
	at := slices.IndexFunc(steps, same)
	next := (at + 1) % len(steps)
	for next != at && same(steps[next]) {
		next = (next + 1) % len(steps)
	}
	var res api.Result
	for _, f := range folds {
		if !slices.ContainsFunc(steps[next], func(sp span) bool { return sp.first == t.LineOf(f.Start) && sp.last == t.LineOf(f.End) }) {
			res.Undecorate = append(res.Undecorate, f.ID)
		}
	}
	for _, sp := range steps[next] {
		if !shut(sp) {
			res.Decorate = append(res.Decorate, m.fold(t, sp.first, sp.last))
		}
	}
	return res
}

// isClosed reports whether one of folds closes sp.
func isClosed(t api.Text, folds []api.Decoration, sp span) bool {
	return slices.ContainsFunc(folds, func(f api.Decoration) bool {
		return t.LineOf(f.Start) == sp.first && t.LineOf(f.End) == sp.last
	})
}

// enable opens every closed fold, keeping where each was as a decoration
// that hides nothing, or closes those again when there are any, as zi
// turns folding off and on.
func (m *Module) enable(s api.Scope, folds []api.Decoration) api.Result {
	id, _ := strconv.Atoi(s.About["buffer"])
	var kept []api.Decoration
	for _, d := range s.Decorations() {
		if !d.Fold && d.Buffer == id {
			kept = append(kept, d)
		}
	}
	from, fold := folds, false
	if len(kept) > 0 {
		from, fold = kept, true
	}
	var res api.Result
	for _, d := range from {
		m.ids++
		res.Undecorate = append(res.Undecorate, d.ID)
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: d.Start, End: d.End, Fold: fold})
	}
	return res
}

// depths returns, by line, how deep a fold the line starts would be: 1
// with no line above it indented less, and one more for each line it is
// inside the fold of. Lines of blanks have none.
func depths(t api.Text, tab int) map[int]int {
	out := map[int]int{}
	var open []int // the indents of the lines whose folds hold the line
	for l := 0; l < t.Lines(); l++ {
		col, ok := indentCol(t, l, tab)
		if !ok {
			continue
		}
		for len(open) > 0 && open[len(open)-1] >= col {
			open = open[:len(open)-1]
		}
		out[l] = len(open) + 1
		open = append(open, col)
	}
	return out
}

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

// moveTo returns the line n folds below line, where one starts, or above
// it, where one ends. A closed fold counts as one line, which is its first,
// and false means there are fewer than n.
func moveTo(sh shape, folds []api.Decoration, line, n int, down bool) (int, bool) {
	t, step := sh.t, -1
	if down {
		step = 1
	}
	for l := line + step; l >= 0 && l < t.Lines(); l += step {
		first := firstOf(t, folds, l)
		switch {
		case first != l && down:
			continue
		case first != l:
			l = first
		case down && !startsFold(sh, folds, l), !down && !sh.ends(l):
			continue
		}
		if n--; n == 0 {
			return l, true
		}
	}
	return 0, false
}

// startsFold reports whether a fold starts on line, closed or in sh.
func startsFold(sh shape, folds []api.Decoration, line int) bool {
	for _, f := range folds {
		if sh.t.LineOf(f.Start) == line {
			return true
		}
	}
	_, ok := sh.end(line)
	return ok
}

// endsFold reports whether a fold by indent ends on line. One does when
// the next line with text, if any, is indented no more than the nearest
// line above that is indented less.
func endsFold(t api.Text, line, tab int) bool {
	ind, ok := indentCol(t, line, tab)
	if !ok {
		return false
	}
	after := -1
	for l := line + 1; l < t.Lines(); l++ {
		if col, ok := indentCol(t, l, tab); ok {
			after = col
			break
		}
	}
	for l := line - 1; l >= 0; l-- {
		if col, ok := indentCol(t, l, tab); ok && col < ind {
			return col >= after
		}
	}
	return false
}
