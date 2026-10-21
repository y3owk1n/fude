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
	"<Up>": "k", "<Down>": "j", "<Left>": "h", "<Right>": "l", "<Home>": "0", "<End>": "$", "<PageUp>": "<C-b>", "<PageDown>": "<C-f>", "<Del>": "x",
	"<S-Left>": "b", "<C-Left>": "B", "<S-Right>": "w", "<C-Right>": "W", "<S-Up>": "<C-b>", "<S-Down>": "<C-f>", "<C-Home>": "gg",
}

// enterLine starts a command line after prompt with line typed, which
// returns to back.
func (m *Machine) enterLine(back Mode, prompt, line string) {
	m.back, m.mode, m.prompt = back, Cmdline, prompt
	m.line, m.lineAt, m.recalling = line, len(line), false
}

// cmdline takes a key while a command line is typed.
func (m *Machine) cmdline(k Key) error {
	if k != "<Up>" && k != "<Down>" {
		m.recalling = false
	}
	if m.pending == "<C-r>" {
		m.pending = ""
		return m.typeRegister(k)
	}
	if ck, ok := cmdlineKeys[string(k)]; ok {
		return ck.run(m, k, m.line[:m.lineAt], m.line[m.lineAt:])
	}
	if k.isChar() {
		m.typeLine(string(k))
	}
	return nil
}

// cmdlineKey is a key that edits or ends the command line.
type cmdlineKey struct {
	help string
	run  func(m *Machine, k Key, before, after string) error
}

// leaveLine ends the command line without running it.
func leaveLine(m *Machine, _ Key, _, _ string) error {
	m.mode = m.back
	m.count, m.op, m.opCount, m.reg = 0, "", 0, 0
	return nil
}

// eraseLine is <BS> and <C-h>, which leave an empty line.
func eraseLine(m *Machine, k Key, before, after string) error {
	if m.line == "" {
		return leaveLine(m, k, before, after)
	}
	_, n := utf8.DecodeLastRuneInString(before)
	m.cut(m.lineAt-n, m.lineAt)
	return nil
}

// cmdlineKeys are those keys. init fills them because <CR> runs the line,
// which can type into another.
var cmdlineKeys map[string]cmdlineKey

func init() {
	home := func(m *Machine, _ Key, _, _ string) error {
		m.lineAt = 0
		return nil
	}
	end := func(m *Machine, _ Key, _, _ string) error {
		m.lineAt = len(m.line)
		return nil
	}
	recall := func(m *Machine, k Key, _, _ string) error {
		m.recall(k == "<Up>")
		return nil
	}
	cmdlineKeys = map[string]cmdlineKey{
		"<Esc>": {"leaves the command line", leaveLine},
		"<C-c>": {"leaves the command line", leaveLine},
		"<CR>": {"runs the line", func(m *Machine, _ Key, _, _ string) error {
			m.mode = m.back
			m.remember()
			if line := m.line; m.prompt == ":" {
				// ": gets the line after it runs, so a command that lists the
				// registers shows the line before it.
				err := m.ex(line)
				if line != "" {
					m.keep(':', line)
				}
				return err
			}
			// An empty search is the last, with its offset.
			line := m.line
			if line == "" {
				line = strings.ReplaceAll(m.search.pattern, m.prompt, `\`+m.prompt) + m.prompt + m.search.offset
			}
			return m.command(m.prompt + line)
		}},
		"<BS>":  {"deletes the character before the cursor, or leaves an empty line", eraseLine},
		"<C-h>": {"deletes the character before the cursor, or leaves an empty line", eraseLine},
		"<Del>": {"deletes the character under the cursor", func(m *Machine, _ Key, _, after string) error {
			_, n := utf8.DecodeRuneInString(after)
			m.cut(m.lineAt, m.lineAt+n)
			return nil
		}},
		"<C-w>": {"deletes the word before the cursor", func(m *Machine, _ Key, before, _ string) error {
			m.cut(lineWordStart(before), m.lineAt)
			return nil
		}},
		"<C-u>": {"deletes all before the cursor", func(m *Machine, _ Key, _, _ string) error {
			m.cut(0, m.lineAt)
			return nil
		}},
		"<Left>": {"moves left", func(m *Machine, _ Key, before, _ string) error {
			_, n := utf8.DecodeLastRuneInString(before)
			m.lineAt -= n
			return nil
		}},
		"<Right>": {"moves right", func(m *Machine, _ Key, _, after string) error {
			_, n := utf8.DecodeRuneInString(after)
			m.lineAt += n
			return nil
		}},
		"<S-Left>":  {"goes to the start of the word before the cursor", wordLeft},
		"<C-Left>":  {"goes to the start of the word before the cursor", wordLeft},
		"<S-Right>": {"goes to the start of the next word", wordRight},
		"<C-Right>": {"goes to the start of the next word", wordRight},
		"<Home>":    {"goes to the start of the line", home},
		"<C-b>":     {"goes to the start of the line", home},
		"<End>":     {"goes to the end of the line", end},
		"<C-e>":     {"goes to the end of the line", end},
		"<Up>":      {"recalls an older line that starts with what is typed", recall},
		"<Down>":    {"recalls a newer line that starts with what is typed", recall},
		"<C-r>": {"types the register named next, or with <C-w> the word under the cursor", func(m *Machine, k Key, _, _ string) error {
			m.pending = k
			return nil
		}},
	}
}

// typeRegister puts register k in the command line, without its last line
// break, or the word under the cursor for <C-w>.
func (m *Machine) typeRegister(k Key) error {
	if k == "<C-w>" {
		if w, ok := keywordAt(m.h.Text(), m.h.Cursor()); ok {
			m.typeLine(w)
		}
		return nil
	}
	if !k.isChar() {
		return nil
	}
	name, _ := utf8.DecodeRuneInString(string(k))
	r, ok, err := m.fetch(name)
	if ok {
		m.typeLine(strings.TrimSuffix(r.text, "\n"))
	}
	return err
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
// wordLeft and wordRight are <S-Left> and <S-Right> on the command line,
// which go to the start of the word before the cursor or after it.
func wordLeft(m *Machine, _ Key, before, _ string) error {
	m.lineAt = lineWordStart(before)
	return nil
}

func wordRight(m *Machine, _ Key, _, after string) error {
	i := strings.IndexByte(after, ' ')
	if i < 0 {
		m.lineAt = len(m.line)
		return nil
	}
	m.lineAt += i + len(after[i:]) - len(strings.TrimLeft(after[i:], " "))
	return nil
}

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
	h = append(h, m.line)
	m.history[key] = h[max(len(h)-historyLines, 0):]
}

// historyLines is how many lines each history keeps, the newest (B133).
const historyLines = 10000

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
