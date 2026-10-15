package vim

import (
	"fmt"
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
}

// register is yanked or deleted text.
type register struct {
	text     string // a linewise register's text ends in a newline
	linewise bool
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
	if r.start == r.end && !r.lines {
		return nil
	}
	start, end, lines := r.start, r.end, r.lines
	col := column(t, LineStart(t, c), c)
	if m.wantSet {
		col = m.want
	}
	m.wantSet = false

	text := t.Slice(start, end)
	if lines {
		text += "\n"
	}
	m.store(reg, op, register{text, lines})
	label := op + cmd
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
	if cmd == op {
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
		return register{old.text + r.text, false}
	}
	line := func(s string) string {
		if !strings.HasSuffix(s, "\n") {
			return s + "\n"
		}
		return s
	}
	return register{line(old.text) + line(r.text), true}
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
	t, c := m.h.Text(), m.h.Cursor()
	text := strings.Repeat(r.text, n)
	label := "P"
	if after {
		label = "p"
	}
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
