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
	pos := c
	for range n {
		var ok bool
		if forward {
			if pos, ok = p.first(t, pos+1); !ok {
				pos, ok = p.first(t, 0)
				m.msg = "search hit the end, continuing at the start"
			}
		} else if pos, ok = p.last(t, pos); !ok {
			pos, ok = p.last(t, t.Len()+1)
			m.msg = "search hit the start, continuing at the end"
		}
		if !ok {
			m.msg = "pattern not found: " + src
			return c, failed, true
		}
	}
	m.hl = true
	return pos, exclusive, true
}

// searchWindow is how many bytes a search reads first. It doubles until it
// holds a match, so a search near the cursor never reads the whole text.
const searchWindow = 1 << 10

// first returns the start of the first match that starts at or after least.
// It reads from least's line, so ^ and \< see the text before least.
func (p *pattern) first(t Text, least int) (int, bool) {
	if least > t.Len() {
		return 0, false
	}
	from := LineStart(t, least)
	for size := searchWindow; ; size *= 2 {
		to := LineEnd(t, min(from+size, t.Len()))
		for _, mt := range p.all(t.Slice(from, to)) {
			// A match that reaches the window's end may be longer in the
			// whole text, so the search retries it with a bigger window.
			if s, _ := p.span(mt); from+s >= least && (mt[1] < to-from || to == t.Len()) {
				return from + s, true
			}
		}
		if to == t.Len() {
			return 0, false
		}
	}
}

// last returns the start of the last match that starts before limit.
func (p *pattern) last(t Text, limit int) (int, bool) {
	to := LineEnd(t, min(limit, t.Len()))
	for size := searchWindow; ; size *= 2 {
		from := LineStart(t, max(to-size, 0))
		found := -1
		for _, mt := range p.all(t.Slice(from, to)) {
			if s, _ := p.span(mt); from+s < limit {
				found = from + s
			}
		}
		if found >= 0 {
			return found, true
		}
		if from == 0 {
			return 0, false
		}
	}
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
