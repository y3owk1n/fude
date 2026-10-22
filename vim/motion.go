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
		var p int
		k := exclusive
		switch {
		case back && end:
			p, k = wordEndBack(x.t, x.c, x.n, big), inclusive
		case back:
			p = wordBack(x.t, x.c, x.n, big)
		case end:
			p, k = wordEnd(x.t, x.c, x.n, big, false), inclusive
		default:
			p = wordForward(x.t, x.c, x.n, big, false)
		}
		// One with nowhere to go, at either end of the text, fails.
		if clamp(x.t, p) == x.c {
			return x.c, failed
		}
		return m.moved(p, k, false)
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
		// The row starts at the first cluster left columns in, counted from
		// a place the host knows the column of when it is before left.
		tab := m.h.TabWidth()
		from, x := m.h.RowFrom(ls, c)
		for from > ls && x > left {
			from, x = m.h.RowFrom(ls, from-1)
		}
		start, sx := ColumnStartFrom(t, from, x, left, tab, nil)
		end, _ := ColumnStartFrom(t, start, sx, left+shown, tab, nil)
		return start, end, shown
	}
	rs := m.rowsAt(t, ls, c, cols, 0, 1)
	if rs.r+1 < len(rs.starts) {
		le = rs.starts[rs.r+1]
	}
	return rs.starts[rs.r], le, cols
}

// rows is some rows of a line as the host wraps it: where each begins,
// the index of the one asked for, the display column the first begins at,
// and the inlines and hang the rows count (B295, B489). known is a place
// on the row asked for whose column the host gave, and knownX that column.
type rows struct {
	starts        []int
	r, x          int
	in            []Inline
	hang          int
	known, knownX int
}

// rowsAt returns the rows of the line at ls, as the host wraps it at cols,
// from the one that holds p: it and up to before rows before it and after
// rows after it. A walk starts from a row the host gives near where it
// goes, in place of the line's start.
func (m *Machine) rowsAt(t Text, ls, p, cols, before, after int) rows {
	in, hang, tab := m.h.Shown(ls, LineEnd(t, ls)), m.h.Hang(ls), m.h.TabWidth()
	// Without wrap a line is one row, which columns count on from a place
	// the host knows.
	if cols == math.MaxInt {
		from, x := m.h.RowFrom(ls, p)
		return rows{starts: []int{ls}, in: in, known: from, knownX: x}
	}
	wr := Wrap{Cols: cols, Hang: hang, Tab: tab, In: in, Words: m.h.WordWrap()}
	for back := p; ; {
		from, col := m.h.RowFrom(ls, back)
		starts := RowsFrom(t, ls, from, col, math.MaxInt, p, wr, nil)
		r := len(starts) - 1
		for r > 0 && starts[r] > p {
			r--
		}
		if r < before && from > ls {
			back = from - 1
			continue
		}
		k := max(r-before, 0)
		x := ColumnFrom(t, from, col, starts[k], tab, nil)
		starts, r = starts[k:], r-k
		if more := after + 1 - (len(starts) - 1 - r); more > 0 && len(starts)-1 > r {
			last := starts[len(starts)-1]
			starts = append(starts, RowsFrom(t, ls, last, ColumnFrom(t, starts[0], x, last, tab, nil), more+1, math.MaxInt, wr, nil)[1:]...)
		}
		return rows{starts, r, x, in, hang, starts[r], ColumnFrom(t, starts[0], x, starts[r], tab, nil)}
	}
}

// column returns the display columns from where row i of rs begins to the
// place pos on it, inlines counting.
func (rs rows) column(t Text, ls, i, pos, tab int) int {
	x0, at := ColumnFrom(t, rs.starts[0], rs.x, rs.starts[i], tab, nil), 0
	if len(rs.in) == 0 && i == rs.r && rs.known >= rs.starts[i] && rs.known <= pos {
		return ColumnFrom(t, rs.known, rs.knownX, pos, tab, nil) - x0
	}
	walkFrom(t, ls, rs.starts[i], x0, tab, rs.in, func(p, x, w int, _ string) bool {
		at = x + w - x0
		if p >= pos {
			at = x - x0
		}
		return p < pos
	}, nil, nil)
	return at
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
	return m.seekOffset(x.c, m.search.pattern, m.search.offset, m.search.forward == (x.cmd == "n"), x.n, true)
}

