package fold

import (
	"slices"
	"strconv"

	"github.com/y3owk1n/fude/api"
)

// shape is where a buffer's folds may be: by indent, or by the folds its
// grammar's folds query captures when it has one (B224, B591).
type shape struct {
	t       api.Text
	tab     int
	byQuery bool
	query   []span // the folds the query captures, by their first lines
}

// shapeOf returns where the scope's buffer may fold.
func shapeOf(s api.Scope) (shape, error) {
	tab, _ := strconv.Atoi(s.About["tab"])
	sh := shape{t: s.Text, tab: tab}
	if s.Folds == nil {
		return sh, nil
	}
	spans, ok, err := s.Folds()
	if !ok || err != nil {
		return sh, err
	}
	sh.byQuery = true
	for _, sp := range spans {
		first, last := s.Text.LineOf(sp.Start), s.Text.LineOf(max(sp.End-1, sp.Start))
		if last > first {
			sh.query = append(sh.query, span{first: first, last: last})
		}
	}
	return sh, nil
}

// end returns the last line of the fold line starts, the longest when more
// start there, or false when none does.
func (sh shape) end(line int) (int, bool) {
	if !sh.byQuery {
		return foldEnd(sh.t, line, sh.tab)
	}
	last := line
	for _, sp := range sh.query {
		if sp.first == line {
			last = max(last, sp.last)
		}
	}
	return last, last > line
}

// all returns every fold, by first line.
func (sh shape) all() []span {
	if sh.byQuery {
		return sh.query
	}
	var out []span
	for l := 0; l < sh.t.Lines(); l++ {
		if last, ok := foldEnd(sh.t, l, sh.tab); ok {
			out = append(out, span{first: l, last: last})
		}
	}
	return out
}

// holding returns the folds that hold line, whether closed or not.
func (sh shape) holding(line int) []span {
	if !sh.byQuery {
		return indentHolding(sh.t, line, sh.tab)
	}
	var out []span
	for _, sp := range sh.query {
		if sp.first <= line && line <= sp.last {
			out = append(out, sp)
		}
	}
	return out
}

// ends reports whether a fold ends on line.
func (sh shape) ends(line int) bool {
	if !sh.byQuery {
		return endsFold(sh.t, line, sh.tab)
	}
	return slices.ContainsFunc(sh.query, func(sp span) bool { return sp.last == line })
}

// depth returns how deep a fold that starts on a line would be: 1 in no
// fold, and one more for each fold the line is inside.
func (sh shape) depth() func(line int) int {
	if !sh.byQuery {
		d := depths(sh.t, sh.tab)
		return func(line int) int { return d[line] }
	}
	return func(line int) int {
		n := 1
		for _, sp := range sh.query {
			if sp.first < line && line <= sp.last {
				n++
			}
		}
		return n
	}
}

// mark puts in marks, by line from first to last, open on a line that
// starts a fold and sep on another line a fold holds (B488).
func (sh shape) mark(first, last int, open, sep string, marks map[int]string) {
	if !sh.byQuery {
		mark(sh.t, first, last, sh.tab, open, sep, marks)
		return
	}
	for l := first; l <= last; l++ {
		for _, sp := range sh.query {
			if sp.first == l {
				marks[l] = open
				break
			}
			if sp.first < l && l <= sp.last {
				marks[l] = sep
			}
		}
	}
}
