package editor

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/y3owk1n/fude/api"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/pty"
	"github.com/y3owk1n/fude/internal/shell"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// terminal is a program running in a pseudo-terminal, which its buffer
// shows (B205).
type terminal struct {
	pty        *pty.Terminal
	em         *vt.Emulator
	name       string // what runs, as the buffer names it
	typing     bool   // keys go to the program, in terminal mode
	held       bool   // <C-\> came, so <C-n> leaves terminal mode
	esc        bool   // an <Esc> waits for a second one, which leaves terminal mode
	cols, rows int
	exited     bool
	owner      api.Module // the module that ran it, nil for :terminal (B228)
	task       string     // the name its module ran it under
	started    time.Time  // when it started
}

// terminalKeys are a terminal window's own keys in normal mode, which
// [keymap.terminal] changes (B142, B205).
var terminalKeys = map[string]string{"q": "<Plug>(terminal.close)"}

// terminalMode is the overlay of a terminal window. In terminal mode every key
// goes to the program, and in normal mode i and a go back to terminal mode
// (B205).
type terminalMode struct{}

func (terminalMode) key(e *Session, k vim.Key) bool {
	t := e.win.buf.term
	switch {
	case t == nil:
		return false
	case t.typing:
		e.terminalKey(k)
	case (k == "i" || k == "a") && e.vim.Idle():
		e.enterTerminal()
	default:
		return false
	}
	return true
}

// closeTerminal closes the current terminal window once its program has
// ended. A module's closes while it runs too, and the program goes on
// (B231).
func (e *Session) closeTerminal() {
	if t := e.win.buf.term; t != nil && !t.exited && t.owner == nil {
		e.message = "the program is still running, and :q stops it"
		return
	}
	if err := e.closeWindow(e.win); err != nil {
		e.message = err.Error()
	}
}

// escTwice is how soon a second <Esc> must follow the first to leave
// terminal mode. Without one, the first goes to the program then.
const escTwice = 300 * time.Millisecond

// termKeys are the keys that are not text, as the program gets them.
var termKeys = map[vim.Key]rune{
	"<CR>": uv.KeyEnter, "<Esc>": uv.KeyEscape, "<BS>": uv.KeyBackspace, "\t": uv.KeyTab,
	"<Up>": uv.KeyUp, "<Down>": uv.KeyDown, "<Left>": uv.KeyLeft, "<Right>": uv.KeyRight,
	"<Home>": uv.KeyHome, "<End>": uv.KeyEnd, "<Del>": uv.KeyDelete,
	"<PageUp>": uv.KeyPgUp, "<PageDown>": uv.KeyPgDown,
}

// terminalWindow shows b in a new window placed where says, above or below
// terminal_height percent of the screen tall, with none of the window
// options, and moves to it. A locked one stays there (B205, B367).
func (e *Session) terminalWindow(b *buffer, where string, lock bool) {
	e.openIn(b, where)
	if where == "above" || where == "below" {
		e.root.leaf(e.win).size = max((e.height*int(e.cfg.TerminalHeight)+50)/100, 3)
	}
	e.win.opts, e.win.locked = options{}, lock
	e.layout()
}

