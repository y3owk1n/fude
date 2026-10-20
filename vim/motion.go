package vim

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Tabstop is the width of a tab when the host does not say.
const Tabstop = 8

// kind is how an operator treats the text a motion crosses.
type kind int

const (
	exclusive kind = iota
	inclusive
	linewise
	failed // the motion could not move, so an operator does nothing
)

// motionCall is what a motion gets: the text, the cursor, the count typed
// (0 for none), n for the count or 1, the start of the cursor's line, the
// keys typed and the operator it is the target of, or "".
type motionCall struct {
	t                Text
	c, count, n, sol int
	cmd, op          string
}

// motionKey is a motion and what it does.
type motionKey struct {
	help string
	run  func(m *Machine, x motionCall) (int, kind)
}

// moved ends a motion at p of kind k, keeping the column j and k aim for
// when keep is set.
func (m *Machine) moved(p int, k kind, keep bool) (int, kind) {
	m.wantSet = keep
	return p, k
}

// wordMotion is w and W, b and B when back, e and E when end, or ge and gE
// when both.
func wordMotion(back, end bool) func(m *Machine, x motionCall) (int, kind) {
	return func(m *Machine, x motionCall) (int, kind) {
		big := unicode.IsUpper(rune(x.cmd[len(x.cmd)-1]))
		switch {
		case back && end:
			return m.moved(wordEndBack(x.t, x.c, x.n, big), inclusive, false)
		case back:
			return m.moved(wordBack(x.t, x.c, x.n, big), exclusive, false)
		case end:
			return m.moved(wordEnd(x.t, x.c, x.n, big, false), inclusive, false)
		}
		return m.moved(wordForward(x.t, x.c, x.n, big, false), exclusive, false)
	}
}

// lineFirstMotion is +, <CR>, - and _, which go to the first non-blank of
// a line below or above.
func lineFirstMotion(m *Machine, x motionCall) (int, kind) {
	var line int
	switch x.cmd {
	case "-":
		line = m.foldUp(x.t, x.sol, x.n)
	case "_":
		line = m.foldDown(x.t, x.sol, x.n-1)
	default:
		line = m.foldDown(x.t, x.sol, x.n)
	}
	if line == x.sol && x.cmd != "_" {
		return x.c, failed
	}
	return m.moved(firstNonBlank(x.t, line, LineEnd(x.t, line)), linewise, false)
}

// unmatchedMotion is [( and [{, or ]) and ]} going forward.
func unmatchedMotion(m *Machine, x motionCall) (int, kind) {
	open, close := byte('('), byte(')')
	if strings.ContainsAny(x.cmd, "{}") {
		open, close = '{', '}'
	}
	p := outer(x.t, x.c, x.n, open, close)
	if x.cmd[0] == ']' {
		p = outerClose(x.t, x.c, x.n, open, close)
	}
	if p < 0 {
		return x.c, failed
	}
	return m.moved(p, exclusive, false)
}

// sectionMotion is ]] and ][ going forward, or [[ and [] going back, to a
// line starting with { for ]] and [[, or with } for ][ and []. After an
// operator ]] also stops below a line starting with }. With none left it
// goes to the first line, or to the last character of the text.
func sectionMotion(m *Machine, x motionCall) (int, kind) {
	forward := x.cmd[0] == ']'
	ch := byte('{')
	if x.cmd == "][" || x.cmd == "[]" {
		ch = '}'
	}
	line, last := x.t.LineOf(x.c), x.t.Lines()-1
	for range x.n {
		for {
			if forward && line == last {
				return m.moved(lastChar(x.t, x.t.Len()), inclusive, false)
			}
			if !forward && line == 0 {
				return m.moved(0, exclusive, false)
			}
			if forward {
				line++
			} else {
				line--
			}
			s := x.t.LineStart(line)
			if at(x.t, s) == ch || x.cmd == "]]" && x.op != "" && at(x.t, x.t.LineStart(line-1)) == '}' {
				break
			}
		}
	}
	return m.moved(x.t.LineStart(line), exclusive, false)
}

