package vim

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// compile returns src as a pattern, reusing the last one compiled.
func (m *Machine) compile(src string) (*pattern, error) {
	// With smartcase, an uppercase letter in the pattern asks for case.
	ignore := m.ignoreCase && !(m.smartCase && hasUpper(src))
	key := patternKey{src, m.lastRep, ignore}
	if p, ok := m.res[key]; ok {
		return p, nil
	}
	p, err := compile(src, m.lastRep, ignore)
	if err != nil {
		return nil, err
	}
	if len(m.res) >= 16 {
		m.res = nil // the patterns typed on the way to this one
	}
	if m.res == nil {
		m.res = map[patternKey]*pattern{}
	}
	m.res[key] = p
	return p, nil
}

// patternKey is what a compiled pattern depends on: its source, the
// replacement ~ stands for, and whether it ignores case.
type patternKey struct {
	src, rep string
	ignore   bool
}

// IgnoreCase sets whether searches and substitutes ignore case, and
// whether a pattern with an uppercase letter matches case anyway (B120).
func (m *Machine) IgnoreCase(ignore, smart bool) { m.ignoreCase, m.smartCase = ignore, smart }

// hasUpper reports whether src has an uppercase letter that is not part
// of a backslash item, such as \S.
func hasUpper(src string) bool {
	for i := 0; i < len(src); i++ {
		switch c := src[i]; {
		case c == '\\':
			i++
		case c >= 'A' && c <= 'Z':
			return true
		}
	}
	return false
}

// seek returns the n-th match of src after c, or before it when searching
// back, wrapping around the ends of the text.
func (m *Machine) seek(c int, src string, forward bool, n int) (int, kind) {
	t := m.h.Text()
	if src == "" {
		m.msg = "no previous search"
		return c, failed
	}
	p, err := m.compile(src)
	if err != nil {
		m.msg = err.Error()
		return c, failed
	}
	pos := c
	for range n {
		// After wrapping, a search reads only up to where it began.
		at, ok := 0, false
		if forward {
			if at, ok = p.first(t, pos+1, t.Len()+1); !ok {
				at, ok = p.first(t, 0, pos+1)
				m.msg = "search hit the end, continuing at the start"
			}
		} else if at, ok = p.last(t, 0, pos); !ok {
			at, ok = p.last(t, pos, t.Len()+1)
			m.msg = "search hit the start, continuing at the end"
		}
		if !ok {
			m.msg = "pattern not found: " + src
			return c, failed
		}
		pos = at
	}
	m.hl = true
	return pos, exclusive
}

// seekOffset is seek with a search offset: e for the match's last
// character, s or b for its first, each with a count of characters to
// move, or a count of lines alone. With e, s or b, a match counts when the
// place the offset names is past the cursor less the count. So /pat/e
// takes the match the cursor is on, and n the next. c is where the search
// starts. again is true for n and N, which start a line offset from the
// line the offset left.
func (m *Machine) seekOffset(c int, src, off string, forward bool, n int, again bool) (int, kind) {
	if off == "" {
		return m.seek(c, src, forward, n)
	}
	t := m.h.Text()
	anchor, count := byte('l'), off
	if strings.ContainsRune("esb", rune(off[0])) {
		anchor, count = off[0], off[1:]
	}
	d := 0
	switch count {
	case "":
	case "+":
		d = 1
	case "-":
		d = -1
	default:
		v, err := strconv.Atoi(count)
		if err != nil {
			m.msg = "bad search offset: " + off
			return c, failed
		}
		d = v
	}
	if anchor == 'l' {
		from := c
		if again {
			from = lineBy(t, LineStart(t, c), -d)
			if forward {
				from = LineEnd(t, from)
			}
		}
		at, k := m.seek(from, src, forward, n)
		if k == failed {
			return c, failed
		}
		return lineBy(t, LineStart(t, at), d), linewise
	}
	p, err := m.compile(src)
	if err != nil {
		m.msg = err.Error()
		return c, failed
	}
	// place is where in a match the offset counts from.
	place := func(at int) int {
		if anchor != 'e' {
			return at
		}
		end := p.endAt(t, at)
		return max(prev(t, LineStart(t, end), end), at)
	}
	from := step(t, c, -d)
	if d > 0 && c < d {
		from = -1 // before the text, so a match at its start counts
	}
	for range n {
		at, ok := p.after(t, from, place, forward)
		if !ok {
			m.msg = "pattern not found: " + src
			return c, failed
		}
		from = place(at)
	}
	m.hl = true
	if anchor == 'e' {
		return step(t, from, d), inclusive
	}
	return step(t, from, d), exclusive
}