// openTerminal runs arg through the shell, or the shell alone, in a window
// below terminal_height percent of the screen tall, in terminal mode, and
// returns its buffer, or nil when it could not. A module's terminal leaves
// the cursor in the window it ran from, and its title is the module's name
// and task.
func (e *Session) openTerminal(term api.Terminal, owner api.Module, where string) *buffer {
	arg, task := term.Command, term.Name
	from := e.win
	argv := shell.Command(arg)
	if _, err := e.add(""); err != nil {
		e.message = err.Error()
		return nil
	}
	b := e.bufs[len(e.bufs)-1]
	b.file.readonly = true
	b.title = "[terminal " + cmp.Or(arg, argv[0]) + "]"
	if owner != nil {
		b.title = "[" + owner.Name() + " " + task + "]"
	}
	e.terminalWindow(b, where, term.Lock)
	t := &terminal{name: cmp.Or(arg, argv[0]), typing: true, cols: max(e.win.w, 1), rows: max(e.win.h-1, 1), owner: owner, task: task, started: time.Now()}
	t.em = vt.NewEmulator(t.cols, t.rows)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	// fude open, keys and state in the terminal act on this session (B355).
	if e.socket != "" {
		cmd.Env = append(cmd.Env, "FUDE_SOCKET="+e.socket, "FUDE_SESSION="+string(e.origin))
	}
	pt, err := pty.Start(cmd, t.cols, t.rows)
	if err != nil {
		// The window and buffer go, so no terminal shows without a program.
		if cerr := e.closeWindow(e.win); cerr != nil {
			err = errors.Join(err, cerr)
		}
		e.close(b, true)
		e.message = err.Error()
		return nil
	}
	t.pty, b.term = pt, t
	// A float is as big as the screen lets a terminal be, once it is one.
	if e.win.anchor != "" {
		e.place(e.win)
	}
	e.record("term", "started "+t.name, 0)
	// The emulator's answers and the keys it encodes go to the program.
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := t.em.Read(buf)
			if err != nil {
				return
			}
			if _, err := pt.Write(buf[:n]); err != nil {
				return
			}
		}
	}()
	// What the program writes reaches the emulator on the editor loop.
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		buf := make([]byte, 32<<10)
		for {
			n, err := pt.Read(buf)
			if n > 0 {
				out := append([]byte(nil), buf[:n]...)
				e.queue <- func() { t.em.Write(out) }
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := pt.Wait()
		// Its last output reaches the emulator before its end, unless a
		// child it left keeps the terminal open.
		select {
		case <-drained:
		case <-time.After(100 * time.Millisecond):
		}
		e.queue <- func() { e.terminalExited(b, err) }
	}()
	if owner != nil {
		e.win = from
	}
	return b
}

// terminalExited notes that b's program ended, and shows its last screen
// as text in normal mode, with how it ended in the buffer's name. The
// module that ran it hears terminal.exited about b (B230, B231).
func (e *Session) terminalExited(b *buffer, err error) {
	t := b.term
	if t == nil || t.exited {
		return
	}
	t.exited = true
	status := 0
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		status = exit.ExitCode()
	}
	b.title = fmt.Sprintf("[terminal %s, exited %d]", t.name, status)
	e.record("term", fmt.Sprintf("%s exited %d", t.name, status), time.Since(t.started))
	if t.typing {
		t.typing = false
		e.freeze(b)
	}
	if p := t.owner; p != nil && slices.Contains(e.modules, p) {
		took := time.Since(t.started).Round(100 * time.Millisecond)
		b.title = fmt.Sprintf("[%s %s ✓ %s]", p.Name(), t.task, took)
		if status != 0 {
			b.title = fmt.Sprintf("[%s %s ✗ %d, %s]", p.Name(), t.task, status, took)
		}
		e.listen(p).add(event{name: "terminal.exited", detail: t.task, buf: b, extra: api.Context{"status": strconv.Itoa(status), "took": took.String()}, from: e})
	}
}

// terminalKey sends k to the program of the current window's terminal.
// <C-\><C-n>, or <Esc> twice, leaves terminal mode instead. An <Esc> waits
// for a second one, and goes to the program ahead of any other key, so an
// Alt chord that comes as <Esc> and a key stays one.
func (e *Session) terminalKey(k vim.Key) {
	t := e.win.buf.term
	switch {
	case t.esc && k == "<Esc>":
		t.esc = false
		e.leaveTerminal()
		return
	case t.esc:
		t.esc = false
		t.em.SendKey(uv.KeyPressEvent{Code: uv.KeyEscape})
	case k == "<Esc>":
		t.esc = true
		return
	case t.held:
		t.held = false
		if k == "<C-n>" {
			e.leaveTerminal()
			return
		}
		t.em.SendKey(uv.KeyPressEvent{Code: '\\', Mod: uv.ModCtrl})
	case k == "<C-\\>":
		t.held = true
		return
	}
	if code, ok := termKeys[k]; ok {
		t.em.SendKey(uv.KeyPressEvent{Code: code})
		return
	}
	if k == "<S-Tab>" {
		t.em.SendKey(uv.KeyPressEvent{Code: uv.KeyTab, Mod: uv.ModShift})
		return
	}
	if c, ok := strings.CutPrefix(string(k), "<C-"); ok && len(c) == 2 {
		t.em.SendKey(uv.KeyPressEvent{Code: rune(c[0]), Mod: uv.ModCtrl})
		return
	}
	if !strings.HasPrefix(string(k), "<") || k == "<" {
		t.em.SendText(string(k))
	}
}

