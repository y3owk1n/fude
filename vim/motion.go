package vim

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Tabstop is the width of a tab.
const Tabstop = 8

// kind is how an operator treats the text a motion crosses.
type kind int

const (
	exclusive kind = iota
	inclusive
	linewise
	failed // the motion could not move, so an operator does nothing
)

// motion returns where cmd moves the cursor and its kind, or false if cmd is
// not a motion.
func (m *Machine) motion(cmd string, count int) (int, kind, bool) {
	t, c := m.h.Text(), m.h.Cursor()
	n := max(count, 1)
	start := LineStart(t, c)
	if cmd == "gj" || cmd == "gk" {
		p, ok := m.screenLine(t, c, n, cmd == "gj")
		if !ok {
			return c, failed, true
		}
		return p, exclusive, true
	}
	p, k := c, exclusive
	keep := false // whether to keep the column j and k aim for
	switch cmd {
	case "h":
		for range n {
			p = prev(t, start, p)
		}
	case "l":
		for range n {
			p = next(t, p, LineEnd(t, c))
		}
	case "j", "k", "gg", "G":
		line := LineStart(t, t.Len())
		switch {
		case cmd == "j":
			line = down(t, start, n)
		case cmd == "k":
			line = up(t, start, n)
		case cmd == "gg" || count > 0:
			line = down(t, 0, n-1)
		}
		if line == start && (cmd == "j" || cmd == "k") {
			return c, failed, true
		}
		p, k, keep = m.toLine(t, c, line), linewise, true
	case "go":
		// Byte n of the text, counting from 1, at the start of its character.
		p = min(n-1, t.Len())
		for p > 0 && p < t.Len() && !utf8.RuneStart(t.Slice(p, p+1)[0]) {
			p--
		}
		p = clamp(t, p)
	case "H", "M", "L":
		p, k, keep = m.toLine(t, c, m.window(t, cmd, n)), linewise, true
	case "%":
		if count > 0 {
			p, k, keep = m.toLine(t, c, down(t, 0, (count*t.Lines()+99)/100-1)), linewise, true
			break
		}
		var ok bool
		if p, ok = match(t, c); !ok {
			return c, failed, true
		}
		k = inclusive
	case ")":
		p = sentenceForward(t, c, n)
	case "(":
		p = sentenceBack(t, c, n)
	case "}":
		p = paragraphForward(t, c, n)
	case "{":
		p = paragraphBack(t, c, n)
	case "0":
		p = start
	case "^":
		p = firstNonBlank(t, start, LineEnd(t, c))
	case "$":
		p, k = lastChar(t, LineEnd(t, down(t, start, n-1))), inclusive
		m.want, keep = math.MaxInt, true
	case "w", "W":
		p = wordForward(t, c, n, cmd == "W", false)
	case "b", "B":
		p = wordBack(t, c, n, cmd == "B")
	case "e", "E":
		p, k = wordEnd(t, c, n, cmd == "E", false), inclusive
	case "n", "N":
		return m.seek(m.search.pattern, m.search.forward == (cmd == "n"), n)
	case "*", "#":
		word, ok := keywordAt(t, c)
		if !ok {
			m.msg = "no word under the cursor"
			return c, failed, true
		}
		m.search.pattern, m.search.forward = `\<`+word+`\>`, cmd == "*"
		return m.seek(m.search.pattern, m.search.forward, n)
	case ";", ",":
		f := m.find
		if f == "" {
			return c, failed, true
		}
		if cmd == "," {
			f = reverse[f[:1]] + f[1:]
		}
		p, ok := find(t, c, n, f, true)
		return m.found(p, f, ok)
	default:
		if (cmd[0] == '`' || cmd[0] == '\'') && len(cmd) == 2 {
			p, ok := m.h.Mark(rune(cmd[1]))
			switch {
			case !ok:
				m.msg = "mark not set"
				return c, failed, true
			case cmd[0] == '\'':
				s := LineStart(t, p)
				return firstNonBlank(t, s, LineEnd(t, s)), linewise, true
			}
			return p, exclusive, true
		}
		if cmd[0] == '/' || cmd[0] == '?' {
			m.search.pattern, m.search.forward = cmd[1:], cmd[0] == '/'
			return m.seek(m.search.pattern, m.search.forward, n)
		}
		if len(cmd) < 2 || !strings.Contains("ftFT", cmd[:1]) || !Key(cmd[1:]).isChar() {
			return c, exclusive, false
		}
		m.find = cmd
		p, ok := find(t, c, n, cmd, false)
		return m.found(p, cmd, ok)
	}
	m.wantSet = keep
	return p, k, true
}