// after returns the first match whose place is after from, or searching
// back the last whose place is before it, wrapping around the text.
func (p *pattern) after(t Text, from int, place func(int) int, forward bool) (int, bool) {
	if forward {
		for lo, wrapped := LineStart(t, max(from, 0)), false; ; {
			at, ok := p.first(t, lo, t.Len()+1)
			switch {
			case ok && place(at) > from:
				return at, true
			case ok:
				lo = at + 1
			case wrapped || lo == 0:
				return 0, false
			default:
				lo, wrapped, from = 0, true, -1
			}
		}
	}
	for hi, wrapped := t.Len()+1, false; ; {
		at, ok := p.last(t, 0, hi)
		switch {
		case ok && place(at) < from:
			return at, true
		case ok:
			hi = at
		case wrapped:
			return 0, false
		default:
			hi, wrapped, from = t.Len()+1, true, t.Len()+1
		}
	}
}

// lineBy returns the start of the line n lines below the one starting at
// ls, or above for a negative n.
func lineBy(t Text, ls, n int) int {
	if n < 0 {
		return up(t, ls, -n)
	}
	return down(t, ls, n)
}

// step moves d characters from p, back for a negative d, across lines,
// staying in the text.
func step(t Text, p, d int) int {
	for ; d > 0 && p < t.Len(); d-- {
		p = next(t, p, t.Len())
	}
	for ; d < 0 && p > 0; d++ {
		p = prev(t, 0, p)
	}
	return p
}

// searchWindow is how many bytes a search reads first. It doubles until it
// holds a match, so a search near the cursor never reads the whole text.
const searchWindow = 1 << 10

// first returns the start of the first match that starts at or after least
// and before limit. It reads from least's line, so ^ and \< see the text
// before least.
func (p *pattern) first(t Text, least, limit int) (int, bool) {
	limit = min(limit, t.Len()+1)
	if least >= limit {
		return 0, false
	}
	if p.lit != "" {
		return p.firstLit(t, least, limit)
	}
	from := LineStart(t, least)
	for size := searchWindow; ; size *= 2 {
		to := LineEnd(t, min(from+size, t.Len()))
		retry := false // a match that reaches the window's end
		for _, mt := range p.all(t.Slice(from, to)) {
			s, _ := p.span(mt)
			switch {
			case from+s < least:
			case from+s >= limit:
				return 0, false
			// A match that reaches the window's end may be longer in the
			// whole text, so the search retries it with a bigger window.
			case mt[1] == to-from && to < t.Len():
				retry = true
			default:
				return from + s, true
			}
		}
		if to == t.Len() || to >= limit && !retry {
			return 0, false
		}
	}
}

// last returns the start of the last match that starts at or after least
// and before limit.
func (p *pattern) last(t Text, least, limit int) (int, bool) {
	limit = min(limit, t.Len()+1)
	if p.lit != "" {
		return p.lastLit(t, least, limit)
	}
	to := LineEnd(t, min(limit, t.Len()))
	for size := searchWindow; ; size *= 2 {
		from := LineStart(t, max(to-size, 0))
		found := -1
		for _, mt := range p.all(t.Slice(from, to)) {
			if s, _ := p.span(mt); from+s < limit {
				found = from + s
			}
		}
		switch {
		case found >= least:
			return found, true
		case found >= 0 || from <= least:
			return 0, false
		}
	}
}