// rowMotion is g0, g^, gm and g$, which go along the cursor's screen row.
// The row is where the line wraps, or with wrap off what the view shows.
func rowMotion(m *Machine, x motionCall) (int, kind) {
	tab := m.h.TabWidth()
	start, end, cols := m.row(x.t, x.c)
	switch x.cmd {
	case "g^":
		return m.moved(firstNonBlank(x.t, start, end), exclusive, false)
	case "gm":
		return m.moved(atColumn(x.t, start, cols/2, tab), exclusive, false)
	case "g$":
		m.want = math.MaxInt
		return m.moved(lastChar(x.t, end), inclusive, true)
	}
	return m.moved(start, exclusive, false)
}

// row returns where the screen row that holds c starts and ends, and how
// many columns it shows.
func (m *Machine) row(t Text, c int) (start, end, cols int) {
	ls, le := LineStart(t, c), LineEnd(t, c)
	_, _, cols = m.h.Viewport()
	if cols == math.MaxInt {
		left, shown := m.h.Sideways()
		if shown <= 0 {
			return ls, le, math.MaxInt
		}
		tab := m.h.TabWidth()
		return colStart(t, ls, left, tab), next(t, atColumn(t, ls, left+shown-1, tab), le), shown
	}
	starts, _ := m.rows(t, ls, cols)
	r := len(starts) - 1
	for r > 0 && starts[r] > c {
		r--
	}
	if r+1 < len(starts) {
		le = starts[r+1]
	}
	return starts[r], le, cols
}

// rows returns where each row of the line at ls starts, as the host wraps
// it at cols, and the inlines the rows count (B295).
func (m *Machine) rows(t Text, ls, cols int) ([]int, []Inline) {
	in := m.h.Shown(ls, LineEnd(t, ls))
	if m.h.WordWrap() {
		return WordRows(t, ls, cols, m.h.TabWidth(), in), in
	}
	return Rows(t, ls, cols, m.h.TabWidth(), in), in
}

// lineMotion is j, k, gg and G.
func lineMotion(m *Machine, x motionCall) (int, kind) {
	line := LineStart(x.t, x.t.Len())
	switch {
	case x.cmd == "j":
		line = m.foldDown(x.t, x.sol, x.n)
	case x.cmd == "k":
		line = m.foldUp(x.t, x.sol, x.n)
	case x.cmd == "gg" || x.count > 0:
		line = down(x.t, 0, x.n-1)
	}
	if line == x.sol && (x.cmd == "j" || x.cmd == "k") {
		return x.c, failed
	}
	return m.moved(m.toLine(x.t, x.c, line), linewise, true)
}

// screenMotion is gj and gk.
func screenMotion(m *Machine, x motionCall) (int, kind) {
	p, ok := m.screenLine(x.t, x.c, x.n, x.cmd == "gj")
	if !ok {
		return x.c, failed
	}
	return p, exclusive
}

// windowMotion is H, M and L.
func windowMotion(m *Machine, x motionCall) (int, kind) {
	return m.moved(m.toLine(x.t, x.c, m.window(x.t, x.cmd, x.n)), linewise, true)
}

// searchMotion is n and N.
func searchMotion(m *Machine, x motionCall) (int, kind) {
	p, k := m.seek(m.search.pattern, m.search.forward == (x.cmd == "n"), x.n)
	return p, k
}

// starMotion is * and #.
func starMotion(m *Machine, x motionCall) (int, kind) {
	word, ok := keywordAt(x.t, x.c)
	if !ok {
		m.msg = "no word under the cursor"
		return x.c, failed
	}
	m.search.pattern, m.search.forward = `\<`+word+`\>`, x.cmd == "*"
	p, k := m.seek(m.search.pattern, m.search.forward, x.n)
	return p, k
}

// repeatFindMotion is ; and ,.
func repeatFindMotion(m *Machine, x motionCall) (int, kind) {
	f := m.find
	if f == "" {
		return x.c, failed
	}
	if x.cmd == "," {
		f = reverse[f[:1]] + f[1:]
	}
	p, ok := find(x.t, x.c, x.n, f, true)
	return m.found(p, f, ok)
}