// starMotion is * and #, and g* and g#, which match the word inside others
// too.
func starMotion(m *Machine, x motionCall) (int, kind) {
	word, ok := keywordAt(x.t, x.c)
	if !ok {
		m.msg = "no word under the cursor"
		return x.c, failed
	}
	if x.cmd[0] != 'g' {
		word = `\<` + word + `\>`
	}
	m.search.pattern, m.search.forward, m.search.offset = word, strings.HasSuffix(x.cmd, "*"), ""
	return m.seek(x.c, m.search.pattern, m.search.forward, x.n)
}

// declMotion is gd and gD, which go to the first match of the word under
// the cursor in the text, or for gd from the blank line above the line
// starting with { above the cursor.
func declMotion(m *Machine, x motionCall) (int, kind) {
	word, ok := keywordAt(x.t, x.c)
	if !ok {
		m.msg = "no word under the cursor"
		return x.c, failed
	}
	m.search.pattern, m.search.forward, m.search.offset = `\<`+word+`\>`, true, ""
	p, err := m.compile(m.search.pattern)
	if err != nil {
		m.msg = err.Error()
		return x.c, failed
	}
	line := 0
	if x.cmd == "gd" {
		line = x.t.LineOf(x.c)
		for line > 0 && at(x.t, x.t.LineStart(line)) != '{' {
			line--
		}
		for line > 0 && LineEnd(x.t, x.t.LineStart(line)) > x.t.LineStart(line) {
			line--
		}
	}
	m.hl = true
	pos, _ := p.first(x.t, x.t.LineStart(line), x.t.Len()+1)
	return pos, exclusive
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
	p, ok := m.mark(x.t, x.c, name)
	t := x.t
	// A file mark in another text shows that text, but not for an
	// operator, which acts within one.
	if !ok && name >= 'A' && name <= 'Z' && x.op == "" && m.h.ShowMark(name) {
		t = m.h.Text()
		p, ok = m.h.Mark(name)
	}
	switch {
	case !ok:
		m.msg = "mark not set"
		return x.c, failed
	case x.cmd[0] == '\'':
		s := LineStart(t, p)
		return firstNonBlank(t, s, LineEnd(t, s)), linewise
	}
	return p, exclusive
}

// searchForMotion is / and ? with a pattern. It starts where the search
// typed started, which <C-g> and <C-t> move. The pattern is cut at the
// first delim not after a backslash, and what follows is the offset, as
// /pat/e+1. An empty pattern is the last one.
func searchForMotion(m *Machine, x motionCall) (int, kind) {
	parts := splitDelim(x.cmd[1:], x.cmd[0])
	if parts[0] != "" {
		m.search.pattern = parts[0]
	}
	m.search.forward, m.search.offset = x.cmd[0] == '/', ""
	if len(parts) > 1 {
		m.search.offset = parts[1]
	}
	return m.seekOffset(m.searchFrom, m.search.pattern, m.search.offset, m.search.forward, x.n, false)
}

