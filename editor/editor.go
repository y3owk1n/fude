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
}

// Open opens path. A path that does not exist opens an empty buffer.
func Open(path string) (*Editor, error) {
	f, text, err := read(path)
	if err != nil {
		return nil, err
	}
	e := &Editor{file: f, buf: kernel.New(text, nil), before: map[kernel.NodeID]int{}}
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
	}
}

// Paste inserts text as typed, with no key mapping applied (B8).
func (e *Editor) Paste(text string) {
	e.message = ""
	if err := e.vim.Paste(text); err != nil {
		e.message = err.Error()
	}
}

// Text returns the buffer's current text.
func (e *Editor) Text() kernel.Snapshot { return e.buf.Snapshot() }

// Cursor returns the cursor's byte offset.
func (e *Editor) Cursor() int { return e.cursor }

// Mode returns the vim mode.
func (e *Editor) Mode() vim.Mode { return e.vim.Mode() }

// Line returns the command line being typed.
func (e *Editor) Line() string { return e.vim.Line() }

// Message returns the last message for the user, or "".
func (e *Editor) Message() string { return e.message }

// Done reports whether the user has quit.
func (e *Editor) Done() bool { return e.done }

func (e *Editor) ex(line string) {
	switch strings.TrimSpace(line) {
	case "w":
		e.write()
	case "q":
		if e.buf.Head() != e.saved {
			e.message = fmt.Sprintf("E37: No write since last change to \"%s\" (add ! to override)", e.file.path)
			return
		}
		e.done = true
	case "q!":
		e.done = true
	default:
		e.message = "E492: Not an editor command: " + line
	}
}

func (e *Editor) write() {
	if e.file.readonly {
		e.message = fmt.Sprintf("E45: \"%s\" is read-only", e.file.path)
		return
	}
	s := e.buf.Snapshot()
	n, err := e.file.write(s.Slice(0, s.Len()))
	if err != nil {
		e.message = "E212: " + err.Error()
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
		return errors.New("E21: cannot make changes, the file is read-only")
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
