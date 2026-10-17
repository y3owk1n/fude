package vim

import (
	"errors"
	"fmt"
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

// seek returns the n-th match of src after the cursor, or before it when
// searching back, wrapping around the ends of the text.
func (m *Machine) seek(src string, forward bool, n int) (int, kind) {
	t, c := m.h.Text(), m.h.Cursor()
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
// pattern ignore or match case. The cursor goes to the start of the line
// holding the last replacement.
func (m *Machine) substitute(first, last int, pat, rep, flags string) error {
	if pat == "" {
		return errors.New("no previous substitute")
	}
	rep = replaceTilde(rep, m.lastRep)
	m.lastRep = rep
	m.lastSub.pattern, m.lastSub.rep, m.lastSub.flags = pat, rep, strings.Trim(flags, "&")
	m.search.pattern, m.search.forward, m.hl = pat, true, true
	edits, err := m.subEdits(first, last, pat, rep, flags)
	if err != nil {
		return err
	}
	if len(edits) == 0 {
		return fmt.Errorf("pattern not found: %s", pat)
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
