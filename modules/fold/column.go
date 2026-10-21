package fold

import (
	"cmp"
	"encoding/json"
	"strconv"

	"github.com/y3owk1n/fude/api"
)

// column returns res with the fold column's signs on the lines the windows
// on the scope's buffer show, from a window's height above to one below,
// in place of those it showed before, or with none while fold.column is
// off. A closed fold's first line shows fill_chars.fold_close, a line
// that starts an open fold fold_open, and the other lines a fold holds
// fold_sep (B488).
func (m *Module) column(s api.Scope, res api.Result) api.Result {
	buf := s.About["buffer"]
	res.Undecorate = append(res.Undecorate, m.signs[buf]...)
	delete(m.signs, buf)
	if on, _ := s.Option("fold.column"); on != "true" {
		return res
	}
	t := s.Text
	tab, _ := strconv.Atoi(s.About["tab"])
	open, shut, sep := fillChar(s, "fold_open", "-"), fillChar(s, "fold_close", "+"), fillChar(s, "fold_sep", "│")
	// The closed folds once res applies.
	gone := map[int]bool{}
	for _, id := range res.Undecorate {
		gone[id] = true
	}
	shown := map[int]string{}
	for _, d := range append(closed(s), res.Decorate...) {
		if d.Fold && !gone[d.ID] {
			shown[t.LineOf(d.Start)] = shut
		}
	}
	marks := map[int]string{}
	for _, w := range s.Windows() {
		if strconv.Itoa(w.Buffer) == buf {
			at := t.LineOf(w.Top)
			mark(t, max(at-w.Height, 0), min(at+2*w.Height, t.Lines()-1), tab, open, sep, marks)
		}
	}
	for line, sign := range marks {
		m.ids++
		res.Decorate = append(res.Decorate, api.Decoration{ID: m.ids, Start: t.LineStart(line), End: t.LineStart(line), Sign: cmp.Or(shown[line], sign), Style: "fold.column", Layer: -2})
		m.signs[buf] = append(m.signs[buf], m.ids)
	}
	return res
}

// fillChar returns the character fill_chars gives name, or def.
func fillChar(s api.Scope, name, def string) string {
	var c string
	if v, ok := s.Config("fill_chars." + name); ok && json.Unmarshal([]byte(v), &c) == nil && c != "" {
		return c
	}
	return def
}

// mark puts in marks, by line from first to last, open on a line that
// starts a fold and sep on another line a fold holds, a blank line counting
// as held when the next line with text is.
func mark(t api.Text, first, last, tab int, open, sep string, marks map[int]string) {
	// holds are the indents of the lines whose folds hold the line, found
	// going up from first until a line with none.
	var holds []int
	limit := -1
	if col, ok := nextText(t, first-1, tab); ok {
		limit = col
	}
	for l := first - 1; l >= 0 && limit > 0; l-- {
		if col, ok := indentCol(t, l, tab); ok && col < limit {
			holds, limit = append([]int{col}, holds...), col
		}
	}
	for l := first; l <= last; l++ {
		col, ok := indentCol(t, l, tab)
		if !ok {
			if next, ok := nextText(t, l, tab); ok && len(holds) > 0 && holds[0] < next {
				marks[l] = sep
			}
			continue
		}
		for len(holds) > 0 && holds[len(holds)-1] >= col {
			holds = holds[:len(holds)-1]
		}
		if next, ok := nextText(t, l, tab); ok && next > col {
			marks[l] = open
		} else if len(holds) > 0 {
			marks[l] = sep
		}
		holds = append(holds, col)
	}
}

// nextText returns the indent of the first line with text after line, or
// false when there is none.
func nextText(t api.Text, line, tab int) (int, bool) {
	for l := line + 1; l < t.Lines(); l++ {
		if col, ok := indentCol(t, l, tab); ok {
			return col, true
		}
	}
	return 0, false
}
