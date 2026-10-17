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
	ls = LineStart(t, at)
	for _, mt := range p.all(t.Slice(ls, LineEnd(t, at))) {
		if s, e := p.span(mt); ls+s == at {
			return at, ls + e, nil
		}
	}
	return at, next(t, at, LineEnd(t, at)), nil
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

// selectRange selects [s, e) in visual mode, as the only region when text names
// what <C-n> adds more of.
func (m *Machine) selectRange(s, e int, text string) {
	t := m.h.Text()
	m.mode, m.visual, m.anchor = Visual, "v", s
	m.h.SetCursor(prev(t, LineStart(t, e), e))
	if text != "" {
		m.regions, m.multi, m.multiAt = []region{{start: s, end: e}}, text, e
	}
}

// addRegion is <C-n>. In normal mode it selects the word under the cursor as
// a region, and in visual mode the selection, or with regions it adds the
// next match of their text after the last one, wrapping around. The text
// matches as a whole word when it began as the word under the cursor
// (B232).
func (m *Machine) addRegion() error {
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
	if len(m.regions) == 0 {
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
	// From the region added last, around the text once, to the first match
	// no region holds.
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
		end := at + 1
		ls := LineStart(t, at)
		for _, mt := range p.all(t.Slice(ls, LineEnd(t, at))) {
			if s, e := p.span(mt); ls+s == at {
				end = ls + e
			}
		}
		if !slices.ContainsFunc(m.regions, func(r region) bool { return at < r.end && r.start < end }) {
			m.regions = append(m.regions, region{start: at, end: end})
			slices.SortFunc(m.regions, func(a, b region) int { return a.start - b.start })
			m.multiAt, m.anchor = end, at
			m.h.SetCursor(prev(t, LineStart(t, end), end))
			return nil
		}
		pos = end
	}
}

// plain returns s as a pattern that matches it alone.
func plain(s string) string { return `\V` + strings.ReplaceAll(s, `\`, `\\`) }
