// Package editor hosts the kernel and the vim machine for a set of files
// shown in a layout of windows.
package editor

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// Editor is a set of open files shown in windows. Keys edit the current
// window's buffer through the vim machine.
type Editor struct {
	bufs    []*buffer
	next    int // the id the next buffer gets
	root    *node
	win     *window // the current window
	vim     *vim.Machine
	message string
	done    bool
	cfg     config

	width, height int // the space the windows share

	now func() time.Time // the clock edits are stamped with
}

// place is a position in a buffer, kept as a mark so it follows edits.
type place struct {
	buf  *buffer
	mark kernel.MarkID
}

// Open opens each path as a buffer and shows the first. A path that does not
// exist opens an empty buffer. With no paths it opens one empty buffer with
// no file.
func Open(paths ...string) (*Editor, error) {
	if len(paths) == 0 {
		paths = []string{""}
	}
	e := &Editor{next: 1, now: time.Now, cfg: defaults}
	e.vim = vim.New(host{e})
	for i, p := range paths {
		msg, err := e.add(p)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			e.message = msg
		}
	}
	e.win = newWindow(e.bufs[0], 0, 0)
	e.root = &node{win: e.win}
	e.layout()
	return e, nil
}

// Start reads config.toml and opens the files a command line names. +N puts
// the cursor on the first non-blank of line N of the first file.
func Start(args []string) (*Editor, error) {
	line := 0
	var paths []string
	for _, a := range args {
		if n, ok := strings.CutPrefix(a, "+"); ok {
			if v, err := strconv.Atoi(n); err == nil {
				line = v
				continue
			}
		}
		paths = append(paths, a)
	}
	e, err := Open(paths...)
	if err != nil {
		return nil, err
	}
	if line > 0 {
		t := e.Text()
		p := 0
		for range line - 1 {
			if le := vim.LineEnd(t, p); le < t.Len() {
				p = le + 1
			}
		}
		l := t.Slice(p, vim.LineEnd(t, p))
		e.setCursor(p + len(l) - len(strings.TrimLeft(l, " \t")))
	}
	cfg, err := loadConfig()
	if err != nil {
		e.message = err.Error()
	}
	e.cfg = cfg
	for mode, maps := range map[vim.Mode]map[string]string{vim.Normal: cfg.Keymap.Normal, vim.Visual: cfg.Keymap.Visual, vim.Insert: cfg.Keymap.Insert} {
		for from, to := range maps {
			e.vim.Map(mode, from, to)
		}
	}
	return e, nil
}

// add opens path as a new buffer at the end of the list.
func (e *Editor) add(path string) (string, error) {
	b, msg, err := openBuffer(e.next, path)
	if err != nil {
		return "", err
	}
	e.next++
	e.bufs = append(e.bufs, b)
	return msg, nil
}

// Feed handles one key. An error becomes the message.
func (e *Editor) Feed(k vim.Key) {
	e.message = ""
	e.settle(e.vim.Feed(k))
}

// Wait reports whether keys are held for a longer mapping, and how long to
// wait for the next key before calling Timeout.
func (e *Editor) Wait() (time.Duration, bool) {
	return time.Duration(e.cfg.KeyTimeout) * time.Millisecond, e.vim.Waiting()
}

// Timeout runs the keys held for a longer mapping when no key came in time.
func (e *Editor) Timeout() {
	e.message = ""
	e.settle(e.vim.Timeout())
}

// settle shows the error or message from the keys just run, and scrolls to
// the cursor.
func (e *Editor) settle(err error) {
	if err != nil {
		e.message = err.Error()
	} else if msg := e.vim.Message(); msg != "" && e.message == "" {
		e.message = msg
	}
	if log := e.win.buf.log; log != nil {
		if err := log.failed(); err != nil {
			e.message = err.Error()
		}
	}
	e.reveal()
}

// SetSize sets the columns and rows the windows share, status lines
// included.
func (e *Editor) SetSize(w, h int) {
	e.width, e.height = w, h
	e.layout()
	e.reveal()
}

// View is one window as a client draws it.
type View struct {
	Text              kernel.Snapshot
	Top, Cursor       int
	X, Y, W, H        int // H counts the status line on the bottom row
	Name              string
	Modified, Current bool
}

// Views returns every window from the top left.
func (e *Editor) Views() []View {
	var out []View
	for _, w := range e.root.windows() {
		out = append(out, View{
			Text: w.buf.text.Snapshot(), Top: e.topOf(w), Cursor: w.pos(),
			X: w.x, Y: w.y, W: w.w, H: w.h,
			Name: w.buf.name(), Modified: w.buf.modified(), Current: w == e.win,
		})
	}
	return out
}

