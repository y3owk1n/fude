package editor

import (
	"cmp"
	"slices"
	"strings"

	"github.com/clipperhouse/displaywidth"

	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// drawn is what a window draws in a range of its lines that the text does
// not hold.
type drawn struct {
	ghosts  []wire.Ghost
	inlines []vim.Inline           // the ghosts, as the rows and columns count them
	virtual map[int][]wire.Virtual // by line, the rows drawn above and below it (B299)
	show    string                 // show_break, which a wrapped line's rows after its first start with (B489)
}

// shown returns what w draws in its lines from start to end that the text
// does not hold: inlay hints, proposals' text and the replacements modules
// put, by position, and the rows decorations draw between lines. The cursor's
// line, and in the current window a selection's lines, show their text as
// it is, without replacements or rows of their own other than an insert's.
// So do all the lines of a block that holds one of them (B71, B216, B295,
// B299, B300).
func (e *Session) shown(w *window, start, end int) drawn {
	b := w.buf
	if b.inlines == 0 {
		return drawn{show: e.cfg.ShowBreak}
	}
	t := b.text.Snapshot()
	first, last := t.LineOf(w.pos()), t.LineOf(w.pos())
	if sel, ok := e.Selection(); ok && w == e.win {
		first, last = min(first, t.LineOf(sel.Start)), max(last, t.LineOf(sel.End))
	}
	for _, ds := range e.decos {
		for _, d := range ds {
			if d.buf == b && d.block {
				if s, en := t.LineOf(b.text.Mark(d.start)), t.LineOf(b.text.Mark(d.end)); s <= last && en >= first {
					first, last = min(first, s), max(last, en)
				}
			}
		}
	}
	out := drawn{show: e.cfg.ShowBreak}
	for _, ds := range e.decos {
		for _, d := range ds {
			if d.buf != b || !d.inline() {
				continue
			}
			s, en := b.text.Mark(d.start), b.text.Mark(d.end)
			line := t.LineOf(s)
			// An insert's rows are ghost text, which shows on the cursor's line
			// too.
			rows := len(d.lines) > 0 && !d.block && d.image == "" && (d.insert || line < first || line > last) && s >= start && s <= end
			if rows {
				if out.virtual == nil {
					out.virtual = map[int][]wire.Virtual{}
				}
				for _, text := range d.lines {
					out.virtual[line] = append(out.virtual[line], wire.Virtual{Text: text, Style: d.style, Above: d.above})
				}
			}
			switch {
			case d.block:
			case d.image != "" && s >= start && s <= end:
				// An image shows on the cursor's line too, so passing it moves
				// no text.
				p := e.picture(d.image)
				if p == nil || e.cell[0] == 0 || e.cell[1] == 0 {
					continue
				}
				if out.virtual == nil {
					out.virtual = map[int][]wire.Virtual{}
				}
				rows, cols := e.imageRows(w, p)
				for r := range rows {
					out.virtual[line] = append(out.virtual[line], wire.Virtual{Image: p.id, Row: r, Cols: cols})
				}
			case rows && !d.insert:
				// Its rows, drawn above, are all it shows.
			case d.replace && (line < first || line > last) && s < en && t.LineOf(en) == line && s >= start && s <= end:
				out.ghosts = append(out.ghosts, wire.Ghost{Pos: s, End: en, Text: d.note, Style: d.style})
			case d.cover && s >= start && s <= end && t.LineOf(en) == line:
				// The note takes the cells of the text it covers.
				ls, tab := t.LineStart(line), e.tabWidth(b)
				text := d.note
				if pad := vim.Column(t, ls, en, tab, nil) - vim.Column(t, ls, s, tab, nil) - displaywidth.String(text); pad > 0 {
					text += strings.Repeat(" ", pad)
				}
				out.ghosts = append(out.ghosts, wire.Ghost{Pos: s, End: en, Text: text, Style: d.style})
			case d.insert && s >= start && s <= end:
				out.ghosts = append(out.ghosts, wire.Ghost{Pos: s, Text: d.note, Style: d.style})
			case !d.replace && !d.insert && !d.cover && len(d.lines) == 0 && d.image == "" && en >= start && en <= end:
				out.ghosts = append(out.ghosts, wire.Ghost{Pos: en, Text: d.note, Stale: d.style == "proposal.stale"})
			}
		}
	}
	slices.SortStableFunc(out.ghosts, func(a, b wire.Ghost) int { return cmp.Compare(a.Pos, b.Pos) })
	out.inlines = wire.Inlines(out.ghosts)
	return out
}

// rows is how many rows d draws above and below line.
func (d drawn) rows(line int) int { return len(d.virtual[line]) }