// mark returns where mark name is. ( ) { } are where those motions go from
// c.
func (m *Machine) mark(t Text, c int, name rune) (int, bool) {
	switch name {
	case '(':
		return sentenceBack(t, c, 1), true
	case ')':
		return sentenceForward(t, c, 1), true
	case '{':
		return paragraphBack(t, c, 1), true
	case '}':
		return paragraphForward(t, c, 1), true
	}
	return m.h.Mark(name)
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
		n := x.n
		// Past the line's end it comes back over the columns there first
		// (B490).
		if past := m.Past(); x.op == "" && past > 0 {
			if n < past {
				m.want = column(x.t, x.sol, x.c, m.h.TabWidth()) + past - n
				return m.moved(x.c, exclusive, true)
			}
			n -= past
		}
		p := x.c
		for range n {
			p = prev(x.t, x.sol, p)
		}
		return m.moved(p, exclusive, false)
	}},
	"l": {"moves right", func(m *Machine, x motionCall) (int, kind) {
		end := LineEnd(x.t, x.c)
		p := x.c
		for range x.n {
			p = next(x.t, p, end)
		}
		// With VirtualEdit, it goes on past the line's end (B490).
		if x.op == "" && m.virtual() && p == end {
			m.want = m.shownColumn(x.t, x.c) + m.Past() + x.n
			return m.moved(end, exclusive, true)
		}
		return m.moved(p, exclusive, false)
	}},
	"j":  {"moves down a line", lineMotion},
	"k":  {"moves up a line", lineMotion},
	"gg": {"goes to the first line, or line n", lineMotion},
	"G":  {"goes to the last line, or line n", lineMotion},
	"gj": {"moves down a row on the screen", screenMotion},
	"gk": {"moves up a row on the screen", screenMotion},
	"]C": {"goes to the next multicursor, n times", func(m *Machine, x motionCall) (int, kind) {
		return m.toCursor(x)
	}},
	"[C": {"goes to the previous multicursor, n times", func(m *Machine, x motionCall) (int, kind) {
		return m.toCursor(x)
	}},
	"<C-End>": {"goes to the last character of the last line, or of line n", func(m *Machine, x motionCall) (int, kind) {
		line := LineStart(x.t, x.t.Len())
		if x.count > 0 {
			line = down(x.t, 0, x.count-1)
		}
		return m.moved(lastChar(x.t, LineEnd(x.t, line)), inclusive, false)
	}},
	"<BS>": {"moves left, n characters, onto the line before at a line's start", func(m *Machine, x motionCall) (int, kind) {
		if x.c == 0 {
			return x.c, failed
		}
		p := x.c
		for range x.n {
			if p == 0 {
				break
			}
			// From a line's start it goes to the last character of the line
			// before, and after an operator to its line break, which the
			// operator then takes.
			if p == LineStart(x.t, p) {
				if p--; x.op == "" && p > LineStart(x.t, p) {
					p = prev(x.t, LineStart(x.t, p), p)
				}
				m.wrapped = x.op != ""
				continue
			}
			p = prev(x.t, LineStart(x.t, p), p)
		}
		return m.moved(p, exclusive, false)
	}},
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
	"g*":             {"searches forward for the word under the cursor, also inside other words", starMotion},
	"g#":             {"searches back for the word under the cursor, also inside other words", starMotion},
	"gd":             {"goes to the first match of the word under the cursor in the function", declMotion},
	"gD":             {"goes to the first match of the word under the cursor in the file", declMotion},
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

// VirtualEdit sets where the cursor may go past the end of its line. It
// goes on in a block selection for block, in normal and visual mode for
// all, and one column past its last character in normal mode for onemore. Any other
// value keeps it on the line (B490).
func (m *Machine) VirtualEdit(where string) { m.virtualEdit = where }

// Past returns how many columns past the end of its line the cursor shows,
// for a host to draw it there (B490).
func (m *Machine) Past() int {
	t, c := m.h.Text(), m.h.Cursor()
	if !m.virtual() || c != m.pastAt || c != LineEnd(t, c) {
		return 0
	}
	return m.past
}

// virtual reports whether the cursor may go past its line's end now.
func (m *Machine) virtual() bool {
	switch m.virtualEdit {
	case "all":
		return m.mode == Normal || m.mode == Visual
	case "block":
		return m.mode == Visual && m.visual == "<C-v>"
	}
	return false
}

// land returns where a motion that ends at p leaves the cursor, and keeps
// how far past its line's end it shows. Past the line's last character it
// goes one column with onemore, and with VirtualEdit out to the column j
// and k aim for (B490).
func (m *Machine) land(t Text, p int) int {
	m.past = 0
	onemore := m.virtualEdit == "onemore" && m.mode == Normal
	if !m.virtual() && !onemore {
		return clamp(t, p)
	}
	if !m.wantSet || m.want == math.MaxInt {
		return p
	}
	end := LineEnd(t, p)
	w := column(t, LineStart(t, p), end, m.h.TabWidth())
	if m.want < w {
		return p
	}
	if m.virtual() {
		m.pastAt, m.past = end, m.want-w
	}
	return end
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
	if !m.wantSet {
		m.want, m.wantSet = m.shownColumn(t, c), true
	}
	return atColumn(t, line, m.want, m.h.TabWidth())
}

