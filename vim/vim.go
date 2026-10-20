// Package vim is a vim grammar state machine. The host feeds it keys, and it
// reads and edits the host's text through Host.
//
// The machine owns modes, counts, operators, motions, text objects,
// registers, dot repeat, macros, search and the ex commands that edit text.
// The host owns the text, its undo history, files, windows and what shows
// on screen. The machine starts no goroutines, does no I/O and reads no
// clock. A host drives it from one goroutine with Feed. When Waiting
// reports keys that wait for more, the host calls Timeout after its delay.
//
// A host can keep its text in the kernel of github.com/y3owk1n/fude/core.
// The kernel's Snapshot is a Text as it is, and kernel.Edit(e) converts an
// Edit.
package vim

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Text is the host's text. Positions are byte offsets, and lines count from
// 0.
type Text interface {
	Len() int
	Slice(start, end int) string
	Lines() int
	LineStart(line int) int
	LineOf(pos int) int
}

// Edit replaces [Start, End) with Text.
type Edit struct {
	Start, End int
	Text       string
}

// ErrNoStep is what Host's Undo and Redo return at the end of the history.
var ErrNoStep = errors.New("no step to move to")

// Host owns the text, the cursor and the undo history. Edits between
// BeginGroup and EndGroup are one undo step, and groups nest. Undo and Redo
// return where the step's text starts and the cursor from before the step,
// or ErrNoStep when there is no step to move to. A host embeds Defaults for
// the methods it has no use for.
type Host interface {
	Text() Text
	Cursor() int
	SetCursor(pos int)
	// Apply makes edits, sorted and apart, at offsets in the text before
	// them. label is the keys of the command. It makes all of them, or none
	// and returns an error.
	Apply(edits []Edit, label string) error
	BeginGroup()
	EndGroup()
	// Unhandled gets what the machine leaves to the host: an ex line such as
	// ":w", and a <C-w> key with its count, such as "2<C-w>+".
	Unhandled(keys string)
	Undo() (start, cursor int, err error)
	Redo() (start, cursor int, err error)
	Clipboard() (string, error) // the system clipboard, the "+ and "* register
	SetClipboard(text string) error
	// SetMark keeps a mark that moves with edits, staying after text
	// inserted at it. Names are mark letters, or runes from 0xF0000 up,
	// which the machine uses for its own marks.
	SetMark(name rune, pos int)
	Mark(name rune) (int, bool)
	// Viewport returns the first line shown, how many rows fit and how wide
	// they are, or math.MaxInt wide when lines do not wrap.
	Viewport() (top, height, width int)
	SetTop(top int)
	// These keep the jumplist and changelist. PushJump records where a jump
	// leaves from. Jump and Change move n entries back, or forward when n is
	// negative, and report false at either end. A jump may change buffers.
	PushJump(pos int)
	Jump(n int) bool
	Change(n int) bool
	// Step moves n states later on the timeline in the order they were made,
	// across branches, or earlier when n is negative. It returns where the
	// last change it made starts, or false at either end.
	Step(n int) (int, bool)
	IndentWidth() int // the columns >, <, <C-t> and <C-d> shift by
	TabWidth() int    // the columns a tab shows as
	// Fold returns the first and last line of the closed fold that holds
	// line, or line twice outside one. j and k pass over a closed fold as
	// one line, and a linewise command takes all of it (B223).
	Fold(line int) (first, last int)
	// Shown returns the inlines the window shows on the text from start to
	// end, which gj and gk count as the window draws them (B295).
	Shown(start, end int) []Inline
	// WordWrap reports whether the window's rows end after a space that
	// fits, as WordRows breaks them, which gj and gk count by.
	WordWrap() bool
	// Editable returns why the text may not change, or nil. A key that
	// would start insert or replace mode returns that reason and stays in
	// normal mode (B302).
	Editable() error
}