// markMotion is ` and ' with a mark. ` and ' name one mark, where the last
// jump left from.
func markMotion(m *Machine, x motionCall) (int, kind) {
	name := rune(x.cmd[1])
	if name == '`' {
		name = '\''
	}
	p, ok := m.h.Mark(name)
	switch {
	case !ok:
		m.msg = "mark not set"
		return x.c, failed
	case x.cmd[0] == '\'':
		s := LineStart(x.t, p)
		return firstNonBlank(x.t, s, LineEnd(x.t, s)), linewise
	}
	return p, exclusive
}

// searchForMotion is / and ? with a pattern.
func searchForMotion(m *Machine, x motionCall) (int, kind) {
	m.search.pattern, m.search.forward = x.cmd[1:], x.cmd[0] == '/'
	p, k := m.seek(m.search.pattern, m.search.forward, x.n)
	return p, k
}

// findMotion is f, F, t and T with a character.
func findMotion(m *Machine, x motionCall) (int, kind) {
	m.find = x.cmd
	p, ok := find(x.t, x.c, x.n, x.cmd, false)
	return m.found(p, x.cmd, ok)
}

// motions are the motions by their keys. A key such as f{char} stands for
// f and any character, and motionOf finds the entry for typed keys.
var motions = map[string]motionKey{
	"h": {"moves left", func(m *Machine, x motionCall) (int, kind) {
		p := x.c
		for range x.n {
			p = prev(x.t, x.sol, p)
		}
		return m.moved(p, exclusive, false)
	}},
	"l": {"moves right", func(m *Machine, x motionCall) (int, kind) {
		p := x.c
		for range x.n {
			p = next(x.t, p, LineEnd(x.t, x.c))
		}
		return m.moved(p, exclusive, false)
	}},
	"j":  {"moves down a line", lineMotion},
	"k":  {"moves up a line", lineMotion},
	"gg": {"goes to the first line, or line n", lineMotion},
	"G":  {"goes to the last line, or line n", lineMotion},
	"gj": {"moves down a row on the screen", screenMotion},
	"gk": {"moves up a row on the screen", screenMotion},
	"go": {"goes to byte n", func(m *Machine, x motionCall) (int, kind) {
		p := min(x.n-1, x.t.Len())
		for p > 0 && p < x.t.Len() && !utf8.RuneStart(x.t.Slice(p, p+1)[0]) {
			p--
		}
		return m.moved(clamp(x.t, p), exclusive, false)
	}},
	"H": {"goes to the top line of the window, or line n from it", windowMotion},
	"M": {"goes to the middle line of the window", windowMotion},
	"L": {"goes to the bottom line of the window, or line n from it", windowMotion},
	"%": {"goes to the bracket matching the next one on the line, or n% down the file", func(m *Machine, x motionCall) (int, kind) {
		if x.count > 0 {
			return m.moved(m.toLine(x.t, x.c, down(x.t, 0, (x.count*x.t.Lines()+99)/100-1)), linewise, true)
		}
		p, ok := match(x.t, x.c)
		if !ok {
			return x.c, failed
		}
		return m.moved(p, inclusive, false)
	}},
	")": {"goes to the next sentence", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(sentenceForward(x.t, x.c, x.n), exclusive, false)
	}},
	"(": {"goes to the start of the sentence", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(sentenceBack(x.t, x.c, x.n), exclusive, false)
	}},
	"}": {"goes to the next blank line", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(paragraphForward(x.t, x.c, x.n), exclusive, false)
	}},
	"{": {"goes to the blank line above", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(paragraphBack(x.t, x.c, x.n), exclusive, false)
	}},
	"0": {"goes to the start of the line", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(x.sol, exclusive, false)
	}},
	"^": {"goes to the first non-blank of the line", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(firstNonBlank(x.t, x.sol, LineEnd(x.t, x.c)), exclusive, false)
	}},
	"$": {"goes to the end of the line, or of the line n-1 below", func(m *Machine, x motionCall) (int, kind) {
		m.want = math.MaxInt
		return m.moved(lastChar(x.t, LineEnd(x.t, down(x.t, x.sol, x.n-1))), inclusive, true)
	}},
	"g_": {"goes to the last non-blank of the line, or of the line n-1 below", func(m *Machine, x motionCall) (int, kind) {
		s := down(x.t, x.sol, x.n-1)
		line := x.t.Slice(s, LineEnd(x.t, s))
		return m.moved(lastChar(x.t, s+len(strings.TrimRight(line, " \t"))), inclusive, false)
	}},
	"|": {"goes to column n", func(m *Machine, x motionCall) (int, kind) {
		return m.moved(atColumn(x.t, x.sol, x.n-1, m.h.TabWidth()), exclusive, false)
	}},
	"+":    {"goes to the first non-blank n lines down", lineFirstMotion},
	"<CR>": {"goes to the first non-blank n lines down", lineFirstMotion},
	"-":    {"goes to the first non-blank n lines up", lineFirstMotion},
	"_":    {"goes to the first non-blank of the line, or of the line n-1 below", lineFirstMotion},
	"[(":   {"goes to the n-th unmatched ( before the cursor", unmatchedMotion},
	"[{":   {"goes to the n-th unmatched { before the cursor", unmatchedMotion},
	"])":   {"goes to the n-th unmatched ) after the cursor", unmatchedMotion},
	"]}":   {"goes to the n-th unmatched } after the cursor", unmatchedMotion},
	"]]":   {"goes to the next line starting with {", sectionMotion},
	"][":   {"goes to the next line starting with }", sectionMotion},
	"[[":   {"goes to the line starting with { above", sectionMotion},
	"[]":   {"goes to the line starting with } above", sectionMotion},
	"g0":   {"goes to the start of the screen row", rowMotion},
	"g^":   {"goes to the first non-blank of the screen row", rowMotion},
	"gm":   {"goes half a screen width along the screen row", rowMotion},
	"g$":   {"goes to the end of the screen row", rowMotion},
	"gM": {"goes to the middle of the line's text, or n% along it", func(m *Machine, x motionCall) (int, kind) {
		tab := m.h.TabWidth()
		w := column(x.t, x.sol, LineEnd(x.t, x.c), tab)
		pct := 50
		if x.count > 0 {
			pct = min(x.count, 100)
		}
		return m.moved(atColumn(x.t, x.sol, w*pct/100, tab), exclusive, false)
	}},
	"w":              {"goes to the start of the next word", wordMotion(false, false)},
	"W":              {"goes to the start of the next word of non-blanks", wordMotion(false, false)},
	"b":              {"goes to the start of the word", wordMotion(true, false)},
	"B":              {"goes to the start of the word of non-blanks", wordMotion(true, false)},
	"e":              {"goes to the end of the word", wordMotion(false, true)},
	"E":              {"goes to the end of the word of non-blanks", wordMotion(false, true)},
	"ge":             {"goes back to the end of the word before", wordMotion(true, true)},
	"gE":             {"goes back to the end of the word of non-blanks before", wordMotion(true, true)},
	"n":              {"goes to the next match of the last search", searchMotion},
	"N":              {"goes to the previous match of the last search", searchMotion},
	"*":              {"searches forward for the word under the cursor", starMotion},
	"#":              {"searches back for the word under the cursor", starMotion},
	";":              {"repeats the last f, t, F or T", repeatFindMotion},
	",":              {"repeats the last f, t, F or T the other way", repeatFindMotion},
	"`{mark}":        {"goes to the mark", markMotion},
	"'{mark}":        {"goes to the first non-blank of the mark's line", markMotion},
	"/{pattern}<CR>": {"searches forward for pattern", searchForMotion},
	"?{pattern}<CR>": {"searches back for pattern", searchForMotion},
	"f{char}":        {"goes to the next char on the line", findMotion},
	"F{char}":        {"goes back to char on the line", findMotion},
	"t{char}":        {"goes to just before the next char on the line", findMotion},
	"T{char}":        {"goes back to just after char on the line", findMotion},
}

