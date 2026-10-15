package vim

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// region is the text an operator covers.
type region struct {
	start, end int
	lines      bool
	// adjusted marks an exclusive motion that became linewise. A delete then
	// puts the cursor on the first non-blank instead of keeping the column.
	adjusted bool
	// block selects display columns left to right, inclusive, of every line
	// from start to end. right is math.MaxInt after $.
	block       bool
	left, right int
}

// register is yanked or deleted text.
type register struct {
	text     string // a linewise register's text ends in a newline
	linewise bool
	block    bool // one line of the text per line of the block
}

// operate applies d, c or y to the text cmd covers. cmd is a motion, a text
// object, or the operator again for whole lines.
func (m *Machine) operate(op string, reg rune, cmd string, count int) error {
	t, c := m.h.Text(), m.h.Cursor()
	r, ok := m.span(t, c, op, cmd, count)
	if !ok {
		m.beep = true
		return nil
	}
	return m.act(op, reg, op+cmd, r)
}

// act applies d, c or y to r.
func (m *Machine) act(op string, reg rune, label string, r region) error {
	if r.block {
		return m.actBlock(op, reg, label, r)
	}
	if r.start == r.end && !r.lines {
		if op == "c" {
			m.startInsert(Key(label), r.start)
		}
		return nil
	}
	t, c := m.h.Text(), m.h.Cursor()
	start, end, lines := r.start, r.end, r.lines
	col := column(t, LineStart(t, c), c)
	if m.wantSet {
		col = m.want
	}
	m.wantSet = false
	switch op {
	case "g~", "gu", "gU":
		return m.changeCase(op, label, r)
	case ">", "<":
		return m.shift(op, label, r, col)
	}

	text := t.Slice(start, end)
	if lines {
		text += "\n"
	}
	m.store(reg, op, register{text: text, linewise: lines})
	switch {
	case op == "y" && lines:
		if s := LineStart(t, start); s != LineStart(t, c) {
			m.h.SetCursor(atColumn(t, s, column(t, LineStart(t, c), c)))
		}
		return nil
	case op == "y":
		m.h.SetCursor(start)
		return nil
	case op == "c" && lines:
		indent := t.Slice(start, firstNonBlank(t, start, LineEnd(t, start)))
		m.startInsert(Key(label), start)
		if err := m.apply([]Edit{{Start: start, End: end, Text: indent}}, label); err != nil {
			return err
		}
		m.h.SetCursor(start + len(indent))
		return nil
	case op == "c":
		m.startInsert(Key(label), start)
		return m.apply([]Edit{{Start: start, End: end}}, label)
	}

	if lines {
		// Deleting lines also deletes the newline after them, or the one before
		// on the last line.
		if end < t.Len() {
			end++
		} else if start > 0 {
			start--
		}
	}
	if err := m.apply([]Edit{{Start: start, End: end}}, label); err != nil {
		return err
	}
	t = m.h.Text()
	switch s := LineStart(t, start); {
	case lines && r.adjusted:
		m.h.SetCursor(firstNonBlank(t, s, LineEnd(t, s)))
	case lines:
		m.h.SetCursor(atColumn(t, s, col))
	default:
		m.h.SetCursor(clamp(t, start))
	}
	return nil
}

// span returns the text an operator covers, or false when cmd covers none.
func (m *Machine) span(t Text, c int, op, cmd string, count int) (region, bool) {
	n := max(count, 1)
	if cmd == op || len(op) == 2 && cmd == op[1:] {
		s := LineStart(t, c)
		return region{start: s, end: LineEnd(t, down(t, s, n-1)), lines: true}, true
	}
	if r, ok := textObject(t, c, count, cmd); ok {
		return r, true
	}
	var to int
	var k kind
	var ok bool
	switch big := cmd == "W"; {
	case op == "c" && (cmd == "w" || cmd == "W") && class(t, c, big) != 0:
		to, k = wordEnd(t, c, n, big, true), inclusive
	case cmd == "w" || cmd == "W":
		to, k = wordForward(t, c, n, big, true), exclusive
	default:
		if to, k, ok = m.motion(cmd, count); !ok || k == failed {
			return region{}, false
		}
	}
	r := region{start: min(c, to), end: max(c, to), lines: k == linewise}
	switch k {
	case linewise:
		r.start, r.end = LineStart(t, r.start), LineEnd(t, r.end)
	case inclusive:
		r.end = next(t, r.end, LineEnd(t, r.end))
	case exclusive:
		// An exclusive motion ending in column 0 stops at the end of the line
		// before, and covers whole lines if it started at or before the first
		// non-blank.
		if r.end > r.start && r.end == LineStart(t, r.end) {
			r.end--
			if s := LineStart(t, r.start); r.start <= firstNonBlank(t, s, LineEnd(t, s)) {
				r.start, r.lines, r.adjusted = s, true, true
			}
		}
	}
	return r, true
}

// store saves r in reg, or in the registers Neovim uses when reg is 0: "0 for
// a yank, "1 to "9 for a deleted line, "- for a smaller delete.
func (m *Machine) store(reg rune, op string, r register) {
	if m.regs == nil {
		m.regs = map[rune]register{}
	}
	switch {
	case reg == '_':
		return
	case unicode.IsUpper(reg):
		reg = unicode.ToLower(reg)
		if old, ok := m.regs[reg]; ok {
			r = appendRegister(old, r)
		}
		m.regs[reg] = r
	case reg != 0:
		m.regs[reg] = r
	case op == "y":
		m.regs['0'] = r
	case r.linewise || strings.Contains(r.text, "\n"):
		for i := '9'; i > '1'; i-- {
			if old, ok := m.regs[i-1]; ok {
				m.regs[i] = old
			}
		}
		m.regs['1'] = r
	default:
		m.regs['-'] = r
	}
	m.regs['"'] = r
}

