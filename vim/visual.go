package vim

import (
	"fmt"
	"math"
	"strings"
)

// Selection is the visual selection, for a client to draw.
type Selection struct {
	Kind        Key // v, V or <C-v>
	Start, End  int // the selected bytes, or every line a block spans
	Block       bool
	Left, Right int      // a block's display columns, inclusive
	Regions     [][2]int // every region :select chose, when it chose several
}

// Selection returns the visual selection, or false outside visual mode.
func (m *Machine) Selection() (Selection, bool) {
	if m.mode != Visual {
		return Selection{}, false
	}
	r := m.selection()
	s := Selection{Kind: m.visual, Start: r.start, End: r.end, Block: r.block, Left: r.left, Right: r.right}
	for _, g := range m.regions {
		s.Regions = append(s.Regions, [2]int{g.start, g.end})
	}
	return s, true
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
	c := m.h.Cursor()
	m.h.SetMark('<', min(m.anchor, c))
	m.h.SetMark('>', max(m.anchor, c))
	m.lastVisual, m.anchorFirst = m.visual, m.anchor <= c
	m.mode = Normal
}

// reselect is gv, which selects what the last visual mode selected.
func (m *Machine) reselect() {
	a, okA := m.h.Mark('<')
	b, okB := m.h.Mark('>')
	if !okA || !okB || m.lastVisual == "" {
		return
	}
	if !m.anchorFirst {
		a, b = b, a
	}
	m.mode, m.visual, m.anchor = Visual, m.lastVisual, a
	m.h.SetCursor(b)
}

// blockInsertStart starts insert mode on the first line of a block, before
// it for I or after it for A. Leaving insert mode copies the typed text to
// the other lines, padding short ones for A.
func (m *Machine) blockInsertStart(after bool) error {
	t := m.h.Text()
	r := m.selection()
	toEnd := after && r.right == math.MaxInt
	m.leaveVisual()
	col := r.left
	if after && !toEnd {
		col = r.right + 1
	}
	pos := colStart(t, r.start, col)
	if toEnd {
		pos = LineEnd(t, r.start)
	}
	label := "<C-v>I"
	if after {
		label = "<C-v>A"
	}
	m.startInsert(Key(label), pos)
	if w := column(t, r.start, pos); after && !toEnd && w < col {
		if err := m.insertText(strings.Repeat(" ", col-w)); err != nil {
			return err
		}
	}
	m.blockInsert = &blockInsert{
		start: m.h.Cursor(), col: col, lines: t.LineOf(r.end) - t.LineOf(r.start),
		toEnd: toEnd, pad: after, home: colStart(t, r.start, r.left),
	}
	return nil
}

// visualOps are the keys that apply an operator to the selection, and
// whether they take its whole lines.
var visualOps = map[string]struct {
	op    string
	lines bool
}{
	"d": {"d", false}, "x": {"d", false}, "c": {"c", false}, "s": {"c", false}, "y": {"y", false},
	"~": {"g~", false}, "u": {"gu", false}, "U": {"gU", false},
	"g~": {"g~", false}, "gu": {"gu", false}, "gU": {"gU", false},
	">": {">", true}, "<": {"<", true},
	"X": {"d", true}, "D": {"d", true}, "Y": {"y", true}, "C": {"c", true}, "S": {"c", true}, "R": {"c", true},
}

// replaceSelection replaces every character of the selection with k,
// keeping its line breaks.
func (m *Machine) replaceSelection(k Key) error {
	r := m.selection()
	m.leaveVisual()
	t := m.h.Text()
	ranges := lineRanges(t, r)
	var edits []Edit
	for _, rg := range ranges {
		n := 0
		for p := rg[0]; p < rg[1]; p = next(t, p, rg[1]) {
			n++
		}
		if n > 0 {
			edits = append(edits, Edit{Start: rg[0], End: rg[1], Text: strings.Repeat(string(k), n)})
		}
	}
	if len(edits) > 0 {
		if err := m.apply(edits, "r"+string(k)); err != nil {
			return err
		}
	}
	m.h.SetCursor(ranges[0][0])
	return nil
}

