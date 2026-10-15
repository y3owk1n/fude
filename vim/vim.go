// Package vim is a vim grammar state machine. The host feeds it keys, and it
// reads and edits the host's text through Host.
package vim

import (
	"strings"
	"unicode/utf8"
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
	Unhandled(keys string) // an ex line such as ":w"
}

// Mode is normal, insert or command line.
type Mode int

const (
	Normal Mode = iota
	Insert
	Cmdline
)

// Machine turns keys into cursor moves and edits.
type Machine struct {
	h       Host
	mode    Mode
	label   string
	line    string
	count   int
	pending Key    // g, f, t, F or T waiting for the next key
	find    string // the last f, t, F or T and its character
	want    int    // the column j and k aim for
	wantSet bool
}

// New returns a machine in normal mode.
func New(h Host) *Machine { return &Machine{h: h} }

// Mode returns the current mode.
func (m *Machine) Mode() Mode { return m.mode }

// Line returns the command line being typed, without its ":".
func (m *Machine) Line() string { return m.line }

// Feed handles one key. It returns the host's error if an edit fails.
func (m *Machine) Feed(k Key) error {
	switch m.mode {
	case Insert:
		return m.insert(k)
	case Cmdline:
		m.cmdline(k)
		return nil
	}
	return m.normal(k)
}

// Paste inserts s as typed text, with no key mapping applied. In normal mode
// it goes after the cursor and the mode stays normal, as in Neovim.
func (m *Machine) Paste(s string) error {
	switch m.mode {
	case Insert:
		return m.insertText(s)
	case Cmdline:
		m.line += s
		return nil
	}
	t, c := m.h.Text(), m.h.Cursor()
	at := next(t, c, LineEnd(t, c))
	if err := m.h.Apply([]Edit{{Start: at, End: at, Text: s}}, "paste"); err != nil {
		return err
	}
	end := at + len(s)
	t = m.h.Text()
	m.h.SetCursor(prev(t, LineStart(t, end), end))
	return nil
}

func (m *Machine) normal(k Key) error {
	cmd := string(m.pending) + string(k)
	m.pending = ""
	if len(cmd) == 1 && cmd >= "0" && cmd <= "9" && (cmd != "0" || m.count > 0) {
		m.count = m.count*10 + int(cmd[0]-'0')
		return nil
	}
	if len(cmd) == 1 && strings.Contains("gftFT", cmd) {
		m.pending = k
		return nil
	}
	count := m.count
	m.count = 0
	if p, ok := m.motion(cmd, count); ok {
		m.h.SetCursor(p)
		return nil
	}
	m.wantSet = false

	t, c := m.h.Text(), m.h.Cursor()
	start, end := LineStart(t, c), LineEnd(t, c)
	switch k {
	case ":":
		m.mode = Cmdline
		m.line = ""
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
		m.h.SetCursor(prev(t, LineStart(t, c), c))
	case "<CR>":
		return m.insertText("\n")
	case "<BS>":
		if c == 0 {
			return nil
		}
		// At a line start, backspace joins the lines.
		p := prev(t, LineStart(t, c), c)
		if p == c {
			p = c - 1
		}
		if err := m.h.Apply([]Edit{{Start: p, End: c}}, m.label); err != nil {
			return err
		}
		m.h.SetCursor(p)
	case "<Left>", "<Right>":
		n := prev(t, LineStart(t, c), c)
		if k == "<Right>" {
			n = next(t, c, LineEnd(t, c))
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

func (m *Machine) cmdline(k Key) {
	switch {
	case k == "<Esc>", k == "<BS>" && m.line == "":
		m.mode = Normal
	case k == "<CR>":
		m.mode = Normal
		m.h.Unhandled(":" + m.line)
	case k == "<BS>":
		_, n := utf8.DecodeLastRuneInString(m.line)
		m.line = m.line[:len(m.line)-n]
	case k.isChar():
		m.line += string(k)
	}
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