// motionOf returns the key in motions that cmd is, or false.
func motionOf(cmd string) (string, bool) {
	if _, ok := motions[cmd]; ok {
		return cmd, true
	}
	switch {
	case cmd == "":
		return "", false
	case (cmd[0] == '`' || cmd[0] == '\'') && len(cmd) == 2:
		return cmd[:1] + "{mark}", true
	case cmd[0] == '/' || cmd[0] == '?':
		return cmd[:1] + "{pattern}<CR>", true
	case len(cmd) >= 2 && strings.Contains("ftFT", cmd[:1]) && Key(cmd[1:]).isChar():
		return cmd[:1] + "{char}", true
	}
	return "", false
}

// motion returns where cmd moves the cursor and its kind, or false if cmd is
// not a motion. op is the operator it is the target of, or "".
func (m *Machine) motion(cmd string, count int, op string) (int, kind, bool) {
	key, ok := motionOf(cmd)
	if !ok {
		return m.h.Cursor(), exclusive, false
	}
	t, c := m.h.Text(), m.h.Cursor()
	p, k := motions[key].run(m, motionCall{t, c, count, max(count, 1), LineStart(t, c), cmd, op})
	return p, k, true
}

// found turns a find's result into a motion. f and t are inclusive, F and T
// exclusive.
func (m *Machine) found(p int, f string, ok bool) (int, kind) {
	m.wantSet = false
	switch {
	case !ok:
		return p, failed
	case f[0] == 'f' || f[0] == 't':
		return p, inclusive
	}
	return p, exclusive
}

