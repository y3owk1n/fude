package vim

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

func (m *Machine) insert(k Key) error {
	t, c := m.h.Text(), m.h.Cursor()
	if m.pending == "<C-r>" {
		m.pending = ""
		name, _ := utf8.DecodeRuneInString(string(k))
		if r, ok := m.regs[unicode.ToLower(name)]; ok && k.isChar() {
			return m.typeText(r.text)
		}
		return nil
	}
	switch k {
	case "<Esc>":
		if b := m.blockInsert; b != nil {
			m.blockInsert = nil
			if err := m.finishBlock(b); err != nil {
				return err
			}
		}
		if err := m.dropIndent(); err != nil {
			return err
		}
		t, c = m.h.Text(), m.h.Cursor()
		m.h.EndGroup()
		m.mode = Normal
		m.h.SetCursor(prev(t, LineStart(t, c), c))
	case "<CR>":
		return m.newline()
	case "<BS>":
		if m.mode == Replace {
			return m.unreplace()
		}
		return m.erase(m.backspace(t, c))
	case "<C-w>", "<C-u>":
		// Neovim maps both to start a new undo step first.
		m.h.EndGroup()
		m.h.BeginGroup()
		p := wordBefore(t, c, m.insStart)
		if k == "<C-u>" {
			p = lineBefore(t, c, m.insStart)
		}
		m.insStart = min(m.insStart, p)
		return m.erase(p)
	case "<C-r>":
		m.pending = k
	case "<C-o>":
		m.h.EndGroup()
		m.mode, m.oneshot, m.armed = Normal, true, false
	case "<C-t>", "<C-d>":
		return m.indentLine(k == "<C-t>")
	case "<Left>", "<Right>":
		n := prev(t, LineStart(t, c), c)
		if k == "<Right>" {
			n = next(t, c, LineEnd(t, c))
		}
		// A move splits the undo step, as in Neovim.
		if n != c {
			m.h.EndGroup()
			m.h.BeginGroup()
			m.h.SetCursor(n)
			m.insStart = n
		}
	default:
		if k.isChar() {
			return m.typeText(string(k))
		}
	}
	return nil
}

func (m *Machine) startInsert(k Key, pos int) {
	m.mode = Insert
	m.label = string(k)
	m.h.BeginGroup()
	m.h.SetCursor(pos)
	m.insStart, m.autoIndent = pos, false
}

// typeText inserts text the user typed or pasted.
func (m *Machine) typeText(s string) error {
	m.autoIndent = false
	return m.insertText(s)
}

func (m *Machine) insertText(s string) error {
	if m.mode == Replace {
		return m.overwrite(s)
	}
	c := m.h.Cursor()
	if err := m.apply([]Edit{{Start: c, End: c, Text: s}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(c + len(s))
	return nil
}

// erase deletes from p to the cursor.
func (m *Machine) erase(p int) error {
	c := m.h.Cursor()
	if p >= c {
		return nil
	}
	if err := m.apply([]Edit{{Start: p, End: c}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(p)
	return nil
}

// backspace returns where <BS> deletes back to. In leading blanks it removes
// one shiftwidth, as smarttab does, and at a line start it joins the lines.
func (m *Machine) backspace(t Text, c int) int {
	ls := LineStart(t, c)
	if c == ls {
		return max(c-1, 0)
	}
	if strings.TrimLeft(t.Slice(ls, c), " \t") == "" {
		target := (column(t, ls, c) - 1) / shiftwidth * shiftwidth
		p := c
		for p > ls && column(t, ls, p) > target {
			p = prev(t, ls, p)
		}
		return p
	}
	return prev(t, ls, c)
}

// wordBefore returns where <C-w> deletes back to: the start of the word
// before c, past any blanks, stopping once at where typing began.
func wordBefore(t Text, c, start int) int {
	ls := LineStart(t, c)
	if c == ls {
		return max(c-1, 0)
	}
	p := c
	for p > ls && class(t, prev(t, ls, p), false) == 0 {
		p = prev(t, ls, p)
	}
	if p > ls {
		k := class(t, prev(t, ls, p), false)
		for p > ls && class(t, prev(t, ls, p), false) == k {
			p = prev(t, ls, p)
		}
	}
	if c > start && p < start && start >= ls {
		p = start
	}
	return p
}

// lineBefore returns where <C-u> deletes back to: where typing began on this
// line, or else the indent, or else the line start.
func lineBefore(t Text, c, start int) int {
	ls := LineStart(t, c)
	switch fnb := firstNonBlank(t, ls, LineEnd(t, ls)); {
	case c > start && start >= ls:
		return start
	case c > fnb:
		return fnb
	case c > ls:
		return ls
	}
	return max(c-1, 0)
}

// newline breaks the line at the cursor. The new line copies the indent of
// the current one, and an indent nothing was typed after goes away, as
// autoindent does.
func (m *Machine) newline() error {
	if m.mode == Replace {
		return m.overwrite("\n")
	}
	t, c := m.h.Text(), m.h.Cursor()
	ls := LineStart(t, c)
	indent := t.Slice(ls, min(firstNonBlank(t, ls, LineEnd(t, ls)), c))
	if err := m.dropIndent(); err != nil {
		return err
	}
	if err := m.insertText("\n" + indent); err != nil {
		return err
	}
	m.autoIndent = indent != ""
	return nil
}

// openAbove opens a line above the cursor's for O, with the same indent.
func (m *Machine) openAbove() error {
	t, c := m.h.Text(), m.h.Cursor()
	indent := t.Slice(c, firstNonBlank(t, c, LineEnd(t, c)))
	if err := m.apply([]Edit{{Start: c, End: c, Text: indent + "\n"}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(c + len(indent))
	m.autoIndent = indent != ""
	return nil
}

// dropIndent removes an autoindent nothing was typed after.
func (m *Machine) dropIndent() error {
	if !m.autoIndent {
		return nil
	}
	m.autoIndent = false
	t, c := m.h.Text(), m.h.Cursor()
	ls, le := LineStart(t, c), LineEnd(t, c)
	if c != le || ls == le || strings.TrimLeft(t.Slice(ls, le), " \t") != "" {
		return nil
	}
	if err := m.apply([]Edit{{Start: ls, End: le}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(ls)
	return nil
}

// indentLine shifts the cursor's line one shiftwidth in for <C-t>, or out for
// <C-d>, keeping the cursor on the same text.
func (m *Machine) indentLine(in bool) error {
	t, c := m.h.Text(), m.h.Cursor()
	ls := LineStart(t, c)
	ind := firstNonBlank(t, ls, LineEnd(t, ls))
	w := column(t, ls, ind)
	if in {
		w += shiftwidth
	} else {
		w = max(w-shiftwidth, 0)
	}
	s := indentText(w)
	if err := m.apply([]Edit{{Start: ls, End: ind, Text: s}}, m.label); err != nil {
		return err
	}
	if c >= ind {
		m.h.SetCursor(c + len(s) - (ind - ls))
	} else {
		m.h.SetCursor(ls + len(s))
	}
	return nil
}
