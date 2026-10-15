package vim

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// arrows are the normal and visual mode commands that the arrows, <Home>,
// <End>, <Del> and the paging keys stand for.
var arrows = map[Key]Key{
	"<Up>": "k", "<Down>": "j", "<Home>": "0", "<End>": "$", "<PageUp>": "<C-b>", "<PageDown>": "<C-f>", "<Del>": "x",
}

// enterLine starts a command line after prompt with line typed, which
// returns to back.
func (m *Machine) enterLine(back Mode, prompt, line string) {
	m.back, m.mode, m.prompt = back, Cmdline, prompt
	m.line, m.lineAt, m.recalling = line, len(line), false
}

// cmdline takes a key while a command line is typed. The cursor moves with
// <Left>, <Right>, <Home> or <C-b> and <End> or <C-e>. <BS> and <Del>
// delete a character, <C-w> the word before the cursor and <C-u> all
// before it. <Up> and <Down> recall older and newer lines that start with
// the text typed.
func (m *Machine) cmdline(k Key) error {
	if k != "<Up>" && k != "<Down>" {
		m.recalling = false
	}
	before, after := m.line[:m.lineAt], m.line[m.lineAt:]
	switch {
	case k == "<Esc>", k == "<BS>" && m.line == "":
		m.mode = m.back
		m.count, m.op, m.opCount, m.reg = 0, "", 0, 0
	case k == "<CR>":
		m.mode = m.back
		m.remember()
		if m.prompt == ":" {
			return m.ex(m.line)
		}
		line := m.line
		if line == "" {
			line = m.search.pattern
		}
		return m.command(m.prompt + line)
	case k == "<BS>":
		_, n := utf8.DecodeLastRuneInString(before)
		m.cut(m.lineAt-n, m.lineAt)
	case k == "<Del>":
		_, n := utf8.DecodeRuneInString(after)
		m.cut(m.lineAt, m.lineAt+n)
	case k == "<C-w>":
		m.cut(lineWordStart(before), m.lineAt)
	case k == "<C-u>":
		m.cut(0, m.lineAt)
	case k == "<Left>":
		_, n := utf8.DecodeLastRuneInString(before)
		m.lineAt -= n
	case k == "<Right>":
		_, n := utf8.DecodeRuneInString(after)
		m.lineAt += n
	case k == "<Home>", k == "<C-b>":
		m.lineAt = 0
	case k == "<End>", k == "<C-e>":
		m.lineAt = len(m.line)
	case k == "<Up>", k == "<Down>":
		m.recall(k == "<Up>")
	case k.isChar():
		m.typeLine(string(k))
	}
	return nil
}

// typeLine puts s in the command line at the cursor.
func (m *Machine) typeLine(s string) {
	m.line = m.line[:m.lineAt] + s + m.line[m.lineAt:]
	m.lineAt += len(s)
}

// cut deletes [from, to) of the command line, where the cursor ends up.
func (m *Machine) cut(from, to int) {
	m.line, m.lineAt = m.line[:from]+m.line[to:], from
}

// lineWordStart returns where the word that s ends with starts, after any spaces
// at its end: a run of letters, digits and _, or of other characters.
func lineWordStart(s string) int {
	isWord := func(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	i := len(strings.TrimRight(s, " "))
	last, _ := utf8.DecodeLastRuneInString(s[:i])
	for i > 0 {
		r, n := utf8.DecodeLastRuneInString(s[:i])
		if r == ' ' || isWord(r) != isWord(last) {
			break
		}
		i -= n
	}
	return i
}

// historyOf names the history of the line being typed: : for commands, and
// / for searches either way.
func (m *Machine) historyOf() string {
	if m.prompt == ":" {
		return ":"
	}
	return "/"
}

// remember adds the line just entered to its history, as the newest.
func (m *Machine) remember() {
	if m.line == "" {
		return
	}
	if m.history == nil {
		m.history = map[string][]string{}
	}
	key := m.historyOf()
	h := slices.DeleteFunc(m.history[key], func(l string) bool { return l == m.line })
	m.history[key] = append(h, m.line)
}

// recall puts the next older or newer line of the history that starts with
// the text typed before recalling in the command line, and that text after
// the newest.
func (m *Machine) recall(older bool) {
	h := m.history[m.historyOf()]
	if !m.recalling {
		m.recalling, m.recalled, m.recallFrom = true, len(h), m.line
	}
	step := 1
	if older {
		step = -1
	}
	for i := m.recalled + step; i >= 0; i += step {
		if i >= len(h) {
			m.recalled = len(h)
			m.SetLine(m.recallFrom)
			return
		}
		if strings.HasPrefix(h[i], m.recallFrom) {
			m.recalled = i
			m.SetLine(h[i])
			return
		}
	}
}
