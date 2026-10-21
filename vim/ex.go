package vim

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Command is an ex command, as help shows it.
type Command struct {
	Name  string   // its full name
	Short []string // the other names it answers to
	Args  string   // what may follow it
	Help  string   // what it does
}

// exCommand is one of the machine's ex commands and what runs it.
type exCommand struct {
	Command
	run func(m *Machine, x exCall) error
}

// exCall is what an ex command gets from its command line. first and last
// are the starts of the range's first and last lines, ranged says whether
// the line had a range and zero whether it was line 0, above the first.
type exCall struct {
	t            Text
	c            int // the cursor
	first, last  int
	ranged, zero bool
	name, arg    string
}

// counted splits a count off the end of the argument, as :d, :y, :j, :>
// and :< take one for that many lines from the range's last. It returns
// the lines it covers and the argument before it, or the range and the
// whole argument without one.
func (x exCall) counted() (first, last int, arg string) {
	arg = strings.TrimRight(x.arg, " ")
	i := len(arg)
	for i > 0 && arg[i-1] >= '0' && arg[i-1] <= '9' {
		i--
	}
	n, err := strconv.Atoi(arg[i:])
	if err != nil || n == 0 {
		return x.first, x.last, x.arg
	}
	return x.last, down(x.t, x.last, n-1), strings.TrimRight(arg[:i], " ")
}

// rangeOrAll returns the range, or every line when there is none.
func (x exCall) rangeOrAll() (int, int) {
	if x.ranged {
		return x.first, x.last
	}
	return 0, LineStart(x.t, x.t.Len())
}

// exCommands are the machine's ex commands, and exByName finds one by any
// of its names. init fills them because :g runs ex, which reads them, and
// Go rejects a variable whose value refers to itself.
var (
	exCommands []exCommand
	exByName   map[string]*exCommand
)

