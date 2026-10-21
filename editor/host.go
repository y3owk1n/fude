package editor

import (
	"cmp"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// host implements vim.Host for an Editor, acting on the current window.
type host struct{ e *Session }

func (h host) Text() vim.Text    { return h.e.Text() }
func (h host) Cursor() int       { return h.e.Cursor() }
func (h host) SetCursor(pos int) { h.e.setCursor(pos) }
func (h host) BeginGroup() {
	h.begin()
	h.e.depth++
	h.e.win.buf.text.BeginGroup(h.e.origin)
}

func (h host) EndGroup() {
	h.e.depth--
	h.e.win.buf.text.EndGroup(h.e.origin)
	h.end()
}

// begin notes the cursor when a new undo step may begin.
func (h host) begin() {
	if h.e.depth == 0 {
		h.e.pending = h.e.Cursor()
	}
}

// end saves the noted cursor for the step just made, if there is one.
func (h host) end() {
	b := h.e.win.buf
	if head := b.text.Head(); head != b.head {
		b.before[head] = h.e.pending
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

// Viewport gives a width no line reaches without wrap, so gj is j.
func (h host) Viewport() (int, int, int) {
	if !h.e.win.opts.Wrap {
		return h.e.Top(), h.e.rows(), math.MaxInt
	}
	return h.e.Top(), h.e.rows(), h.e.win.cols()
}
func (h host) SetTop(top int) { h.e.win.buf.text.MoveMark(h.e.win.top, top) }

// Sideways is the window's when its lines do not wrap.
func (h host) Sideways() (int, int) {
	if h.e.win.opts.Wrap {
		return 0, 0
	}
	return h.e.win.left, h.e.win.cols()
}
func (h host) SetLeft(left int) { h.e.win.left = left }
func (h host) FileName() string { return h.e.win.buf.file.path }
func (h host) PushJump(pos int) { h.e.pushJump(pos) }
func (h host) Jump(n int) bool  { return h.e.jump(n) }

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

// IndentWidth and TabWidth are the current buffer's.
func (h host) IndentWidth() int { return h.e.indentWidth(h.e.win.buf) }
func (h host) TabWidth() int    { return h.e.tabWidth(h.e.win.buf) }

// Fold returns the closed fold of the current window that holds line
// (B224).
func (h host) Fold(line int) (int, int) { return h.e.win.fold(h.e.Text(), line) }

// Shown returns what the current window shows in its lines from start to
// end that the text does not hold (B295).
func (h host) WordWrap() bool { return h.e.win.anchor != "" }

func (h host) Shown(start, end int) []vim.Inline {
	return h.e.shown(h.e.win, start, end).inlines
}

// indentWidth is the columns an indent takes in b, as a module set it,
// or else its filetype's indent_width, or else the global one, or else the
// tab width (B238).
func (ed *Editor) indentWidth(b *buffer) int {
	if v, ok := b.set["indent_width"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
		return ed.tabWidth(b)
	}
	w := ed.cfg.IndentWidth
	if ft, ok := ed.cfg.Filetype[ed.filetype(b)]; ok && ft.IndentWidth > 0 {
		w = ft.IndentWidth
	}
	if w == 0 {
		return ed.tabWidth(b)
	}
	return int(w)
}

// tabWidth is the columns a tab shows as in b, as a module set it, or
// else its filetype's tab_width, or else the global one, or else vim's
// (B238).
func (ed *Editor) tabWidth(b *buffer) int {
	if n, err := strconv.Atoi(b.set["tab_width"]); err == nil && n > 0 {
		return n
	}
	w := ed.cfg.TabWidth
	if ft, ok := ed.cfg.Filetype[ed.filetype(b)]; ok && ft.TabWidth > 0 {
		w = ft.TabWidth
	}
	return cmp.Or(int(w), vim.Tabstop)
}

// Undo reverts the session's newest change. When another origin changed the
// text since, it becomes a new node and the cursor goes to its start.
func (h host) Undo() (int, int, error) {
	b := h.e.win.buf
	node, n := b.text.Head(), b.text.NodeCount()
	start, err := b.text.UndoOrigin(h.e.origin, "undo", h.e.now())
	switch {
	case errors.Is(err, kernel.ErrNothing):
		return 0, 0, vim.ErrNoStep
	case errors.Is(err, kernel.ErrOverlap):
		return 0, 0, errors.New("cannot undo, a later change overlaps it")
	case err != nil:
		return 0, 0, err
	}
	cursor := b.before[node]
	if b.text.NodeCount() > n {
		cursor = start
	}
	b.head = b.text.Head()
	h.e.touch(b, "undo", b.touched)
	return start, cursor, nil
}

// Redo moves to the newest node undone from the head, whoever made it.
func (h host) Redo() (int, int, error) {
	b := h.e.win.buf
	start, ok := b.text.Redo()
	b.head = b.text.Head()
	if !ok {
		return 0, 0, vim.ErrNoStep
	}
	h.e.touch(b, "redo", b.touched)
	return start, b.before[b.head], nil
}

func (h host) Unhandled(k string) error {
	// A count before <C-w> sizes a window (B193).
	digits := len(k) - len(strings.TrimLeft(k, "0123456789"))
	count, _ := strconv.Atoi(k[:digits])
	k = k[digits:]
	if w, ok := strings.CutPrefix(k, "<C-w>"); ok {
		// The layout's keys do not reach a float, so <C-w> in one closes it.
		if f := h.e.win; f.anchor != "" {
			h.e.closeFloat(f)
			return nil
		}
		return h.e.window(w, count)
	}
	return h.e.ex(strings.TrimPrefix(k, ":"))
}

// Editable returns why the current buffer's text may not change, or nil
// (B4, B247, B302).
func (h host) Editable() error {
	b := h.e.win.buf
	// A window fude writes, such as a float, help or a list, is text to read
	// (B142).
	if b.written() {
		return errors.New("this window takes no typing, and q closes it")
	}
	if b.file.readonly {
		return errors.New("cannot make changes, the file is read-only")
	}
	// A read-only session may still edit its own buffers, such as the
	// finder's query and a proposal's copy.
	if err := h.e.mayEdit(); err != nil && slices.Contains(h.e.bufs, b) {
		return err
	}
	return nil
}

func (h host) Apply(edits []vim.Edit, label string) error {
	if err := h.Editable(); err != nil {
		return err
	}
	b := h.e.win.buf
	t := kernel.Transaction{Base: b.text.Snapshot().Version(), Origin: h.e.origin, Label: label, Time: h.e.now()}
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
