package vim

import (
	"slices"
	"strings"
)

// textObjectKey is an object that i selects inside of and a around, and
// what each selects.
type textObjectKey struct {
	inner, around string
	find          func(t Text, c, n int, around bool) (region, bool)
}

// bracket is the object of the text between open and close.
func bracket(open, close byte) textObjectKey {
	return textObjectKey{
		"the text inside " + string(open) + string(close), string(open) + string(close) + " and the text inside",
		func(t Text, c, n int, around bool) (region, bool) { return block(t, c, n, open, close, around) },
	}
}

// quoted is the object of the text between two of q on the line.
func quoted(q byte) textObjectKey {
	return textObjectKey{
		"the text inside " + string(q) + string(q), string(q) + string(q) + ", the text inside and the blanks after",
		func(t Text, c, n int, around bool) (region, bool) {
			start, end, ok := quote(t, c, q, n > 1, around)
			return region{start: start, end: end}, ok
		},
	}
}

// words is the object of words, or of words of non-blanks when big.
func words(big bool) textObjectKey {
	kind := "words"
	if big {
		kind = "words of non-blanks"
	}
	return textObjectKey{
		"n " + kind + " or blank runs", "n " + kind + " with the blanks after",
		func(t Text, c, n int, around bool) (region, bool) {
			start, end, ok := word(t, c, n, big, around)
			return region{start: start, end: end}, ok
		},
	}
}

// textObjects are the objects by the key after i or a.
var textObjects = map[string]textObjectKey{
	"w": words(false), "W": words(true),
	"s": {"n sentences", "n sentences with the blanks after", sentence},
	"p": {"n paragraphs", "n paragraphs with the blank lines after", func(t Text, c, n int, around bool) (region, bool) {
		start, end := paragraph(t, c, n, around)
		return region{start: start, end: end, lines: true}, true
	}},
	"l": {"the line without its leading and trailing blanks", "every line", func(t Text, c, _ int, around bool) (region, bool) {
		if around {
			return region{start: 0, end: t.Len(), lines: true}, true
		}
		ls := LineStart(t, c)
		line := t.Slice(ls, LineEnd(t, c))
		start := firstNonBlank(t, ls, ls+len(line))
		return region{start: start, end: max(ls+len(strings.TrimRight(line, " \t")), start)}, true
	}},
	`"`: quoted('"'), "'": quoted('\''), "`": quoted('`'),
	"(": bracket('(', ')'), ")": bracket('(', ')'), "b": bracket('(', ')'),
	"[": bracket('[', ']'), "]": bracket('[', ']'),
	"{": bracket('{', '}'), "}": bracket('{', '}'), "B": bracket('{', '}'),
	"<": bracket('<', '>'), ">": bracket('<', '>'),
}

// Object adds a text object such as af, which works after every operator and
// in visual mode. Its name starts with i, a, [ or ]. In visual mode find gets
// the selection, and after an operator the character under the cursor. It
// returns the text the object selects from there, or false when there is
// none. A count runs find again on what it returned. A nil find removes it.
func (m *Machine) Object(name string, find func(t Text, start, end int) (int, int, bool)) {
	if find == nil {
		delete(m.objects, name)
		return
	}
	if m.objects == nil {
		m.objects = map[string]func(Text, int, int) (int, int, bool){}
	}
	m.objects[name] = find
}

// object returns the text an object selects, trying the ones the host added
// first.
func (m *Machine) object(t Text, c, count int, cmd string) (region, bool) {
	find, ok := m.objects[cmd]
	if !ok {
		return textObject(t, c, count, cmd)
	}
	r := region{start: c, end: next(t, c, LineEnd(t, c))}
	if m.mode == Visual {
		r = m.selection()
	}
	found := false
	for range max(count, 1) {
		start, end, ok := find(t, r.start, r.end)
		if !ok {
			break
		}
		r, found = region{start: start, end: end}, true
	}
	return r, found
}

// textObject returns the text an object such as iw or a( selects, or false
// if cmd is not one or nothing matches.
func textObject(t Text, c, count int, cmd string) (region, bool) {
	if len(cmd) < 2 || (cmd[0] != 'i' && cmd[0] != 'a') {
		return region{}, false
	}
	obj, ok := textObjects[cmd[1:]]
	if !ok {
		return region{}, false
	}
	return obj.find(t, c, max(count, 1), cmd[0] == 'a')
}

