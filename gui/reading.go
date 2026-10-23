package gui

import (
	"cmp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/frontend/client"
	"github.com/y3owk1n/fude/frontend/wire"
	"github.com/y3owk1n/fude/vim"
)

// reading is text as an assistive app reads it: a buffer's, a terminal's
// screen or what a palette has typed, with the cursor, the selection and
// the part in view, in bytes (B581).
type reading struct {
	name   string
	buffer int
	text   kernel.Snapshot
	plain  string // the text when it is no buffer's
	own    bool   // plain holds the text
	cursor int
	chosen [2]int // the visual selection, from its first byte to past its last, or the cursor twice
	view   [2]int // the bytes in view
}

// readingOf is how v reads, with sel its selection while visual.
func readingOf(v wire.Window, sel vim.Selection, visual bool) reading {
	if v.Cells != nil {
		var b strings.Builder
		at := 0
		for y, row := range v.Cells {
			if y > 0 {
				b.WriteByte('\n')
			}
			for x, c := range row {
				if y == v.CellY && x == v.CellX {
					at = b.Len()
				}
				if c.Width != 0 {
					b.WriteString(cmp.Or(c.Text, " "))
				}
			}
		}
		return typed(v.Name, b.String(), at)
	}
	r := reading{name: v.Name, buffer: v.Buffer, text: v.Text, cursor: v.Cursor}
	r.chosen = [2]int{r.cursor, r.cursor}
	if visual && v.Current {
		r.chosen = [2]int{sel.Start, sel.End}
	}
	r.view = r.chosen
	if n := len(v.Ranges); n > 0 {
		r.view = [2]int{v.Ranges[0].Start, v.Ranges[n-1].End}
	}
	return r
}

// typed is a reading of plain text with the cursor at byte at.
func typed(name, text string, at int) reading {
	return reading{name: name, plain: text, own: true, cursor: at, chosen: [2]int{at, at}, view: [2]int{0, len(text)}}
}

// label names r: its file's name, or what it is.
func (r reading) label() string {
	_, name := client.SplitName(r.name)
	return name
}

// textKey says which text a reading holds, to know when it changes.
type textKey struct {
	buffer  int
	version kernel.Version
	own     bool
	plain   string
}

func (r reading) key() textKey { return textKey{r.buffer, r.text.Version(), r.own, r.plain} }

// told is what an element last told assistive apps of.
type told struct {
	key    textKey
	chosen [2]int
	spoke  string // the item or the message last spoken
}

// spoken is a reading's text in the units a system's accessibility counts
// it in, with each line's start, made when an assistive app asks.
type spoken struct {
	count  func(rune) int // the units of a character
	key    textKey
	made   bool
	text   string
	starts []int // each line's first byte
	units  []int // each line's first unit
	total  int
}

// whole is r's text.
func (r reading) whole() string {
	if r.own {
		return r.plain
	}
	return r.text.Slice(0, r.text.Len())
}

// of makes s hold r's text, unless it already does.
func (s *spoken) of(r reading) *spoken {
	if !s.made || s.key != r.key() {
		s.hold(r.key(), r.whole())
	}
	return s
}

// hold makes s hold text, which key says.
func (s *spoken) hold(key textKey, text string) {
	s.key, s.made, s.text = key, true, text
	s.starts, s.units, s.total = []int{0}, []int{0}, 0
	for i, c := range s.text {
		s.total += s.count(c)
		if c == '\n' {
			s.starts, s.units = append(s.starts, i+1), append(s.units, s.total)
		}
	}
}

// unit is the unit at byte b.
func (s *spoken) unit(b int) int {
	b = min(max(b, 0), len(s.text))
	n := sort.SearchInts(s.starts, b+1) - 1
	u := s.units[n]
	for _, c := range s.text[s.starts[n]:b] {
		u += s.count(c)
	}
	return u
}

// byteAt is the byte at unit u.
func (s *spoken) byteAt(u int) int {
	u = min(max(u, 0), s.total)
	n := s.line(u)
	b, at := s.starts[n], s.units[n]
	for at < u && b < len(s.text) {
		c, size := utf8.DecodeRuneInString(s.text[b:])
		at += s.count(c)
		b += size
	}
	return b
}

// line is the line unit u is on.
func (s *spoken) line(u int) int { return max(sort.SearchInts(s.units, u+1)-1, 0) }

// lineUnits are the units of line n, its newline included.
func (s *spoken) lineUnits(n int) (int, int) {
	if n < 0 || n >= len(s.units) {
		return 0, 0
	}
	if n+1 < len(s.units) {
		return s.units[n], s.units[n+1]
	}
	return s.units[n], s.total
}

// slice is the text of units from to to.
func (s *spoken) slice(from, to int) string {
	return s.text[s.byteAt(from):s.byteAt(max(from, to))]
}

// wordAt returns the word at unit u, from its start to the next word's,
// or the space it is in.
func (s *spoken) wordAt(u int) (int, int) {
	b := s.byteAt(u)
	word := func(b int) bool {
		c, _ := utf8.DecodeRuneInString(s.text[b:])
		return b < len(s.text) && (c == '_' || unicode.IsLetter(c) || unicode.IsDigit(c))
	}
	from, to := b, b
	for from > 0 && word(from-1) == word(b) {
		_, n := utf8.DecodeLastRuneInString(s.text[:from])
		from -= n
	}
	for to < len(s.text) && word(to) == word(b) {
		_, n := utf8.DecodeRuneInString(s.text[to:])
		to += n
	}
	for to < len(s.text) && s.text[to] == ' ' {
		to++
	}
	return s.unit(from), s.unit(to)
}

// utf16Units counts a character as macOS and Windows count text, in UTF-16
// units.
func utf16Units(r rune) int { return max(utf16.RuneLen(r), 1) }

// statusText is what a status bar says, its parts in order.
func statusText(left, right []client.Part) string {
	var said []string
	for _, p := range slices.Concat(left, right) {
		if t := strings.TrimSpace(p.Text); t != "" {
			said = append(said, t)
		}
	}
	return strings.Join(said, ", ")
}

// cellsText is the text of cells, a line each row, without the spaces at
// each row's end.
func cellsText(cells [][]cell) string {
	rows := make([]string, len(cells))
	for y, row := range cells {
		var b strings.Builder
		for _, c := range row {
			b.WriteString(c.text)
		}
		rows[y] = strings.TrimRight(b.String(), " ")
	}
	return strings.TrimRight(strings.Join(rows, "\n"), "\n")
}

// itemText is what a menu's item says.
func itemText(it wire.Item) string {
	return strings.TrimSpace(it.Label + " " + it.Detail)
}

// reading is how b reads when it holds text: a float's, or what the
// palette or the finder has typed.
func (b *boxed) reading() (reading, bool) {
	switch {
	case b.read != nil:
		return *b.read, true
	case b.list != nil && b.list.typing:
		name := "command line"
		if b.prompt == "search" {
			name = "search"
		}
		return typed(name, b.list.input, b.list.at), true
	}
	return reading{}, false
}
