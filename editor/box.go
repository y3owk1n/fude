package editor

import (
	"slices"
	"strings"

	"github.com/clipperhouse/displaywidth"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// box is how a panel in a box of its own draws: its styles, the first line
// of each of its cards, and its foot (B159).
type box struct {
	spans, footSpans []syntax.Span
	cards            []int
	foot             string
}

// boxPanel shows p in a box of its own, which takes the focus, or replaces
// the text of the one open under its title.
func (e *Session) boxPanel(p api.Panel) bool {
	b := e.win.buf
	if open := e.under != nil && b.title == p.Title; open {
		t := b.text.Snapshot()
		if old := t.Slice(0, t.Len()); old != p.Text {
			if err := b.text.Apply(kernel.Transaction{Base: t.Version(), Edits: []kernel.Edit{replace(old, p.Text)}}); err != nil {
				e.message = err.Error()
				return false
			}
		}
	} else {
		if p.Update {
			return false
		}
		if e.under != nil {
			e.unfocus()
		}
		e.focus(p.Text)
		b = e.win.buf
		b.title = p.Title
		b.keys = e.panelKeys(b, p.Keys)
	}
	b.box = &box{spans: styled(p.Spans), footSpans: styled(p.FootSpans), cards: p.Cards, foot: p.Foot}
	return true
}

// boxFloat is the box of the panel with the focus. It is as wide as its
// widest line, and sits a fifth of the way down, or higher when it is too
// tall for that. The cursor stays on the first row of a card, which is
// raised, and the box scrolls to show all of that card.
func (e *Session) boxFloat(bx *box) wire.Float {
	t := e.win.buf.text.Snapshot()
	text := t.Slice(0, t.Len())
	w := displaywidth.String(bx.foot)
	for line := range strings.Lines(text) {
		w = max(w, displaywidth.String(strings.TrimSuffix(line, "\n")))
	}
	w = min(w+4, e.width)
	// Its border, the rule and the foot take four rows.
	h := min(t.Lines()+4, e.height-2)
	y := max(min(e.height/5, e.height-1-h), 0)
	spans, top := bx.spans, 0
	if len(bx.cards) > 0 {
		line, at := t.LineOf(e.Cursor()), 0
		for i, c := range bx.cards {
			if c <= line {
				at = i
			}
		}
		head := t.LineStart(min(bx.cards[at], t.Lines()-1))
		if e.Cursor() != head {
			e.setCursor(head)
		}
		spans = append(slices.Clone(spans), syntax.Span{Start: head, End: vim.LineEnd(t, head) + 1, Name: "menu.selected"})
		last := t.Lines() - 1
		if at+1 < len(bx.cards) {
			last = bx.cards[at+1] - 1
		}
		top = t.LineStart(max(last-(h-4)+1, 0))
	}
	return wire.Float{Text: text, Spans: spans, Foot: bx.foot, FootSpans: bx.footSpans, Top: top, Cursor: e.Cursor(), Title: strings.Trim(e.win.buf.title, "[]"), X: (e.width - w) / 2, Y: y, W: w, H: h}
}

// boxKeys is the overlay of a panel with cards, which takes its own keys
// and : alone. It is cards, not text, so no other key, such as / or v, does
// anything there (B159).
type boxKeys struct{}

func (boxKeys) key(e *Session, k vim.Key) bool {
	b := e.win.buf
	if b.box == nil || len(b.box.cards) == 0 || e.vim.Mode() != vim.Normal || !e.vim.Idle() || k == ":" {
		return false
	}
	own := false
	e.localKeys(b).Each(vim.Normal, func(from, _ string) {
		own = own || vim.Keys(from)[0] == k
	})
	return !own
}