// Top returns the start of the current window's first line shown.
func (e *Editor) Top() int { return e.topOf(e.win) }

func (e *Editor) topOf(w *window) int {
	return vim.LineStart(w.buf.text.Snapshot(), w.buf.text.Mark(w.top))
}

// rows is how many lines of text the current window shows.
func (e *Editor) rows() int { return max(e.win.h-1, 1) }

// reveal scrolls the current window the least it must to show the cursor's
// line.
func (e *Editor) reveal() {
	w := e.win
	t, c, top := w.buf.text.Snapshot(), w.pos(), e.Top()
	if c < top {
		w.buf.text.MoveMark(w.top, vim.LineStart(t, c))
		return
	}
	below := 0
	for p := top; p < vim.LineStart(t, c); p = vim.LineEnd(t, p) + 1 {
		below++
	}
	for ; below >= e.rows(); below-- {
		top = vim.LineEnd(t, top) + 1
	}
	w.buf.text.MoveMark(w.top, top)
}

// Matches returns the search matches between from and to, for highlighting.
func (e *Editor) Matches(from, to int) [][2]int { return e.vim.Matches(from, to) }

// Paste inserts text as typed, with no key mapping applied (B8).
func (e *Editor) Paste(text string) {
	e.message = ""
	if err := e.vim.Paste(text); err != nil {
		e.message = err.Error()
	}
	e.reveal()
}

// Text returns the current window's text.
func (e *Editor) Text() kernel.Snapshot { return e.win.buf.text.Snapshot() }

// Cursor returns the cursor's byte offset in the current window.
func (e *Editor) Cursor() int { return e.win.pos() }

func (e *Editor) setCursor(pos int) { e.win.buf.text.MoveMark(e.win.cursor, pos) }

// Mode returns the vim mode.
func (e *Editor) Mode() vim.Mode { return e.vim.Mode() }

// Selection returns the visual selection, or false outside visual mode.
func (e *Editor) Selection() (vim.Selection, bool) { return e.vim.Selection() }

// Line returns the command line being typed.
func (e *Editor) Line() string { return e.vim.Line() }

// Message returns the last message for the user, or "".
func (e *Editor) Message() string { return e.message }

// Done reports whether the user has quit.
func (e *Editor) Done() bool { return e.done }

// Recording returns the register a macro is being recorded into, or 0.
func (e *Editor) Recording() rune { return e.vim.Recording() }

// show switches the current window to b. Leaving a buffer is a jump.
func (e *Editor) show(b *buffer) {
	if b != e.win.buf {
		e.pushJump(e.Cursor())
		e.win.show(b)
	}
}

// pushJump records pos in the current window's buffer as a jump, dropping
// any older one on the same line of the same buffer.
func (e *Editor) pushJump(pos int) {
	w := e.win
	b, t := w.buf, w.buf.text.Snapshot()
	var kept []place
	for _, j := range w.jumps {
		if j.buf != b || !sameLine(t, b.text.Mark(j.mark), pos) {
			kept = append(kept, j)
		}
	}
	w.jumps = append(kept, place{b, b.text.SetMark(pos, kernel.Right)})
	w.jumpAt = len(w.jumps)
}

// jump moves n jumps back, or forward when n is negative. The first step
// back records where the cursor is, so <C-i> can return to it.
func (e *Editor) jump(n int) bool {
	w := e.win
	if n > 0 && w.jumpAt == len(w.jumps) {
		e.pushJump(e.Cursor())
		w.jumpAt = len(w.jumps) - 1
	}
	i := w.jumpAt - n
	if i < 0 || i >= len(w.jumps) {
		return false
	}
	j := w.jumps[i]
	if j.buf != w.buf {
		w.show(j.buf)
	}
	w.jumpAt = i
	e.setCursor(j.buf.text.Mark(j.mark))
	return true
}

// window runs a <C-w> command.
func (e *Editor) window(k string) {
	var err error
	switch k {
	case "s", "S", "<C-s>":
		e.split(false)
	case "v", "<C-v>":
		e.split(true)
	case "h", "j", "k", "l":
		e.moveTo(k[0])
	case "<C-h>", "<C-j>", "<C-k>", "<C-l>":
		e.moveTo(k[3])
	case "c", "<C-c>":
		err = e.closeWindow(e.win)
	case "q", "<C-q>":
		e.quit(false)
	case "o", "<C-o>":
		e.only()
	case "=":
		// Splits always share their space equally, so there is nothing to
		// even out.
	}
	if err != nil {
		e.message = err.Error()
	}
}

