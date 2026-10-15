package vim

import (
	"fmt"
	"strings"
	"unicode"
)

// register is yanked or deleted text.
type register struct {
	text     string // a linewise register's text ends in a newline
	linewise bool
}

// operate applies d, c or y to the text cmd covers. cmd is a motion, or the
// operator again for whole lines.
func (m *Machine) operate(op string, reg rune, cmd string, count int) error {
	t, c := m.h.Text(), m.h.Cursor()
	n := max(count, 1)
	start, end, lines := LineStart(t, c), 0, true
	if cmd == op {
		end = LineEnd(t, down(t, start, n-1))
	} else {
		var to int
		var k kind
		switch big := cmd == "W"; {
		case op == "c" && (cmd == "w" || cmd == "W") && class(t, c, big) != 0:
			to, k = wordEnd(t, c, n, big, true), inclusive
		case cmd == "w" || cmd == "W":
			to, k = wordForward(t, c, n, big, true), exclusive
		default:
			var ok bool
			if to, k, ok = m.motion(cmd, count); !ok || k == failed {
				return nil
			}
		}
		start, end, lines = min(c, to), max(c, to), k == linewise
		switch k {
		case linewise:
			start, end = LineStart(t, start), LineEnd(t, end)
		case inclusive:
			end = next(t, end, LineEnd(t, end))
		case exclusive:
			// An exclusive motion ending in column 0 stops at the end of the
			// line before, and covers whole lines if it started at or before
			// the first non-blank.
			if end > start && end == LineStart(t, end) {
				end--
				if s := LineStart(t, start); start <= firstNonBlank(t, s, LineEnd(t, s)) {
					start, lines = s, true
				}
			}
		}
		if start == end && !lines {
			return nil
		}
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
		if err := m.h.Apply([]Edit{{Start: start, End: end, Text: indent}}, label); err != nil {
			return err
		}
		m.h.SetCursor(start + len(indent))
		return nil
	case op == "c":
		m.startInsert(Key(label), start)
		return m.h.Apply([]Edit{{Start: start, End: end}}, label)
	}

	col := column(t, LineStart(t, c), c)
	if lines {
		// Deleting lines also deletes the newline after them, or the one before
		// on the last line.
		if end < t.Len() {
			end++
		} else if start > 0 {
			start--
		}
	}
	if err := m.h.Apply([]Edit{{Start: start, End: end}}, label); err != nil {
		return err
	}
	t = m.h.Text()
	if lines {
		m.h.SetCursor(atColumn(t, LineStart(t, start), col))
	} else {
		m.h.SetCursor(clamp(t, start))
	}
	return nil
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
	if err := m.h.Apply([]Edit{{Start: at, End: at, Text: text}}, label); err != nil {
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