// word selects n runs of word characters or blanks on the line. With around
// set each word takes the blanks after it, or before it at the line's end,
// and each blank run takes the word after it.
func word(t Text, c, n int, big, around bool) (int, int, bool) {
	ls, le := LineStart(t, c), LineEnd(t, c)
	if ls == le {
		return c, c, false
	}
	runEnd := func(p int) int {
		k := class(t, p, big)
		for p < le && class(t, p, big) == k {
			p = next(t, p, le)
		}
		return p
	}
	start := c
	for start > ls && class(t, prev(t, ls, start), big) == class(t, c, big) {
		start = prev(t, ls, start)
	}
	onBlank := class(t, c, big) == 0
	end := c
	for range n {
		if end >= le {
			return c, c, false
		}
		end = runEnd(end)
		if around && end < le && (onBlank || class(t, end, big) == 0) {
			end = runEnd(end)
		}
	}
	if around && !onBlank && class(t, prev(t, ls, end), big) != 0 {
		for start > ls && class(t, prev(t, ls, start), big) == 0 {
			start = prev(t, ls, start)
		}
	}
	return start, end, true
}

// sentence selects n sentences or gaps between them in the paragraph. With
// around set each sentence takes the blanks after it, or before it when it
// is the last. A selection that ends a line and starts at or before its first
// non-blank covers whole lines.
func sentence(t Text, c, n int, around bool) (region, bool) {
	segs := segments(t, c)
	if len(segs) == 0 {
		return region{}, false
	}
	k := 0
	for k < len(segs)-1 && segs[k].end <= c {
		k++
	}
	start, last := segs[k].start, k
	for i := 1; i < n; i++ {
		if last++; last >= len(segs) {
			return region{}, false
		}
	}
	if around {
		switch {
		case segs[k].blank && last+1 < len(segs):
			last++
		case !segs[last].blank && last+1 < len(segs):
			last++
		case !segs[k].blank && k > 0:
			start = segs[k-1].start
		}
	}
	r := region{start: start, end: segs[last].end}
	if s := LineStart(t, start); r.end == LineEnd(t, r.end) && r.end < t.Len() && start <= firstNonBlank(t, s, LineEnd(t, s)) {
		r = region{start: s, end: r.end, lines: true, adjusted: true}
	}
	return r, true
}

// seg is a sentence, or the blanks between two.
type seg struct {
	start, end int
	blank      bool
}

// segments splits the paragraph at c into sentences and the blanks between.
func segments(t Text, c int) []seg {
	ps, pe := paragraph(t, c, 1, false)
	text := t.Slice(ps, pe)
	var segs []seg
	for i := 0; i < len(text); {
		j := i
		blank := isBlank(text[i])
		if blank {
			for j < len(text) && isBlank(text[j]) {
				j++
			}
		} else {
			for j < len(text) && !sentenceEnd(text, j) {
				j++
			}
			j = min(j+1, len(text))
			for j < len(text) && strings.IndexByte(`)]"'`, text[j]) >= 0 {
				j++
			}
		}
		segs = append(segs, seg{ps + i, ps + j, blank})
		i = j
	}
	return segs
}

// sentenceEnd reports whether text[i] is a ., ! or ? that ends a sentence.
// Closers may follow it, then a blank or the end of the text.
func sentenceEnd(text string, i int) bool {
	if strings.IndexByte(".!?", text[i]) < 0 {
		return false
	}
	j := i + 1
	for j < len(text) && strings.IndexByte(`)]"'`, text[j]) >= 0 {
		j++
	}
	return j == len(text) || isBlank(text[j])
}

func isBlank(b byte) bool { return b == ' ' || b == '\t' || b == '\n' }