// putOver replaces the selection with register reg n times, as one undo
// step. Without keep, the text it replaces goes to the registers as a
// delete's does. A line register goes on lines of its own, and lines take
// other text as a line.
func (m *Machine) putOver(reg rune, keep bool, n int) error {
	if reg == 0 {
		reg = '"'
	}
	put, ok, err := m.fetch(reg)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("nothing in register %c", reg)
	}
	r := m.selection()
	m.leaveVisual()
	t := m.h.Text()
	text := strings.Repeat(put.text, n)
	ranges := lineRanges(t, r)
	var edits []Edit
	var old []string
	switch {
	case r.block:
		// Text with no line break goes on every line of the block, and other
		// text on its first.
		fill := !strings.Contains(text, "\n")
		for i, rg := range ranges {
			e := Edit{Start: rg[0], End: rg[1]}
			if fill || i == 0 {
				e.Text = strings.TrimSuffix(text, "\n")
			}
			edits, old = append(edits, e), append(old, t.Slice(rg[0], rg[1]))
		}
	case r.lines:
		edits, old = []Edit{{Start: r.start, End: r.end, Text: strings.TrimSuffix(text, "\n")}}, []string{t.Slice(r.start, r.end) + "\n"}
	case put.linewise:
		edits, old = []Edit{{Start: r.start, End: r.end, Text: "\n" + text}}, []string{t.Slice(r.start, r.end)}
	default:
		edits, old = []Edit{{Start: r.start, End: r.end, Text: text}}, []string{t.Slice(r.start, r.end)}
	}
	label := "P"
	if !keep {
		label = "p"
		m.store(0, "d", register{text: strings.Join(old, "\n"), linewise: r.lines, block: r.block})
	}
	if err := m.apply(edits, label); err != nil {
		return err
	}
	t = m.h.Text()
	at := edits[0].Start
	if r.lines || put.linewise {
		at += len(edits[0].Text) - len(strings.TrimLeft(edits[0].Text, "\n"))
		m.h.SetCursor(firstNonBlank(t, at, LineEnd(t, at)))
		return nil
	}
	m.h.SetCursor(lastChar(t, at+len(edits[0].Text)))
	return nil
}

// visualKey handles a command in visual mode.
func (m *Machine) visualKey(cmd string, count int, reg rune) error {
	if len(m.regions) > 0 {
		if ok, err := m.regionKey(cmd, reg); ok {
			return err
		}
	}
	t, c := m.h.Text(), m.h.Cursor()
	if m.ops[cmd] != nil {
		r := m.selection()
		m.leaveVisual()
		return m.act(cmd, reg, string(m.visual)+cmd, r)
	}
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
	case "I", "A":
		if m.visual == "<C-v>" {
			return m.blockInsertStart(cmd == "A")
		}
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
	case "J", "gJ":
		r := m.selection()
		m.leaveVisual()
		last := r.end
		if !r.lines && !r.block && last > r.start {
			last--
		}
		m.h.SetCursor(r.start)
		return m.join(max(t.LineOf(last)-t.LineOf(r.start), 1), cmd == "J")
	case "p", "P":
		return m.putOver(reg, cmd == "P", max(count, 1))
	}
	if v, ok := visualOps[cmd]; ok {
		r := m.selection()
		switch {
		case m.visual == "<C-v>" && (cmd == "D" || cmd == "C"):
			// In a block, they act to the end of every line.
			r.right = math.MaxInt
		case v.lines && !r.lines:
			last := r.end
			if !r.block && last > r.start {
				last--
			}
			r = region{start: LineStart(t, r.start), end: LineEnd(t, last), lines: true}
		}
		m.leaveVisual()
		if err := m.act(v.op, reg, string(m.visual)+cmd, r); err != nil {
			return err
		}
		if v.op == "y" && r.lines {
			m.h.SetCursor(r.start)
		}
		return nil
	}
	if len(cmd) > 1 && cmd[0] == 'r' && Key(cmd[1:]).isChar() {
		return m.replaceSelection(Key(cmd[1:]))
	}
	if r, ok := m.object(t, c, count, cmd); ok {
		m.anchor = r.start
		if r.lines {
			m.visual = "V"
			m.h.SetCursor(LineStart(t, r.end))
		} else {
			// An object that ends with a line break leaves the cursor on it,
			// so the selection includes it.
			m.visual = "v"
			if r.end > r.start && t.Slice(r.end-1, r.end) == "\n" {
				m.h.SetCursor(r.end - 1)
			} else {
				m.h.SetCursor(prev(t, LineStart(t, r.end), r.end))
			}
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
