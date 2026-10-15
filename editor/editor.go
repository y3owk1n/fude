// Package editor hosts the kernel and the vim machine for a set of files.
package editor

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// Editor is a set of open files, one of them shown. Keys edit it through the
// vim machine.
type Editor struct {
	bufs    []*buffer
	cur     *buffer
	next    int // the id the next buffer gets
	vim     *vim.Machine
	message string
	done    bool
	height  int // how many lines the view shows

	jumps  []place
	jumpAt int // the jump <C-o> last moved to, or len(jumps)
}

// place is a position in a buffer, kept as a mark so it follows edits.
type place struct {
	buf  *buffer
	mark kernel.MarkID
}

// show switches to b. Leaving a buffer is a jump.
func (e *Editor) show(b *buffer) {
	if b != e.cur {
		e.pushJump(e.cur.cursor)
		e.cur = b
	}
}

// pushJump records pos in the shown buffer as a jump, dropping any older one
// on the same line of the same buffer.
func (e *Editor) pushJump(pos int) {
	b, t := e.cur, e.cur.text.Snapshot()
	var kept []place
	for _, j := range e.jumps {
		if j.buf != b || !sameLine(t, b.text.Mark(j.mark), pos) {
			kept = append(kept, j)
		}
	}
	e.jumps = append(kept, place{b, b.text.SetMark(pos, kernel.Right)})
	e.jumpAt = len(e.jumps)
}

// jump moves n jumps back, or forward when n is negative. The first step
// back records where the cursor is, so <C-i> can return to it.
func (e *Editor) jump(n int) bool {
	if n > 0 && e.jumpAt == len(e.jumps) {
		e.pushJump(e.cur.cursor)
		e.jumpAt = len(e.jumps) - 1
	}
	i := e.jumpAt - n
	if i < 0 || i >= len(e.jumps) {
		return false
	}
	j := e.jumps[i]
	e.jumpAt, e.cur = i, j.buf
	e.cur.cursor = j.buf.text.Mark(j.mark)
	return true
}

// Open opens each path as a buffer and shows the first. A path that does not
// exist opens an empty buffer. With no paths it opens one empty buffer with
// no file.
func Open(paths ...string) (*Editor, error) {
	if len(paths) == 0 {
		paths = []string{""}
	}
	e := &Editor{next: 1}
	e.vim = vim.New(host{e})
	for i, p := range paths {
		msg, err := e.add(p)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			e.cur, e.message = e.bufs[0], msg
		}
	}
	return e, nil
}

// Start opens the files a command line names. +N puts the cursor on the
// first non-blank of line N of the first file.
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
		e.cur.cursor = p + len(l) - len(strings.TrimLeft(l, " \t"))
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
	if err := e.vim.Feed(k); err != nil {
		e.message = err.Error()
	} else if msg := e.vim.Message(); msg != "" && e.message == "" {
		e.message = msg
	}
	e.reveal()
}

// SetHeight sets how many lines of text the view shows.
func (e *Editor) SetHeight(h int) {
	e.height = h
	e.reveal()
}

// Top returns the start of the first line shown.
func (e *Editor) Top() int {
	return vim.LineStart(e.cur.text.Snapshot(), e.cur.text.Mark(e.cur.top))
}