// found turns a find's result into a motion. f and t are inclusive, F and T
// exclusive.
func (m *Machine) found(p int, f string, ok bool) (int, kind, bool) {
	m.wantSet = false
	switch {
	case !ok:
		return p, failed, true
	case f[0] == 'f' || f[0] == 't':
		return p, inclusive, true
	}
	return p, exclusive, true
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
		m.want, m.wantSet = column(t, LineStart(t, c), c), true
	}
	return atColumn(t, line, m.want)
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
	depth := 0
	for q := p + 1; q < t.Len(); q++ {
		switch at(t, q) {
		case open:
			depth++
		case close:
			if depth == 0 {
				return q, true
			}
			depth--
		}
	}
	return c, false
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

// up returns the start of the line n lines above the one at start, or 0.
func up(t Text, start, n int) int {
	return t.LineStart(max(t.LineOf(start)-n, 0))
}

// column returns the display column of pos on the line at start.
func column(t Text, start, pos int) int {
	return textWidth(t.Slice(start, pos), 0)
}

// textWidth returns the display width of s starting at column col.
func textWidth(s string, col int) int {
	w := 0
	for g := graphemes.FromString(s); g.Next(); {
		w += width(g.Value(), col+w)
	}
	return w
}

// colStart returns the first cluster on the line at start that begins at or
// after display column col, or the line's end.
func colStart(t Text, start, col int) int {
	end := LineEnd(t, start)
	x := 0
	for g := graphemes.FromString(t.Slice(start, end)); g.Next(); {
		if x >= col {
			return start + g.Start()
		}
		x += width(g.Value(), x)
	}
	return end
}

// atColumn returns the cluster covering col on the line at start, or the
// line's last cluster.
func atColumn(t Text, start, col int) int {
	p, x := start, 0
	for g := graphemes.FromString(t.Slice(start, LineEnd(t, start))); g.Next(); {
		p = start + g.Start()
		if x += width(g.Value(), x); x > col {
			break
		}
	}
	return p
}

// screenLine returns where n rows below the cursor, or above it when down
// is false, the cursor goes on lines that wrap at the view's width, at the
// cursor's column in its row, or false when there is no row to move to.
func (m *Machine) screenLine(t Text, c, n int, down bool) (int, bool) {
	_, _, cols := m.h.Viewport()
	ls := LineStart(t, c)
	rows := Rows(t, ls, cols)
	r := len(rows) - 1
	for r > 0 && rows[r] > c {
		r--
	}
	at := column(t, ls, c) - column(t, ls, rows[r])
	// A count past the last row stops there, and no row to move to fails.
moves:
	for i := range n {
		switch {
		case down && r+1 < len(rows):
			r++
		case down && LineEnd(t, ls) < t.Len():
			ls = LineEnd(t, ls) + 1
			rows, r = Rows(t, ls, cols), 0
		case !down && r > 0:
			r--
		case !down && ls > 0:
			ls = LineStart(t, ls-1)
			rows = Rows(t, ls, cols)
			r = len(rows) - 1
		case i == 0:
			return 0, false
		default:
			break moves
		}
	}
	// The cluster at the same column of the row, or the row's last.
	end := LineEnd(t, ls)
	if r+1 < len(rows) {
		end = rows[r+1]
	}
	p, x := rows[r], 0
	base := column(t, ls, rows[r])
	for g := graphemes.FromString(t.Slice(rows[r], end)); g.Next(); {
		p = rows[r] + g.Start()
		if x += width(g.Value(), base+x); x > at {
			break
		}
	}
	return clamp(t, p), true
}

// Rows returns where each screen row of the line at start begins when it
// wraps at cols columns: start, then each cluster that does not fit on the
// row before it. A tab is as wide as it is at its column in the line.
func Rows(t Text, start, cols int) []int {
	rows := []int{start}
	x, col := 0, 0
	for g := graphemes.FromString(t.Slice(start, LineEnd(t, start))); g.Next(); {
		w := width(g.Value(), col)
		if x > 0 && x+w > cols {
			rows, x = append(rows, start+g.Start()), 0
		}
		x, col = x+w, col+w
	}
	return rows
}

func width(cluster string, col int) int {
	if cluster == "\t" {
		return Tabstop - col%Tabstop
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
