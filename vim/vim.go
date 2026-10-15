// Package vim is a vim grammar state machine. The host feeds it keys, and it
// reads and edits the host's text through Host.
package vim

import (
	"strings"

	"github.com/clipperhouse/uax29/v2/graphemes"
)

// Text is the host's text. Positions are byte offsets.
type Text interface {
	Len() int
	Slice(start, end int) string
}

// Edit replaces [Start, End) with Text.
type Edit struct {
	Start, End int
	Text       string
}

// Host owns the text and the cursor. Edits between BeginGroup and EndGroup
// are one undo step.
type Host interface {
	Text() Text
	Cursor() int
	SetCursor(pos int)
	Apply(edits []Edit, label string) error
	BeginGroup()
	EndGroup()
}

// Mode is normal or insert.
type Mode int

const (
	Normal Mode = iota
	Insert
)

// Machine turns keys into cursor moves and edits.
type Machine struct {
	h     Host
	mode  Mode
	label string
}

// New returns a machine in normal mode.
func New(h Host) *Machine { return &Machine{h: h} }

// Mode returns the current mode.
func (m *Machine) Mode() Mode { return m.mode }

// Feed handles one key. It returns the host's error if an edit fails.
func (m *Machine) Feed(k Key) error {
	if m.mode == Insert {
		return m.insert(k)
	}
	return m.normal(k)
}

func (m *Machine) normal(k Key) error {
	t, c := m.h.Text(), m.h.Cursor()
	start, end := lineStart(t, c), lineEnd(t, c)
	switch k {
	case "h":
		m.h.SetCursor(prev(t, start, c))
	case "l":
		if n := next(t, c, end); n < end {
			m.h.SetCursor(n)
		}
	case "i":
		m.startInsert(k, c)
	case "a":
		m.startInsert(k, next(t, c, end))
	case "I":
		m.startInsert(k, firstNonBlank(t, start, end))
	case "A":
		m.startInsert(k, end)
	case "o":
		m.startInsert(k, end)
		return m.insertText("\n")
	case "O":
		m.startInsert(k, start)
		if err := m.insertText("\n"); err != nil {
			return err
		}
		m.h.SetCursor(start)
	}
	return nil
}

func (m *Machine) insert(k Key) error {
	t, c := m.h.Text(), m.h.Cursor()
	switch k {
	case "<Esc>":
		m.h.EndGroup()
		m.mode = Normal
		m.h.SetCursor(prev(t, lineStart(t, c), c))
	case "<CR>":
		return m.insertText("\n")
	case "<BS>":
		if c == 0 {
			return nil
		}
		// At a line start, backspace joins the lines.
		p := prev(t, lineStart(t, c), c)
		if p == c {
			p = c - 1
		}
		if err := m.h.Apply([]Edit{{Start: p, End: c}}, m.label); err != nil {
			return err
		}
		m.h.SetCursor(p)
	case "<Left>", "<Right>":
		n := prev(t, lineStart(t, c), c)
		if k == "<Right>" {
			n = next(t, c, lineEnd(t, c))
		}
		// A move splits the undo step, as in Neovim.
		if n != c {
			m.h.EndGroup()
			m.h.BeginGroup()
			m.h.SetCursor(n)
		}
	default:
		if k.isChar() {
			return m.insertText(string(k))
		}
	}
	return nil
}

func (m *Machine) startInsert(k Key, pos int) {
	m.mode = Insert
	m.label = string(k)
	m.h.SetCursor(pos)
	m.h.BeginGroup()
}

func (m *Machine) insertText(s string) error {
	c := m.h.Cursor()
	if err := m.h.Apply([]Edit{{Start: c, End: c, Text: s}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(c + len(s))
	return nil
}

// chunk is how many bytes line scans read at a time.
const chunk = 256

func lineStart(t Text, pos int) int {
	for pos > 0 {
		lo := max(pos-chunk, 0)
		if i := strings.LastIndexByte(t.Slice(lo, pos), '\n'); i >= 0 {
			return lo + i + 1
		}
		pos = lo
	}
	return 0
}

func lineEnd(t Text, pos int) int {
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

// next returns the end of the grapheme cluster at pos, or end.
func next(t Text, pos, end int) int {
	if pos >= end {
		return end
	}
	g := graphemes.FromString(t.Slice(pos, end))
	g.Next()
	return pos + g.End()
}

// prev returns the start of the grapheme cluster before pos, or start.
func prev(t Text, start, pos int) int {
	p := start
	for g := graphemes.FromString(t.Slice(start, pos)); g.Next(); {
		p = start + g.Start()
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
