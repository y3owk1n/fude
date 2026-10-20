package vim

import (
	"strings"
	"unicode/utf8"
)

func (m *Machine) insert(k Key) error {
	t, c := m.h.Text(), m.h.Cursor()
	if k != "<Up>" && k != "<Down>" {
		m.wantSet = false
	}
	if m.pending == "<C-r>" {
		m.pending = ""
		name, _ := utf8.DecodeRuneInString(string(k))
		if !k.isChar() {
			return nil
		}
		r, ok, err := m.fetch(name)
		if err != nil || !ok {
			return err
		}
		return m.typeText(r.text)
	}
	if name, ok := strings.CutPrefix(string(k), "<Plug>("); ok {
		// A host's command, as a mapping in insert mode runs it.
		return m.plug(strings.TrimSuffix(name, ")"), 0, true)
	}
	// A key the host added runs before the key's own work, and with no
	// edits lets the key do it (B323).
	kc, hooked := m.keyed[Insert][string(k)]
	hooked = hooked && m.mode == Insert && !m.asTyped
	if hooked && !k.isChar() {
		if done, err := m.typedKeyed(kc, t, c); done {
			return err
		}
	}
	if ik, ok := inserts[string(k)]; ok {
		return ik.run(m, t, c, k)
	}
	if k == "\t" && m.expandTab {
		iw := m.h.IndentWidth()
		return m.typeText(strings.Repeat(" ", iw-column(t, LineStart(t, c), c, m.h.TabWidth())%iw))
	}
	if k.isChar() {
		if err := m.closeIndent(t, c, string(k)); err != nil {
			return err
		}
		if hooked {
			if done, err := m.typedKeyed(kc, m.h.Text(), m.h.Cursor()); done {
				return err
			}
		}
		return m.typeText(string(k))
	}
	return nil
}

// typedKeyed runs the host's command for a key typed at c. Its edits go in
// as typed text does, with the cursor after the first one's text. It
// reports whether there were any.
func (m *Machine) typedKeyed(kc keyedCommand, t Text, c int) (bool, error) {
	edits := kc.run(t, c, c, false, nil)
	if len(edits) == 0 {
		return false, nil
	}
	m.autoIndent = false
	if err := m.apply(edits, m.label); err != nil {
		return true, err
	}
	m.h.SetCursor(edits[0].Start + len(edits[0].Text))
	return true, nil
}

// insertKey is an insert mode key that does more than type itself.
type insertKey struct {
	help string
	run  func(m *Machine, t Text, c int, k Key) error
}

// inserts are those keys. They work in replace mode too.
var inserts = map[string]insertKey{
	"<Esc>": {"leaves insert mode", func(m *Machine, _ Text, _ int, _ Key) error {
		m.cursors = nil
		if b := m.blockInsert; b != nil {
			m.blockInsert = nil
			if err := m.finishBlock(b); err != nil {
				return err
			}
			m.h.EndGroup()
			m.mode = Normal
			m.h.SetCursor(clamp(m.h.Text(), b.home))
			return nil
		}
		if err := m.dropIndent(); err != nil {
			return err
		}
		t, c := m.h.Text(), m.h.Cursor()
		m.h.EndGroup()
		m.mode = Normal
		m.h.SetMark('^', c)
		m.h.SetCursor(prev(t, LineStart(t, c), c))
		return nil
	}},
	"<CR>":  {"breaks the line", func(m *Machine, _ Text, _ int, _ Key) error { return m.newline() }},
	"<BS>":  {"deletes the character before the cursor", backspaceKey},
	"<C-h>": {"deletes the character before the cursor", backspaceKey},
	"<C-w>": {"deletes the word before the cursor", eraseBack},
	"<C-u>": {"deletes what was typed on the line, or the line before the cursor", eraseBack},
	"<C-r>": {"types the register named next", func(m *Machine, _ Text, _ int, k Key) error {
		m.pending = k
		return nil
	}},
	"<C-o>": {"runs one normal mode command, then comes back", func(m *Machine, _ Text, _ int, _ Key) error {
		// The other cursors of a change on several regions stop here.
		m.cursors = nil
		m.h.EndGroup()
		m.mode, m.oneshot, m.armed = Normal, true, false
		return nil
	}},
	"<C-t>": {"indents the line", indentKey},
	"<C-d>": {"outdents the line", indentKey},
	"<Del>": {"deletes the character under the cursor, or joins the next line", func(m *Machine, t Text, c int, _ Key) error {
		end := next(t, c, LineEnd(t, c))
		if end == c && c < t.Len() {
			end = c + 1 // the line break, which joins the next line
		}
		return m.apply([]Edit{{Start: c, End: end}}, m.label)
	}},
	"<Left>":  {"moves left", moveKey},
	"<Right>": {"moves right", moveKey},
	"<Up>":    {"moves up a line", moveKey},
	"<Down>":  {"moves down a line", moveKey},
	"<Home>":  {"goes to the start of the line", moveKey},
	"<End>":   {"goes to the end of the line", moveKey},
}

// backspaceKey is <BS> and <C-h>, which put back what replace mode typed
// over.
func backspaceKey(m *Machine, t Text, c int, _ Key) error {
	if m.mode == Replace {
		return m.unreplace()
	}
	return m.erase(m.backspace(t, c))
}

// eraseBack is <C-w> and <C-u>, which start a new undo step first.
func eraseBack(m *Machine, t Text, c int, k Key) error {
	m.h.EndGroup()
	m.h.BeginGroup()
	p := wordBefore(t, c, m.insStart)
	if k == "<C-u>" {
		p = lineBefore(t, c, m.insStart)
	}
	m.insStart = min(m.insStart, p)
	return m.erase(p)
}