// quit closes the current window, or quits once it is the last one. Without
// force it refuses to quit while a buffer has unsaved changes.
func (e *Editor) quit(force bool) {
	if e.root.win == nil {
		if err := e.closeWindow(e.win); err != nil {
			e.message = err.Error()
		}
		return
	}
	for _, b := range e.bufs {
		if b.modified() && !force {
			e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
			return
		}
	}
	for _, b := range e.bufs {
		b.close()
	}
	e.done = true
}

func (e *Editor) ex(line string) {
	name, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "w":
		e.write()
	case "q", "quit":
		e.quit(false)
	case "q!", "quit!":
		e.quit(true)
	case "reg", "registers":
		e.registers(arg)
	case "e", "edit":
		e.edit(arg)
	case "sp", "split", "vs", "vsplit":
		e.split(name[0] == 'v')
		if arg != "" {
			e.edit(arg)
		}
	case "timeline":
		e.timeline()
	case "restore":
		e.restore(arg)
	case "diff":
		e.diff(arg)
	case "earlier", "later":
		e.travel(arg, name == "later")
	case "recover":
		e.recover()
	case "on", "only":
		e.only()
	case "clo", "close":
		if err := e.closeWindow(e.win); err != nil {
			e.message = err.Error()
		}
	case "ls", "buffers", "files":
		e.list()
	case "b", "buffer":
		if b := e.find(arg); b != nil {
			e.show(b)
		}
	case "bn", "bnext", "bp", "bprevious", "bN", "bNext":
		step := 1
		if name != "bn" && name != "bnext" {
			step = len(e.bufs) - 1
		}
		e.show(e.bufs[(index(e.bufs, e.win.buf)+step)%len(e.bufs)])
	case "bd", "bdelete", "bd!", "bdelete!":
		b := e.win.buf
		if arg != "" {
			b = e.find(arg)
		}
		if b != nil {
			e.close(b, strings.HasSuffix(name, "!"))
		}
	default:
		e.message = "not a command: " + line
	}
}

// edit shows the file at path, opening it if no buffer holds it.
func (e *Editor) edit(path string) {
	if path == "" {
		e.message = "no file name"
		return
	}
	for _, b := range e.bufs {
		if b.is(path) {
			e.show(b)
			return
		}
	}
	msg, err := e.add(path)
	if err != nil {
		e.message = err.Error()
		return
	}
	e.show(e.bufs[len(e.bufs)-1])
	e.message = msg
}

// list shows every buffer with its number, % for the current window's and +
// for a modified one, as :ls does.
func (e *Editor) list() {
	var shown []string
	for _, b := range e.bufs {
		flags := ""
		if b == e.win.buf {
			flags += "%"
		}
		if b.modified() {
			flags += "+"
		}
		shown = append(shown, fmt.Sprintf("%d%s \"%s\"", b.id, flags, b.name()))
	}
	e.message = strings.Join(shown, "  ")
}

// find returns the buffer numbered s, or the one whose name contains s.
func (e *Editor) find(s string) *buffer {
	var found *buffer
	for _, b := range e.bufs {
		switch {
		case strconv.Itoa(b.id) == s:
			return b
		case s != "" && strings.Contains(b.name(), s):
			if found != nil {
				e.message = "more than one buffer matches " + s
				return nil
			}
			found = b
		}
	}
	if found == nil {
		e.message = "no buffer matches " + s
	}
	return found
}

// close removes b, refusing a modified buffer unless forced. Windows showing
// it move to the next buffer, or to a new empty buffer if none is left.
func (e *Editor) close(b *buffer, force bool) {
	if b.modified() && !force {
		e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
		return
	}
	b.close()
	i := index(e.bufs, b)
	e.bufs = append(e.bufs[:i], e.bufs[i+1:]...)
	if len(e.bufs) == 0 {
		if _, err := e.add(""); err != nil {
			e.message = err.Error()
			return
		}
	}
	for _, w := range e.root.windows() {
		var kept []place
		for _, j := range w.jumps {
			if j.buf != b {
				kept = append(kept, j)
			}
		}
		w.jumps, w.jumpAt = kept, len(kept)
		if w.buf == b {
			w.show(e.bufs[min(i, len(e.bufs)-1)])
		}
	}
}

// registers shows each named register, as :reg does.
func (e *Editor) registers(names string) {
	var shown []string
	for _, r := range names {
		if text, ok := e.vim.Register(r); ok {
			shown = append(shown, fmt.Sprintf("\"%c   %s", r, strings.ReplaceAll(text, "\n", "^J")))
		}
	}
	e.message = strings.Join(shown, "  ")
}