// Defaults is the part of a Host a host may leave out by embedding it. It
// has no undo history, jumps, changes or system clipboard, ignores what
// reaches Unhandled, shows a tab as Tabstop columns, indents by as many and
// has no folds. A host then writes Text, Cursor, SetCursor, Apply, SetMark,
// Mark, Viewport and SetTop.
type Defaults struct{}

func (Defaults) BeginGroup()                {}
func (Defaults) EndGroup()                  {}
func (Defaults) Unhandled(string)           {}
func (Defaults) Undo() (int, int, error)    { return 0, 0, ErrNoStep }
func (Defaults) Redo() (int, int, error)    { return 0, 0, ErrNoStep }
func (Defaults) Step(int) (int, bool)       { return 0, false }
func (Defaults) PushJump(int)               {}
func (Defaults) Jump(int) bool              { return false }
func (Defaults) Change(int) bool            { return false }
func (Defaults) IndentWidth() int           { return Tabstop }
func (Defaults) TabWidth() int              { return Tabstop }
func (Defaults) Fold(line int) (int, int)   { return line, line }
func (Defaults) Shown(int, int) []Inline    { return nil }
func (Defaults) WordWrap() bool             { return false }
func (Defaults) Editable() error            { return nil }
func (Defaults) Clipboard() (string, error) { return "", errNoClipboard }
func (Defaults) SetClipboard(string) error  { return errNoClipboard }

var errNoClipboard = errors.New("there is no system clipboard")

// Mode is normal, insert, command line, visual or replace.
type Mode int

const (
	Normal Mode = iota
	Insert
	Cmdline
	Visual
	Replace
)

