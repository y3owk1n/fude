package vim

import (
	"cmp"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// ex runs a command line. The machine runs the commands that edit text and
// :noh, and hands anything else to the host.
func (m *Machine) ex(line string) error {
	t, c := m.h.Text(), m.h.Cursor()
	first, last, rest, ranged, err := m.lineRange(t, c, line)
	if err != nil {
		return err
	}
	name, arg := exName(rest)
	switch name {
	case "":
		if ranged {
			m.h.SetCursor(firstNonBlank(t, last, LineEnd(t, last)))
		}
		return nil
	case "select":
		if !ranged {
			first, last = 0, LineStart(t, t.Len())
		}
		return m.selectQuery(arg, first, LineEnd(t, last))
	case "noh", "nohlsearch":
		m.hl = false
		return nil
	case "&&":
		return m.substitute(first, last, m.lastSub.pattern, m.lastSub.rep, m.lastSub.flags)
	case "&":
		return m.substitute(first, last, m.lastSub.pattern, m.lastSub.rep, "")
	case "s":
		if arg == "" {
			return m.substitute(first, last, m.lastSub.pattern, m.lastSub.rep, "")
		}
		parts := splitDelim(arg[1:], arg[0])
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		pat := parts[0]
		if pat == "" {
			pat = m.search.pattern
		}
		return m.substitute(first, last, pat, parts[1], parts[2])
	case "d", "delete", "y", "yank":
		return m.exYank(name[0] == 'd', first, last, arg)
	case "m", "move", "t", "co", "copy":
		target, rest, ok, err := m.address(t, c, arg)
		if err != nil {
			return err
		}
		if !ok || rest != "" {
			return errors.New("bad destination: " + arg)
		}
		if name[0] == 'm' {
			return m.moveLines(first, last, target)
		}
		return m.copyLines(first, last, target)
	case ">", "<":
		times := 1 + strings.Count(arg, name)
		lines := t.LineOf(last) - t.LineOf(first)
		if err := m.shift(name, ":"+name, region{start: first, end: LineEnd(t, last), lines: true}, 0, times); err != nil {
			return err
		}
		t = m.h.Text()
		ls := down(t, first, lines)
		m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
		return nil
	case "g", "global", "g!", "v", "vglobal":
		if !ranged {
			first, last = 0, LineStart(t, t.Len())
		}
		if arg == "" {
			return errors.New("missing pattern for :" + name)
		}
		parts := splitDelim(arg[1:], arg[0])
		return m.global(first, last, parts[0], strings.Join(parts[1:], string(arg[0])), name == "g" || name == "global")
	case "norm", "normal":
		return m.normalLines(first, last, arg)
	case "sor", "sort":
		if !ranged {
			first, last = 0, LineStart(t, t.Len())
		}
		return m.sortLines(first, last, arg)
	case "j", "join":
		// Without a range the line joins the one below it.
		m.h.SetCursor(first)
		return m.join(max(t.LineOf(last)-t.LineOf(first), 1), !strings.HasPrefix(arg, "!"))
	case "ret", "retab":
		if !ranged {
			first, last = 0, LineStart(t, t.Len())
		}
		return m.retab(first, last, strings.HasPrefix(arg, "!"))
	}
	m.h.Unhandled(":" + line)
	return nil
}

// ExNames returns the machine's ex commands, one full name each, as a
// command line's completion offers them.
func ExNames() []string {
	return []string{"copy", "delete", "global", "join", "move", "nohlsearch", "normal", "retab", "select", "sort", "vglobal", "yank"}
}

// LineRange reads the range line starts with, for the host's own commands:
// the starts of its first and last lines, the rest of line, and whether it
// had a range. Without one it is the cursor's line.
func (m *Machine) LineRange(line string) (first, last int, rest string, ranged bool, err error) {
	return m.lineRange(m.h.Text(), m.h.Cursor(), line)
}

// sortLines sorts the lines from first to last, as :sort does (B156). The
// flags in arg are ! to reverse, n to sort by each line's first number, i
// to ignore case and u to keep one of each line that sorts the same.
func (m *Machine) sortLines(first, last int, arg string) error {
	t := m.h.Text()
	end := LineEnd(t, last)
	lines := strings.Split(t.Slice(first, end), "\n")
	key := func(l string) string {
		if strings.Contains(arg, "i") {
			return strings.ToLower(l)
		}
		return l
	}
	number := func(l string) (int, bool) {
		n, err := strconv.Atoi(firstNumber.FindString(l))
		return n, err == nil
	}
	compare := func(a, b string) int {
		if strings.Contains(arg, "n") {
			// A line without a number goes before the ones with one.
			x, okx := number(a)
			y, oky := number(b)
			if c := cmp.Compare(btoi(okx), btoi(oky)); c != 0 || !okx {
				return c
			}
			return cmp.Compare(x, y)
		}
		return strings.Compare(key(a), key(b))
	}
	slices.SortStableFunc(lines, compare)
	if strings.HasPrefix(arg, "!") {
		slices.Reverse(lines)
	}
	if strings.Contains(arg, "u") {
		lines = slices.CompactFunc(lines, func(a, b string) bool { return compare(a, b) == 0 })
	}
	if err := m.apply([]Edit{{Start: first, End: end, Text: strings.Join(lines, "\n")}}, ":sort"); err != nil {
		return err
	}
	m.h.SetCursor(first)
	return nil
}

// firstNumber finds the number :sort n sorts a line by.
var firstNumber = regexp.MustCompile(`-?[0-9]+`)

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// retab rewrites the leading blanks of the lines from first to last, as
// :retab does (B156): as spaces with expand_tab, or with force as tabs and
// then spaces without it.
func (m *Machine) retab(first, last int, force bool) error {
	if !m.expandTab && !force {
		return nil
	}
	t := m.h.Text()
	var edits []Edit
	for ls := first; ; ls = LineEnd(t, ls) + 1 {
		end := firstNonBlank(t, ls, LineEnd(t, ls))
		if w := column(t, ls, end); w > 0 {
			blanks := strings.Repeat(" ", w)
			if !m.expandTab {
				blanks = strings.Repeat("\t", w/Tabstop) + strings.Repeat(" ", w%Tabstop)
			}
			if blanks != t.Slice(ls, end) {
				edits = append(edits, Edit{Start: ls, End: end, Text: blanks})
			}
		}
		if ls >= last || LineEnd(t, ls) == t.Len() {
			break
		}
	}
	if len(edits) == 0 {
		return nil
	}
	return m.apply(edits, ":retab")
}

// exName splits a command line after its range into the command's name and
// its argument. A name is a run of letters, >, <, &, && or g!.
func exName(s string) (string, string) {
	s = strings.TrimLeft(s, " ")
	switch {
	case s == "":
		return "", ""
	case s[0] == '>' || s[0] == '<':
		return s[:1], s[1:]
	case strings.HasPrefix(s, "&&"):
		return "&&", s[2:]
	case strings.HasPrefix(s, "g!"):
		return "g!", s[2:]
	}
	i := 0
	for i < len(s) && (s[i] >= 'a' && s[i] <= 'z' || s[i] >= 'A' && s[i] <= 'Z') {
		i++
	}
	if i == 0 {
		return s[:1], s[1:]
	}
	return s[:i], strings.TrimLeft(s[i:], " ")
}

// exYank yanks lines first to last, or deletes them, into the register named
// by arg. A delete puts the cursor on the first non-blank of the line that
// takes their place.
func (m *Machine) exYank(del bool, first, last int, arg string) error {
	var reg rune
	if arg != "" {
		reg = rune(arg[0])
	}
	op := "y"
	if del {
		op = "d"
	}
	c := m.h.Cursor()
	if err := m.act(op, reg, ":"+op, region{start: first, end: LineEnd(m.h.Text(), last), lines: true}); err != nil {
		return err
	}
	t := m.h.Text()
	if !del {
		m.h.SetCursor(c)
		return nil
	}
	ls := LineStart(t, min(first, t.Len()))
	m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
	return nil
}

// moveLines moves lines first to last below the line starting at target, or
// above the first line when target is -1, and puts the cursor on the last
// line moved.
func (m *Machine) moveLines(first, last, target int) error {
	t := m.h.Text()
	end := LineEnd(t, last)
	if target >= first && target <= last {
		return errors.New("cannot move lines into themselves")
	}
	block := t.Slice(first, end)
	var e Edit
	var landed int // where the block starts afterwards
	if target < first {
		at := 0
		if target >= 0 {
			at = LineEnd(t, target) + 1
		}
		if at == first {
			m.cursorOnLast(first, block)
			return nil
		}
		e = Edit{Start: at, End: end, Text: block + "\n" + strings.TrimSuffix(t.Slice(at, first), "\n")}
		landed = at
	} else {
		tend := LineEnd(t, target)
		middle := t.Slice(end+1, tend)
		e = Edit{Start: first, End: tend, Text: middle + "\n" + block}
		landed = first + len(middle) + 1
	}
	if err := m.apply([]Edit{e}, ":m"); err != nil {
		return err
	}
	m.cursorOnLast(landed, block)
	return nil
}

// copyLines copies lines first to last below the line starting at target, or
// above the first line when target is -1, and puts the cursor on the last
// line copied.
func (m *Machine) copyLines(first, last, target int) error {
	t := m.h.Text()
	block := t.Slice(first, LineEnd(t, last))
	e, landed := Edit{Start: 0, End: 0, Text: block + "\n"}, 0
	if target >= 0 {
		at := LineEnd(t, target)
		e, landed = Edit{Start: at, End: at, Text: "\n" + block}, at+1
	}
	if err := m.apply([]Edit{e}, ":t"); err != nil {
		return err
	}
	m.cursorOnLast(landed, block)
	return nil
}

// cursorOnLast puts the cursor on the first non-blank of the last line of
// block, which now starts at start.
func (m *Machine) cursorOnLast(start int, block string) {
	t := m.h.Text()
	ls := start + strings.LastIndexByte(block, '\n') + 1
	m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
}

// global runs cmd on every line from first to last that matches pat, or that
// does not when match is false, as one undo step. Marks track each matching
// line while cmd changes the text.
func (m *Machine) global(first, last int, pat, cmd string, match bool) error {
	if pat == "" {
		pat = m.search.pattern
	}
	p, err := m.compile(pat)
	if err != nil {
		return err
	}
	m.search.pattern, m.search.forward, m.hl = pat, true, true
	t := m.h.Text()
	var found []int
	for ls := first; ; ls = LineEnd(t, ls) + 1 {
		if (p.re.FindStringIndex(t.Slice(ls, LineEnd(t, ls))) != nil) == match {
			found = append(found, ls)
		}
		if ls >= last || LineEnd(t, ls) == t.Len() {
			break
		}
	}
	for i, ls := range found {
		m.h.SetMark(globalMark+rune(i), ls)
	}
	m.h.BeginGroup()
	defer m.h.EndGroup()
	for i := range found {
		pos, _ := m.h.Mark(globalMark + rune(i))
		m.h.SetCursor(LineStart(m.h.Text(), pos))
		if err := m.ex(cmd); err != nil {
			return err
		}
	}
	return nil
}

// globalMark is the first of the private marks :g keeps on the lines it found.
const globalMark = 0xF0000

// normalLines runs keys in normal mode at the start of each line from first
// to last, as one undo step, ending any command left unfinished.
func (m *Machine) normalLines(first, last int, keys string) error {
	t := m.h.Text()
	n := 1
	for ls := first; ls < last && LineEnd(t, ls) < t.Len(); ls = LineEnd(t, ls) + 1 {
		n++
	}
	m.h.SetMark(globalMark-1, first)
	m.h.BeginGroup()
	defer m.h.EndGroup()
	for i := range n {
		start, _ := m.h.Mark(globalMark - 1)
		t = m.h.Text()
		ls := down(t, LineStart(t, start), i)
		if i > 0 && ls == LineStart(t, start) {
			break
		}
		m.h.SetCursor(ls)
		if err := m.run(Keys(keys), true); err != nil {
			return err
		}
		for m.mode != Normal || !m.idle() {
			if err := m.feed("<Esc>"); err != nil {
				return err
			}
		}
	}
	return nil
}

// lineRange parses a leading range: %, or one or two addresses joined by a
// comma. It returns the starts of the first and last lines, the rest of s,
// and whether s had a range. Without one both lines are the cursor's.
func (m *Machine) lineRange(t Text, c int, s string) (int, int, string, bool, error) {
	if rest, ok := strings.CutPrefix(s, "%"); ok {
		return 0, LineStart(t, t.Len()), rest, true, nil
	}
	first, rest, ok, err := m.address(t, c, s)
	if err != nil || !ok {
		return LineStart(t, c), LineStart(t, c), s, false, err
	}
	last := first
	if r, found := strings.CutPrefix(rest, ","); found {
		l, r2, ok, err := m.address(t, c, r)
		if err != nil {
			return 0, 0, "", false, err
		}
		if ok {
			last, rest = l, r2
		}
	}
	return min(first, last), max(first, last), rest, true, nil
}

// address parses one line address: a line number, ., $, a mark such as 'a,
// /pat/ or ?pat?, then any +N and -N offsets. An offset alone counts from the
// cursor's line. It returns the line's start, or -1 for line 0.
func (m *Machine) address(t Text, c int, s string) (int, string, bool, error) {
	line, ok := LineStart(t, c), true
	switch {
	case strings.HasPrefix(s, "."):
		s = s[1:]
	case strings.HasPrefix(s, "$"):
		line, s = LineStart(t, t.Len()), s[1:]
	case strings.HasPrefix(s, "'") && len(s) > 1:
		p, set := m.h.Mark(rune(s[1]))
		if !set {
			return 0, s, false, errors.New("mark not set")
		}
		line, s = LineStart(t, p), s[2:]
	case strings.HasPrefix(s, "/") || strings.HasPrefix(s, "?"):
		// The search starts on the line after the cursor's, or before it.
		parts := splitDelim(s[1:], s[0])
		from := LineStart(t, c)
		if s[0] == '/' {
			from = LineEnd(t, c)
		}
		m.h.SetCursor(from)
		p, k, _ := m.seek(parts[0], s[0] == '/', 1)
		m.h.SetCursor(c)
		if k == failed {
			return 0, s, false, errors.New(m.msg)
		}
		line = LineStart(t, p)
		s = strings.Join(parts[1:], string(s[0]))
	default:
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		switch n, _ := strconv.Atoi(s[:i]); {
		case i > 0 && n == 0:
			line = -1
		case i > 0:
			line = down(t, 0, n-1)
		default:
			ok = len(s) > 0 && (s[0] == '+' || s[0] == '-')
		}
		s = s[i:]
	}
	for len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		i := 1
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		n := 1
		if i > 1 {
			n, _ = strconv.Atoi(s[1:i])
		}
		if s[0] == '+' {
			line = down(t, max(line, 0), n)
		} else {
			line = up(t, max(line, 0), n)
		}
		s = s[i:]
	}
	return line, s, ok, nil
}

// splitDelim splits s at each delim not after a backslash, and drops the
// backslash before an escaped delim.
func splitDelim(s string, delim byte) []string {
	var parts []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == delim:
			b.WriteByte(delim)
			i++
		case s[i] == '\\' && i+1 < len(s):
			b.WriteString(s[i : i+2])
			i++
		case s[i] == delim:
			parts = append(parts, b.String())
			b.Reset()
		default:
			b.WriteByte(s[i])
		}
	}
	return append(parts, b.String())
}
