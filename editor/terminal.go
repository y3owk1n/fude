package editor

import (
	"cmp"
	"errors"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/pty"
	"github.com/y3owk1n/fude/vim"
)

// terminal is a program running in a pseudo-terminal, which its buffer
// shows (B205).
type terminal struct {
	cmd        *exec.Cmd
	pty        *os.File
	em         *vt.Emulator
	name       string // what runs, as the buffer names it
	typing     bool   // keys go to the program, in terminal mode
	held       bool   // <C-\> came, so <C-n> leaves terminal mode
	esc        bool   // an <Esc> waits for a second one, which leaves terminal mode
	cols, rows int
	exited     bool
	task       string    // the task it runs, "" for :terminal (B228)
	started    time.Time // when the task started
}

// Cell is a character of a terminal window's screen with its look. A nil
// colour is the theme's.
type Cell struct {
	Text                                            string
	Width                                           int
	Fg, Bg                                          color.Color
	Bold, Faint, Italic, Underline, Reverse, Strike bool
}

// terminalKeys are a terminal window's own keys in normal mode, which
// [keymap.terminal] changes (B142, B205).
var terminalKeys = map[string]string{"q": "<Plug>(terminal.close)"}

// closeTerminal closes the current terminal window once its program has
// ended. A task's closes while it runs too, and the task goes on (B231).
func (e *Session) closeTerminal() {
	if t := e.win.buf.term; t != nil && !t.exited && t.task == "" {
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

// openTerminal runs arg through the shell, or the shell alone, in a window
// below a third of the screen tall, in terminal mode. A task's window keeps
// the cursor in the window it ran from, and its name says the task.
func (e *Session) openTerminal(arg, task string) {
	from := e.win
	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	argv := []string{shell}
	if arg != "" {
		argv = append(argv, "-c", arg)
	}
	if _, err := e.add(""); err != nil {
		e.message = err.Error()
		return
	}
	b := e.bufs[len(e.bufs)-1]
	b.file.readonly = true
	b.title = "[terminal " + cmp.Or(arg, shell) + "]"
	if task != "" {
		b.title = "[task " + task + "]"
	}
	e.split(false)
	e.root.leaf(e.win).size = max(e.height/3, 3)
	e.show(b)
	e.win.opts = options{Wrap: false}
	e.layout()
	t := &terminal{name: cmp.Or(arg, shell), typing: true, cols: max(e.win.w, 1), rows: max(e.win.h-1, 1), task: task, started: time.Now()}
	t.em = vt.NewEmulator(t.cols, t.rows)
	t.cmd = exec.Command(argv[0], argv[1:]...)
	t.cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	f, err := pty.Start(t.cmd, t.cols, t.rows)
	if err != nil {
		// The window goes, so no terminal shows without a program.
		if cerr := e.closeWindow(e.win); cerr != nil {
			err = errors.Join(err, cerr)
		}
		e.message = err.Error()
		return
	}
	t.pty, b.term = f, t
	// The emulator's answers and the keys it encodes go to the program.
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := t.em.Read(buf)
			if err != nil {
				return
			}
			if _, err := f.Write(buf[:n]); err != nil {
				return
			}
		}
	}()
	// What the program writes reaches the emulator on the editor loop.
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
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
		err := t.cmd.Wait()
		e.queue <- func() { e.terminalExited(b, err) }
	}()
	if task != "" {
		e.win = from
	}
}

// terminalExited notes that b's program ended, and shows its last screen
// as text in normal mode, with how it ended in the buffer's name.
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
	if t.task != "" {
		took := time.Since(t.started).Round(100 * time.Millisecond)
		b.title = fmt.Sprintf("[task %s ✓ %s]", t.task, took)
		e.message = fmt.Sprintf("the %s task ended in %s", t.task, took)
		if status != 0 {
			b.title = fmt.Sprintf("[task %s ✗ %d, %s]", t.task, status, took)
			e.message = fmt.Sprintf("the %s task failed with %d after %s", t.task, status, took)
		}
	}
	if t.typing {
		t.typing = false
		e.freeze(b)
	}
	if t.task != "" {
		e.taskPlaces(b, t.task)
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
	at := vim.ColumnStart(s, s.LineStart(last), cur.X, e.tabWidth(b))
	for _, s := range e.sessions {
		for _, w := range s.root.windows() {
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
func (t *terminal) screen() ([][]Cell, int, int) {
	rows := make([][]Cell, t.rows)
	for y := range rows {
		rows[y] = make([]Cell, t.cols)
		for x := range rows[y] {
			c := t.em.CellAt(x, y)
			if c == nil {
				rows[y][x] = Cell{Text: " ", Width: 1}
				continue
			}
			a := c.Style.Attrs
			rows[y][x] = Cell{
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
	return pty.Resize(t.pty, cols, rows)
}

// stop ends t's program and lets its pseudo-terminal go.
func (t *terminal) stop() {
	if !t.exited {
		t.cmd.Process.Kill()
	}
	t.pty.Close()
	t.em.Close()
}