// shownColumn is the column the cursor at c shows in. Outside insert mode
// that is a tab's last column, as Vim draws it there, and else it is the
// first column of the character.
func (m *Machine) shownColumn(t Text, c int) int {
	tab := m.h.TabWidth()
	ls := LineStart(t, c)
	if m.mode != Insert && m.mode != Replace && c < LineEnd(t, c) && t.Slice(c, c+1) == "\t" {
		return column(t, ls, c+1, tab) - 1
	}
	return column(t, ls, c, tab)
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
	case isWord(r):
		return 2
	}
	return 1
}

// isWord reports whether r is part of a word: a letter, a digit or _.
func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

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
func column(t Text, start, pos, tab int) int { return ColumnFrom(t, start, 0, pos, tab, nil) }

// marks is how far apart the places a walk of a line tells of are.
const marks = 64 << 10

// marker returns note and spoil. note tells mark of a place marks bytes
// past the last one it told of, from from on, and whether only printable
// ASCII came between. spoil records that something else came.
func marker(from int, mark func(p, x int, plain bool)) (note func(p, x int), spoil func()) {
	plain := true
	note = func(p, x int) {
		if mark != nil && p-from >= marks {
			mark(p, x, plain)
			from, plain = p, true
		}
	}
	return note, func() { plain = false }
}

// printable reports whether c is one printable ASCII character.
func printable(c string) bool { return len(c) == 1 && c[0] >= ' ' && c[0] < 0x7f }

// ColumnFrom returns the display column of pos on its line given that the
// cluster at from, before it on the line, starts at column x. Given mark, it
// tells mark of clusters it passes, marks bytes apart, their columns and
// whether only printable ASCII came since the last.
func ColumnFrom(t Text, from, x, pos, tab int, mark func(p, x int, plain bool)) int {
	note, spoil := marker(from, mark)
	clusters(t, from, pos, func(p int, c string) bool {
		note(p, x)
		if !printable(c) {
			spoil()
		}
		x += width(c, x, tab)
		return true
	}, func(p, n int) bool {
		note(p, x)
		x += n
		return true
	}, nil)
	return x
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
	p, _ := ColumnStartFrom(t, start, 0, col, tab, nil)
	return p
}

// ColumnStartFrom returns the first cluster at or after display column col
// on the line from from on, or the line's end, and the column it starts at,
// given that the cluster at from starts at column x. It tells mark of
// clusters as ColumnFrom does.
func ColumnStartFrom(t Text, from, x, col, tab int, mark func(p, x int, plain bool)) (int, int) {
	at := LineEnd(t, from)
	note, spoil := marker(from, mark)
	clusters(t, from, at, func(p int, c string) bool {
		if x >= col {
			at = p
			return false
		}
		note(p, x)
		if !printable(c) {
			spoil()
		}
		x += width(c, x, tab)
		return true
	}, func(p, n int) bool {
		note(p, x)
		if x+n > col {
			at, x = p+max(col-x, 0), max(x, col)
			return false
		}
		x += n
		return true
	}, nil)
	return at, x
}

// atColumn returns the cluster covering col on the line at start, or the
// line's last cluster.
func atColumn(t Text, start, col, tab int) int {
	at, x := start, 0
	clusters(t, start, LineEnd(t, start), func(p int, c string) bool {
		at = p
		x += width(c, x, tab)
		return x <= col
	}, func(p, n int) bool {
		if x+n > col {
			at = p + max(col-x, 0)
			return false
		}
		at, x = p+n-1, x+n
		return true
	}, nil)
	return at
}

