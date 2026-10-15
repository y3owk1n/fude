package vim

import (
	"math"
	"strings"
)

// Selection is the visual selection, for a client to draw.
type Selection struct {
	Kind        Key // v, V or <C-v>
	Start, End  int // the selected bytes, or every line a block spans
	Block       bool
	Left, Right int // a block's display columns, inclusive
}

// Selection returns the visual selection, or false outside visual mode.
func (m *Machine) Selection() (Selection, bool) {
	if m.mode != Visual {
		return Selection{}, false
	}
	r := m.selection()
	return Selection{m.visual, r.start, r.end, r.block, r.left, r.right}, true
}

// selection returns the text visual mode covers. Charwise it includes the
// cluster under each end, and the line break after $ or on an empty line.
func (m *Machine) selection() region {
	t, c := m.h.Text(), m.h.Cursor()
	s, e := min(m.anchor, c), max(m.anchor, c)
	toEnd := m.wantSet && m.want == math.MaxInt
	switch m.visual {
	case "V":
		return region{start: LineStart(t, s), end: LineEnd(t, e), lines: true}
	case "<C-v>":
		ca, cc := column(t, LineStart(t, m.anchor), m.anchor), column(t, LineStart(t, c), c)
		wa := max(textWidth(t.Slice(m.anchor, next(t, m.anchor, LineEnd(t, m.anchor))), ca), 1)
		wc := max(textWidth(t.Slice(c, next(t, c, LineEnd(t, c))), cc), 1)
		r := region{start: LineStart(t, s), end: LineEnd(t, e), block: true, left: min(ca, cc), right: max(ca+wa, cc+wc) - 1}
		if toEnd {
			r.right = math.MaxInt
		}
		return r
	}
	end := next(t, e, LineEnd(t, e))
	if (end == e || toEnd && e == c) && e < t.Len() {
		end = LineEnd(t, e) + 1
	}
	return region{start: s, end: min(end, t.Len())}
}

// leaveVisual returns to normal mode, keeping the selection's ends for '<
// and '>.
func (m *Machine) leaveVisual() {
	r := m.selection()
	m.h.SetMark('<', r.start)
	m.h.SetMark('>', max(r.start, r.end-1))
	m.mode = Normal
}

// visualKey handles a command in visual mode.
func (m *Machine) visualKey(cmd string, count int, reg rune) error {
	t, c := m.h.Text(), m.h.Cursor()
	switch cmd {
	case "<Esc>":
		m.leaveVisual()
		m.h.SetCursor(clamp(t, c))
		return nil
	case "v", "V", "<C-v>":
		if Key(cmd) == m.visual {
			m.leaveVisual()
			m.h.SetCursor(clamp(t, c))
		}
		m.visual = Key(cmd)
		return nil
	case ":":
		// The command line starts with the range of the selected lines.
		m.leaveVisual()
		m.back, m.mode, m.prompt, m.line = Normal, Cmdline, ":", "'<,'>"
		return nil
	case "o":
		m.anchor, c = c, m.anchor
		m.h.SetCursor(c)
		return nil
	case "*", "#":
		// Search for the selected text literally.
		r := m.selection()
		m.leaveVisual()
		pat := strings.ReplaceAll(strings.ReplaceAll(t.Slice(r.start, r.end), `\`, `\\`), "\n", `\n`)
		m.search.pattern, m.search.forward = `\V`+pat, cmd == "*"
		p, k, _ := m.seek(m.search.pattern, m.search.forward, max(count, 1))
		if k != failed {
			m.h.SetCursor(clamp(t, p))
		}
		return nil
	case "d", "x", "c", "y":
		op := cmd
		if op == "x" {
			op = "d"
		}
		r := m.selection()
		m.leaveVisual()
		if err := m.act(op, reg, string(m.visual)+op, r); err != nil {
			return err
		}
		if op == "y" && r.lines {
			m.h.SetCursor(r.start)
		}
		return nil
	}
	if r, ok := textObject(t, c, count, cmd); ok {
		m.anchor = r.start
		if r.lines {
			m.visual = "V"
			m.h.SetCursor(LineStart(t, r.end))
		} else {
			m.h.SetCursor(prev(t, LineStart(t, r.end), r.end))
		}
		return nil
	}
	if p, k, ok := m.motion(cmd, count); ok {
		if k == failed {
			m.beep = true
		} else {
			m.h.SetCursor(clamp(t, p))
		}
	}
	return nil
}