// Machine turns keys into cursor moves and edits.
type Machine struct {
	h       Host
	mode    Mode
	label   string
	line    string
	lineAt  int // the cursor in line, in bytes
	count   int
	op      string // d, c or y waiting for a motion
	opCount int
	reg     rune // the register named with ", or 0
	regs    map[rune]register
	shown   []Key  // the keys of the command being typed, for Shown
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
	visual     Key // v, V or <C-v>
	anchor     int // the end of the selection the cursor is not on
	lastVisual Key // the kind of the last selection, for gv

	anchorFirst bool
	blockInsert *blockInsert
	replaced    []string // what each key typed in replace mode overwrote

	// Command line and search.
	prompt string // :, / or ?
	// The command lines and searches entered, oldest first, by : and /, and
	// the one <Up> and <Down> recalled with the text typed before them.
	history    map[string][]string
	recalled   int
	recalling  bool
	recallFrom string
	back       Mode // the mode a search returns to
	search     struct {
		pattern string
		forward bool
	}
	hl                    bool                    // matches of the last search are highlighted
	res                   map[patternKey]*pattern // compiled patterns, as a search and a :s preview take turns
	ignoreCase, smartCase bool
	clipboard             bool   // commands without a register use the host's clipboard too
	flash                 [2]int // what the last key yanked or put, empty when nothing
	flashPut              bool   // the last key put it, not yanked it
	expandTab             bool   // indents are spaces only
	asTyped               bool   // insert mode types each key as it is (B323)
	wait                  *charWait
	lastRep               string // the previous replacement, for ~
	lastSub               struct{ pattern, rep, flags string }
	msg                   string

	// Insert mode.
	insStart   int  // where typing began, the limit of <C-w> and <C-u>
	autoIndent bool // the line's indent came from autoindent and is untouched
	oneshot    bool // <C-o> is running one normal command
	armed      bool // that command has begun

	// Keymaps.
	maps  Keymaps
	local *Keymaps // a kind of buffer's mappings, over maps
	held  []Key    // typed keys that start a longer mapping
	ahead int      // how many of held insert mode typed already (B229)

	objects map[string]func(Text, int, int) (int, int, bool) // text objects the host added
	ops     map[string]hostOperator                          // operators the host added
	cmds    map[string]func(Text, int, int) []Edit           // commands the host added
	keyed   map[Mode]map[string]keyedCommand                 // commands the host added by their keys

	// :select.
	selector func(Text, string, int, int) ([][2]int, error)
	regions  []region // in visual mode, every region :select or <C-n> chose, lowest first
	multi    string   // the pattern <C-n> adds regions of (B232)
	multiAt  int      // where the region <C-n> added last ends
	cursors  []rune   // in insert mode, the marks of the cursors after the first
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

// Line returns the command line being typed, with its prompt.
func (m *Machine) Line() string { return m.prompt + m.line }

// SetLine replaces the command line being typed after its prompt, as
// completion does.
func (m *Machine) SetLine(s string) { m.line, m.lineAt = s, len(s) }

// Flash returns the text the last key yanked or put, and whether it put
// it, or false when it did neither (B139).
func (m *Machine) Flash() (start, end int, put, ok bool) {
	return m.flash[0], m.flash[1], m.flashPut, m.flash[1] > m.flash[0]
}

// LineCursor returns where the cursor is in Line, in bytes.
func (m *Machine) LineCursor() int { return len(m.prompt) + m.lineAt }

// Message returns what the last key has to tell the user, or "".
func (m *Machine) Message() string { return m.msg }

// Recording returns the register a macro is being recorded into, or 0.
func (m *Machine) Recording() rune { return m.recording }

// Saved is the text a register holds, and whether it is whole lines or one
// line for each line of a block.
type Saved struct {
	Text            string
	Linewise, Block bool
}

// Registers returns what every register holds, by name, but the
// clipboard's, which Register reads.
func (m *Machine) Registers() map[rune]Saved {
	out := make(map[rune]Saved, len(m.regs))
	for name, r := range m.regs {
		out[name] = Saved{r.text, r.linewise, r.block}
	}
	return out
}

// SetRegister puts s in register name, as a yank into it would. "+ and
// "* are the host's clipboard.
func (m *Machine) SetRegister(name rune, s Saved) error {
	if name == '+' || name == '*' {
		return m.h.SetClipboard(s.Text)
	}
	if m.regs == nil {
		m.regs = map[rune]register{}
	}
	m.regs[unicode.ToLower(name)] = register{s.Text, s.Linewise, s.Block}
	return nil
}

// SetSearch makes pattern the last search, which n finds, going forward or
// back, and highlights its matches.
func (m *Machine) SetSearch(pattern string, forward bool) {
	m.search.pattern, m.search.forward, m.hl = pattern, forward, true
}

// Search returns the last search's pattern, "" before any, and whether it
// went forward.
func (m *Machine) Search() (string, bool) { return m.search.pattern, m.search.forward }

// History returns the lines entered with prompt, : or /, oldest first.
func (m *Machine) History(prompt string) []string { return slices.Clone(m.history[prompt]) }

// Register returns the text in register name, or false when it is empty,
// and the error reading the clipboard for "+ and "*.
func (m *Machine) Register(name rune) (string, bool, error) {
	r, ok, err := m.fetch(name)
	return r.text, ok && err == nil, err
}

// fetch returns register name, or false when it is empty. "+ and "* are the
// host's clipboard, linewise when its text ends in a line break.
func (m *Machine) fetch(name rune) (register, bool, error) {
	if name == '+' || name == '*' {
		text, err := m.h.Clipboard()
		return register{text: text, linewise: strings.HasSuffix(text, "\n")}, text != "", err
	}
	r, ok := m.regs[unicode.ToLower(name)]
	return r, ok, nil
}

// Feed handles one key. It returns the host's error if an edit fails.
func (m *Machine) Feed(k Key) error {
	defer m.show(k)
	m.beep, m.msg, m.flash = false, "", [2]int{}
	if m.mode == Normal {
		// The host's own edits, such as another client's, can leave the
		// cursor past the end of its line.
		m.h.SetCursor(clamp(m.h.Text(), m.h.Cursor()))
	}
	if m.recording != 0 {
		if k == "q" && m.idle() && len(m.held) == 0 {
			m.stopRecording()
			return nil
		}
		m.macro = append(m.macro, k)
	}
	return m.press(k)
}

// feed handles one key, from the user or a replay, and keeps the keys of the
// last change for dot repeat.
func (m *Machine) feed(k Key) error {
	if m.idle() {
		m.keys, m.changed = m.keys[:0], false
	}
	m.counted = false
	if a, ok := arrows[k]; ok && (m.mode == Normal || m.mode == Visual) && m.pending == "" {
		k = a
	}
	var err error
	switch m.mode {
	case Insert, Replace:
		err = m.insert(k)
	case Cmdline:
		err = m.cmdline(k)
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
	// <C-o> returns to insert mode once its command is done. A command that
	// enters insert mode ends <C-o>. A visual command returns to insert mode
	// when visual mode ends.
	switch {
	case m.oneshot && m.mode != Normal:
		m.oneshot = m.mode == Visual
	case m.oneshot && m.idle() && m.armed:
		m.oneshot, m.mode = false, Insert
		m.h.BeginGroup()
		m.insStart = m.h.Cursor()
	case m.oneshot && m.idle():
		m.armed = true
	}
	return err
}

// Shown returns the keys of the command being typed, or "" between
// commands (B38).
func (m *Machine) Shown() string {
	var b strings.Builder
	for _, k := range m.shown {
		b.WriteString(string(k))
	}
	return b.String()
}

// show keeps k for Shown while a command is being typed in normal or visual
// mode, and forgets the keys once it is done.
func (m *Machine) show(k Key) {
	typing := m.pending != "" || m.op != "" || m.count > 0 || m.reg != 0 || len(m.held) > 0 || m.wait != nil
	if !typing || m.mode == Insert || m.mode == Cmdline || m.mode == Replace {
		m.shown = nil
		return
	}
	m.shown = append(m.shown, k)
}

// Idle reports whether the machine is in normal mode between commands, with
// no count, register or held key.
func (m *Machine) Idle() bool { return m.idle() && len(m.held) == 0 }

// idle reports whether the machine is between commands.
func (m *Machine) idle() bool {
	return m.mode == Normal && m.pending == "" && m.op == "" && m.count == 0 && m.reg == 0 && m.wait == nil
}

// run feeds keys until one fails or a motion beeps, through the keymap when
// mapped is set.
func (m *Machine) run(keys []Key, mapped bool) error {
	if m.depth++; m.depth > 100 {
		m.depth--
		return errors.New("macro nested too deeply")
	}
	defer func() { m.depth-- }()
	for _, k := range keys {
		if k == "<Nop>" {
			continue
		}
		step := m.feed
		if mapped {
			step = m.press
		}
		if err := step(k); err != nil || m.beep {
			m.held = nil
			return err
		}
	}
	if mapped && len(m.held) > 0 {
		return m.flush()
	}
	return nil
}

// apply edits through the host and marks the command as a change. While a
// change on several regions types into all of them, it repeats the edits at
// every cursor.
func (m *Machine) apply(edits []Edit, label string) error {
	m.changed = true
	if len(m.cursors) > 0 && (m.mode == Insert || m.mode == Replace) {
		edits = m.mirror(edits)
	}
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
	return m.run(append(keys, d.keys...), false)
}

// execute replays the macro in register name n times.
func (m *Machine) execute(name rune, n int) error {
	if name == '@' {
		name = m.executed
	}
	r, ok, err := m.fetch(name)
	if !ok || err != nil {
		return err
	}
	m.executed = name
	for range n {
		if err := m.run(Keys(r.text), true); err != nil || m.beep {
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
// it goes after the cursor and the mode stays normal.
func (m *Machine) Paste(s string) error {
	switch m.mode {
	case Insert, Replace:
		return m.typeText(s)
	case Cmdline:
		m.typeLine(s)
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

// prefixes are the keys normal reads itself, before or as a command, as
// help shows them.
var prefixes = []Binding{
	{"normal", "{count}", "repeats the command that follows, or sets its n"},
	{"normal", `"{reg}`, "names the register the next command uses"},
	{"normal", "m{a-z}", "sets a mark at the cursor"},
	{"normal", "q{reg}", "records keys into the register until q"},
	{"normal", "@{reg}", "replays the register, n times"},
	{"normal", "<C-w>{key}", "runs a window command"},
}

func (m *Machine) normal(k Key) error {
	if m.wait != nil {
		return m.takeChar(k)
	}
	cmd := string(m.pending) + string(k)
	m.pending = ""
	if len(cmd) == 1 && cmd >= "0" && cmd <= "9" && (cmd != "0" || m.count > 0) {
		m.count = m.count*10 + int(cmd[0]-'0')
		m.counted = true
		return nil
	}
	if m.op != "" {
		// A host's operator or command whose keys start with the pending
		// operator's, such as ys or ds, takes the key after it.
		if _, ok := m.ops[m.op+cmd]; ok {
			m.op += cmd
			return nil
		}
		if kc, ok := m.keyed[Normal][m.op+cmd]; ok {
			keys, c := m.op+cmd, m.h.Cursor()
			m.op, m.opCount, m.count, m.reg = "", 0, 0, 0
			return m.runKeyed(kc, keys, region{start: c, end: c})
		}
	}
	if len(m.op) == 2 && cmd == m.op[1:] {
		// A doubled operator such as gqq, whose last key would otherwise
		// wait for more.
		return m.command(cmd)
	}
	if len(cmd) == 1 && strings.Contains("gftFT\"q@rm`'zZ[]", cmd) {
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
	if cmd[0] == 'm' && len(cmd) == 2 && cmd[1] >= 'a' && cmd[1] <= 'z' {
		m.h.SetMark(rune(cmd[1]), m.h.Cursor())
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
	if cmd == "<C-w>" {
		m.pending = k
		return nil
	}
	if strings.HasPrefix(cmd, "<C-w>") {
		if m.count > 0 {
			cmd = strconv.Itoa(m.count) + cmd
		}
		m.count, m.reg, m.op, m.opCount = 0, 0, "", 0
		m.h.Unhandled(cmd)
		return nil
	}
	if (m.op != "" || m.mode == Visual) && (cmd == "i" || cmd == "a") {
		m.pending = k
		return nil
	}
	if cmd == "/" || cmd == "?" {
		// The count, register and operator wait for the search to finish.
		m.enterLine(m.mode, cmd, "")
		return nil
	}
	return m.command(cmd)
}

// command runs a complete normal or visual mode command.
func (m *Machine) command(cmd string) error {
	k := Key(cmd)
	if m.mode == Visual {
		count, reg := m.count, m.reg
		m.count, m.reg = 0, 0
		m.dotCount, m.dotReg = count, reg
		return m.visualKey(cmd, count, reg)
	}
	if _, host := m.ops[cmd]; m.op == "" && (operators[cmd] != "" || host) {
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
		return m.operate(op, reg, cmd, count, op+cmd)
	}
	if a, ok := aliases[cmd]; ok {
		return m.operate(a.to[:1], reg, a.to[1:], count, cmd)
	}
	if ok, err := m.edit(cmd, count); ok {
		return err
	}
	if p, k, ok := m.motion(cmd, count); ok {
		switch {
		case k == failed:
			m.beep = true
		case m.oneshot && cmd == "$":
			// After <C-o>, $ leaves the cursor past the end, ready to type.
			m.h.SetCursor(LineEnd(m.h.Text(), p))
		case m.oneshot:
			m.h.SetCursor(p)
		default:
			if isJump(cmd) {
				m.h.PushJump(m.h.Cursor())
			}
			m.h.SetCursor(clamp(m.h.Text(), p))
		}
		return nil
	}
	if ok, err := m.scroll(cmd, count); ok {
		return err
	}
	m.wantSet = false

	t, c := m.h.Text(), m.h.Cursor()
	if name, ok := strings.CutPrefix(cmd, "<Plug>("); ok {
		return m.plug(strings.TrimSuffix(name, ")"), count, false)
	}
	key := cmd
	if k == "\t" {
		key = "<C-i>"
	}
	nk, ok := normals[key]
	if !ok {
		return nil
	}
	return nk.run(m, normalCall{t, c, LineStart(t, c), LineEnd(t, c), k, count, reg})
}

// normalCall is what a normal mode command gets: the text, the cursor, the
// start and end of its line, the key, the count typed (0 for none) and the
// register named.
type normalCall struct {
	t             Text
	c, start, end int
	k             Key
	count         int
	reg           rune
}

// n is the count, or 1 without one.
func (x normalCall) n() int { return max(x.count, 1) }

// normalKey is a normal mode command that is not an operator, a motion, an
// edit or a scroll.
type normalKey struct {
	help string
	run  func(m *Machine, x normalCall) error
}

// insertAt starts insert mode at where returns.
func insertAt(where func(x normalCall) int) func(m *Machine, x normalCall) error {
	return func(m *Machine, x normalCall) error {
		return m.startInsert(x.k, where(x))
	}
}

// visualMode starts the kind of visual mode its key names.
func visualMode(m *Machine, x normalCall) error {
	m.mode, m.visual, m.anchor = Visual, x.k, x.c
	return nil
}

// normals are the other normal mode commands, by their keys. <Tab> is <C-i>.
// init fills them because . and Q run keys, which come back to them.
var normals map[string]normalKey

func init() {
	normals = map[string]normalKey{
		"p": {"puts the register after the cursor, n times", func(m *Machine, x normalCall) error {
			return m.put(x.reg, true, x.n())
		}},
		"P": {"puts the register before the cursor, n times", func(m *Machine, x normalCall) error {
			return m.put(x.reg, false, x.n())
		}},
		"u": {"undoes n changes", func(m *Machine, x normalCall) error {
			return m.undo(true, x.n())
		}},
		"<C-r>": {"redoes n changes", func(m *Machine, x normalCall) error {
			return m.undo(false, x.n())
		}},
		".": {"repeats the last change, with n as its count", func(m *Machine, x normalCall) error {
			return m.repeat(x.count)
		}},
		"Q": {"replays the last recorded register, n times", func(m *Machine, x normalCall) error {
			if m.recorded == 0 {
				return nil
			}
			return m.execute(m.recorded, x.n())
		}},
		":": {"opens the command line, with n lines from the cursor's as its range", func(m *Machine, x normalCall) error {
			line := ""
			if x.count > 0 {
				line = ".,.+" + strconv.Itoa(x.count-1)
			}
			m.enterLine(Normal, ":", line)
			return nil
		}},
		"<C-l>": {"hides the last search's highlight", func(m *Machine, _ normalCall) error {
			m.hl = false
			return nil
		}},
		"gn":    {"selects the next match of the last search", func(m *Machine, _ normalCall) error { return m.selectMatch(false) }},
		"gN":    {"selects the match of the last search before the cursor", func(m *Machine, _ normalCall) error { return m.selectMatch(true) }},
		"<C-n>": {"selects the word under the cursor, for <C-n> in visual mode to add its next match", func(m *Machine, _ normalCall) error { return m.addRegion() }},
		"]e": {"moves the line n lines down", func(m *Machine, x normalCall) error {
			_, err := m.moveBy(x.start, x.start, max(x.count, 1), "]e")
			return err
		}},
		"[e": {"moves the line n lines up", func(m *Machine, x normalCall) error {
			_, err := m.moveBy(x.start, x.start, -max(x.count, 1), "[e")
			return err
		}},
		"gv": {"selects the last selection again", func(m *Machine, _ normalCall) error {
			m.reselect()
			return nil
		}},
		"ZZ": {"writes the buffer if it is modified, and closes the window", func(m *Machine, _ normalCall) error {
			m.h.Unhandled(":x")
			return nil
		}},
		"ZQ": {"closes the window without writing", func(m *Machine, _ normalCall) error {
			m.h.Unhandled(":q!")
			return nil
		}},
		"<C-^>": {"shows the buffer shown before, or buffer n", func(m *Machine, x normalCall) error {
			if x.count > 0 {
				m.h.Unhandled(":b " + strconv.Itoa(x.count))
			} else {
				m.h.Unhandled(":b #")
			}
			return nil
		}},
		"<C-o>": {"goes back n places in the jump list", func(m *Machine, x normalCall) error {
			m.walk(m.h.Jump, x.n())
			return nil
		}},
		"<C-i>": {"goes forward n places in the jump list", func(m *Machine, x normalCall) error {
			m.walk(m.h.Jump, -x.n())
			return nil
		}},
		"g-": {"goes n states back in the text's history", func(m *Machine, x normalCall) error {
			return m.stepState(-x.n())
		}},
		"g+": {"goes n states forward in the text's history", func(m *Machine, x normalCall) error {
			return m.stepState(x.n())
		}},
		"g;": {"goes back n places in the change list", func(m *Machine, x normalCall) error {
			m.walk(m.h.Change, x.n())
			return nil
		}},
		"g,": {"goes forward n places in the change list", func(m *Machine, x normalCall) error {
			m.walk(m.h.Change, -x.n())
			return nil
		}},
		"&": {"repeats the last substitute on the line, with its flags", func(m *Machine, _ normalCall) error {
			return m.ex("&&")
		}},
		"v":     {"selects characters", visualMode},
		"V":     {"selects lines", visualMode},
		"<C-v>": {"selects a block", visualMode},
		"i":     {"inserts before the cursor", insertAt(func(x normalCall) int { return x.c })},
		"a":     {"inserts after the cursor", insertAt(func(x normalCall) int { return next(x.t, x.c, x.end) })},
		"I":     {"inserts before the line's first non-blank", insertAt(func(x normalCall) int { return firstNonBlank(x.t, x.start, x.end) })},
		"A":     {"inserts at the end of the line", insertAt(func(x normalCall) int { return x.end })},
		"o": {"opens a line below and inserts", func(m *Machine, x normalCall) error {
			if err := m.startInsert(x.k, x.end); err != nil {
				return err
			}
			return m.newline()
		}},
		"O": {"opens a line above and inserts", func(m *Machine, x normalCall) error {
			if err := m.startInsert(x.k, x.start); err != nil {
				return err
			}
			return m.openAbove()
		}},
	}
}

// stepState moves n states through the text's history, back for a
// negative n, and puts the cursor where the state changed.
func (m *Machine) stepState(n int) error {
	start, ok := m.h.Step(n)
	switch {
	case !ok && n < 0:
		return errors.New("already at the oldest change")
	case !ok:
		return errors.New("already at the newest change")
	}
	m.h.SetCursor(clamp(m.h.Text(), start))
	return nil
}

// undo moves n steps back, or forward for redo. The cursor goes back to
// where it was before the step if that is on the step's first line, and to
// that line's first non-blank otherwise.
func (m *Machine) undo(back bool, n int) error {
	step := m.h.Redo
	if back {
		step = m.h.Undo
	}
	for i := range n {
		start, cursor, err := step()
		switch {
		case errors.Is(err, ErrNoStep) && i > 0:
			return nil
		case errors.Is(err, ErrNoStep) && back:
			return errors.New("already at the oldest change")
		case errors.Is(err, ErrNoStep):
			return errors.New("already at the newest change")
		case err != nil:
			return err
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
