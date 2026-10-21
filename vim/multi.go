package vim

import (
	"errors"
	"slices"
	"strings"
)

// searchMatch returns the match of the last search under c, or else the next
// one after it, or the one before it when back, wrapping around the text,
// for gn and gN (B232).
func (m *Machine) searchMatch(t Text, c int, back bool) (int, int, error) {
	if m.search.pattern == "" {
		return 0, 0, errors.New("no previous search")
	}
	p, err := m.compile(m.search.pattern)
	if err != nil {
		return 0, 0, err
	}
	ls, le := LineStart(t, c), LineEnd(t, c)
	for _, mt := range p.all(t.Slice(ls, le)) {
		if s, e := p.span(mt); s < e && ls+s <= c && c < ls+e {
			return ls + s, ls + e, nil
		}
	}
	var at int
	var ok bool
	if back {
		if at, ok = p.last(t, 0, c); !ok {
			at, ok = p.last(t, c, t.Len()+1)
		}
	} else if at, ok = p.first(t, c+1, t.Len()+1); !ok {
		at, ok = p.first(t, 0, c+1)
	}
	if !ok {
		return 0, 0, errors.New("pattern not found: " + m.search.pattern)
	}
	return at, p.endAt(t, at), nil
}

// endAt returns where the match of p that starts at at ends, or the end of
// the character at at when none starts there.
func (p *pattern) endAt(t Text, at int) int {
	ls := LineStart(t, at)
	for _, mt := range p.all(t.Slice(ls, LineEnd(t, at))) {
		if s, e := p.span(mt); ls+s == at {
			return ls + e
		}
	}
	return next(t, at, LineEnd(t, at))
}

// selectMatch selects the match gn or gN finds in visual mode.
func (m *Machine) selectMatch(back bool) error {
	t := m.h.Text()
	s, e, err := m.searchMatch(t, m.h.Cursor(), back)
	if err != nil {
		m.msg = err.Error()
		return nil
	}
	m.selectRange(s, e, "")
	return nil
}

// selectRange selects [s, e) in visual mode, and with text, the pattern
// <C-n> places cursors at the next match of.
func (m *Machine) selectRange(s, e int, text string) {
	t := m.h.Text()
	m.mode, m.visual, m.anchor = Visual, "v", s
	m.h.SetCursor(prev(t, LineStart(t, e), e))
	if text != "" {
		m.multi, m.multiAt = text, e
	}
}

// addCursor is <C-n>. In normal mode it selects the word under the cursor.
// In visual mode it places a cursor that keeps the selection, then selects
// the next match of its text that no cursor selects, going round the text.
// The text matches as a whole word when it began as the word under the
// cursor (B232).
func (m *Machine) addCursor() error {
	m.once = true
	t, c := m.h.Text(), m.h.Cursor()
	if m.mode == Normal {
		s, e, ok := word(t, c, 1, false, false)
		if !ok || class(t, c, false) != 2 {
			m.beep = true
			return nil
		}
		m.selectRange(s, e, `\<`+plain(t.Slice(s, e))+`\>`)
		return nil
	}
	if m.multi == "" {
		r := m.selection()
		if r.lines || r.block || t.LineOf(r.start) != t.LineOf(max(r.end-1, r.start)) {
			m.beep = true
			return nil
		}
		m.selectRange(r.start, r.end, plain(t.Slice(r.start, r.end)))
	}
	p, err := m.compile(m.multi)
	if err != nil {
		return err
	}
	taken := []int{min(m.anchor, m.h.Cursor())}
	for _, x := range m.cursors {
		a, _ := m.h.Mark(mark(x, 1))
		taken = append(taken, min(a, m.at(x)))
	}
	// From the match selected last, around the text once, to the first
	// match no cursor selects.
	pos, wrapped := m.multiAt, false
	for {
		at, ok := p.first(t, pos, t.Len()+1)
		if !ok || wrapped && at >= m.multiAt {
			if wrapped || !ok && pos == 0 {
				m.msg = "no more matches"
				return nil
			}
			pos, wrapped = 0, true
			continue
		}
		end := p.endAt(t, at)
		if !slices.Contains(taken, at) {
			x := m.place(m.h.Cursor())
			m.h.SetMark(mark(x, 1), m.anchor)
			m.sortCursors()
			m.multiAt, m.anchor = end, at
			m.h.SetCursor(prev(t, LineStart(t, end), end))
			return nil
		}
		pos = end
	}
}

// Selector lets :select choose where to place cursors. find returns the
// ranges that query selects in [start, end) of t.
func (m *Machine) Selector(find func(t Text, query string, start, end int) ([][2]int, error)) {
	m.selector = find
}

// selectQuery runs :select over [start, end). It places a cursor selecting
// each range the query chooses, and the cursor selects the first. Of two
// that overlap, the earlier stays (B42).
func (m *Machine) selectQuery(query string, start, end int) error {
	if m.selector == nil {
		return errors.New("nothing to select with")
	}
	t := m.h.Text()
	found, err := m.selector(t, query, start, end)
	if err != nil {
		return err
	}
	slices.SortFunc(found, func(a, b [2]int) int { return a[0] - b[0] })
	var rs [][2]int
	for _, f := range found {
		if f[0] < f[1] && (len(rs) == 0 || f[0] >= rs[len(rs)-1][1]) {
			rs = append(rs, f)
		}
	}
	if len(rs) == 0 {
		return errors.New("the query selected nothing")
	}
	for _, r := range rs[1:] {
		m.placeSelecting(r[0], r[1])
	}
	m.sortCursors()
	m.selectRange(rs[0][0], rs[0][1], "")
	return nil
}

// plain returns s as a pattern that matches it alone.
func plain(s string) string { return `\V` + strings.ReplaceAll(s, `\`, `\\`) }