// reveal scrolls the view the least it must to show the cursor's line.
func (e *Editor) reveal() {
	b := e.cur
	t, c, top := b.text.Snapshot(), b.cursor, e.Top()
	if c < top {
		b.text.MoveMark(b.top, vim.LineStart(t, c))
		return
	}
	below := 0
	for p := top; p < vim.LineStart(t, c); p = vim.LineEnd(t, p) + 1 {
		below++
	}
	for ; below >= max(e.height, 1); below-- {
		top = vim.LineEnd(t, top) + 1
	}
	b.text.MoveMark(b.top, top)
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

// Text returns the shown buffer's text.
func (e *Editor) Text() kernel.Snapshot { return e.cur.text.Snapshot() }

// Cursor returns the cursor's byte offset.
func (e *Editor) Cursor() int { return e.cur.cursor }

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

func (e *Editor) ex(line string) {
	name, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)
	switch name {
	case "w":
		e.write()
	case "q":
		for _, b := range e.bufs {
			if b.modified() {
				e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
				return
			}
		}
		e.done = true
	case "q!":
		e.done = true
	case "reg", "registers":
		e.registers(arg)
	case "e", "edit":
		e.edit(arg)
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
		e.show(e.bufs[(e.index(e.cur)+step)%len(e.bufs)])
	case "bd", "bdelete", "bd!", "bdelete!":
		b := e.cur
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

// list shows every buffer with its number, % for the shown one and + for a
// modified one, as :ls does.
func (e *Editor) list() {
	var shown []string
	for _, b := range e.bufs {
		flags := ""
		if b == e.cur {
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

func (e *Editor) index(b *buffer) int {
	for i, x := range e.bufs {
		if x == b {
			return i
		}
	}
	return -1
}

// close removes b, refusing a modified buffer unless forced. The shown
// buffer moves to the next one, or to a new empty buffer if none is left.
func (e *Editor) close(b *buffer, force bool) {
	if b.modified() && !force {
		e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", b.name())
		return
	}
	i := e.index(b)
	e.bufs = append(e.bufs[:i], e.bufs[i+1:]...)
	var kept []place
	for _, j := range e.jumps {
		if j.buf != b {
			kept = append(kept, j)
		}
	}
	e.jumps, e.jumpAt = kept, len(kept)
	if len(e.bufs) == 0 {
		if _, err := e.add(""); err != nil {
			e.message = err.Error()
			return
		}
	}
	if b == e.cur {
		e.cur = e.bufs[min(i, len(e.bufs)-1)]
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
	b := e.cur
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
	e.message = fmt.Sprintf("\"%s\" %s%dB written", b.file.path, tag, n)
}

// host implements vim.Host for an Editor, acting on the shown buffer.
type host struct{ e *Editor }

func (h host) Text() vim.Text    { return h.e.cur.text.Snapshot() }
func (h host) Cursor() int       { return h.e.cur.cursor }
func (h host) SetCursor(pos int) { h.e.cur.cursor = pos }
func (h host) BeginGroup() {
	b := h.e.cur
	h.begin()
	b.depth++
	b.text.BeginGroup()
}

func (h host) EndGroup() {
	b := h.e.cur
	b.depth--
	b.text.EndGroup()
	h.end()
}

// begin notes the cursor when a new undo step may begin.
func (h host) begin() {
	if b := h.e.cur; b.depth == 0 {
		b.pending = b.cursor
	}
}

// end saves the noted cursor for the step just made, if there is one.
func (h host) end() {
	b := h.e.cur
	if head := b.text.Head(); head != b.head {
		b.before[head] = b.pending
		b.head = head
	}
}

func (h host) SetMark(name rune, pos int) {
	b := h.e.cur
	if id, ok := b.marks[name]; ok {
		b.text.MoveMark(id, pos)
		return
	}
	b.marks[name] = b.text.SetMark(pos, kernel.Right)
}

func (h host) Mark(name rune) (int, bool) {
	b := h.e.cur
	id, ok := b.marks[name]
	if !ok {
		return 0, false
	}
	return b.text.Mark(id), true
}

func (h host) Viewport() (int, int) { return h.e.Top(), h.e.height }
func (h host) PushJump(pos int)     { h.e.pushJump(pos) }
func (h host) Jump(n int) bool      { return h.e.jump(n) }

func (h host) Change(n int) bool {
	b := h.e.cur
	i := b.changeAt - n
	if i < 0 || i >= len(b.changes) {
		return false
	}
	b.changeAt, b.cursor = i, b.text.Mark(b.changes[i])
	return true
}
func (h host) SetTop(top int) { h.e.cur.text.MoveMark(h.e.cur.top, top) }

func (h host) Undo() (int, int, bool) {
	b := h.e.cur
	node := b.text.Head()
	start, ok := b.text.Undo()
	b.head = b.text.Head()
	return start, b.before[node], ok
}

func (h host) Redo() (int, int, bool) {
	b := h.e.cur
	start, ok := b.text.Redo()
	b.head = b.text.Head()
	return start, b.before[b.head], ok
}
func (h host) Unhandled(k string) { h.e.ex(strings.TrimPrefix(k, ":")) }

func (h host) Apply(edits []vim.Edit, label string) error {
	b := h.e.cur
	if b.file.readonly {
		return errors.New("cannot make changes, the file is read-only")
	}
	t := kernel.Transaction{Base: b.text.Snapshot().Version(), Origin: "user", Label: label}
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