// clamp keeps a normal-mode cursor off a line's end, unless the line is
// empty.
func clamp(t Text, p int) int {
	if s, e := LineStart(t, p), LineEnd(t, p); p >= e && p > s {
		return prev(t, s, e)
	}
	return p
}

// lastChar returns the last cluster before end on its line, or end on an
// empty line.
func lastChar(t Text, end int) int {
	return prev(t, LineStart(t, end), end)
}

// toLine returns the cluster under the column j and k aim for on the line
// starting at line. It sets that column from c first if it is unset.
func (m *Machine) toLine(t Text, c, line int) int {
	tab := m.h.TabWidth()
	if !m.wantSet {
		m.want, m.wantSet = column(t, LineStart(t, c), c, tab), true
	}
	return atColumn(t, line, m.want, tab)
}

// match returns the bracket matching the first one at or after c on its
// line, as % does.
func match(t Text, c int) (int, bool) {
	i := strings.IndexAny(t.Slice(c, LineEnd(t, c)), "(){}[]")
	if i < 0 {
		return c, false
	}
	p := c + i
	k := strings.IndexByte("(){}[]", at(t, p))
	open, close := "(){}[]"[k&^1], "(){}[]"[k|1]
	if at(t, p) == close {
		q := outer(t, p, 1, open, close)
		return q, q >= 0
	}
	if q := closer(t, p, open, close); q >= 0 {
		return q, true
	}
	return c, false
}

// Pair returns the bracket that matches the one at pos, looking no further
// than [from, to), or false when pos is not on a bracket or its match is
// outside it (B121).
func Pair(t Text, pos, from, to int) (int, bool) {
	if pos >= t.Len() {
		return 0, false
	}
	k := strings.IndexByte("(){}[]", t.Slice(pos, pos+1)[0])
	if k < 0 {
		return 0, false
	}
	open, close := "(){}[]"[k&^1], "(){}[]"[k|1]
	depth := 0
	if k%2 == 0 {
		for i, c := range []byte(t.Slice(pos+1, max(to, pos+1))) {
			if c == open {
				depth++
			} else if c == close && depth == 0 {
				return pos + 1 + i, true
			} else if c == close {
				depth--
			}
		}
		return 0, false
	}
	s := t.Slice(min(from, pos), pos)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == close {
			depth++
		} else if s[i] == open && depth == 0 {
			return pos - len(s) + i, true
		} else if s[i] == open {
			depth--
		}
	}
	return 0, false
}