// leaveTerminal goes to normal mode in the current window's terminal.
func (e *Session) leaveTerminal() {
	e.win.buf.term.typing = false
	e.freeze(e.win.buf)
}

// enterTerminal goes back to terminal mode in the current window's
// terminal, while its program runs.
func (e *Session) enterTerminal() {
	if t := e.win.buf.term; t.exited {
		e.message = "the program in this terminal has ended"
	} else {
		t.typing = true
	}
}

// freeze replaces b's text with its terminal's scrollback and screen, for
// normal mode to move through, search and yank. The cursor goes where the
// program's was, and each window on b gets new marks.
func (e *Session) freeze(b *buffer) {
	t := b.term
	var lines []string
	for y := range t.em.ScrollbackLen() {
		lines = append(lines, textRow(t.cols, func(x int) *uv.Cell { return t.em.ScrollbackCellAt(x, y) }))
	}
	for y := range t.rows {
		lines = append(lines, textRow(t.cols, func(x int) *uv.Cell { return t.em.CellAt(x, y) }))
	}
	cur := t.em.CursorPosition()
	last := len(lines) - t.rows + cur.Y
	for len(lines) > last+1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	b.text, b.marks = kernel.New(strings.Join(lines, "\n"), nil), map[rune]kernel.MarkID{}
	// The program's cursor is a column, which the line's characters make a
	// byte.
	s := b.text.Snapshot()
	at := vim.ColumnStart(s, s.LineStart(last), cur.X, e.tabWidth(b), nil)
	for _, s := range e.sessions {
		for _, w := range append(s.windows(), s.hidden...) {
			if w.buf == b {
				w.cursor, w.top = b.text.SetMark(at, kernel.Right), b.text.SetMark(0, kernel.Left)
			}
		}
	}
	b.last, b.lastTop = at, 0
	e.reveal()
}

// textRow returns a line of cols cells as text, without the blanks after it.
func textRow(cols int, cell func(x int) *uv.Cell) string {
	var s strings.Builder
	for x := 0; x < cols; x++ {
		c := cell(x)
		switch {
		case c == nil:
			s.WriteByte(' ')
		case c.Width == 0:
			// The second column of a wide character.
		case c.Content == "":
			s.WriteByte(' ')
		default:
			s.WriteString(c.Content)
		}
	}
	return strings.TrimRight(s.String(), " ")
}

// screen returns the cells of t's screen, row by row, for a window in
// terminal mode, and where the program's cursor is, or -1, -1 when the
// program hides it.
func (t *terminal) screen() ([][]wire.Cell, int, int) {
	rows := make([][]wire.Cell, t.rows)
	for y := range rows {
		rows[y] = make([]wire.Cell, t.cols)
		for x := range rows[y] {
			c := t.em.CellAt(x, y)
			if c == nil {
				rows[y][x] = wire.Cell{Text: " ", Width: 1}
				continue
			}
			a := c.Style.Attrs
			rows[y][x] = wire.Cell{
				Text: c.Content, Width: c.Width, Fg: c.Style.Fg, Bg: c.Style.Bg,
				Bold: a&uv.AttrBold != 0, Faint: a&uv.AttrFaint != 0, Italic: a&uv.AttrItalic != 0,
				Underline: c.Style.Underline != 0, Reverse: a&uv.AttrReverse != 0, Strike: a&uv.AttrStrikethrough != 0,
			}
		}
	}
	if t.em.CursorHidden() {
		return rows, -1, -1
	}
	p := t.em.CursorPosition()
	return rows, p.X, p.Y
}

// fit resizes t to cols by rows, telling the program.
func (t *terminal) fit(cols, rows int) error {
	if cols < 1 || rows < 1 || cols == t.cols && rows == t.rows || t.exited {
		return nil
	}
	t.cols, t.rows = cols, rows
	t.em.Resize(cols, rows)
	return t.pty.Resize(cols, rows)
}

