package editor

import (
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/syntax"
	"github.com/y3owk1n/fude/vim"
)

// pick is a module's menu at the cursor, as code actions show: its items,
// the one selected, and the module and id its choice goes to (B227, B293).
type pick struct {
	owner api.Module
	id    uint32
	b     *buffer
	items []api.MenuItem
	sel   int
}

// offerMenu opens m from p, or gives the menu of the same id that shows its
// items, keeping the selection. Opening leaves visual mode, as the menu
// takes the keys.
func (e *Session) offerMenu(p api.Module, b *buffer, m *api.Menu) {
	if e.pick != nil && e.pick.owner == p && e.pick.id == m.ID {
		e.pick.items, e.pick.sel = m.Items, min(e.pick.sel, max(len(m.Items)-1, 0))
	} else {
		if e.vim.Mode() == vim.Visual {
			e.settle(e.vim.Feed("<Esc>"))
		}
		e.pick = &pick{owner: p, id: m.ID, b: b, items: m.Items}
	}
	e.drawMenu()
}

// drawMenu draws the menu at the cursor, each item with its number, title
// and kind, and what the selected one does beside it.
func (e *Session) drawMenu() {
	p := e.pick
	var s strings.Builder
	var spans []syntax.Span
	width, kinds := 0, 0
	for _, it := range p.items {
		width, kinds = max(width, utf8.RuneCountInString(it.Title)), max(kinds, utf8.RuneCountInString(it.Kind))
	}
	// A title too long for the windows ends in an ellipsis, so each row
	// keeps to one line. The box's border and padding, the number and the
	// kind take the rest of the room (B227).
	_, room, _ := e.area()
	width = max(min(width, room-4-3-2-kinds), 1)
	for i, it := range p.items {
		if i > 0 {
			s.WriteByte('\n')
		}
		start := s.Len()
		num := " "
		if i < 9 {
			num = strconv.Itoa(i + 1)
		}
		title := it.Title
		if r := []rune(title); len(r) > width {
			title = string(r[:width-1]) + "…"
		}
		spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(num), Name: "comment"})
		s.WriteString(num + "  " + title)
		if it.Kind != "" {
			s.WriteString(strings.Repeat(" ", width-utf8.RuneCountInString(title)+2))
			spans = append(spans, syntax.Span{Start: s.Len(), End: s.Len() + len(it.Kind), Name: "comment"})
			s.WriteString(it.Kind)
		}
		if it.Disabled != "" {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "comment"})
		}
		if i == p.sel {
			spans = append(spans, syntax.Span{Start: start, End: s.Len(), Name: "menu.selected"})
		}
	}
	e.float.text, e.float.spans = s.String(), spans
	e.float.doc, e.float.docSpans = "", nil
	if doc := p.items[p.sel].Doc; doc != nil {
		if doc.Spans == nil {
			e.float.doc, e.float.docSpans = e.markdown(doc.Text)
		} else {
			e.float.doc, e.float.docSpans = doc.Text, styled(doc.Spans)
		}
	}
}

// key takes a key while the menu shows. <C-n>, <C-p>, j, k and the arrows
// move the selection, <CR> takes the selected item and a digit the one it
// numbers, and any other key closes the menu (B227).
func (p *pick) key(e *Session, k vim.Key) bool {
	if p == nil {
		return false
	}
	defer e.reveal()
	switch k {
	case "<C-n>", "<Down>", "j", "\t":
		p.sel = (p.sel + 1) % len(p.items)
	case "<C-p>", "<Up>", "k", "<S-Tab>":
		p.sel = (p.sel + len(p.items) - 1) % len(p.items)
	case "<CR>", "1", "2", "3", "4", "5", "6", "7", "8", "9":
		i := p.sel
		if k != "<CR>" {
			i = int(k[0] - '1')
		}
		e.chose(p, i, i < len(p.items))
		return true
	default:
		e.chose(p, -1, false)
		return true
	}
	e.drawMenu()
	return true
}

// chose closes the menu and tells its module which item was picked, or
// that none was.
func (e *Session) chose(p *pick, i int, ok bool) {
	e.pick, e.float.text, e.float.doc, e.float.docSpans = nil, "", "", nil
	if !ok {
		i = -1
	}
	if slices.Contains(e.modules, p.owner) {
		e.listen(p.owner).add(event{buf: p.b, id: p.id, extra: api.Context{"picked": strconv.Itoa(i), "ok": strconv.FormatBool(ok)}, from: e})
	}
}