// screenLine returns where n rows below the cursor, or above it when down
// is false, the cursor goes on lines that wrap at the view's width, at the
// cursor's column in its row, or false when there is no row to move to.
// The rows are as the host shows them, inlines included (B295).
func (m *Machine) screenLine(t Text, c, n int, down bool) (int, bool) {
	tab := m.h.TabWidth()
	_, _, cols := m.h.Viewport()
	ls := LineStart(t, c)
	before, after := n, 0
	if down {
		before, after = 0, n
	}
	rs := m.rowsAt(t, ls, c, cols, before, after)
	at := rs.column(t, ls, rs.r, c, tab)
	if rs.starts[rs.r] > ls {
		at += rs.hang
	}
	// A count past the last row stops there, and no row to move to fails.
moves:
	for i := range n {
		switch {
		case down && rs.r+1 < len(rs.starts):
			rs.r++
		case down && LineEnd(t, ls) < t.Len():
			ls = LineEnd(t, ls) + 1
			rs = m.rowsAt(t, ls, ls, cols, 0, n-i)
		case !down && rs.r > 0:
			rs.r--
		case !down && ls > 0:
			ls = LineStart(t, ls-1)
			rs = m.rowsAt(t, ls, LineEnd(t, ls), cols, n-i, 0)
		case i == 0:
			return 0, false
		default:
			break moves
		}
	}
	// The place at the same column of the row, or the row's last.
	start, end := rs.starts[rs.r], LineEnd(t, ls)
	if rs.r+1 < len(rs.starts) {
		end = rs.starts[rs.r+1]
	}
	if start > ls {
		at -= rs.hang
	}
	to, x0 := start, ColumnFrom(t, rs.starts[0], rs.x, start, tab, nil)
	walkFrom(t, ls, start, x0, tab, rs.in, func(p, x, w int, _ string) bool {
		if p >= end && p > start {
			return false
		}
		to = p
		return x-x0+w <= at
	}, nil, nil)
	return clamp(t, to), true
}

func width(cluster string, col, tab int) int {
	switch {
	case cluster == "\t":
		return tab - col%tab
	case printable(cluster):
		return 1
	}
	return displaywidth.String(cluster)
}

// piece is how many bytes a walk over a line reads at a time.
const piece = 4096

// clusters calls f with where each grapheme cluster from start to end
// begins and the cluster, until f returns false. Given run, it calls run in
// its place with where a run of printable ASCII begins and how long it is,
// each character a cluster one column wide, which stops when it returns
// false. Given plain too, which returns where printable ASCII the caller
// knows of from p goes on to, it takes that text to run unread. It reads
// the rest a piece at a time, and an ASCII character before another is a
// cluster of its own.
func clusters(t Text, start, end int, f func(p int, c string) bool, run func(p, n int) bool, plain func(p int) int) {
	for p := start; p < end; {
		// The last character known may begin a cluster with what follows.
		if q := p; run != nil && plain != nil {
			if q = min(plain(p), end); q < end {
				q--
			}
			if q-p > 1 {
				if !run(p, q-p) {
					return
				}
				p = q
				continue
			}
		}
		hi := min(p+piece, end)
		s, i := t.Slice(p, hi), 0
		for i < len(s) {
			if run != nil {
				j := i
				for j < len(s) && s[j] >= ' ' && s[j] < 0x7f {
					j++
				}
				// The run's last character may begin a cluster with what follows.
				if j < len(s) && s[j] >= utf8.RuneSelf || j == len(s) && hi < end {
					j--
				}
				if j-i > 1 {
					if !run(p+i, j-i) {
						return
					}
					i = j
					continue
				}
			}
			n := 1
			if s[i] >= utf8.RuneSelf || i+1 < len(s) && (s[i+1] >= utf8.RuneSelf || s[i] == '\r') {
				g := graphemes.FromString(s[i:])
				g.Next()
				n = g.End()
			}
			// A cluster at the end of the piece may go on past it.
			if i > 0 && i+n == len(s) && hi < end {
				break
			}
			if !f(p+i, s[i:i+n]) {
				return
			}
			i += n
		}
		p += i
	}
}

// chunk is how many bytes line scans read at a time.
const chunk = 256

// LineStart returns the start of the line holding pos. A line longer than
// a chunk is found by its number.
func LineStart(t Text, pos int) int {
	lo := max(pos-chunk, 0)
	if i := strings.LastIndexByte(t.Slice(lo, pos), '\n'); i >= 0 || lo == 0 {
		return lo + i + 1
	}
	return t.LineStart(t.LineOf(pos))
}

// LineEnd returns the end of the line holding pos, before its newline.
func LineEnd(t Text, pos int) int {
	n := t.Len()
	hi := min(pos+chunk, n)
	if i := strings.IndexByte(t.Slice(pos, hi), '\n'); i >= 0 {
		return pos + i
	}
	if hi == n {
		return n
	}
	if line := t.LineOf(pos) + 1; line < t.Lines() {
		return t.LineStart(line) - 1
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
	for p := start; p < end; p += chunk {
		s := t.Slice(p, min(p+chunk, end))
		if i := strings.IndexFunc(s, func(r rune) bool { return r != ' ' && r != '\t' }); i >= 0 {
			return p + i
		}
	}
	return end
}
