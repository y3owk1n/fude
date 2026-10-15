// Package editor hosts the kernel and the vim machine for one file.
package editor

import (
	"errors"
	"fmt"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// Editor is one file in a buffer. Keys edit it through the vim machine.
type Editor struct {
	file    file
	buf     *kernel.Buffer
	vim     *vim.Machine
	cursor  int
	saved   kernel.NodeID
	message string
	done    bool

	// The cursor from before each undo step, for the vim machine's undo.
	before  map[kernel.NodeID]int
	pending int // the cursor when the step being made began
	depth   int
	head    kernel.NodeID

	marks map[rune]kernel.MarkID

	top    kernel.MarkID // on the first line shown, so it follows edits
	height int           // how many lines fit
}

// Open opens path. A path that does not exist opens an empty buffer.
func Open(path string) (*Editor, error) {
	f, text, err := read(path)
	if err != nil {
		return nil, err
	}
	e := &Editor{file: f, buf: kernel.New(text, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	e.top = e.buf.SetMark(0, kernel.Left)
	e.vim = vim.New(host{e})
	switch {
	case f.readonly:
		e.message = fmt.Sprintf("\"%s\" is not valid UTF-8, opened read-only", path)
	case f.isNew:
		e.message = fmt.Sprintf("\"%s\" [New]", path)
	}
	return e, nil
}

// Feed handles one key. An error becomes the message.
func (e *Editor) Feed(k vim.Key) {
	e.message = ""
	if err := e.vim.Feed(k); err != nil {
		e.message = err.Error()
	} else if msg := e.vim.Message(); msg != "" {
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
func (e *Editor) Top() int { return vim.LineStart(e.buf.Snapshot(), e.buf.Mark(e.top)) }

// reveal scrolls the view the least it must to show the cursor's line.
func (e *Editor) reveal() {
	t, c, top := e.buf.Snapshot(), e.cursor, e.Top()
	if c < top {
		e.buf.MoveMark(e.top, vim.LineStart(t, c))
		return
	}
	below := 0
	for p := top; p < vim.LineStart(t, c); p = vim.LineEnd(t, p) + 1 {
		below++
	}
	for ; below >= max(e.height, 1); below-- {
		top = vim.LineEnd(t, top) + 1
	}
	e.buf.MoveMark(e.top, top)
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

// Text returns the buffer's current text.
func (e *Editor) Text() kernel.Snapshot { return e.buf.Snapshot() }

// Cursor returns the cursor's byte offset.
func (e *Editor) Cursor() int { return e.cursor }

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
	if args, ok := strings.CutPrefix(strings.TrimSpace(line), "reg "); ok {
		e.registers(strings.TrimSpace(args))
		return
	}
	switch strings.TrimSpace(line) {
	case "w":
		e.write()
	case "q":
		if e.buf.Head() != e.saved {
			e.message = fmt.Sprintf("no write since the last change to \"%s\" (add ! to override)", e.file.path)
			return
		}
		e.done = true
	case "q!":
		e.done = true
	default:
		e.message = "not a command: " + line
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
	if e.file.readonly {
		e.message = fmt.Sprintf("\"%s\" is read-only", e.file.path)
		return
	}
	s := e.buf.Snapshot()
	n, err := e.file.write(s.Slice(0, s.Len()))
	if err != nil {
		e.message = err.Error()
		return
	}
	tag := ""
	if e.file.isNew {
		tag = "[New] "
	}
	e.file.isNew = false
	e.saved = e.buf.Head()
	e.message = fmt.Sprintf("\"%s\" %s%dB written", e.file.path, tag, n)
}

// host implements vim.Host for an Editor.
type host struct{ e *Editor }

func (h host) Text() vim.Text    { return h.e.buf.Snapshot() }
func (h host) Cursor() int       { return h.e.cursor }
func (h host) SetCursor(pos int) { h.e.cursor = pos }
func (h host) BeginGroup() {
	h.begin()
	h.e.depth++
	h.e.buf.BeginGroup()
}

func (h host) EndGroup() {
	h.e.depth--
	h.e.buf.EndGroup()
	h.end()
}

// begin notes the cursor when a new undo step may begin.
func (h host) begin() {
	if h.e.depth == 0 {
		h.e.pending = h.e.cursor
	}
}

// end saves the noted cursor for the step just made, if there is one.
func (h host) end() {
	if head := h.e.buf.Head(); head != h.e.head {
		h.e.before[head] = h.e.pending
		h.e.head = head
	}
}

func (h host) SetMark(name rune, pos int) {
	if id, ok := h.e.marks[name]; ok {
		h.e.buf.MoveMark(id, pos)
		return
	}
	h.e.marks[name] = h.e.buf.SetMark(pos, kernel.Right)
}

func (h host) Mark(name rune) (int, bool) {
	id, ok := h.e.marks[name]
	if !ok {
		return 0, false
	}
	return h.e.buf.Mark(id), true
}

func (h host) Viewport() (int, int) { return h.e.Top(), h.e.height }
func (h host) SetTop(top int)       { h.e.buf.MoveMark(h.e.top, top) }

func (h host) Undo() (int, int, bool) {
	node := h.e.buf.Head()
	start, ok := h.e.buf.Undo()
	h.e.head = h.e.buf.Head()
	return start, h.e.before[node], ok
}

func (h host) Redo() (int, int, bool) {
	start, ok := h.e.buf.Redo()
	h.e.head = h.e.buf.Head()
	return start, h.e.before[h.e.head], ok
}
func (h host) Unhandled(k string) { h.e.ex(strings.TrimPrefix(k, ":")) }

func (h host) Apply(edits []vim.Edit, label string) error {
	if h.e.file.readonly {
		return errors.New("cannot make changes, the file is read-only")
	}
	t := kernel.Transaction{Base: h.e.buf.Snapshot().Version(), Origin: "user", Label: label}
	for _, ed := range edits {
		t.Edits = append(t.Edits, kernel.Edit(ed))
	}
	h.begin()
	err := h.e.buf.Apply(t)
	h.end()
	return err
}
