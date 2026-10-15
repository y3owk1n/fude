// Package vim is a vim grammar state machine. The host feeds it keys, and it
// reads and edits the host's text through Host.
package vim

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
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

// Host owns the text, the cursor and the undo history. Edits between
// BeginGroup and EndGroup are one undo step. Undo and Redo return where the
// step's text starts and the cursor from before the step, or false when
// there is no step to move to.
type Host interface {
	Text() Text
	Cursor() int
	SetCursor(pos int)
	Apply(edits []Edit, label string) error
	BeginGroup()
	EndGroup()
	Unhandled(keys string) // an ex line such as ":w"
	Undo() (start, cursor int, ok bool)
	Redo() (start, cursor int, ok bool)
}

// Mode is normal, insert, command line or visual.
type Mode int

const (
	Normal Mode = iota
	Insert
	Cmdline
	Visual
)

// Machine turns keys into cursor moves and edits.
type Machine struct {
	h       Host
	mode    Mode
	label   string
	line    string
	count   int
	op      string // d, c or y waiting for a motion
	opCount int
	reg     rune // the register named with ", or 0
	regs    map[rune]register
	pending Key    // g, f, t, F, T, ", q, @ or a text object's i or a
	find    string // the last f, t, F or T and its character
	want    int    // the column j and k aim for
	wantSet bool

	// Dot repeat: the keys of the change being made and the last one.
	keys     []Key
	counted  bool // the key just fed was a count digit or a register prefix
	changed  bool
	dotCount int
	dotReg   rune
	dot      change

	// Macros.
	recording rune // the register being recorded into, or 0
	macro     []Key
	recorded  rune // the register Q replays
	executed  rune // the register @@ replays
	beep      bool // a motion failed, which stops a running macro
	depth     int  // nested replays, to stop runaway recursion

	// Visual mode.
	visual      Key // v, V or <C-v>
	anchor      int // the end of the selection the cursor is not on
	blockInsert *blockInsert
}

// change is the last change, for dot repeat.
type change struct {
	count int
	reg   rune
	keys  []Key
}

// New returns a machine in normal mode.
func New(h Host) *Machine { return &Machine{h: h} }

// Mode returns the current mode.
func (m *Machine) Mode() Mode { return m.mode }

// Line returns the command line being typed, without its ":".
func (m *Machine) Line() string { return m.line }

// Recording returns the register a macro is being recorded into, or 0.
func (m *Machine) Recording() rune { return m.recording }

// Register returns the text in register name.
func (m *Machine) Register(name rune) (string, bool) {
	r, ok := m.regs[unicode.ToLower(name)]
	return r.text, ok
}

// Feed handles one key. It returns the host's error if an edit fails.
func (m *Machine) Feed(k Key) error {
	m.beep = false
	if m.recording != 0 {
		if k == "q" && m.idle() {
			m.stopRecording()
			return nil
		}
		m.macro = append(m.macro, k)
	}
	return m.feed(k)
}

// feed handles one key, from the user or a replay, and keeps the keys of the
// last change for dot repeat.
func (m *Machine) feed(k Key) error {
	if m.idle() {
		m.keys, m.changed = m.keys[:0], false
	}
	m.counted = false
	var err error
	switch m.mode {
	case Insert:
		err = m.insert(k)
	case Cmdline:
		m.cmdline(k)
	default:
		err = m.normal(k)
	}
	if !m.counted {
		m.keys = append(m.keys, k)
	}
	if m.idle() && m.changed {
		m.dot = change{m.dotCount, m.dotReg, slices.Clone(m.keys)}
		m.changed = false
	}
	return err
}

// idle reports whether the machine is between commands.
func (m *Machine) idle() bool {
	return m.mode == Normal && m.pending == "" && m.op == "" && m.count == 0 && m.reg == 0
}

// run feeds keys until one fails or a motion beeps.
func (m *Machine) run(keys []Key) error {
	if m.depth++; m.depth > 100 {
		m.depth--
		return errors.New("E169: Command too recursive")
	}
	defer func() { m.depth-- }()
	for _, k := range keys {
		if err := m.feed(k); err != nil || m.beep {
			return err
		}
	}
	return nil
}

// apply edits through the host and marks the command as a change.
func (m *Machine) apply(edits []Edit, label string) error {
	m.changed = true
	return m.h.Apply(edits, label)
}

// repeat replays the last change, with count in place of its own when set.
func (m *Machine) repeat(count int) error {
	d := m.dot
	if count > 0 {
		d.count = count
	}
	var keys []Key
	if d.reg != 0 {
		keys = append(keys, "\"", Key(string(d.reg)))
	}
	if d.count > 0 {
		keys = append(keys, Keys(strconv.Itoa(d.count))...)
	}
	return m.run(append(keys, d.keys...))
}

// execute replays the macro in register name n times.
func (m *Machine) execute(name rune, n int) error {
	if name == '@' {
		name = m.executed
	}
	r, ok := m.regs[unicode.ToLower(name)]
	if !ok {
		return nil
	}
	m.executed = name
	for range n {
		if err := m.run(Keys(r.text)); err != nil || m.beep {
			return err
		}
	}
	return nil
}