func init() {
	exCommands = []exCommand{
		{Command{"&&", nil, "", "repeats the last substitute with its flags"}, func(m *Machine, x exCall) error {
			return m.substitute(x.first, x.last, m.lastSub.pattern, m.lastSub.rep, m.lastSub.flags)
		}},
		{Command{"&", nil, "", "repeats the last substitute without its flags"}, func(m *Machine, x exCall) error {
			return m.substitute(x.first, x.last, m.lastSub.pattern, m.lastSub.rep, "")
		}},
		{Command{">", nil, "[>...] [count]", "indents the lines, or count lines from the last, once more for each >"}, (*Machine).exShift},
		{Command{"<", nil, "[<...] [count]", "outdents the lines, or count lines from the last, once more for each <"}, (*Machine).exShift},
		{Command{"copy", []string{"co", "t"}, "{address}", "copies the lines below the line at address, 0 for the top"}, (*Machine).exMove},
		{Command{"delete", []string{"d"}, "[x] [count]", "deletes the lines into register x, or count lines from the last"}, (*Machine).exYank},
		{Command{"delmarks", []string{"delm"}, "{marks} | !", "deletes the marks, such as a-c, or with ! every mark from a to z"}, (*Machine).exDelmarks},
		{Command{"global", []string{"g"}, "/{pattern}/{command}", "runs command on each line that matches. Without a range, every line"}, (*Machine).exGlobal},
		{Command{"join", []string{"j"}, "[!] [count]", "joins the lines, or the line with the next, or count lines from the last. ! keeps their blanks"}, func(m *Machine, x exCall) error {
			first, last, arg := x.counted()
			m.h.SetCursor(first)
			if err := m.join(max(x.t.LineOf(last)-x.t.LineOf(first), 1), !strings.HasPrefix(arg, "!")); err != nil {
				return err
			}
			// :j, unlike J, leaves the cursor on the line's first non-blank.
			t := m.h.Text()
			m.h.SetCursor(firstNonBlank(t, first, LineEnd(t, first)))
			return nil
		}},
		{Command{"mark", []string{"ma", "k"}, "{a-zA-Z}", "sets the mark at the start of the range's last line"}, func(m *Machine, x exCall) error {
			if c := x.arg; len(c) != 1 || !(c[0] >= 'a' && c[0] <= 'z' || c[0] >= 'A' && c[0] <= 'Z') {
				return errors.New("a mark is a letter from a to z or A to Z")
			}
			m.h.SetMark(rune(x.arg[0]), x.last)
			return nil
		}},
		{Command{"move", []string{"m"}, "{address}", "moves the lines below the line at address, 0 for the top"}, (*Machine).exMove},
		{Command{"nohlsearch", []string{"noh"}, "", "hides the last search's highlight until the next search"}, func(m *Machine, _ exCall) error {
			m.hl = false
			return nil
		}},
		{Command{"normal", []string{"norm"}, "[!] {keys}", "types keys in normal mode at the start of each line. ! leaves out the mappings"}, func(m *Machine, x exCall) error {
			keys, bang := strings.CutPrefix(x.arg, "!")
			return m.normalLines(x.first, x.last, strings.TrimLeft(keys, " "), !bang)
		}},
		{Command{"put", []string{"pu"}, "[!] [x]", "puts register x on lines of its own below the range's last line, or above it with ! or on line 0"}, (*Machine).exPut},
		{Command{"redo", []string{"red"}, "", "redoes a change, as <C-r> does"}, func(m *Machine, _ exCall) error {
			return m.undo(false, 1)
		}},
		{Command{"retab", []string{"ret"}, "[!]", "rewrites leading blanks as spaces with expand_tab, or as tabs with !. Without a range, every line"}, func(m *Machine, x exCall) error {
			first, last := x.rangeOrAll()
			return m.retab(first, last, strings.HasPrefix(x.arg, "!"))
		}},
		{Command{"substitute", []string{"s"}, "/{pattern}/{replacement}/[flags]", "replaces the first match of pattern on each line, or every match with g. i ignores case and I matches it"}, (*Machine).exSubstitute},
		{Command{"select", nil, "{query}", "puts a cursor on each capture of a syntax query. Without a range, every line"}, func(m *Machine, x exCall) error {
			first, last := x.rangeOrAll()
			return m.selectQuery(x.arg, first, LineEnd(x.t, last))
		}},
		{Command{"sort", []string{"sor"}, "[!][n][i][u]", "sorts the lines. ! reverses, n sorts by number, i ignores case and u drops repeats. Without a range, every line"}, func(m *Machine, x exCall) error {
			first, last := x.rangeOrAll()
			return m.sortLines(first, last, x.arg)
		}},
		{Command{"undo", []string{"u"}, "", "undoes a change, as u does"}, func(m *Machine, _ exCall) error {
			return m.undo(true, 1)
		}},
		{Command{"vglobal", []string{"v", "g!"}, "/{pattern}/{command}", "runs command on each line that does not match. Without a range, every line"}, (*Machine).exGlobal},
		{Command{"yank", []string{"y"}, "[x] [count]", "yanks the lines into register x, or count lines from the last"}, (*Machine).exYank},
	}
	exByName = map[string]*exCommand{}
	for i := range exCommands {
		c := &exCommands[i]
		for _, name := range append([]string{c.Name}, c.Short...) {
			exByName[name] = c
		}
	}
}

// Commands returns the machine's ex commands, for help and completion.
func Commands() []Command {
	out := make([]Command, len(exCommands))
	for i, c := range exCommands {
		out[i] = c.Command
	}
	return out
}

// Ex runs line as a command line typed after :, such as s/a/b/.
func (m *Machine) Ex(line string) error { return m.ex(line) }