// litChunk is how much text a literal search reads at a time. It reads each
// chunk once, plus the literal's length less one byte, so it finds a match
// that crosses into the next chunk while it reads the first.
const litChunk = 1 << 16

// firstLit is first for a literal. It returns the start of its first match
// in [least, limit).
func (p *pattern) firstLit(t Text, least, limit int) (int, bool) {
	n := len(p.lit)
	for at := least; at < limit; at += litChunk {
		s := t.Slice(at, min(at+litChunk+n-1, t.Len()))
		for off := 0; ; off++ {
			i := p.index(s[off:])
			if i < 0 || off+i >= litChunk {
				break
			}
			off += i
			if at+off >= limit {
				return 0, false
			}
			if p.edges(t, at+off) {
				return at + off, true
			}
		}
	}
	return 0, false
}

// lastLit is last for a literal. It returns the start of its last match in
// [least, limit).
func (p *pattern) lastLit(t Text, least, limit int) (int, bool) {
	n := len(p.lit)
	for to := limit; to > least; to -= litChunk {
		from := max(to-litChunk, least)
		s := t.Slice(from, min(to+n-1, t.Len()))
		// A match starts before to, so it ends by to+n-1.
		for end := len(s); ; {
			i := p.lastIndex(s[:end])
			if i < 0 {
				break
			}
			if from+i < to && p.edges(t, from+i) {
				return from + i, true
			}
			end = i + n - 1
		}
	}
	return 0, false
}

// index returns where the literal first is in s, or -1. When it folds case,
// it tracks where each case of the first letter comes next, so it reads s
// once.
func (p *pattern) index(s string) int {
	if !p.fold {
		return strings.Index(s, p.lit)
	}
	lo, up := p.lit[0], upper(p.lit[0])
	i, j := -1, -1 // the next lo and up at or after at, or len(s) for none
	for at := 0; at+len(p.lit) <= len(s); at++ {
		if i < at {
			i = nextByte(s, at, lo)
		}
		if j < at {
			j = nextByte(s, at, up)
		}
		if at = min(i, j); at+len(p.lit) <= len(s) && strings.EqualFold(s[at:at+len(p.lit)], p.lit) {
			return at
		}
	}
	return -1
}

// nextByte returns where c is in s at or after at, or len(s).
func nextByte(s string, at int, c byte) int {
	if i := strings.IndexByte(s[at:], c); i >= 0 {
		return at + i
	}
	return len(s)
}

// lastIndex returns where the literal last is in s, or -1.
func (p *pattern) lastIndex(s string) int {
	if !p.fold {
		return strings.LastIndex(s, p.lit)
	}
	lo, up := p.lit[0], upper(p.lit[0])
	i, j := len(s), len(s) // the last lo and up before end, or -1 for none
	for end := len(s); ; {
		if i >= end {
			i = strings.LastIndexByte(s[:end], lo)
		}
		if j >= end {
			j = strings.LastIndexByte(s[:end], up)
		}
		if end = max(i, j); end < 0 {
			return -1
		}
		if end+len(p.lit) <= len(s) && strings.EqualFold(s[end:end+len(p.lit)], p.lit) {
			return end
		}
	}
}

// upper returns the ASCII letter c in upper case, and c itself otherwise.
func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

// edges reports whether a match at pos starts and ends at word edges, as
// Go's \b finds them. It is true when the pattern does not ask for edges.
func (p *pattern) edges(t Text, pos int) bool {
	if !p.words {
		return true
	}
	end := pos + len(p.lit)
	return wordByte(t, pos-1) != wordByte(t, pos) && wordByte(t, end-1) != wordByte(t, end)
}

