package vim

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Tabstop is the width of a tab, Neovim's default.
const Tabstop = 8

// motion returns where cmd moves the cursor, or false if cmd is not a motion.
func (m *Machine) motion(cmd string, count int) (int, bool) {
	t, c := m.h.Text(), m.h.Cursor()
	n := max(count, 1)
	start := LineStart(t, c)
	p := c
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
		if !m.wantSet {
			m.want, m.wantSet = column(t, start, c), true
		}
		p, keep = atColumn(t, line, m.want), true
	case "0":
		p = start
	case "^":
		p = firstNonBlank(t, start, LineEnd(t, c))
	case "$":
		p = LineEnd(t, down(t, start, n-1))
		m.want, keep = math.MaxInt, true
	case "w", "W":
		p = wordForward(t, c, n, cmd == "W")
	case "b", "B":
		p = wordBack(t, c, n, cmd == "B")
	case "e", "E":
		p = wordEnd(t, c, n, cmd == "E")
	case ";", ",":
		if m.find == "" {
			return c, true
		}
		f := m.find
		if cmd == "," {
			f = reverse[f[:1]] + f[1:]
		}
		p = find(t, c, n, f, true)
	default:
		if len(cmd) < 2 || !strings.Contains("ftFT", cmd[:1]) || !Key(cmd[1:]).isChar() {
			return c, false
		}
		m.find = cmd
		p = find(t, c, n, cmd, false)
	}
	m.wantSet = keep
	// In normal mode the cursor stays off a line's end, unless the line is empty.
	if s, e := LineStart(t, p), LineEnd(t, p); p >= e && p > s {
		p = prev(t, s, e)
	}
	return p, true
}

var reverse = map[string]string{"f": "F", "F": "f", "t": "T", "T": "t"}

// find returns where f, t, F or T and a character land after n matches, or
// c if there are fewer. again skips a match next to c, as ; and , do.
func find(t Text, c, n int, cmd string, again bool) int {
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
				return c
			}
			p = from + i
			from = p + len(ch)
		}
		if key == "t" {
			p = prev(t, start, p)
		}
		return p
	}
	to := c
	if key == "T" && again {
		to = prev(t, start, to)
	}
	for range n {
		i := strings.LastIndex(t.Slice(start, to), ch)
		if i < 0 {
			return c
		}
		p = start + i
		to = p
	}
	if key == "T" {
		p = next(t, p, end)
	}
	return p
}

// wordForward is w, or W when big is set. An empty line counts as a word.
func wordForward(t Text, p, n int, big bool) int {
	end := t.Len()
	for range n {
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
			if p++; p == end || at(t, p) == '\n' {
				break
			}
		}
	}
	return p
}

// wordEnd is e, or E when big is set.
func wordEnd(t Text, p, n int, big bool) int {
	end := t.Len()
	for range n {
		p = next(t, p, end)
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
	for range n {
		e := LineEnd(t, start)
		if e == t.Len() {
			break
		}
		start = e + 1
	}
	return start
}

// up returns the start of the line n lines above the one at start, or 0.
func up(t Text, start, n int) int {
	for range n {
		if start == 0 {
			break
		}
		start = LineStart(t, start-1)
	}
	return start
}

// column returns the display column of pos on the line at start.
func column(t Text, start, pos int) int {
	col := 0
	for g := graphemes.FromString(t.Slice(start, pos)); g.Next(); {
		col += width(g.Value(), col)
	}
	return col
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
