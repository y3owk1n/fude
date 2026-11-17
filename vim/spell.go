package vim

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Fold sets how patterns see the text: fold returns s spelled as patterns
// match it and, for each of its bytes, the start and end in s of the
// character the byte came from. The machine folds one line at a time. nil
// matches the text as it is (B608).
func (m *Machine) Fold(fold func(s string) (string, [][2]int)) {
	m.speller, m.res, m.typed = nil, nil, nil
	if fold != nil {
		m.speller = &speller{fold: fold}
	}
}

// spellerHolds is how many bytes of lines and their spellings a speller
// keeps before it starts again.
const spellerHolds = 16 << 20

// speller spells lines with the host's fold, keeping each line it spelled,
// so a search, the next one and the highlights fold a line once.
type speller struct {
	fold  func(string) (string, [][2]int)
	lines map[string]*spelt
	held  int // bytes in lines
}

// spelt is a line as the fold spells it. src is nil when the fold keeps the
// line as it is.
type spelt struct {
	s   string
	src [][2]int
}

// line returns l spelled.
func (sp *speller) line(l string) *spelt {
	if x, ok := sp.lines[l]; ok {
		return x
	}
	l = strings.Clone(l) // not the slice of the text it came from
	s, src := sp.fold(l)
	x := &spelt{s: l}
	if s != l {
		x.s, x.src = s, src
	}
	if sp.lines == nil || sp.held > spellerHolds {
		sp.lines, sp.held = map[string]*spelt{}, 0
	}
	sp.lines[l] = x
	sp.held += len(l) + len(x.s) + 16*len(x.src)
	return x
}

// spelling is a piece of the text spelled line by line, with a line break
// after each line but the last.
type spelling struct {
	s     string
	parts []part
	size  int // the length of the text it spells
}

// part is one line of a spelling: where it starts in the spelling,
// where its text starts and how long that is.
type part struct {
	at, from, n int
	x           *spelt
}

// spell spells the text from from to to.
func (sp *speller) spell(t Text, from, to int) *spelling {
	text := t.Slice(from, to)
	f := &spelling{size: len(text)}
	var b strings.Builder
	for off := 0; ; {
		end := len(text)
		if i := strings.IndexByte(text[off:], '\n'); i >= 0 {
			end = off + i
		}
		x := sp.line(text[off:end])
		f.parts = append(f.parts, part{at: b.Len(), from: off, n: end - off, x: x})
		b.WriteString(x.s)
		if end == len(text) {
			break
		}
		b.WriteByte('\n')
		off = end + 1
	}
	f.s = b.String()
	return f
}

// at returns the part that spells byte i of the spelling, and where i is in
// that part's spelling.
func (f *spelling) at(i int) (part, int) {
	k := sort.Search(len(f.parts), func(k int) bool { return f.parts[k].at > i }) - 1
	return f.parts[k], i - f.parts[k].at
}

// starts reports whether a character's spelling starts at i, the only place
// a match may start.
func (f *spelling) starts(i int) bool {
	if i >= len(f.s) {
		return true
	}
	p, k := f.at(i)
	switch {
	case k == 0 || k >= len(p.x.s):
		return true
	case p.x.src == nil:
		return utf8.RuneStart(p.x.s[k])
	}
	return p.x.src[k] != p.x.src[k-1]
}

// char returns the span of the text of the character byte i spells.
func (f *spelling) char(i int) (int, int) {
	if i >= len(f.s) {
		return f.size, f.size
	}
	p, k := f.at(i)
	switch {
	case k >= len(p.x.s): // the line break
		return p.from + p.n, p.from + p.n + 1
	case p.x.src == nil:
		return p.from + k, p.from + k + 1
	}
	return p.from + p.x.src[k][0], p.from + p.x.src[k][1]
}

// out returns the span of the text that the spelling's [s, e) spells, from
// the start of the character s is in to the end of the one e-1 is in.
func (f *spelling) out(s, e int) (int, int) {
	start, _ := f.char(s)
	if e <= s {
		return start, start
	}
	_, end := f.char(e - 1)
	return start, end
}

// all returns every match in t from from to to, each as Go submatch indexes
// from from. With a fold, it matches the text's spelling.
func (p *pattern) all(t Text, from, to int) [][]int {
	if p.speller == nil {
		return p.re.FindAllStringSubmatchIndex(t.Slice(from, to), -1)
	}
	f := p.speller.spell(t, from, to)
	mts := p.spelled(f)
	for _, mt := range mts {
		for k := 0; k < len(mt); k += 2 {
			if mt[k] >= 0 {
				mt[k], mt[k+1] = f.out(mt[k], mt[k+1])
			}
		}
	}
	return mts
}

// spelled returns the matches in f's spelling, which start only where a
// character's spelling does. The regexp may find a match inside a
// character's spelling that hides one starting after it, so the search goes
// on from the next character's spelling.
func (p *pattern) spelled(f *spelling) [][]int {
	s := f.s
	// next returns where the next character's spelling after i starts.
	next := func(i int) int {
		for i++; i < len(s) && !f.starts(i); i++ {
		}
		return i
	}
	var out [][]int
	end := -1 // where the last match ends
	for j := 0; j <= len(s); {
		mt := p.leftmost(s, j)
		switch {
		case mt == nil:
			return out
		case !f.starts(mt[0]), mt[0] == mt[1] && mt[0] == end:
			j = next(mt[0])
		default:
			out = append(out, mt)
			end, j = mt[1], mt[1]
			if mt[0] == mt[1] || !f.starts(j) {
				j = next(j)
			}
		}
	}
	return out
}

// leftmost returns the leftmost match in s that starts at or after j, as Go
// submatch indexes. It reads the character before j, so ^ and \< see it.
func (p *pattern) leftmost(s string, j int) []int {
	if j == 0 {
		return p.re.FindStringSubmatchIndex(s)
	}
	_, w := utf8.DecodeLastRuneInString(s[:j])
	mt := p.prefixed.FindStringSubmatchIndex(s[j-w:])
	if mt == nil {
		return nil
	}
	for k := range mt {
		if mt[k] >= 0 {
			mt[k] += j - w
		}
	}
	_, w = utf8.DecodeRuneInString(s[mt[0]:])
	mt[0] += w // the character before the match, which prefixed only reads
	return mt
}
