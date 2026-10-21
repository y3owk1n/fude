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
	Regions     [][2]int // with cursors placed, every cursor's selection and its own
}

// Selection returns the visual selection, or false outside visual mode.
func (m *Machine) Selection() (Selection, bool) {
	if m.mode != Visual {
		return Selection{}, false
	}
	r := m.selection()
	return Selection{Kind: m.visual, Start: r.start, End: r.end, Block: r.block, Left: r.left, Right: r.right, Regions: m.selections()}, true
}

// selection returns the text visual mode covers. Charwise it includes the
// cluster under each end, and the line break after $ or on an empty line.
func (m *Machine) selection() region { return m.selectionAt(m.anchor, m.h.Cursor()) }

// selectionAt is selection with its ends at anchor and c.
func (m *Machine) selectionAt(anchor, c int) region {
	tab := m.h.TabWidth()
	t := m.h.Text()
	s, e := min(anchor, c), max(anchor, c)
	toEnd := m.wantSet && m.want == math.MaxInt
	switch m.visual {
	case "V":
		return m.folded(t, region{start: LineStart(t, s), end: LineEnd(t, e), lines: true})
	case "<C-v>":
		ca, cc := column(t, LineStart(t, anchor), anchor, tab), column(t, LineStart(t, c), c, tab)
		// With VirtualEdit, the cursor's corner may be past its line's end
		// (B490).
		if c == m.h.Cursor() {
			cc += m.Past()
		}
		wa := max(textWidth(t.Slice(anchor, next(t, anchor, LineEnd(t, anchor))), ca, tab), 1)
		wc := max(textWidth(t.Slice(c, next(t, c, LineEnd(t, c))), cc, tab), 1)
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
	m.mode, m.multi = Normal, ""
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
	tab := m.h.TabWidth()
	t := m.h.Text()
	r := m.selection()
	toEnd := after && r.right == math.MaxInt
	m.leaveVisual()
	col := r.left
	if after && !toEnd {
		col = r.right + 1
	}
	pos := colStart(t, r.start, col, tab)
	if toEnd {
		pos = LineEnd(t, r.start)
	}
	label := "<C-v>I"
	if after {
		label = "<C-v>A"
	}
	if err := m.startInsert(Key(label), pos); err != nil {
		return err
	}
	if w := column(t, r.start, pos, tab); after && !toEnd && w < col {
		if err := m.insertText(strings.Repeat(" ", col-w)); err != nil {
			return err
		}
	}
	m.blockInsert = &blockInsert{
		start: m.h.Cursor(), col: col, lines: t.LineOf(r.end) - t.LineOf(r.start),
		toEnd: toEnd, pad: after, home: colStart(t, r.start, r.left, tab),
	}
	return nil
}

// visualOp is a key that applies an operator to the selection, whether it
// takes the selection's whole lines, and what it does.
type visualOp struct {
	op    string
	lines bool
	help  string
}

// visualOps are the keys that apply an operator to the selection.
var visualOps = map[string]visualOp{
	"d": {"d", false, "deletes the selection"}, "x": {"d", false, "deletes the selection"},
	"c": {"c", false, "deletes the selection, then inserts"}, "s": {"c", false, "deletes the selection, then inserts"},
	"y": {"y", false, "yanks the selection"},
	"~": {"g~", false, "switches the case of the selection"}, "g~": {"g~", false, "switches the case of the selection"},
	"u": {"gu", false, "lowercases the selection"}, "gu": {"gu", false, "lowercases the selection"},
	"U": {"gU", false, "uppercases the selection"}, "gU": {"gU", false, "uppercases the selection"},
	"g?": {"g?", false, "rot13-encodes the selection"},
	"gw": {"gw", true, "formats the selected lines as gq does, leaving the cursor where it was"},
	"!":  {"!", true, "opens the command line to put the selected lines through a shell command"},
	">":  {">", true, "indents the selected lines and keeps them selected"}, "<": {"<", true, "outdents the selected lines and keeps them selected"},
	"X": {"d", true, "deletes the selected lines"}, "D": {"d", true, "deletes the selected lines, or to the end of each line of a block"},
	"Y": {"y", true, "yanks the selected lines"},
	"C": {"c", true, "changes the selected lines, or to the end of each line of a block"},
	"S": {"c", true, "changes the selected lines"}, "R": {"c", true, "changes the selected lines"},
}

// replaceSelection replaces every character of the selection with k,
// keeping its line breaks.
func (m *Machine) replaceSelection(k Key) error {
	r := m.selection()
	m.leaveVisual()
	t := m.h.Text()
	ranges := m.lineRanges(t, r)
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
	reg, put, ok, err := m.fetchPut(reg)
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
	ranges := m.lineRanges(t, r)
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

// visualCall is what a visual mode command gets: the text, the cursor, the
// keys, the count typed (0 for none) and the register named.
type visualCall struct {
	t     Text
	c     int
	cmd   string
	count int
	reg   rune
}

// visualKey is a visual mode command that is not an operator, an object or a
// motion.
type visualKey struct {
	help string
	run  func(m *Machine, x visualCall) error
}

// visuals are those commands by their keys. r{char} stands for r and any
// character, which visualKey checks after visualOps.
var visuals = map[string]visualKey{
	"]e": {"moves the selected lines n lines down and keeps them selected", func(m *Machine, x visualCall) error {
		return m.moveSelection(max(x.count, 1), "]e")
	}},
	"[e": {"moves the selected lines n lines up and keeps them selected", func(m *Machine, x visualCall) error {
		return m.moveSelection(-max(x.count, 1), "[e")
	}},
	"<Esc>": {"leaves visual mode", leaveSelection},
	"<C-c>": {"leaves visual mode", leaveSelection},
	"v":     {"selects characters, or leaves when it does", switchVisual},
	"V":     {"selects lines, or leaves when it does", switchVisual},
	"<C-v>": {"selects a block, or leaves when it does", switchVisual},
	"I": {"inserts before a block on each of its lines, or at the start of the first selected line", func(m *Machine, x visualCall) error {
		if m.visual == "<C-v>" {
			return m.blockInsertStart(false)
		}
		r := m.selection()
		m.leaveVisual()
		return m.startInsert("I", LineStart(x.t, r.start))
	}},
	"A": {"inserts after a block on each of its lines, or at the end of the last selected line", func(m *Machine, x visualCall) error {
		if m.visual == "<C-v>" {
			return m.blockInsertStart(true)
		}
		r := m.selection()
		m.leaveVisual()
		end := r.end
		if !r.lines && end > r.start {
			end--
		}
		return m.startInsert("A", LineEnd(x.t, end))
	}},
	":": {"opens the command line with the selected lines as its range", func(m *Machine, _ visualCall) error {
		m.leaveVisual()
		m.enterLine(Normal, ":", "'<,'>")
		return nil
	}},
	"o": {"moves the cursor to the other end of the selection", otherEnd},
	"<C-n>": {"places a multicursor and selects the next match", func(m *Machine, _ visualCall) error {
		return m.addCursor()
	}},
	"Q": {"places a multicursor on each selected line, with follow on", func(m *Machine, _ visualCall) error {
		m.eachLine()
		return nil
	}},
	"O":      {"moves the cursor to the other end of the selection, or of a block's line", otherEnd},
	"<C-a>":  {"adds n to the first number on each selected line", incrementSelection},
	"<C-x>":  {"subtracts n from the first number on each selected line", incrementSelection},
	"g<C-a>": {"adds n, 2n, 3n and on to the first number on each selected line", incrementSelection},
	"g<C-x>": {"subtracts n, 2n, 3n and on from the first number on each selected line", incrementSelection},
	"*":      {"searches forward for the selected text", searchSelection},
	"#":      {"searches back for the selected text", searchSelection},
	"J":      {"joins the selected lines, with spaces", joinSelection},
	"gJ":     {"joins the selected lines, as they are", joinSelection},
	"p": {"replaces the selection with the register, n times", func(m *Machine, x visualCall) error {
		return m.putOver(x.reg, false, max(x.count, 1))
	}},
	"P": {"replaces the selection with the register, n times, and keeps the register", func(m *Machine, x visualCall) error {
		return m.putOver(x.reg, true, max(x.count, 1))
	}},
	"r{char}": {"replaces every selected character with char", func(m *Machine, x visualCall) error {
		return m.replaceSelection(Key(x.cmd[1:]))
	}},
}

// switchVisual is v, V and <C-v>, which switch to their kind of selection,
// or leave visual mode when it is the one shown.
func switchVisual(m *Machine, x visualCall) error {
	if Key(x.cmd) == m.visual {
		m.leaveVisual()
		m.h.SetCursor(clamp(x.t, x.c))
	}
	m.visual = Key(x.cmd)
	return nil
}

// otherEnd is o, which moves the cursor to the selection's other end, and
// O, which in a block moves it to the other corner on its line.
func otherEnd(m *Machine, x visualCall) error {
	if x.cmd == "o" || m.visual != "<C-v>" {
		m.anchor, x.c = x.c, m.anchor
		m.h.SetCursor(x.c)
		return nil
	}
	tab := m.h.TabWidth()
	as, cs := LineStart(x.t, m.anchor), LineStart(x.t, x.c)
	ac, cc := column(x.t, as, m.anchor, tab), column(x.t, cs, x.c, tab)
	m.anchor = atColumn(x.t, as, cc, tab)
	m.h.SetCursor(atColumn(x.t, cs, ac, tab))
	return nil
}

// incrementSelection is <C-a> and <C-x>, which add n to the first number
// of each selected line, or subtract it, and g<C-a> and g<C-x>, which add
// n to the first number, 2n to the second and on.
func incrementSelection(m *Machine, x visualCall) error {
	n := max(x.count, 1)
	if strings.HasSuffix(x.cmd, "<C-x>") {
		n = -n
	}
	r := m.selection()
	m.leaveVisual()
	var edits []Edit
	for _, rg := range m.lineRanges(x.t, r) {
		by := n
		if x.cmd[0] == 'g' {
			by = n * (len(edits) + 1)
		}
		ls := LineStart(x.t, rg[0])
		e, ok, err := numberEdit(x.t, ls, rg[0]-ls, by)
		if err != nil {
			return err
		}
		if ok && e.Start < rg[1] {
			edits = append(edits, e)
		}
	}
	if len(edits) == 0 {
		m.beep = true
		return nil
	}
	if err := m.apply(edits, "<C-a>"); err != nil {
		return err
	}
	m.h.SetCursor(r.start)
	return nil
}

// searchSelection is * and #, which search for the selected text as it is.
func searchSelection(m *Machine, x visualCall) error {
	r := m.selection()
	m.leaveVisual()
	pat := strings.ReplaceAll(strings.ReplaceAll(x.t.Slice(r.start, r.end), `\`, `\\`), "\n", `\n`)
	m.search.pattern, m.search.forward, m.search.offset = `\V`+pat, x.cmd == "*", ""
	// The search starts at the selection's start, so # finds the match
	// before it, not the selection.
	m.h.SetCursor(r.start)
	p, k := m.seek(r.start, m.search.pattern, m.search.forward, max(x.count, 1))
	if k != failed {
		m.h.SetCursor(clamp(x.t, p))
	}
	return nil
}

// joinSelection is J and gJ, which join the selected lines.
func joinSelection(m *Machine, x visualCall) error {
	r := m.selection()
	m.leaveVisual()
	last := r.end
	if !r.lines && !r.block && last > r.start {
		last--
	}
	m.h.SetCursor(r.start)
	return m.join(max(x.t.LineOf(last)-x.t.LineOf(r.start), 1), x.cmd == "J")
}

// leaveSelection is <Esc> and <C-c> in visual mode.
func leaveSelection(m *Machine, x visualCall) error {
	m.leaveVisual()
	m.h.SetCursor(clamp(x.t, x.c))
	return nil
}

// visualKey handles a command in visual mode.
func (m *Machine) visualKey(cmd string, count int, reg rune) error {
	t, c := m.h.Text(), m.h.Cursor()
	if name, ok := strings.CutPrefix(cmd, "<Plug>("); ok {
		// A host's command runs with the selection kept, as a mapping in
		// visual mode runs it.
		return m.plug(strings.TrimSuffix(name, ")"), count, false)
	}
	if _, ok := m.ops[cmd]; ok {
		r := m.selection()
		m.leaveVisual()
		return m.act(cmd, reg, string(m.visual)+cmd, r)
	}
	if vk, ok := visuals[cmd]; ok {
		return vk.run(m, visualCall{t, c, cmd, count, reg})
	}
	if kc, ok := m.keyed[Visual][cmd]; ok {
		r, keys := m.selection(), string(m.visual)+cmd
		m.leaveVisual()
		return m.runKeyed(kc, keys, r)
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
		if v.op == ">" || v.op == "<" {
			if err := m.shift(v.op, string(m.visual)+cmd, r, max(count, 1)); err != nil {
				return err
			}
			// The selection stays, so the next > or < shifts it again.
			m.reselect()
			return nil
		}
		if err := m.act(v.op, reg, string(m.visual)+cmd, r); err != nil {
			return err
		}
		if v.op == "y" && r.lines {
			m.h.SetCursor(r.start)
		}
		return nil
	}
	if len(cmd) > 1 && cmd[0] == 'r' && Key(cmd[1:]).isChar() {
		return visuals["r{char}"].run(m, visualCall{t, c, cmd, count, reg})
	}
	if r, ok := m.object(t, c, count, cmd); ok {
		r = m.grow(t, count, cmd, r)
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
	// A scroll moves the cursor, and the selection ends at the cursor.
	if ok, err := m.scroll(cmd, count); ok {
		return err
	}
	if p, k, ok := m.motion(cmd, count, ""); ok {
		if k == failed {
			m.beep = true
		} else {
			m.h.SetCursor(m.land(t, p))
		}
	}
	return nil
}

// grow widens r, what one of the machine's own objects selects at the
// cursor, when the selection is more than the cursor's character. A word,
// sentence or paragraph object adds the next ones after the selection, and a
// pair, quote or tag takes the next one out while r is inside it.
func (m *Machine) grow(t Text, count int, cmd string, r region) region {
	if _, ok := textObjects[cmd[1:]]; !ok || m.anchor == m.h.Cursor() {
		return r
	}
	sel := m.selection()
	if strings.Contains("wWsp", cmd[1:]) {
		from := sel.end
		if sel.lines {
			from++
		}
		more, ok := textObject(t, from, count, cmd)
		if from >= t.Len() || !ok {
			return r
		}
		return region{start: sel.start, end: more.end, lines: more.lines}
	}
	for n := max(count, 1) + 1; r.start >= sel.start && r.end <= sel.end; n++ {
		out, ok := textObject(t, m.h.Cursor(), n, cmd)
		if !ok || out == r {
			break
		}
		r = out
	}
	return r
}