// sentenceForward is ), which stops at the next sentence or empty line.
func sentenceForward(t Text, c, n int) int {
	for range n {
		next := -1
		for _, s := range segments(t, c) {
			if !s.blank && s.start > c {
				next = s.start
				break
			}
		}
		if next < 0 {
			_, pe := paragraph(t, c, 1, false)
			next = min(pe+1, t.Len())
		}
		c = next
	}
	return c
}

// sentenceBack is (, which stops at the start of a sentence or an empty line.
func sentenceBack(t Text, c, n int) int {
	for range n {
		start := -1
		for _, s := range segments(t, c) {
			if !s.blank && s.start < c {
				start = s.start
			}
		}
		if start < 0 {
			ps, _ := paragraph(t, c, 1, false)
			start = 0
			if ps > 0 {
				start = LineStart(t, ps-1)
			}
		}
		c = start
	}
	return c
}

// paragraphForward is }, which stops at the next empty line or the end.
func paragraphForward(t Text, c, n int) int {
	for range n {
		p := LineStart(t, c)
		for p < t.Len() && LineEnd(t, p) == p {
			p++
		}
		for {
			le := LineEnd(t, p)
			if le == t.Len() {
				p = le
				break
			}
			if p = le + 1; LineEnd(t, p) == p {
				break
			}
		}
		c = p
	}
	return c
}

// paragraphBack is {, which stops at the previous empty line or the start.
func paragraphBack(t Text, c, n int) int {
	for range n {
		p := LineStart(t, c)
		for p > 0 && LineEnd(t, p) == p {
			p = LineStart(t, p-1)
		}
		for p > 0 {
			if p = LineStart(t, p-1); LineEnd(t, p) == p {
				break
			}
		}
		c = p
	}
	return c
}

var reverse = map[string]string{"f": "F", "F": "f", "t": "T", "T": "t"}

// find returns where f, t, F or T and a character land after n matches, or
// false if there are fewer. again skips a match next to c, as ; and , do.
func find(t Text, c, n int, cmd string, again bool) (int, bool) {
	key, ch := cmd[:1], cmd[1:]
	start, end := LineStart(t, c), LineEnd(t, c)
	p := c
	if key == "f" || key == "t" {
		from := next(t, c, end)
		if key == "t" && again {
			from = next(t, from, end)
		}
		for range n {
			i := strings.Index(t.Slice(from, end), ch)
			if i < 0 {
				return c, false
			}
			p = from + i
			from = p + len(ch)
		}
		if key == "t" {
			p = prev(t, start, p)
		}
		return p, true
	}
	to := c
	if key == "T" && again {
		to = prev(t, start, to)
	}
	for range n {
		i := strings.LastIndex(t.Slice(start, to), ch)
		if i < 0 {
			return c, false
		}
		p = start + i
		to = p
	}
	if key == "T" {
		p = next(t, p, end)
	}
	return p, true
}

// wordForward is w, or W when big is set. An empty line counts as a word.
// Under an operator the last word stops at its line's end.
func wordForward(t Text, p, n int, big, op bool) int {
	end := t.Len()
	for i := range n {
		if c := class(t, p, big); c != 0 {
			for p < end && class(t, p, big) == c {
				p = next(t, p, end)
			}
		}
		for p < end && class(t, p, big) == 0 {
			if at(t, p) != '\n' {
				p = next(t, p, end)
				continue
			}
			if p++; op && i == n-1 || p == end || at(t, p) == '\n' {
				break
			}
		}
	}
	return p
}

// wordEnd is e, or E when big is set. With stop set, a cursor on a word's
// last character stays there, as cw needs.
func wordEnd(t Text, p, n int, big, stop bool) int {
	end := t.Len()
	for i := range n {
		if !stop || i > 0 || class(t, p, big) == 0 {
			p = next(t, p, end)
		}
		for p < end && class(t, p, big) == 0 {
			p = next(t, p, end)
		}
		c := class(t, p, big)
		for p < end {
			q := next(t, p, end)
			if q >= end || class(t, q, big) != c {
				break
			}
			p = q
		}
	}
	return p
}