// stopRecording saves the recorded keys as text in the register, appending
// for an uppercase name.
func (m *Machine) stopRecording() {
	var b strings.Builder
	for _, k := range m.macro {
		if k == "<" {
			k = "<lt>"
		}
		b.WriteString(string(k))
	}
	name := unicode.ToLower(m.recording)
	if m.regs == nil {
		m.regs = map[rune]register{}
	}
	r := register{text: b.String()}
	if unicode.IsUpper(m.recording) {
		r.text = m.regs[name].text + r.text
	}
	m.regs[name] = r
	m.recorded, m.recording, m.macro = name, 0, nil
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
	if err := m.apply([]Edit{{Start: at, End: at, Text: s}}, "paste"); err != nil {
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
		m.counted = true
		return nil
	}
	if len(cmd) == 1 && strings.Contains("gftFT\"q@", cmd) {
		m.pending = k
		m.counted = cmd == "\""
		return nil
	}
	if cmd[0] == '"' {
		if Key(cmd[1:]).isChar() {
			m.reg, _ = utf8.DecodeRuneInString(cmd[1:])
		}
		m.counted = true
		return nil
	}
	if (cmd[0] == 'q' || cmd[0] == '@') && len(cmd) > 1 {
		count := max(m.count, 1)
		m.count = 0
		if !Key(cmd[1:]).isChar() {
			return nil
		}
		name, _ := utf8.DecodeRuneInString(cmd[1:])
		if cmd[0] == 'q' {
			m.recording, m.macro = name, nil
			return nil
		}
		return m.execute(name, count)
	}
	if (m.op != "" || m.mode == Visual) && (cmd == "i" || cmd == "a") {
		m.pending = k
		return nil
	}
	if m.mode == Visual {
		count, reg := m.count, m.reg
		m.count, m.reg = 0, 0
		m.dotCount, m.dotReg = count, reg
		return m.visualKey(cmd, count, reg)
	}
	if m.op == "" && (cmd == "d" || cmd == "c" || cmd == "y") {
		m.op, m.opCount, m.count = cmd, m.count, 0
		return nil
	}
	count := m.count
	if m.opCount > 0 {
		count = m.opCount * max(count, 1)
	}
	op, reg := m.op, m.reg
	m.count, m.op, m.opCount, m.reg = 0, "", 0, 0
	m.dotCount, m.dotReg = count, reg
	if op != "" {
		return m.operate(op, reg, cmd, count)
	}
	if p, k, ok := m.motion(cmd, count); ok {
		if k == failed {
			m.beep = true
		} else {
			m.h.SetCursor(clamp(m.h.Text(), p))
		}
		return nil
	}
	m.wantSet = false

	t, c := m.h.Text(), m.h.Cursor()
	start, end := LineStart(t, c), LineEnd(t, c)
	switch k {
	case "p", "P":
		return m.put(reg, k == "p", max(count, 1))
	case "u", "<C-r>":
		return m.undo(k == "u", max(count, 1))
	case ".":
		return m.repeat(count)
	case "Q":
		if m.recorded == 0 {
			return nil
		}
		return m.execute(m.recorded, max(count, 1))
	case ":":
		m.mode = Cmdline
		m.line = ""
	case "v", "V", "<C-v>":
		m.mode, m.visual, m.anchor = Visual, k, c
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
		if b := m.blockInsert; b != nil {
			m.blockInsert = nil
			if err := m.finishBlock(b); err != nil {
				return err
			}
			t, c = m.h.Text(), m.h.Cursor()
		}
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
		if err := m.apply([]Edit{{Start: p, End: c}}, m.label); err != nil {
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

// undo moves n steps back, or forward for redo. The cursor goes back to
// where it was before the step if that is on the step's first line, as in
// Neovim, and to that line's first non-blank otherwise.
func (m *Machine) undo(back bool, n int) error {
	step := m.h.Redo
	if back {
		step = m.h.Undo
	}
	for i := range n {
		start, cursor, ok := step()
		switch {
		case !ok && i > 0:
			return nil
		case !ok && back:
			return errors.New("Already at oldest change")
		case !ok:
			return errors.New("Already at newest change")
		}
		t := m.h.Text()
		top := LineStart(t, start)
		p := firstNonBlank(t, top, LineEnd(t, top))
		if cursor >= top && cursor <= t.Len() && LineStart(t, cursor) == top {
			p = cursor
		}
		m.h.SetCursor(clamp(t, p))
	}
	return nil
}

func (m *Machine) startInsert(k Key, pos int) {
	m.mode = Insert
	m.label = string(k)
	m.h.BeginGroup()
	m.h.SetCursor(pos)
}

func (m *Machine) insertText(s string) error {
	c := m.h.Cursor()
	if err := m.apply([]Edit{{Start: c, End: c, Text: s}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(c + len(s))
	return nil
}