// paragraph selects n runs of blank or non-blank lines, from the start of
// the first to the end of the last. A line of only blanks counts as blank.
// With around set a paragraph takes the blank lines after it, or before it
// at the end of the text.
func paragraph(t Text, c, n int, around bool) (int, int) {
	blank := func(p int) bool {
		return strings.TrimLeft(t.Slice(p, LineEnd(t, p)), " \t") == ""
	}
	extend := func(p int) int {
		k := blank(p)
		for {
			le := LineEnd(t, p)
			if le == t.Len() || blank(le+1) != k {
				return p
			}
			p = le + 1
		}
	}
	next := func(p int) (int, bool) {
		le := LineEnd(t, p)
		if le == t.Len() {
			return p, false
		}
		return extend(le + 1), true
	}
	ls := LineStart(t, c)
	onBlank := blank(ls)
	start := ls
	for start > 0 && blank(LineStart(t, start-1)) == onBlank {
		start = LineStart(t, start-1)
	}
	end := extend(ls)
	runs := n
	if around {
		runs = 2 * n
	}
	trailing := true
	for i := 1; i < runs; i++ {
		e, ok := next(end)
		if !ok {
			trailing = false
			break
		}
		end = e
	}
	if around && !onBlank && !trailing {
		for start > 0 && blank(LineStart(t, start-1)) {
			start = LineStart(t, start-1)
		}
	}
	return start, LineEnd(t, end)
}

// quote selects the quoted text around or after c on its line. Quotes after
// a backslash do not count. withQuotes keeps the quotes, as a count of 2 does,
// and around also takes the blanks after the string, or before it.
func quote(t Text, c int, q byte, withQuotes, around bool) (int, int, bool) {
	ls, le := LineStart(t, c), LineEnd(t, c)
	line := t.Slice(ls, le)
	var qs []int
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++
		case q:
			qs = append(qs, i)
		}
	}
	col := c - ls
	open, close := -1, -1
	if k := slices.Index(qs, col); k >= 0 {
		// On a quote, pairs count from the start of the line.
		if k%2 == 1 {
			open, close = qs[k-1], qs[k]
		} else if k+1 < len(qs) {
			open, close = qs[k], qs[k+1]
		}
	} else {
		// Off a quote, take the quotes around c, or the first string after.
		i, _ := slices.BinarySearch(qs, col)
		switch {
		case i > 0 && i < len(qs):
			open, close = qs[i-1], qs[i]
		case i == 0 && len(qs) > 1:
			open, close = qs[0], qs[1]
		}
	}
	if open < 0 || close < 0 {
		return c, c, false
	}
	start, end := ls+open+1, ls+close
	if !withQuotes && !around {
		return start, end, true
	}
	start, end = start-1, end+1
	if around {
		e := end
		for e < le && (line[e-ls] == ' ' || line[e-ls] == '\t') {
			e++
		}
		if e > end {
			return start, e, true
		}
		for start > ls && (line[start-ls-1] == ' ' || line[start-ls-1] == '\t') {
			start--
		}
	}
	return start, end, true
}

// block selects the n-th pair of open and close around c, or the first pair
// after c on its line. If the open bracket ends its line and the close
// bracket starts its line, the inside is the whole lines between them.
func block(t Text, c, n int, open, close byte, around bool) (region, bool) {
	o, k := c, n
	if at(t, c) == open {
		k--
	}
	if k > 0 {
		o = outer(t, c, k, open, close)
	}
	if o < 0 {
		i := strings.IndexByte(t.Slice(c, LineEnd(t, c)), open)
		if i < 0 {
			return region{}, false
		}
		o = c + i
	}
	cl, depth := -1, 0
	for p := o + 1; p < t.Len(); p++ {
		switch at(t, p) {
		case open:
			depth++
		case close:
			if depth == 0 {
				cl = p
			}
			depth--
		}
		if cl >= 0 {
			break
		}
	}
	if cl < 0 {
		return region{}, false
	}
	if around {
		return region{start: o, end: cl + 1}, true
	}
	if at(t, o+1) == '\n' && firstNonBlank(t, LineStart(t, cl), cl) == cl && o+2 <= cl {
		end := LineStart(t, cl) - 1
		return region{start: o + 2, end: end, lines: true}, end >= o+2
	}
	return region{start: o + 1, end: cl}, true
}

// outer returns the n-th unmatched open before c, or -1.
func outer(t Text, c, n int, open, close byte) int {
	depth := 0
	for p := c - 1; p >= 0; p-- {
		switch at(t, p) {
		case close:
			depth++
		case open:
			if depth == 0 {
				if n--; n == 0 {
					return p
				}
				continue
			}
			depth--
		}
	}
	return -1
}