// stop ends t's program and lets its pseudo-terminal go.
func (t *terminal) stop() {
	if !t.exited {
		t.pty.Kill()
	}
	t.pty.Close()
	// The emulator's Close races its Read, which the goroutine passing its
	// answers to the program waits in. Closing its input pipe ends that Read
	// without the race.
	if pw, ok := t.em.InputPipe().(*io.PipeWriter); ok {
		pw.Close()
		return
	}
	t.em.Close()
}

// moduleTerminal runs t for p in a terminal window below. If p's terminal
// of t's name still runs, its window shows again instead. The window of
// one that ended closes first (B228, B231).
func (e *Session) moduleTerminal(p api.Module, t api.Terminal) {
	where := e.placement(p, t.Where, t.Lock, "below")
	switch {
	case t.Buffer != 0:
		i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.id == t.Buffer && b.term != nil })
		if i < 0 {
			e.message = fmt.Sprintf("no terminal %d", t.Buffer)
			return
		}
		e.focusTerminal(e.bufs[i], where, t.Lock)
		return
	case t.Name == "" && t.Command == "":
		e.toggleShell(t, where)
		return
	case t.Name == "":
		// One with no name is a terminal of its own, as :terminal cmd opens
		// (B335).
		e.openTerminal(t, nil, where)
		return
	}
	for _, b := range e.bufs {
		if term := b.term; term != nil && term.owner == p && term.task == t.Name && !term.exited {
			from := e.win
			if e.showTerminal(b, where, t.Lock) {
				e.win = from
			}
			return
		}
	}
	for _, w := range e.root.windows() {
		if term := w.buf.term; term != nil && term.owner == p && term.task == t.Name {
			if err := e.closeWindow(w); err != nil {
				e.message = err.Error()
				return
			}
		}
	}
	e.openTerminal(t, p, where)
}

// toggleShell shows the session's shell numbered t.Shell, or 1, in terminal
// mode, starting it the first time and again once it ended in a window
// placed where says, or hides its window when that is the current one
// (B205).
func (e *Session) toggleShell(t api.Terminal, where string) {
	n := max(t.Shell, 1)
	b := e.shells[n]
	switch {
	case b == nil || b.term.exited || !slices.Contains(e.bufs, b):
		// The window of one that ended closes first, as a task's does.
		for _, w := range e.windows() {
			if b == nil || w.buf != b {
				continue
			}
			if err := e.closeWindow(w); err != nil {
				e.message = err.Error()
				return
			}
		}
		if b = e.openTerminal(t, nil, where); b == nil {
			return
		}
		b.title = fmt.Sprintf("[shell %d]", n)
		if e.shells == nil {
			e.shells = map[int]*buffer{}
		}
		e.shells[n] = b
	case e.win.buf == b:
		if err := e.hide(e.win); err != nil {
			e.message = err.Error()
		}
	default:
		e.focusTerminal(b, where, t.Lock)
	}
}

// focusTerminal moves to the window on b, a terminal. With none showing,
// it shows the hidden one where it was, or opens one placed where says.
// The terminal goes to terminal mode while its program runs.
func (e *Session) focusTerminal(b *buffer, where string, lock bool) {
	if i := slices.IndexFunc(e.windows(), func(w *window) bool { return w.buf == b }); i >= 0 {
		e.win = e.windows()[i]
	} else {
		e.showTerminal(b, where, lock)
	}
	b.term.typing = !b.term.exited
}

// showTerminal shows a window on b that is out of sight where it was, or
// else opens one placed where says, unless a window shows b already. It
// reports whether it showed one.
func (e *Session) showTerminal(b *buffer, where string, lock bool) bool {
	if slices.ContainsFunc(e.windows(), func(w *window) bool { return w.buf == b }) {
		return false
	}
	if i := slices.IndexFunc(e.hidden, func(w *window) bool { return w.buf == b }); i >= 0 {
		e.unhide(e.hidden[i])
	} else {
		e.terminalWindow(b, where, lock)
	}
	return true
}

// runningTasks returns the names of the modules' terminals whose programs
// run, for the status line (B231).
func (e *Session) runningTasks() []string {
	var out []string
	for _, b := range e.bufs {
		if t := b.term; t != nil && t.owner != nil && !t.exited {
			out = append(out, t.task)
		}
	}
	return out
}