// ex runs a command line. The machine runs the commands that edit text and
// :noh, and hands anything else to the host.
func (m *Machine) ex(line string) error {
	// An ex command runs once, not at each cursor, and the keys it runs, as
	// :normal does, are not commands of their own.
	m.once = true
	m.inner++
	defer func() { m.inner-- }()
	t, c := m.h.Text(), m.h.Cursor()
	first, last, rest, ranged, err := m.lineRange(t, c, line)
	if err != nil {
		return err
	}
	// Line 0 is the first line for any command but :put, which puts above
	// it, and :m and :t, which take their address on their own.
	zero := last < 0
	first, last = max(first, 0), max(last, 0)
	name, arg := exName(rest)
	// A | ends the command and starts another, unless a backslash is before
	// it or the command takes | in its argument.
	if i := bar(rest); i >= 0 && !takesBar(name, arg) {
		if err := m.ex(strings.TrimRight(line[:len(line)-len(rest)+i], " ")); err != nil {
			return err
		}
		return m.ex(strings.TrimLeft(rest[i+1:], " "))
	}
	if name == "" {
		if ranged {
			m.jumpFrom(c)
			m.h.SetCursor(firstNonBlank(t, last, LineEnd(t, last)))
		}
		return nil
	}
	if cmd, ok := exByName[name]; ok {
		return cmd.run(m, exCall{t, m.h.Cursor(), first, last, ranged, zero, name, arg})
	}
	return m.h.Unhandled(":" + line)
}

// bar returns where the first | not after a backslash is in s, or -1.
func bar(s string) int {
	for i := range len(s) {
		if s[i] == '|' && (i == 0 || s[i-1] != '\\') {
			return i
		}
	}
	return -1
}

// takesBar reports whether the command name reads a | in its argument as
// its own: :g, :v, :normal and :terminal, and a shell command after :!, :r !
// and :w !.
func takesBar(name, arg string) bool {
	switch name {
	case "g", "global", "v", "vglobal", "g!", "normal", "norm", "terminal", "term", "ter", "!":
		return true
	case "r", "read", "w", "write":
		return strings.HasPrefix(arg, "!")
	}
	return false
}

// exSubstitute runs :s, which without an argument repeats the last
// substitute, and with an empty pattern takes the last search's.
func (m *Machine) exSubstitute(x exCall) error {
	if x.arg == "" {
		return m.substitute(x.first, x.last, m.lastSub.pattern, m.lastSub.rep, "")
	}
	parts := splitDelim(x.arg[1:], x.arg[0])
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	pat := cmp.Or(parts[0], m.search.pattern)
	return m.substitute(x.first, x.last, pat, parts[1], parts[2])
}

// exMove runs :m, :t and :co.
func (m *Machine) exMove(x exCall) error {
	target, rest, ok, err := m.address(x.t, x.c, x.arg)
	if err != nil {
		return err
	}
	if !ok || rest != "" {
		return errors.New("bad destination: " + x.arg)
	}
	if x.name[0] == 'm' {
		return m.moveLines(x.first, x.last, target, ":m")
	}
	return m.putLines(x.t.Slice(x.first, LineEnd(x.t, x.last)), target, ":t")
}

// exPut runs :put, which puts the register named by its argument, or else
// the one p puts, as whole lines below the range's last line. ! or line 0
// puts them above it.
func (m *Machine) exPut(x exCall) error {
	arg, above := strings.CutPrefix(x.arg, "!")
	var reg rune
	if arg = strings.TrimSpace(arg); arg != "" {
		reg = rune(arg[0])
	}
	reg, r, ok, err := m.fetchPut(reg)
	switch {
	case err != nil:
		return err
	case !ok:
		return fmt.Errorf("nothing in register %c", reg)
	}
	target := x.last
	if above || x.zero {
		target = -1
		if x.last > 0 {
			target = LineStart(x.t, x.last-1)
		}
	}
	return m.putLines(strings.TrimSuffix(r.text, "\n"), target, ":put")
}

// exDelmarks runs :delmarks, which deletes each mark its argument names, a
// to c for a-c, or with ! every mark from a to z.
func (m *Machine) exDelmarks(x exCall) error {
	names := strings.ReplaceAll(x.arg, " ", "")
	if names == "!" {
		names = "a-z"
	}
	if names == "" {
		return errors.New(":delmarks needs the marks to delete, or !")
	}
	for i := 0; i < len(names); i++ {
		from, to := names[i], names[i]
		if i+2 < len(names) && names[i+1] == '-' {
			to = names[i+2]
			i += 2
		}
		for c := from; c <= to; c++ {
			m.h.DeleteMark(rune(c))
		}
	}
	return nil
}