// appendRegister adds r to old. Mixing a linewise register with a charwise
// one makes it linewise.
func appendRegister(old, r register) register {
	if !old.linewise && !r.linewise {
		return register{text: old.text + r.text}
	}
	line := func(s string) string {
		if !strings.HasSuffix(s, "\n") {
			return s + "\n"
		}
		return s
	}
	return register{text: line(old.text) + line(r.text), linewise: true}
}

// put inserts reg n times, after the cursor for p and before it for P.
func (m *Machine) put(reg rune, after bool, n int) error {
	if reg == 0 {
		reg = '"'
	}
	r, ok := m.regs[unicode.ToLower(reg)]
	if !ok {
		return fmt.Errorf("E353: Nothing in register %c", reg)
	}
	label := "P"
	if after {
		label = "p"
	}
	if r.block {
		return m.putBlock(r.text, after, n, label)
	}
	t, c := m.h.Text(), m.h.Cursor()
	text := strings.Repeat(r.text, n)
	at := c
	switch {
	case r.linewise && after:
		at = LineEnd(t, c)
		text = "\n" + strings.TrimSuffix(text, "\n")
	case r.linewise:
		at = LineStart(t, c)
	case after:
		at = next(t, c, LineEnd(t, c))
	}
	if err := m.apply([]Edit{{Start: at, End: at, Text: text}}, label); err != nil {
		return err
	}
	t = m.h.Text()
	switch {
	case r.linewise && after:
		m.h.SetCursor(firstNonBlank(t, at+1, LineEnd(t, at+1)))
	case r.linewise:
		m.h.SetCursor(firstNonBlank(t, at, LineEnd(t, at)))
	case strings.Contains(text, "\n"):
		m.h.SetCursor(at)
	default:
		m.h.SetCursor(lastChar(t, at+len(text)))
	}
	return nil
}

// actBlock applies d, c or y to the columns of a block. c inserts on the
// first line, and leaving insert mode copies the text to the other lines.
func (m *Machine) actBlock(op string, reg rune, label string, r region) error {
	t := m.h.Text()
	var texts []string
	var edits []Edit
	for ls := r.start; ; {
		a, b := colStart(t, ls, r.left), LineEnd(t, ls)
		if r.right != math.MaxInt {
			b = colStart(t, ls, r.right+1)
		}
		texts = append(texts, t.Slice(a, b))
		if a < b {
			edits = append(edits, Edit{Start: a, End: b})
		}
		le := LineEnd(t, ls)
		if le >= r.end || le == t.Len() {
			break
		}
		ls = le + 1
	}
	m.store(reg, op, register{text: strings.Join(texts, "\n"), block: true})
	top := colStart(t, r.start, r.left)
	switch op {
	case "y":
		m.h.SetCursor(top)
		return nil
	case "c":
		m.startInsert(Key(label), top)
		m.blockInsert = &blockInsert{start: top, col: r.left, lines: len(texts) - 1}
	}
	if len(edits) > 0 {
		if err := m.apply(edits, label); err != nil {
			return err
		}
	}
	if op == "d" {
		m.h.SetCursor(clamp(m.h.Text(), top))
	}
	return nil
}

// blockInsert is a block c waiting for insert mode to end.
type blockInsert struct {
	start, col, lines int
}

// finishBlock copies text typed on the first line of a block c to the same
// column of the block's other lines, skipping lines too short to reach it.
func (m *Machine) finishBlock(b *blockInsert) error {
	t, c := m.h.Text(), m.h.Cursor()
	if c <= b.start || LineStart(t, c) != LineStart(t, b.start) {
		return nil
	}
	typed := t.Slice(b.start, c)
	var edits []Edit
	ls := LineStart(t, b.start)
	for range b.lines {
		if LineEnd(t, ls) == t.Len() {
			break
		}
		ls = LineEnd(t, ls) + 1
		if p := colStart(t, ls, b.col); column(t, ls, p) == b.col {
			edits = append(edits, Edit{Start: p, End: p, Text: typed})
		}
	}
	if len(edits) == 0 {
		return nil
	}
	return m.apply(edits, m.label)
}

// putBlock puts a block n times wide, after the cursor for p and at it for P.
// It pads short lines with spaces, pads the block to a rectangle when text
// follows it, and adds missing lines at the end.
func (m *Machine) putBlock(text string, after bool, n int, label string) error {
	t, c := m.h.Text(), m.h.Cursor()
	col := column(t, LineStart(t, c), c)
	if after && c < LineEnd(t, c) {
		col += textWidth(t.Slice(c, next(t, c, LineEnd(t, c))), col)
	}
	lines := strings.Split(text, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, textWidth(strings.Repeat(l, n), 0))
	}
	var edits []Edit
	tail, top := "", 0
	ls := LineStart(t, c)
	for i, l := range lines {
		l = strings.Repeat(l, n)
		if i > 0 {
			if LineEnd(t, ls) == t.Len() {
				tail += "\n" + strings.Repeat(" ", col) + l
				continue
			}
			ls = LineEnd(t, ls) + 1
		}
		p := colStart(t, ls, col)
		pad := strings.Repeat(" ", col-column(t, ls, p))
		if i == 0 {
			top = p + len(pad)
		}
		s := pad + l
		if p < LineEnd(t, ls) {
			s += strings.Repeat(" ", w-textWidth(l, 0))
		}
		edits = append(edits, Edit{Start: p, End: p, Text: s})
	}
	if tail != "" {
		edits = append(edits, Edit{Start: t.Len(), End: t.Len(), Text: tail})
	}
	if err := m.apply(edits, label); err != nil {
		return err
	}
	m.h.SetCursor(top)
	return nil
}
