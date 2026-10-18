package editor

import (
	"cmp"
	"slices"

	"github.com/y3owk1n/fude/wire"
)

// shown returns what w shows in its lines from start to end that the text
// does not hold, by position: inlay hints, proposals' text and the
// replacements modules put. The cursor's line, and in the current window
// a selection's lines, show their text as it is (B71, B216, B295).
func (e *Session) shown(w *window, start, end int) []wire.Ghost {
	b := w.buf
	if b.inlines == 0 {
		return nil
	}
	t := b.text.Snapshot()
	first, last := t.LineOf(w.pos()), t.LineOf(w.pos())
	if sel, ok := e.Selection(); ok && w == e.win {
		first, last = min(first, t.LineOf(sel.Start)), max(last, t.LineOf(sel.End))
	}
	var out []wire.Ghost
	for _, ds := range e.decos {
		for _, d := range ds {
			if d.buf != b || !d.inline() {
				continue
			}
			s, en := b.text.Mark(d.start), b.text.Mark(d.end)
			switch line := t.LineOf(s); {
			case d.replace && (line < first || line > last) && s < en && t.LineOf(en) == line && s >= start && s <= end:
				out = append(out, wire.Ghost{Pos: s, End: en, Text: d.note, Style: d.style})
			case d.style == "inlay" && s >= start && s <= end:
				out = append(out, wire.Ghost{Pos: s, Text: d.note, Style: "inlay"})
			case !d.replace && d.style != "inlay" && en >= start && en <= end:
				out = append(out, wire.Ghost{Pos: en, Text: d.note, Stale: d.style == "proposal.stale"})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b wire.Ghost) int { return cmp.Compare(a.Pos, b.Pos) })
	return out
}