func (e *Editor) write() {
	b := e.win.buf
	switch {
	case b.file.path == "":
		e.message = "no file name"
		return
	case b.file.readonly:
		e.message = fmt.Sprintf("\"%s\" is read-only", b.file.path)
		return
	}
	s := b.text.Snapshot()
	n, err := b.file.write(s.Slice(0, s.Len()))
	if err != nil {
		e.message = err.Error()
		return
	}
	tag := ""
	if b.file.isNew {
		tag = "[New] "
	}
	b.file.isNew = false
	b.saved = b.text.Head()
	b.saves = append(b.saves, b.saved)
	if b.log != nil {
		b.log.write(record{Kind: "save", Head: b.saved, Hash: hash(s.Slice(0, s.Len()))})
	}
	e.message = fmt.Sprintf("\"%s\" %s%dB written", b.file.path, tag, n)
}

// host implements vim.Host for an Editor, acting on the current window.
type host struct{ e *Editor }

func (h host) Text() vim.Text    { return h.e.Text() }
func (h host) Cursor() int       { return h.e.Cursor() }
func (h host) SetCursor(pos int) { h.e.setCursor(pos) }
func (h host) BeginGroup() {
	b := h.e.win.buf
	h.begin()
	b.depth++
	b.text.BeginGroup()
}

func (h host) EndGroup() {
	b := h.e.win.buf
	b.depth--
	b.text.EndGroup()
	h.end()
}

// begin notes the cursor when a new undo step may begin.
func (h host) begin() {
	if b := h.e.win.buf; b.depth == 0 {
		b.pending = h.e.Cursor()
	}
}

// end saves the noted cursor for the step just made, if there is one.
func (h host) end() {
	b := h.e.win.buf
	if head := b.text.Head(); head != b.head {
		b.before[head] = b.pending
		b.head = head
	}
}

func (h host) SetMark(name rune, pos int) {
	b := h.e.win.buf
	if id, ok := b.marks[name]; ok {
		b.text.MoveMark(id, pos)
		return
	}
	b.marks[name] = b.text.SetMark(pos, kernel.Right)
}

func (h host) Mark(name rune) (int, bool) {
	b := h.e.win.buf
	id, ok := b.marks[name]
	if !ok {
		return 0, false
	}
	return b.text.Mark(id), true
}

func (h host) Viewport() (int, int) { return h.e.Top(), h.e.rows() }
func (h host) SetTop(top int)       { h.e.win.buf.text.MoveMark(h.e.win.top, top) }
func (h host) PushJump(pos int)     { h.e.pushJump(pos) }
func (h host) Jump(n int) bool      { return h.e.jump(n) }

func (h host) Change(n int) bool {
	b := h.e.win.buf
	i := b.changeAt - n
	if i < 0 || i >= len(b.changes) {
		return false
	}
	b.changeAt = i
	h.e.setCursor(b.text.Mark(b.changes[i]))
	return true
}

func (h host) Step(n int) (int, bool) { return h.e.step(n) }

// IndentWidth is the setting for the buffer's filetype, else the top-level
// one.
func (h host) IndentWidth() int {
	w := h.e.cfg.IndentWidth
	if ft, ok := h.e.cfg.Filetype[h.e.win.buf.filetype()]; ok && ft.IndentWidth > 0 {
		w = ft.IndentWidth
	}
	if w == 0 {
		return vim.Tabstop
	}
	return int(w)
}

func (h host) Undo() (int, int, bool) {
	b := h.e.win.buf
	node := b.text.Head()
	start, ok := b.text.Undo()
	b.head = b.text.Head()
	return start, b.before[node], ok
}

func (h host) Redo() (int, int, bool) {
	b := h.e.win.buf
	start, ok := b.text.Redo()
	b.head = b.text.Head()
	return start, b.before[b.head], ok
}

func (h host) Unhandled(k string) {
	if w, ok := strings.CutPrefix(k, "<C-w>"); ok {
		h.e.window(w)
		return
	}
	h.e.ex(strings.TrimPrefix(k, ":"))
}

func (h host) Apply(edits []vim.Edit, label string) error {
	b := h.e.win.buf
	if b.file.readonly {
		return errors.New("cannot make changes, the file is read-only")
	}
	t := kernel.Transaction{Base: b.text.Snapshot().Version(), Origin: "user", Label: label, Time: h.e.now()}
	for _, ed := range edits {
		t.Edits = append(t.Edits, kernel.Edit(ed))
	}
	h.begin()
	err := b.text.Apply(t)
	h.end()
	if err == nil {
		b.pushChange(edits[0].Start)
	}
	return err
}
