package vim

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// aliases are the commands that are an operator and a target.
var aliases = map[string]string{
	"x": "dl", "X": "dh", "s": "cl", "S": "cc", "D": "d$", "C": "c$", "Y": "y$",
}

// operators are the keys that wait for a target.
var operators = map[string]bool{
	"d": true, "c": true, "y": true, "g~": true, "gu": true, "gU": true, ">": true, "<": true,
}

// edit runs a command that changes text without an operator, or reports
// false if cmd is not one.
func (m *Machine) edit(cmd string, count int) (bool, error) {
	n := max(count, 1)
	switch {
	case cmd == "~":
		return true, m.toggleCase(n)
	case cmd == "J" || cmd == "gJ":
		return true, m.join(max(count, 2)-1, cmd == "J")
	case cmd == "<C-a>":
		return true, m.increment(n)
	case cmd == "<C-x>":
		return true, m.increment(-n)
	case cmd == "R":
		m.startInsert("R", m.h.Cursor())
		m.mode, m.replaced = Replace, nil
		return true, nil
	case len(cmd) > 1 && cmd[0] == 'r':
		return true, m.replaceChars(n, Key(cmd[1:]))
	}
	return false, nil
}

// toggleCase switches the case of n characters and moves past them.
func (m *Machine) toggleCase(n int) error {
	t, c := m.h.Text(), m.h.Cursor()
	end := c
	for range n {
		end = next(t, end, LineEnd(t, c))
	}
	if end == c {
		return nil
	}
	s := swapCase(t.Slice(c, end))
	if err := m.apply([]Edit{{Start: c, End: end, Text: s}}, "~"); err != nil {
		return err
	}
	m.h.SetCursor(clamp(m.h.Text(), c+len(s)))
	return nil
}

// join joins n lines below the cursor onto its line. With spaces set, as for
// J, it drops their indent and puts one space between, except after blanks,
// before ")" and next to an empty line. The cursor goes to the last join.
func (m *Machine) join(n int, spaces bool) error {
	t, c := m.h.Text(), m.h.Cursor()
	var edits []Edit
	cur, shift := -1, 0
	for ls := LineStart(t, c); n > 0; n-- {
		le := LineEnd(t, ls)
		if le == t.Len() {
			break
		}
		next := le + 1
		e := Edit{Start: le, End: next}
		if spaces {
			e.End = firstNonBlank(t, next, LineEnd(t, next))
			last := t.Slice(max(le-1, ls), le)
			if le > ls && e.End < LineEnd(t, next) && at(t, e.End) != ')' && last != " " && last != "\t" {
				e.Text = " "
			}
		}
		edits = append(edits, e)
		cur = e.Start + shift
		shift += len(e.Text) - (e.End - e.Start)
		ls = next
	}
	if len(edits) == 0 {
		m.beep = true
		return nil
	}
	if err := m.apply(edits, "J"); err != nil {
		return err
	}
	m.h.SetCursor(clamp(m.h.Text(), cur))
	return nil
}

// replaceChars replaces n characters with k, or with one line break for
// <CR>. It fails when the line has fewer than n characters left.
func (m *Machine) replaceChars(n int, k Key) error {
	if k != "<CR>" && !k.isChar() {
		return nil
	}
	t, c := m.h.Text(), m.h.Cursor()
	end := c
	for range n {
		if end >= LineEnd(t, c) {
			m.beep = true
			return nil
		}
		end = next(t, end, LineEnd(t, c))
	}
	s := strings.Repeat(string(k), n)
	if k == "<CR>" {
		s = "\n"
	}
	if err := m.apply([]Edit{{Start: c, End: end, Text: s}}, "r"+string(k)); err != nil {
		return err
	}
	if k == "<CR>" {
		m.h.SetCursor(c + 1)
	} else {
		m.h.SetCursor(c + len(s) - len(k))
	}
	return nil
}

