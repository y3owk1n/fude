package vim

import (
	"errors"
	"slices"
	"strings"
)

// regionMark is the first of the private marks that hold the other cursors
// of a change on several regions.
const regionMark = 0xF8000

// Selector lets :select choose regions. find returns the ranges that query
// selects in [start, end) of t.
func (m *Machine) Selector(find func(t Text, query string, start, end int) ([][2]int, error)) {
	m.selector = find
}

// Cursors returns where the other cursors are while a change on several
// regions types into all of them.
func (m *Machine) Cursors() []int {
	var out []int
	for _, mk := range m.cursors {
		if p, ok := m.h.Mark(mk); ok {
			out = append(out, p)
		}
	}
	return out
}

// selectQuery runs :select over [start, end) and selects in visual mode
// every region the query chooses. Of two that overlap, the earlier stays.
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
	var rs []region
	for _, f := range found {
		if f[0] < f[1] && (len(rs) == 0 || f[0] >= rs[len(rs)-1].end) {
			rs = append(rs, region{start: f[0], end: f[1]})
		}
	}
	if len(rs) == 0 {
		return errors.New("the query selected nothing")
	}
	m.regions, m.mode, m.visual = rs, Visual, "v"
	m.anchor = rs[0].start
	m.h.SetCursor(prev(t, LineStart(t, rs[0].end), rs[0].end))
	return nil
}

// regionKey runs a visual mode key while :select has chosen regions, and
// reports whether it did. d, x, y, c and the host's operators act on every
// region. Any other key drops all but the first region.
func (m *Machine) regionKey(cmd string, reg rune) (bool, error) {
	rs := m.regions
	m.regions = nil
	if cmd != "d" && cmd != "x" && cmd != "y" && cmd != "c" && m.ops[cmd] == nil {
		return false, nil
	}
	m.leaveVisual()
	t := m.h.Text()
	label := "v" + cmd
	if m.ops[cmd] != nil {
		// Last first, so each region's offsets hold when it is reached.
		m.h.BeginGroup()
		defer m.h.EndGroup()
		for i := len(rs) - 1; i >= 0; i-- {
			if edits := m.ops[cmd](m.h.Text(), rs[i].start, rs[i].end, false); len(edits) > 0 {
				if err := m.apply(edits, label); err != nil {
					return true, err
				}
			}
		}
		m.h.SetCursor(clamp(m.h.Text(), rs[0].start))
		return true, nil
	}
	texts := make([]string, len(rs))
	edits := make([]Edit, len(rs))
	for i, r := range rs {
		texts[i], edits[i] = t.Slice(r.start, r.end), Edit{Start: r.start, End: r.end}
	}
	op := cmd
	if op == "x" {
		op = "d"
	}
	m.store(reg, op, register{text: strings.Join(texts, "\n")})
	if op == "y" {
		m.h.SetCursor(rs[0].start)
		return true, nil
	}
	// Each region's start gets a cursor mark before the delete, so the
	// delete moves it. The cursors start after the delete, so it is not
	// repeated at them.
	var cursors []rune
	if op == "c" {
		m.startInsert(Key(label), rs[0].start)
		for i, r := range rs[1:] {
			mk := rune(regionMark + i)
			m.h.SetMark(mk, r.start)
			cursors = append(cursors, mk)
		}
	}
	if err := m.apply(edits, label); err != nil {
		return true, err
	}
	if op == "c" {
		m.cursors = cursors
	} else {
		m.h.SetCursor(clamp(m.h.Text(), rs[0].start))
	}
	return true, nil
}

// mirror repeats edits made at the cursor at every other cursor, moved by
// how far each is from it. It mirrors by length, so <C-w> takes the same
// number of bytes at each cursor as at the first. Of two edits that overlap,
// the earlier stays.
func (m *Machine) mirror(edits []Edit) []Edit {
	t, c := m.h.Text(), m.h.Cursor()
	out := slices.Clone(edits)
	for _, s := range m.Cursors() {
		for _, e := range edits {
			e.Start, e.End = e.Start+s-c, e.End+s-c
			if e.Start >= 0 && e.End <= t.Len() {
				out = append(out, e)
			}
		}
	}
	slices.SortStableFunc(out, func(a, b Edit) int { return a.Start - b.Start })
	kept := out[:0]
	for _, e := range out {
		if len(kept) == 0 || e.Start >= kept[len(kept)-1].End {
			kept = append(kept, e)
		}
	}
	return kept
}