// exShift runs :> and :<, once more for each > or < after the first.
func (m *Machine) exShift(x exCall) error {
	first, last, arg := x.counted()
	times := 1 + strings.Count(arg, x.name)
	lines := x.t.LineOf(last) - x.t.LineOf(first)
	if err := m.shift(x.name, ":"+x.name, region{start: first, end: LineEnd(x.t, last), lines: true}, times); err != nil {
		return err
	}
	t := m.h.Text()
	ls := down(t, first, lines)
	m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
	return nil
}

// exGlobal runs :g, and :v and :g! for the lines that do not match.
func (m *Machine) exGlobal(x exCall) error {
	first, last := x.rangeOrAll()
	if x.arg == "" {
		return errors.New("missing pattern for :" + x.name)
	}
	parts := splitDelim(x.arg[1:], x.arg[0])
	return m.global(first, last, parts[0], strings.Join(parts[1:], string(x.arg[0])), x.name == "g" || x.name == "global")
}

// LineRange reads the range line starts with, for the host's own commands:
// the starts of its first and last lines, the rest of line, and whether it
// had a range. Without one it is the cursor's line. Line 0 is the first.
func (m *Machine) LineRange(line string) (first, last int, rest string, ranged bool, err error) {
	first, last, rest, ranged, err = m.lineRange(m.h.Text(), m.h.Cursor(), line)
	return max(first, 0), max(last, 0), rest, ranged, err
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
	t, tab := m.h.Text(), m.h.TabWidth()
	var edits []Edit
	for ls := first; ; ls = LineEnd(t, ls) + 1 {
		end := firstNonBlank(t, ls, LineEnd(t, ls))
		if w := column(t, ls, end, tab); w > 0 {
			if blanks := m.indentText(w); blanks != t.Slice(ls, end) {
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
// its argument. A name is a run of letters, >, <, &, && or g!, or k before
// one letter.
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
	switch {
	case i == 0:
		return s[:1], s[1:]
	case i == 2 && s[0] == 'k':
		// :k takes its mark straight after it, as :ka does.
		return "k", s[1:]
	}
	return s[:i], strings.TrimLeft(s[i:], " ")
}

// exYank runs :y, which yanks the lines into the register its argument
// names, and :d, which deletes them and puts the cursor on the first
// non-blank of the line that takes their place.
func (m *Machine) exYank(x exCall) error {
	first, last, arg := x.counted()
	del := x.name[0] == 'd'
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
func (m *Machine) moveLines(first, last, target int, label string) error {
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
	if err := m.apply([]Edit{e}, label); err != nil {
		return err
	}
	m.cursorOnLast(landed, block)
	return nil
}

// putLines puts block on lines of its own below the line starting at
// target, or above the first line when target is -1, and puts the cursor on
// the last line put.
func (m *Machine) putLines(block string, target int, label string) error {
	t := m.h.Text()
	e, landed := Edit{Start: 0, End: 0, Text: block + "\n"}, 0
	if target >= 0 {
		at := LineEnd(t, target)
		e, landed = Edit{Start: at, End: at, Text: "\n" + block}, at+1
	}
	if err := m.apply([]Edit{e}, label); err != nil {
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

// moveBy moves lines first to last n lines down, or up when n is negative,
// as far as the text allows, and returns how far they moved (B236).
func (m *Machine) moveBy(first, last, n int, label string) (int, error) {
	t := m.h.Text()
	a, b := t.LineOf(first), t.LineOf(last)
	if n = max(min(n, t.Lines()-1-b), -a); n == 0 {
		m.beep = true
		return 0, nil
	}
	target := -1
	switch {
	case n > 0:
		target = t.LineStart(b + n)
	case a+n > 0:
		target = t.LineStart(a + n - 1)
	}
	return n, m.moveLines(first, last, target, label)
}

// blankLines puts n blank lines at at, a line's start, or one past the
// text's end for after the last line. The cursor stays on its text (B429).
func (m *Machine) blankLines(at, n int) error {
	t, c := m.h.Text(), m.h.Cursor()
	lines := strings.Repeat("\n", n)
	e := Edit{Start: at, End: at, Text: lines}
	if at > t.Len() {
		e = Edit{Start: t.Len(), End: t.Len(), Text: lines}
	}
	if err := m.apply([]Edit{e}, "blank lines"); err != nil {
		return err
	}
	if at <= c {
		c += n
	}
	m.h.SetCursor(c)
	return nil
}

// moveSelection moves the selected lines n lines down, or up when n is
// negative, and keeps them selected (B236).
func (m *Machine) moveSelection(n int, label string) error {
	t, a, c := m.h.Text(), m.anchor, m.h.Cursor()
	la, lc := t.LineOf(a), t.LineOf(c)
	da, dc := a-t.LineStart(la), c-t.LineStart(lc)
	n, err := m.moveBy(t.LineStart(min(la, lc)), t.LineStart(max(la, lc)), n, string(m.visual)+label)
	if err != nil {
		return err
	}
	t = m.h.Text()
	m.anchor = t.LineStart(la+n) + da
	m.h.SetCursor(t.LineStart(lc+n) + dc)
	return nil
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

// lineMark is the start of the line U puts back, after the mark :normal
// keeps on its first line. globalMark is the first of the marks :g keeps on
// the lines it found.
const (
	lineMark   = OwnMarks + 1
	globalMark = OwnMarks + 2
)

// normalLines runs keys in normal mode at the start of each line from first
// to last, through the mappings when mapped, as one undo step, ending any
// command left unfinished.
func (m *Machine) normalLines(first, last int, keys string, mapped bool) error {
	t := m.h.Text()
	n := 1
	for ls := first; ls < last && LineEnd(t, ls) < t.Len(); ls = LineEnd(t, ls) + 1 {
		n++
	}
	m.h.SetMark(OwnMarks, first)
	m.h.BeginGroup()
	defer m.h.EndGroup()
	for i := range n {
		start, _ := m.h.Mark(OwnMarks)
		t = m.h.Text()
		ls := down(t, LineStart(t, start), i)
		if i > 0 && ls == LineStart(t, start) {
			break
		}
		m.h.SetCursor(ls)
		// A key that failed on the line before stops only that line.
		m.beep = false
		if err := m.run(Keys(keys), mapped); err != nil {
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
// comma, or by a semicolon, which moves the cursor to the first line before
// the second is read. It returns the starts of the first and last lines,
// -1 for line 0, the rest of s, and whether s had a range. Without one both
// lines are the cursor's.
func (m *Machine) lineRange(t Text, c int, s string) (int, int, string, bool, error) {
	if rest, ok := strings.CutPrefix(s, "%"); ok {
		return 0, LineStart(t, t.Len()), rest, true, nil
	}
	first, rest, ok, err := m.address(t, c, s)
	if err != nil || !ok {
		return LineStart(t, c), LineStart(t, c), s, false, err
	}
	last := first
	if rest != "" && (rest[0] == ',' || rest[0] == ';') {
		if rest[0] == ';' {
			// The cursor keeps its column, as far as the line reaches.
			tab := m.h.TabWidth()
			c = clamp(t, atColumn(t, max(first, 0), column(t, LineStart(t, c), c, tab), tab))
			m.h.SetCursor(c)
		}
		// A second address left out is the cursor's line.
		l, r, _, err := m.address(t, c, rest[1:])
		if err != nil {
			return 0, 0, "", false, err
		}
		last, rest = l, r
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
		p, set := m.mark(t, c, rune(s[1]))
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
		p, k := m.seek(from, parts[0], s[0] == '/', 1)
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
