package vim

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// compile returns src as a pattern, reusing the last one compiled.
func (m *Machine) compile(src string) (*pattern, error) {
	if m.re == nil || m.reSrc != src {
		p, err := compile(src, m.lastRep)
		if err != nil {
			return nil, err
		}
		m.re, m.reSrc = p, src
	}
	return m.re, nil
}

// seek returns the n-th match of src after the cursor, or before it when
// searching back, wrapping around the ends of the text.
func (m *Machine) seek(src string, forward bool, n int) (int, kind, bool) {
	t, c := m.h.Text(), m.h.Cursor()
	if src == "" {
		m.msg = "no previous search"
		return c, failed, true
	}
	p, err := m.compile(src)
	if err != nil {
		m.msg = err.Error()
		return c, failed, true
	}
	var starts []int
	for _, mt := range p.all(t.Slice(0, t.Len())) {
		s, _ := p.span(mt)
		starts = append(starts, s)
	}
	if len(starts) == 0 {
		m.msg = "pattern not found: " + src
		return c, failed, true
	}
	m.hl = true
	pos := c
	for range n {
		if forward {
			i := sort.SearchInts(starts, pos+1)
			if i == len(starts) {
				i, m.msg = 0, "search hit the end, continuing at the start"
			}
			pos = starts[i]
		} else {
			i := sort.SearchInts(starts, pos) - 1
			if i < 0 {
				i, m.msg = len(starts)-1, "search hit the start, continuing at the end"
			}
			pos = starts[i]
		}
	}
	return pos, exclusive, true
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

// Matches returns the search matches between from and to for a client to
// highlight: those of the pattern being typed after / or ?, or else of the
// last search until <C-l> or :noh.
func (m *Machine) Matches(from, to int) [][2]int {
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
	t := m.h.Text()
	from, to = LineStart(t, from), LineEnd(t, to)
	var out [][2]int
	for _, mt := range p.all(t.Slice(from, to)) {
		if s, e := p.span(mt); e > s {
			out = append(out, [2]int{from + s, from + e})
		}
	}
	return out
}

// ex runs a command line. The machine runs :s, :&, :&& and :noh, and hands
// anything else to the host.
func (m *Machine) ex(line string) error {
	t, c := m.h.Text(), m.h.Cursor()
	first, last, rest, err := m.lineRange(t, c, line)
	if err != nil {
		return err
	}
	switch {
	case rest == "noh" || rest == "nohlsearch":
		m.hl = false
	case rest == "&&":
		return m.substitute(first, last, m.lastSub.pattern, m.lastSub.rep, m.lastSub.flags)
	case rest == "&" || rest == "s":
		return m.substitute(first, last, m.lastSub.pattern, m.lastSub.rep, "")
	case len(rest) > 1 && rest[0] == 's' && !isWordByte(rest[1]) && rest[1] != ' ':
		parts := splitDelim(rest[2:], rest[1])
		for len(parts) < 3 {
			parts = append(parts, "")
		}
		pat := parts[0]
		if pat == "" {
			pat = m.search.pattern
		}
		return m.substitute(first, last, pat, parts[1], parts[2])
	default:
		m.h.Unhandled(":" + line)
	}
	return nil
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// lineRange parses a leading range: %, or one or two addresses (a line
// number, ., $, '< or '>) joined by a comma. It returns the starts of the
// first and last lines and the rest of s. Without a range both are the
// cursor's line.
func (m *Machine) lineRange(t Text, c int, s string) (int, int, string, error) {
	if rest, ok := strings.CutPrefix(s, "%"); ok {
		return 0, LineStart(t, t.Len()), rest, nil
	}
	var err error
	addr := func(s string) (int, string, bool) {
		switch {
		case strings.HasPrefix(s, "."):
			return LineStart(t, c), s[1:], true
		case strings.HasPrefix(s, "$"):
			return LineStart(t, t.Len()), s[1:], true
		case strings.HasPrefix(s, "'<") || strings.HasPrefix(s, "'>"):
			if !m.hasMarked {
				err = errors.New("no previous selection")
				return 0, s, false
			}
			p := m.marked[0]
			if s[1] == '>' {
				p = m.marked[1]
			}
			return LineStart(t, min(p, t.Len())), s[2:], true
		}
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 {
			return 0, s, false
		}
		n, _ := strconv.Atoi(s[:i])
		return down(t, 0, max(n, 1)-1), s[i:], true
	}
	first, rest, ok := addr(s)
	if !ok {
		return LineStart(t, c), LineStart(t, c), s, err
	}
	last := first
	if r, found := strings.CutPrefix(rest, ","); found {
		if l, r2, ok := addr(r); ok {
			last, rest = l, r2
		}
	}
	return min(first, last), max(first, last), rest, err
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
	src := pat
	switch {
	case strings.Contains(flags, "i"):
		src = `\c` + pat
	case strings.Contains(flags, "I"):
		src = `\C` + pat
	}
	p, err := m.compile(src)
	if err != nil {
		return err
	}
	t := m.h.Text()
	end := LineEnd(t, last)
	text := t.Slice(first, end)
	var edits []Edit
	line := -1
	for _, mt := range p.all(text) {
		s, e := p.span(mt)
		if ls := LineStart(t, first+s); ls != line || strings.Contains(flags, "g") {
			line = ls
			edits = append(edits, Edit{Start: first + s, End: first + e, Text: p.expand(rep, text, mt)})
		}
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
	t = m.h.Text()
	ls := LineStart(t, after)
	m.h.SetCursor(firstNonBlank(t, ls, LineEnd(t, ls)))
	return nil
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