// wordByte reports whether the byte at pos is an ASCII word character, and
// false outside the text.
func wordByte(t Text, pos int) bool {
	if pos < 0 || pos >= t.Len() {
		return false
	}
	c := t.Slice(pos, pos+1)[0]
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// keywordAt returns the word of word characters at or after c on its line.
func keywordAt(t Text, c int) (string, bool) {
	ls, le := LineStart(t, c), LineEnd(t, c)
	p := c
	for p < le && class(t, p, false) != 2 {
		p = next(t, p, le)
	}
	if p == le {
		return "", false
	}
	for p > ls && class(t, prev(t, ls, p), false) == 2 {
		p = prev(t, ls, p)
	}
	e := p
	for e < le && class(t, e, false) == 2 {
		e = next(t, e, le)
	}
	return t.Slice(p, e), true
}

// Matches returns the search matches in t between from and to for a client
// to highlight: those of the pattern being typed after / or ?, or else of
// the last search until <C-l> or :noh. t may be any window's text.
func (m *Machine) Matches(t Text, from, to int) [][2]int {
	src := ""
	switch {
	case m.mode == Cmdline && m.prompt != ":":
		src = m.line
	case m.hl:
		src = m.search.pattern
	}
	if src == "" {
		return nil
	}
	p, err := m.compile(src)
	if err != nil {
		return nil
	}
	from, to = LineStart(t, from), LineEnd(t, to)
	var out [][2]int
	for _, mt := range p.all(t.Slice(from, to)) {
		if s, e := p.span(mt); e > s {
			out = append(out, [2]int{from + s, from + e})
		}
	}
	return out
}

// substitute replaces pat with rep on the lines from first to last, once per
// line or every match with the g flag, in one transaction. i and I make the
// pattern ignore or match case, and & first takes the last substitute's
// flags. The cursor goes to the start of the line holding the last
// replacement.
func (m *Machine) substitute(first, last int, pat, rep, flags string) error {
	if pat == "" {
		return errors.New("no previous substitute")
	}
	if rest, ok := strings.CutPrefix(flags, "&"); ok {
		flags = m.lastSub.flags + rest
	}
	rep = replaceTilde(rep, m.lastRep)
	m.lastRep = rep
	m.lastSub.pattern, m.lastSub.rep, m.lastSub.flags = pat, rep, flags
	m.search.pattern, m.search.forward, m.hl = pat, true, true
	edits, err := m.subEdits(first, last, pat, rep, flags)
	if err != nil {
		return err
	}
	if len(edits) == 0 {
		return fmt.Errorf("pattern not found: %s", pat)
	}
	switch {
	case strings.Contains(flags, "n"):
		lines := map[int]bool{}
		t := m.h.Text()
		for _, e := range edits {
			lines[t.LineOf(e.Start)] = true
		}
		m.msg = fmt.Sprintf("%d matches on %d lines", len(edits), len(lines))
		return nil
	case strings.Contains(flags, "c"):
		m.h.BeginGroup()
		m.confirm = &confirming{edits: edits, from: m.h.Cursor()}
		m.ask()
		return nil
	}
	shift := 0
	for _, e := range edits[:len(edits)-1] {
		shift += len(e.Text) - (e.End - e.Start)
	}
	l := edits[len(edits)-1]
	after := l.Start + shift + len(l.Text)
	if err := m.apply(edits, ":s"); err != nil {
		return err
	}
	t := m.h.Text()
	ls := LineStart(t, after)
	m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
	return nil
}

// confirming is a :s with the c flag, asking before each of its edits, in
// the text as it was. shift is how far the edits made so far moved the
// rest.
type confirming struct {
	edits    []Edit
	at       int
	shift    int
	replaced int
	from     int // the cursor before, where it stays when nothing is replaced
	last     int // where the last replacement starts
}

// ask puts the cursor on the next match :s c asks about and asks.
func (m *Machine) ask() {
	e := m.confirm.edits[m.confirm.at]
	m.h.SetCursor(e.Start + m.confirm.shift)
	m.msg = fmt.Sprintf("replace with %s? y yes, n no, a all, l this and stop, q stop", e.Text)
}

// confirmKey answers what :s c asks: y replaces the match, n skips it, a
// replaces it and every one after, l replaces it and stops, and q, <Esc>
// and <C-c> stop. All it replaces is one undo step.
func (m *Machine) confirmKey(k Key) error {
	c := m.confirm
	replace := func() error {
		e := c.edits[c.at]
		e.Start, e.End = e.Start+c.shift, e.End+c.shift
		if err := m.apply([]Edit{e}, ":s"); err != nil {
			return err
		}
		c.shift += len(e.Text) - (e.End - e.Start)
		c.replaced, c.last = c.replaced+1, e.Start
		return nil
	}
	stop := false
	switch k {
	case "y":
		if err := replace(); err != nil {
			return err
		}
	case "l":
		if err := replace(); err != nil {
			return err
		}
		stop = true
	case "a":
		for ; c.at < len(c.edits); c.at++ {
			if err := replace(); err != nil {
				return err
			}
		}
	case "n":
	case "q", "<Esc>", "<C-c>":
		stop = true
	default:
		m.ask()
		return nil
	}
	if c.at++; !stop && c.at < len(c.edits) {
		m.ask()
		return nil
	}
	m.confirm = nil
	m.h.EndGroup()
	// The cursor ends on the line of the last replacement, as :s leaves it.
	t := m.h.Text()
	m.h.SetCursor(c.from)
	if c.replaced > 0 {
		ls := LineStart(t, c.last)
		m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
	}
	m.msg = fmt.Sprintf("%d substitutions", c.replaced)
	if c.replaced == 1 {
		m.msg = "1 substitution"
	}
	return nil
}

// subEdits returns the edits :s with pat, rep and flags makes on the lines
// from first to last, changing nothing.
func (m *Machine) subEdits(first, last int, pat, rep, flags string) ([]Edit, error) {
	src := pat
	switch {
	case strings.Contains(flags, "i"):
		src = `\c` + pat
	case strings.Contains(flags, "I"):
		src = `\C` + pat
	}
	p, err := m.compile(src)
	if err != nil {
		return nil, err
	}
	t := m.h.Text()
	text := t.Slice(first, LineEnd(t, last))
	var edits []Edit
	line := -1
	for _, mt := range p.all(text) {
		s, e := p.span(mt)
		if ls := LineStart(t, first+s); ls != line || strings.Contains(flags, "g") {
			line = ls
			edits = append(edits, Edit{Start: first + s, End: first + e, Text: p.expand(rep, text, mt)})
		}
	}
	return edits, nil
}

// Preview returns the edits the :s being typed would make on the lines
// from top to end, for the host to show before they are made, and whether
// its replacement is typed yet (B154).
func (m *Machine) Preview(top, end int) ([]Edit, bool) {
	first, last, parts, ok := m.subLine()
	if !ok {
		return nil, false
	}
	t := m.h.Text()
	replacing := len(parts) > 1
	for len(parts) < 3 {
		parts = append(parts, "")
	}
	if first, last = max(first, LineStart(t, top)), min(last, LineStart(t, end)); first > last {
		return nil, false
	}
	edits, err := m.subEdits(first, last, parts[0], replaceTilde(parts[1], m.lastRep), parts[2])
	if err != nil {
		return nil, false
	}
	return edits, replacing
}

// Substituting reports whether the command line is a :s with its pattern
// typed, which Preview shows.
func (m *Machine) Substituting() bool {
	_, _, _, ok := m.subLine()
	return ok
}

// subLine reads the command line being typed as a :s: the lines it covers
// and its pattern, replacement and flags as far as they are typed, or false
// when it is not one with a pattern yet.
func (m *Machine) subLine() (int, int, []string, bool) {
	if m.mode != Cmdline || m.prompt != ":" {
		return 0, 0, nil, false
	}
	t, c := m.h.Text(), m.h.Cursor()
	first, last, rest, _, err := m.lineRange(t, c, m.line)
	name, arg := exName(rest)
	if cmd := exByName[name]; err != nil || cmd == nil || cmd.Name != "substitute" || len(arg) < 2 {
		return 0, 0, nil, false
	}
	parts := splitDelim(arg[1:], arg[0])
	return first, last, parts, parts[0] != ""
}

// replaceTilde puts the previous replacement in place of each ~ not after a
// backslash.
func replaceTilde(rep, last string) string {
	var b strings.Builder
	for i := 0; i < len(rep); i++ {
		switch {
		case rep[i] == '\\' && i+1 < len(rep):
			b.WriteString(rep[i : i+2])
			i++
		case rep[i] == '~':
			b.WriteString(last)
		default:
			b.WriteByte(rep[i])
		}
	}
	return b.String()
}

// expand builds the replacement for match mt of text. & and \0 are the
// match, \1 to \9 its groups, \u \l \U \L \E change case, \r breaks the line
// and \t is a tab.
func (p *pattern) expand(rep, text string, mt []int) string {
	s, e := p.span(mt)
	var b strings.Builder
	var one, all byte
	write := func(str string) {
		for _, r := range str {
			c := one
			if c == 0 {
				c = all
			}
			one = 0
			switch c {
			case 'u', 'U':
				r = unicode.ToUpper(r)
			case 'l', 'L':
				r = unicode.ToLower(r)
			}
			b.WriteRune(r)
		}
	}
	for i := 0; i < len(rep); {
		r, n := utf8.DecodeRuneInString(rep[i:])
		i += n
		switch {
		case r == '&':
			write(text[s:e])
		case r == '\\' && i < len(rep):
			x := rep[i]
			i++
			switch {
			case x == '0':
				write(text[s:e])
			case x >= '1' && x <= '9':
				if g := p.groups[x-'0']; g > 0 && mt[2*g] >= 0 {
					write(text[mt[2*g]:mt[2*g+1]])
				}
			case x == 'u' || x == 'l':
				one = x
			case x == 'U' || x == 'L':
				all = x
			case x == 'E' || x == 'e':
				all = 0
			case x == 'r':
				b.WriteByte('\n')
			case x == 't':
				b.WriteByte('\t')
			default:
				write(string(x))
			}
		default:
			write(string(r))
		}
	}
	return b.String()
}

// Incremental returns where the match the search being typed after / or ?
// goes to starts and ends: the first after the cursor, or before it for ?,
// wrapping around the text, or the one <C-g> and <C-t> moved to, for a host
// to bring into view and light. It is false when no search is typed or
// nothing matches (B21).
func (m *Machine) Incremental() (int, int, bool) {
	if c := m.confirm; c != nil {
		e := c.edits[c.at]
		return e.Start + c.shift, e.End + c.shift, true
	}
	if m.mode != Cmdline || m.prompt == ":" || m.line == "" {
		return 0, 0, false
	}
	pat := splitDelim(m.line, m.prompt[0])[0]
	if pat == "" {
		return 0, 0, false
	}
	p, err := m.compile(pat)
	if err != nil {
		return 0, 0, false
	}
	t := m.h.Text()
	at, ok := p.wrapped(t, m.searchFrom, m.prompt == "/")
	if !ok {
		return 0, 0, false
	}
	return at, p.endAt(t, at), true
}

// wrapped returns the start of the first match after at, or going back the
// last before it, wrapping around the text.
func (p *pattern) wrapped(t Text, at int, forward bool) (int, bool) {
	if forward {
		if to, ok := p.first(t, at+1, t.Len()+1); ok {
			return to, true
		}
		return p.first(t, 0, at+1)
	}
	if to, ok := p.last(t, 0, at); ok {
		return to, true
	}
	return p.last(t, at, t.Len()+1)
}