// indentKey is <C-t> and <C-d>.
func indentKey(m *Machine, _ Text, _ int, k Key) error {
	return m.indentLine(k == "<C-t>")
}

// moveKey is the arrow keys, <Home> and <End>. A move splits the undo step.
func moveKey(m *Machine, t Text, c int, k Key) error {
	n := m.insertMove(t, c, k)
	if n != c {
		m.h.EndGroup()
		m.h.BeginGroup()
		m.h.SetCursor(n)
		m.insStart = n
	}
	return nil
}

// insertMove returns where an arrow key, <Home> or <End> moves the cursor
// from c in insert mode. <Up> and <Down> keep the column, or go to the end
// of a shorter line.
func (m *Machine) insertMove(t Text, c int, k Key) int {
	start := LineStart(t, c)
	switch k {
	case "<Left>":
		return prev(t, start, c)
	case "<Right>":
		return next(t, c, LineEnd(t, c))
	case "<Home>":
		return start
	case "<End>":
		return LineEnd(t, c)
	}
	line := up(t, start, 1)
	if k == "<Down>" {
		line = down(t, start, 1)
	}
	if line == start {
		return c
	}
	if p := m.toLine(t, c, line); column(t, line, LineEnd(t, line), m.h.TabWidth()) > m.want {
		return p
	}
	return LineEnd(t, line)
}

// startInsert starts insert mode at pos for the key k, or returns why the
// text may not change (B302).
func (m *Machine) startInsert(k Key, pos int) error {
	if err := m.h.Editable(); err != nil {
		return err
	}
	m.mode = Insert
	m.label = string(k)
	m.h.BeginGroup()
	m.h.SetCursor(pos)
	m.insStart, m.autoIndent = pos, false
	return nil
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
// one indent width, as smarttab does, and at a line start it joins the lines.
func (m *Machine) backspace(t Text, c int) int {
	tab := m.h.TabWidth()
	ls := LineStart(t, c)
	if c == ls {
		return max(c-1, 0)
	}
	if strings.TrimLeft(t.Slice(ls, c), " \t") == "" {
		iw := m.h.IndentWidth()
		target := (column(t, ls, c, tab) - 1) / iw * iw
		p := c
		for p > ls && column(t, ls, p, tab) > target {
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
// autoindent does. After an opening bracket the new line gets one more
// level, and the bracket's closing one, when it comes next, goes a line
// further down (B202, B211).
func (m *Machine) newline() error {
	if m.mode == Replace {
		return m.overwrite("\n")
	}
	t, c := m.h.Text(), m.h.Cursor()
	ls, le := LineStart(t, c), LineEnd(t, c)
	indent := t.Slice(ls, min(firstNonBlank(t, ls, le), c))
	if err := m.dropIndent(); err != nil {
		return err
	}
	before := strings.TrimRight(t.Slice(ls, c), " \t")
	if closing, ok := brackets[before[max(len(before)-1, 0):]]; ok {
		inner := m.indentText(textWidth(indent, 0, m.h.TabWidth()) + m.h.IndentWidth())
		text := "\n" + inner
		if strings.HasPrefix(strings.TrimLeft(t.Slice(c, le), " \t"), closing) {
			text += "\n" + indent
		}
		if err := m.insertText(text); err != nil {
			return err
		}
		m.h.SetCursor(c + 1 + len(inner))
		m.autoIndent = true
		return nil
	}
	if err := m.insertText("\n" + indent); err != nil {
		return err
	}
	m.autoIndent = indent != ""
	return nil
}

// openAbove opens a line above the cursor's for O, with the same indent, or a
// level more above a closing bracket (B211).
func (m *Machine) openAbove() error {
	t, c := m.h.Text(), m.h.Cursor()
	fnb := firstNonBlank(t, c, LineEnd(t, c))
	indent := t.Slice(c, fnb)
	if strings.Contains(")]}", t.Slice(fnb, min(fnb+1, t.Len()))) && fnb < t.Len() {
		indent = m.indentText(textWidth(indent, 0, m.h.TabWidth()) + m.h.IndentWidth())
	}
	if err := m.apply([]Edit{{Start: c, End: c, Text: indent + "\n"}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(c + len(indent))
	m.autoIndent = indent != ""
	return nil
}

// brackets are the closing brackets by their opening ones.
var brackets = map[string]string{"(": ")", "[": "]", "{": "}"}

// closeIndent gives the cursor's line the indent of the line its opening
// bracket is on when s is a closing bracket typed on a line of blanks
// (B211).
func (m *Machine) closeIndent(t Text, c int, s string) error {
	k := strings.Index(")]}", s)
	ls := LineStart(t, c)
	if k < 0 || m.mode != Insert || strings.TrimLeft(t.Slice(ls, c), " \t") != "" {
		return nil
	}
	open := outer(t, ls, 1, "([{"[k], s[0])
	if open < 0 {
		return nil
	}
	ols := LineStart(t, open)
	indent := t.Slice(ols, firstNonBlank(t, ols, LineEnd(t, ols)))
	if indent == t.Slice(ls, c) {
		return nil
	}
	if err := m.apply([]Edit{{Start: ls, End: c, Text: indent}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(ls + len(indent))
	m.autoIndent = false
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

// indentLine shifts the cursor's line one indent width in for <C-t>, or out for
// <C-d>, keeping the cursor on the same text.
func (m *Machine) indentLine(in bool) error {
	t, c := m.h.Text(), m.h.Cursor()
	ls := LineStart(t, c)
	ind := firstNonBlank(t, ls, LineEnd(t, ls))
	w := column(t, ls, ind, m.h.TabWidth())
	if in {
		w += m.h.IndentWidth()
	} else {
		w = max(w-m.h.IndentWidth(), 0)
	}
	s := m.indentText(w)
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
