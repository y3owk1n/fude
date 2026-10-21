package vim

import (
	"fmt"
	"math"
	"strconv"
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
// object, or the operator again for whole lines. label names the edit, as
// the keys typed for it.
func (m *Machine) operate(op string, reg rune, cmd string, count int, label string) error {
	t, c := m.h.Text(), m.h.Cursor()
	r, ok := m.span(t, c, op, cmd, count)
	if !ok {
		m.beep = true
		return nil
	}
	return m.act(op, reg, label, r)
}

// Operator adds an operator such as gc, which works with every motion and
// text object, in visual mode, and doubled for the current line, as gcc.
// Its name may start with a built-in operator's, as ys starts with y. It
// waits for chars characters typed after the motion and passes them to
// apply, as ys{motion}) does. apply gets the target's range and whether it is
// linewise. A linewise range runs from the start of its first line to the
// end of its last. apply returns the edits it makes, and the cursor then
// goes where it goes after the built-in operators. Dot repeats it with its
// characters. A nil apply removes the operator.
func (m *Machine) Operator(name string, chars int, apply func(t Text, start, end int, lines bool, chars []string) []Edit) {
	if apply == nil {
		delete(m.ops, name)
		return
	}
	if m.ops == nil {
		m.ops = map[string]hostOperator{}
	}
	m.ops[name] = hostOperator{chars, apply}
}

// hostOperator is an operator the host added: how many characters it waits
// for after its motion, and what it does.
type hostOperator struct {
	chars int
	apply func(Text, int, int, bool, []string) []Edit
}

// Keyed adds a command the machine reads as it reads its own, by its keys
// in mode, which waits for chars characters typed after them. In normal
// mode its keys are an operator's followed by one more, as ds is, so the
// operator does not wait to see whether a mapping follows. In visual mode
// its key acts on the selection, as S does. run gets the selection's range,
// or the cursor as both ends, whether the range is linewise, and the
// characters. It returns the edits it makes, and the cursor goes to the
// start of the first.
// Dot repeats it with its characters. In insert mode it runs when its key
// is typed, before the key's own work, at the cursor and with no
// characters, and its key is in Vim notation, such as <Tab>. Its edits go
// in as typed text does, with the cursor after the first one's text. With
// no edits the key does what it does, as autopairs needs. A nil run
// removes it.
func (m *Machine) Keyed(mode Mode, keys string, chars int, run func(t Text, start, end int, lines bool, chars []string) []Edit) {
	if ks := Keys(keys); mode == Insert && len(ks) == 1 {
		keys = string(ks[0])
	}
	if run == nil {
		delete(m.keyed[mode], keys)
		return
	}
	if m.keyed == nil {
		m.keyed = map[Mode]map[string]keyedCommand{}
	}
	if m.keyed[mode] == nil {
		m.keyed[mode] = map[string]keyedCommand{}
	}
	m.keyed[mode][keys] = keyedCommand{chars, run}
}

// keyedCommand is a command the host added by its keys.
type keyedCommand struct {
	chars int
	run   func(Text, int, int, bool, []string) []Edit
}

// runKeyed runs kc on r once its characters are typed.
func (m *Machine) runKeyed(kc keyedCommand, keys string, r region) error {
	run := func(chars []string) error {
		edits := kc.run(m.h.Text(), r.start, r.end, r.lines, chars)
		if len(edits) == 0 {
			return nil
		}
		if err := m.apply(edits, keys+strings.Join(chars, "")); err != nil {
			return err
		}
		m.h.SetCursor(clamp(m.h.Text(), edits[0].Start))
		return nil
	}
	return m.waitFor(kc.chars, run)
}

// waitFor runs run with the n characters typed next, or at once for none.
func (m *Machine) waitFor(n int, run func(chars []string) error) error {
	if n == 0 {
		return run(nil)
	}
	m.wait = &charWait{n: n, run: run}
	return nil
}

// Command adds a normal mode command, run by the key <Plug>(name), for the
// host to map keys to. run gets the cursor and the count, and returns the
// edits it makes. Dot repeats it like any change. A nil run removes it.
func (m *Machine) Command(name string, run func(t Text, pos, count int) []Edit) {
	if run == nil {
		delete(m.cmds, name)
		return
	}
	if m.cmds == nil {
		m.cmds = map[string]func(Text, int, int) []Edit{}
	}
	m.cmds[name] = run
}

// plug runs the command the host added as name, and leaves the cursor where
// its first edit starts, or after the edit's text when typed, as in insert
// mode.
func (m *Machine) plug(name string, count int, typed bool) error {
	run := m.cmds[name]
	if run == nil {
		return nil
	}
	edits := run(m.h.Text(), m.h.Cursor(), count)
	if len(edits) == 0 {
		return nil
	}
	if err := m.apply(edits, "<Plug>("+name+")"); err != nil {
		return err
	}
	if typed {
		m.h.SetCursor(edits[0].Start + len(edits[0].Text))
		return nil
	}
	m.h.SetCursor(clamp(m.h.Text(), edits[0].Start))
	return nil
}

// hostOp applies an operator the host added to r, and leaves the cursor at
// the start of r's first line: on its first non-blank when the operator was
// doubled, at the cursor's column when the cursor was on that line and r is
// linewise, and at r's own column otherwise. The column is a number of bytes
// and does not move with the edits.
func (m *Machine) hostOp(op, label string, r region, chars []string) error {
	t, c := m.h.Text(), m.h.Cursor()
	line := t.LineOf(r.start)
	ls := t.LineStart(line)
	col := r.start - ls
	switch {
	case label == op+op[len(op)-1:] || label == op+op:
		col = firstNonBlank(t, ls, LineEnd(t, ls)) - ls
	case r.lines && LineStart(t, c) == ls:
		col = c - ls
	}
	if edits := m.ops[op].apply(t, r.start, r.end, r.lines, chars); len(edits) > 0 {
		if err := m.apply(edits, label+strings.Join(chars, "")); err != nil {
			return err
		}
	}
	t = m.h.Text()
	ls = t.LineStart(min(line, t.Lines()-1))
	m.h.SetCursor(clamp(t, min(ls+col, LineEnd(t, ls))))
	return nil
}

// act applies d, c or y to r.
func (m *Machine) act(op string, reg rune, label string, r region) error {
	tab := m.h.TabWidth()
	if o, ok := m.ops[op]; ok {
		return m.waitFor(o.chars, func(chars []string) error { return m.hostOp(op, label, r, chars) })
	}
	t, c := m.h.Text(), m.h.Cursor()
	col := column(t, LineStart(t, c), c, tab)
	if m.wantSet {
		col = m.want
	}
	m.wantSet = false
	switch {
	case op == "g~" || op == "gu" || op == "gU" || op == "g?":
		return m.changeCase(op, label, r)
	case op == "gw":
		// gw is the host's gq, with the cursor kept.
		if _, ok := m.ops["gq"]; !ok {
			return nil
		}
		err := m.act("gq", reg, label, r)
		m.h.SetCursor(c)
		return err
	case op == "!":
		return m.filterLine(r)
	case op == ">" || op == "<":
		return m.shift(op, label, r, 1)
	case r.block:
		return m.actBlock(op, reg, label, r)
	case r.start == r.end && !r.lines:
		if op == "c" {
			return m.startInsert(Key(label), r.start)
		}
		return nil
	}
	start, end, lines := r.start, r.end, r.lines

	text := t.Slice(start, end)
	if lines {
		text += "\n"
	}
	m.store(reg, op, register{text: text, linewise: lines})
	if op == "y" {
		m.flash, m.flashPut = [2]int{start, end}, false
		m.changedText(start, end)
	}
	switch {
	case op == "y" && lines:
		if s := LineStart(t, start); s != LineStart(t, c) {
			m.h.SetCursor(atColumn(t, s, column(t, LineStart(t, c), c, tab), tab))
		}
		return nil
	case op == "y":
		m.h.SetCursor(start)
		return nil
	case op == "c" && lines:
		indent := t.Slice(start, firstNonBlank(t, start, LineEnd(t, start)))
		if err := m.startInsert(Key(label), start); err != nil {
			return err
		}
		if err := m.apply([]Edit{{Start: start, End: end, Text: indent}}, label); err != nil {
			return err
		}
		m.h.SetCursor(start + len(indent))
		return nil
	case op == "c":
		if err := m.startInsert(Key(label), start); err != nil {
			return err
		}
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
		m.h.SetCursor(atColumn(t, s, col, tab))
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
		return m.folded(t, region{start: s, end: LineEnd(t, m.foldDown(t, s, n-1)), lines: true}), true
	}
	if cmd == "gn" || cmd == "gN" {
		s, e, err := m.searchMatch(t, c, cmd == "gN")
		if err != nil {
			m.msg = err.Error()
			return region{}, false
		}
		return region{start: s, end: e}, true
	}
	if r, ok := m.object(t, c, count, cmd); ok {
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
		if to, k, ok = m.motion(cmd, count, op); !ok || k == failed {
			return region{}, false
		}
	}
	r := region{start: min(c, to), end: max(c, to), lines: k == linewise}
	switch k {
	case linewise:
		r = m.folded(t, region{start: LineStart(t, r.start), end: LineEnd(t, r.end), lines: true})
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

// store saves r in reg, or in the default registers when reg is 0: "0 for
// a yank, "1 to "9 for a deleted line, "- for a smaller delete. "+ and "*
// are the host's clipboard.
func (m *Machine) store(reg rune, op string, r register) {
	if m.regs == nil {
		m.regs = map[rune]register{}
	}
	switch {
	case reg == '_':
		return
	case strings.ContainsRune(".:/%", reg):
		m.msg = fmt.Sprintf("register %c cannot be written", reg)
		return
	case reg == '+' || reg == '*':
		if err := m.h.SetClipboard(r.text); err != nil {
			m.msg = err.Error()
		}
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
	if reg == 0 && m.clipboard {
		if err := m.h.SetClipboard(r.text); err != nil {
			m.msg = err.Error()
		}
	}
}

// UseClipboard sets whether yanks, deletes and puts without a register use
// the host's clipboard too (B137).
func (m *Machine) UseClipboard(on bool) { m.clipboard = on }

// fetchPut returns the register a put takes and its name: reg, or for 0
// the unnamed register. With the clipboard on, a put without a register
// takes the clipboard, unless it holds the unnamed register's text, which
// keeps its shape as a line or a block.
func (m *Machine) fetchPut(reg rune) (rune, register, bool, error) {
	if reg != 0 {
		r, ok, err := m.fetch(reg)
		return reg, r, ok, err
	}
	if m.clipboard {
		r, ok, err := m.fetch('+')
		if err != nil {
			// The unnamed register puts, and the message says why.
			m.msg = err.Error()
		} else if ok && r.text != m.regs['"'].text {
			return '+', r, true, nil
		}
	}
	r, ok, err := m.fetch('"')
	return '"', r, ok, err
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
// With past the cursor goes just after the text, as for gp and gP. With
// indent, as for ]p and [p, every line moves by as many columns as the
// first must move to have the cursor line's indent.
func (m *Machine) put(reg rune, after bool, n int, past, indent bool) error {
	reg, r, ok, err := m.fetchPut(reg)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("nothing in register %c", reg)
	}
	label := "P"
	if after {
		label = "p"
	}
	if indent && r.linewise {
		r.text = m.reindent(r.text)
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
	m.flash, m.flashPut = [2]int{at, at + len(text)}, true
	if r.linewise && after {
		m.flash[0]++ // after the line break put before the lines
	}
	m.changedText(m.flash[0], m.flash[1])
	t = m.h.Text()
	switch {
	case past && r.linewise && after:
		m.h.SetCursor(clamp(t, min(m.flash[1]+1, t.Len())))
	case past:
		m.h.SetCursor(clamp(t, m.flash[1]))
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

// lineRanges returns the text r covers on each of its lines, without line
// breaks: a block's columns, or else from its start to its end.
func (m *Machine) lineRanges(t Text, r region) [][2]int {
	var out [][2]int
	for a := r.start; ; {
		le := LineEnd(t, a)
		rg := [2]int{a, min(le, r.end)}
		if r.block {
			tab := m.h.TabWidth()
			rg = [2]int{colStart(t, a, r.left, tab), le}
			if r.right != math.MaxInt {
				rg[1] = colStart(t, a, r.right+1, tab)
			}
		}
		out = append(out, rg)
		if le >= r.end || le == t.Len() {
			return out
		}
		a = le + 1
	}
}

// actBlock applies d, c or y to the columns of a block. c inserts on the
// first line, and leaving insert mode copies the text to the other lines.
func (m *Machine) actBlock(op string, reg rune, label string, r region) error {
	t := m.h.Text()
	var texts []string
	var edits []Edit
	for _, rg := range m.lineRanges(t, r) {
		texts = append(texts, t.Slice(rg[0], rg[1]))
		if rg[0] < rg[1] {
			edits = append(edits, Edit{Start: rg[0], End: rg[1]})
		}
	}
	m.store(reg, op, register{text: strings.Join(texts, "\n"), block: true})
	top := colStart(t, r.start, r.left, m.h.TabWidth())
	switch op {
	case "y":
		m.h.SetCursor(top)
		return nil
	case "c":
		if err := m.startInsert(Key(label), top); err != nil {
			return err
		}
		m.blockInsert = &blockInsert{start: top, col: r.left, lines: len(texts) - 1, home: top}
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
	home              int  // the block's top-left, where the cursor ends
	toEnd             bool // insert at each line's end, for $A
	pad               bool // pad short lines with spaces to reach col, for A
}

// finishBlock copies text typed on the first line of a block c to the same
// column of the block's other lines, skipping lines too short to reach it.
func (m *Machine) finishBlock(b *blockInsert) error {
	tab := m.h.TabWidth()
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
		p := colStart(t, ls, b.col, tab)
		w := column(t, ls, p, tab)
		switch {
		case b.toEnd:
			p = LineEnd(t, ls)
			edits = append(edits, Edit{Start: p, End: p, Text: typed})
		case w == b.col:
			edits = append(edits, Edit{Start: p, End: p, Text: typed})
		case b.pad:
			// A tab or wide character over the column takes no padding.
			edits = append(edits, Edit{Start: p, End: p, Text: strings.Repeat(" ", max(b.col-w, 0)) + typed})
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
	tab := m.h.TabWidth()
	t, c := m.h.Text(), m.h.Cursor()
	col := column(t, LineStart(t, c), c, tab)
	if after && c < LineEnd(t, c) {
		col += textWidth(t.Slice(c, next(t, c, LineEnd(t, c))), col, tab)
	}
	lines := strings.Split(text, "\n")
	w := 0
	for _, l := range lines {
		w = max(w, textWidth(strings.Repeat(l, n), 0, tab))
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
		p := colStart(t, ls, col, tab)
		pad := strings.Repeat(" ", max(col-column(t, ls, p, tab), 0))
		if i == 0 {
			top = p + len(pad)
		}
		s := pad + l
		if p < LineEnd(t, ls) {
			s += strings.Repeat(" ", w-textWidth(l, 0, tab))
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

// charWait is a command waiting for the characters typed after it.
type charWait struct {
	n   int
	got []string
	run func(chars []string) error
}

// takeChar gives k to the command that waits for characters. A key that
// is not a character, such as <Esc>, cancels it.
func (m *Machine) takeChar(k Key) error {
	w := m.wait
	if !k.isChar() {
		m.wait = nil
		return nil
	}
	if w.got = append(w.got, string(k)); len(w.got) < w.n {
		return nil
	}
	m.wait = nil
	return w.run(w.got)
}

// reindent returns the lines of text, each moved by as many columns as the
// first must move to have the cursor line's indent. Blank lines stay.
func (m *Machine) reindent(text string) string {
	t, c, tab := m.h.Text(), m.h.Cursor(), m.h.TabWidth()
	ls := LineStart(t, c)
	want := column(t, ls, firstNonBlank(t, ls, LineEnd(t, ls)), tab)
	lines := strings.SplitAfter(text, "\n")
	by := want - textWidth(lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], " \t"))], 0, tab)
	for i, l := range lines {
		body := strings.TrimLeft(l, " \t")
		if strings.TrimSpace(body) == "" {
			continue
		}
		w := textWidth(l[:len(l)-len(body)], 0, tab)
		lines[i] = m.indentText(max(w+by, 0)) + body
	}
	return strings.Join(lines, "")
}

// filterLine opens the command line with the lines of r as its range and
// then !, so a shell command can take them. The range is . for one line
// and .,.+n for more.
func (m *Machine) filterLine(r region) error {
	t := m.h.Text()
	m.h.SetCursor(LineStart(t, r.start))
	line := "."
	if n := t.LineOf(r.end) - t.LineOf(r.start); n > 0 {
		line = ".,.+" + strconv.Itoa(n)
	}
	m.enterLine(Normal, ":", line+"!")
	return nil
}