// overwrite types s over the character under the cursor in replace mode, or
// adds it at the end of the line.
func (m *Machine) overwrite(s string) error {
	t, c := m.h.Text(), m.h.Cursor()
	end := c
	if s != "\n" {
		end = next(t, c, LineEnd(t, c))
	}
	m.replaced = append(m.replaced, t.Slice(c, end))
	if err := m.apply([]Edit{{Start: c, End: end, Text: s}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(c + len(s))
	return nil
}

// unreplace undoes the last overwrite for <BS> in replace mode, or moves
// left once nothing typed is left to undo.
func (m *Machine) unreplace() error {
	t, c := m.h.Text(), m.h.Cursor()
	p := prev(t, 0, c)
	if len(m.replaced) == 0 {
		m.h.SetCursor(max(p, LineStart(t, c)))
		return nil
	}
	orig := m.replaced[len(m.replaced)-1]
	m.replaced = m.replaced[:len(m.replaced)-1]
	if err := m.apply([]Edit{{Start: p, End: c, Text: orig}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(p)
	return nil
}

// increment adds n to the decimal, hex or binary number at or after the
// cursor on its line, keeping its width, and puts the cursor on its last
// digit. A "-" right before a decimal makes it negative.
func (m *Machine) increment(n int) error {
	t, c := m.h.Text(), m.h.Cursor()
	ls := LineStart(t, c)
	line := t.Slice(ls, LineEnd(t, c))
	s, e, base := findNumber(line, c-ls)
	if s < 0 {
		m.beep = true
		return nil
	}
	num := line[s:e]
	var out string
	if base == 10 {
		v, err := strconv.ParseInt(num, 10, 64)
		if err != nil {
			return fmt.Errorf("increment %s: %w", num, err)
		}
		digits := strings.TrimPrefix(num, "-")
		out = strconv.FormatInt(v+int64(n), 10)
		if len(digits) > 1 && digits[0] == '0' {
			neg := strings.HasPrefix(out, "-")
			out = strings.TrimPrefix(out, "-")
			out = strings.Repeat("0", max(len(digits)-len(out), 0)) + out
			if neg {
				out = "-" + out
			}
		}
	} else {
		prefix, digits := num[:2], num[2:]
		v, err := strconv.ParseUint(digits, base, 64)
		if err != nil {
			return fmt.Errorf("increment %s: %w", num, err)
		}
		out = strconv.FormatUint(v+uint64(n), base)
		if strings.ToUpper(digits) == digits && strings.ToLower(digits) != digits {
			out = strings.ToUpper(out)
		}
		out = prefix + strings.Repeat("0", max(len(digits)-len(out), 0)) + out
	}
	if err := m.apply([]Edit{{Start: ls + s, End: ls + e, Text: out}}, "<C-a>"); err != nil {
		return err
	}
	m.h.SetCursor(ls + s + len(out) - 1)
	return nil
}

// findNumber returns the first number in line that ends after col, and its
// base, or -1.
func findNumber(line string, col int) (int, int, int) {
	isDigit := func(b byte, base int) bool {
		switch base {
		case 2:
			return b == '0' || b == '1'
		case 16:
			return strings.IndexByte("0123456789abcdefABCDEF", b) >= 0
		}
		return b >= '0' && b <= '9'
	}
	for p := 0; p < len(line); p++ {
		if !isDigit(line[p], 10) {
			continue
		}
		s, e, base := p, p, 10
		if line[p] == '0' && p+2 < len(line) {
			switch line[p+1] {
			case 'x', 'X':
				base = 16
			case 'b', 'B':
				base = 2
			}
			if base != 10 && isDigit(line[p+2], base) {
				e = p + 2
			} else {
				base = 10
			}
		}
		for e < len(line) && isDigit(line[e], base) {
			e++
		}
		if base == 10 && s > 0 && line[s-1] == '-' {
			s--
		}
		if e > col {
			return s, e, base
		}
		p = e
	}
	return -1, -1, 0
}

// changeCase applies g~, gu or gU to r and puts the cursor at its start.
func (m *Machine) changeCase(op, label string, r region) error {
	t := m.h.Text()
	f := swapCase
	switch op {
	case "gu":
		f = strings.ToLower
	case "gU":
		f = strings.ToUpper
	}
	ranges := lineRanges(t, r)
	var edits []Edit
	for _, rg := range ranges {
		if rg[0] < rg[1] {
			edits = append(edits, Edit{Start: rg[0], End: rg[1], Text: f(t.Slice(rg[0], rg[1]))})
		}
	}
	if len(edits) > 0 {
		if err := m.apply(edits, label); err != nil {
			return err
		}
	}
	m.h.SetCursor(ranges[0][0])
	return nil
}

// shift indents the lines of r by times the indent width for >, or outdents
// them for <, rebuilding the indent from tabs and spaces. Empty lines stay
// empty.
func (m *Machine) shift(op, label string, r region, col, times int) error {
	t := m.h.Text()
	var edits []Edit
	for ls := LineStart(t, r.start); ; {
		le := LineEnd(t, ls)
		if ls < le {
			ind := firstNonBlank(t, ls, le)
			w := column(t, ls, ind)
			if op == ">" {
				w += m.h.IndentWidth() * times
			} else {
				w = max(w-m.h.IndentWidth()*times, 0)
			}
			edits = append(edits, Edit{Start: ls, End: ind, Text: m.indentText(w)})
		}
		if le >= r.end || le == t.Len() {
			break
		}
		ls = le + 1
	}
	if len(edits) > 0 {
		if err := m.apply(edits, label); err != nil {
			return err
		}
	}
	t = m.h.Text()
	m.h.SetCursor(clamp(t, atColumn(t, LineStart(t, r.start), col)))
	return nil
}

// indentText returns an indent w columns wide, in tabs then spaces, or in
// spaces with ExpandTab.
func (m *Machine) indentText(w int) string {
	if m.expandTab {
		return strings.Repeat(" ", w)
	}
	return strings.Repeat("\t", w/Tabstop) + strings.Repeat(" ", w%Tabstop)
}

// ExpandTab sets whether indents are spaces only, and <Tab> in insert mode
// types spaces to the next indent width's column, as Neovim's expandtab
// does with softtabstop=-1 (B122, N10).
func (m *Machine) ExpandTab(on bool) { m.expandTab = on }

func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}