// wordBack is b, or B when big is set. An empty line counts as a word.
func wordBack(t Text, p, n int, big bool) int {
	for range n {
		if p == 0 {
			break
		}
		p = prev(t, 0, p)
		for p > 0 && class(t, p, big) == 0 && !emptyLine(t, p) {
			p = prev(t, 0, p)
		}
		if c := class(t, p, big); c != 0 {
			for p > 0 {
				q := prev(t, 0, p)
				if class(t, q, big) != c {
					break
				}
				p = q
			}
		}
	}
	return p
}

// wordEndBack is ge, or gE when big is set. An empty line counts as a word.
func wordEndBack(t Text, p, n int, big bool) int {
	for range n {
		if p == 0 {
			break
		}
		c := class(t, p, big)
		q := prev(t, 0, p)
		for c != 0 && q > 0 && class(t, q, big) == c {
			q = prev(t, 0, q)
		}
		for q > 0 && class(t, q, big) == 0 && !emptyLine(t, q) {
			q = prev(t, 0, q)
		}
		p = q
	}
	return p
}

// class is 0 for a blank, 1 for punctuation and 2 for a word character.
// With big set every non-blank is 1.
func class(t Text, p int, big bool) int {
	if p >= t.Len() {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(t.Slice(p, min(p+utf8.UTFMax, t.Len())))
	switch {
	case r == ' ' || r == '\t' || r == '\n':
		return 0
	case big:
		return 1
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 2
	}
	return 1
}

func at(t Text, p int) byte {
	if p >= t.Len() {
		return 0
	}
	return t.Slice(p, p+1)[0]
}

func emptyLine(t Text, p int) bool {
	return (p == t.Len() || at(t, p) == '\n') && (p == 0 || at(t, p-1) == '\n')
}

// down returns the start of the line n lines below the one at start, or of
// the last line.
func down(t Text, start, n int) int {
	return t.LineStart(min(t.LineOf(start)+n, t.Lines()-1))
}

// foldDown returns the start of the line n lines below the one at start, a
// closed fold counting as one line, or of the last line (B223).
func (m *Machine) foldDown(t Text, start, n int) int {
	line := t.LineOf(start)
	for ; n > 0; n-- {
		_, last := m.h.Fold(line)
		if last+1 >= t.Lines() {
			break
		}
		line, _ = m.h.Fold(last + 1)
	}
	return t.LineStart(line)
}

// foldUp returns the start of the line n lines above the one at start, a
// closed fold counting as one line, or 0 (B223).
func (m *Machine) foldUp(t Text, start, n int) int {
	line, _ := m.h.Fold(t.LineOf(start))
	for ; n > 0 && line > 0; n-- {
		line, _ = m.h.Fold(line - 1)
	}
	return t.LineStart(line)
}

// folded widens the linewise r to the closed folds its ends are in.
func (m *Machine) folded(t Text, r region) region {
	first, _ := m.h.Fold(t.LineOf(r.start))
	_, last := m.h.Fold(t.LineOf(r.end))
	r.start, r.end = t.LineStart(first), LineEnd(t, t.LineStart(last))
	return r
}

// up returns the start of the line n lines above the one at start, or 0.
func up(t Text, start, n int) int {
	return t.LineStart(max(t.LineOf(start)-n, 0))
}

// column returns the display column of pos on the line at start.
func column(t Text, start, pos, tab int) int {
	return textWidth(t.Slice(start, pos), 0, tab)
}

// textWidth returns the display width of s starting at column col.
func textWidth(s string, col, tab int) int {
	w := 0
	for g := graphemes.FromString(s); g.Next(); {
		w += width(g.Value(), col+w, tab)
	}
	return w
}

// colStart returns the first cluster on the line at start that begins at or
// after display column col, or the line's end.
func colStart(t Text, start, col, tab int) int {
	end := LineEnd(t, start)
	x := 0
	for g := graphemes.FromString(t.Slice(start, end)); g.Next(); {
		if x >= col {
			return start + g.Start()
		}
		x += width(g.Value(), x, tab)
	}
	return end
}

// atColumn returns the cluster covering col on the line at start, or the
// line's last cluster.
func atColumn(t Text, start, col, tab int) int {
	p, x := start, 0
	for g := graphemes.FromString(t.Slice(start, LineEnd(t, start))); g.Next(); {
		p = start + g.Start()
		if x += width(g.Value(), x, tab); x > col {
			break
		}
	}
	return p
}

// screenLine returns where n rows below the cursor, or above it when down
// is false, the cursor goes on lines that wrap at the view's width, at the
// cursor's column in its row, or false when there is no row to move to.
// The rows are as the host shows them, inlines included (B295).
func (m *Machine) screenLine(t Text, c, n int, down bool) (int, bool) {
	tab := m.h.TabWidth()
	_, _, cols := m.h.Viewport()
	ls := LineStart(t, c)
	starts, in := m.rows(t, ls, cols)
	r := len(starts) - 1
	for r > 0 && starts[r] > c {
		r--
	}
	at := Column(t, ls, c, tab, in) - Column(t, ls, starts[r], tab, in)
	// A count past the last row stops there, and no row to move to fails.
moves:
	for i := range n {
		switch {
		case down && r+1 < len(starts):
			r++
		case down && LineEnd(t, ls) < t.Len():
			ls = LineEnd(t, ls) + 1
			starts, in = m.rows(t, ls, cols)
			r = 0
		case !down && r > 0:
			r--
		case !down && ls > 0:
			ls = LineStart(t, ls-1)
			starts, in = m.rows(t, ls, cols)
			r = len(starts) - 1
		case i == 0:
			return 0, false
		default:
			break moves
		}
	}
	// The place at the same column of the row, or the row's last.
	end := LineEnd(t, ls)
	if r+1 < len(starts) {
		end = starts[r+1]
	}
	to, base := starts[r], Column(t, ls, starts[r], tab, in)
	walk(t, ls, tab, in, func(p, x, w int) bool {
		if p < starts[r] {
			return true
		}
		if p >= end && p > starts[r] {
			return false
		}
		to = p
		return x-base+w <= at
	})
	return clamp(t, to), true
}

func width(cluster string, col, tab int) int {
	if cluster == "\t" {
		return tab - col%tab
	}
	return displaywidth.String(cluster)
}

// chunk is how many bytes line scans read at a time.
const chunk = 256

// LineStart returns the start of the line holding pos.
func LineStart(t Text, pos int) int {
	for pos > 0 {
		lo := max(pos-chunk, 0)
		if i := strings.LastIndexByte(t.Slice(lo, pos), '\n'); i >= 0 {
			return lo + i + 1
		}
		pos = lo
	}
	return 0
}

// LineEnd returns the end of the line holding pos, before its newline.
func LineEnd(t Text, pos int) int {
	n := t.Len()
	for pos < n {
		hi := min(pos+chunk, n)
		if i := strings.IndexByte(t.Slice(pos, hi), '\n'); i >= 0 {
			return pos + i
		}
		pos = hi
	}
	return n
}

// window is how far a grapheme lookup reads. Real clusters are shorter.
const window = 128

// next returns the end of the grapheme cluster at pos, or end.
func next(t Text, pos, end int) int {
	if pos >= end {
		return end
	}
	g := graphemes.FromString(t.Slice(pos, min(end, pos+window)))
	g.Next()
	return pos + g.End()
}

// prev returns the start of the grapheme cluster before pos, or start.
func prev(t Text, start, pos int) int {
	lo := max(start, pos-window)
	for lo < pos && !utf8.RuneStart(at(t, lo)) {
		lo++
	}
	p := start
	for g := graphemes.FromString(t.Slice(lo, pos)); g.Next(); {
		p = lo + g.Start()
	}
	return p
}

func firstNonBlank(t Text, start, end int) int {
	line := t.Slice(start, end)
	if i := strings.IndexFunc(line, func(r rune) bool { return r != ' ' && r != '\t' }); i >= 0 {
		return start + i
	}
	return end
}
