package editor

import (
	"fmt"
	"path/filepath"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// buffer is one open file: its text, where the cursor was, and what the vim
// machine needs to undo, mark and scroll it.
type buffer struct {
	id     int
	file   file
	text   *kernel.Buffer
	cursor int
	saved  kernel.NodeID

	// The cursor from before each undo step, for the vim machine's undo.
	before  map[kernel.NodeID]int
	pending int // the cursor when the step being made began
	depth   int
	head    kernel.NodeID

	marks map[rune]kernel.MarkID
	top   kernel.MarkID // on the first line shown, so it follows edits

	changes  []kernel.MarkID // where edits happened, oldest first
	changeAt int             // the change g; last moved to, or len(changes)
}

// pushChange records an edit at pos, replacing the newest entry when it is
// on the same line.
func (b *buffer) pushChange(pos int) {
	t := b.text.Snapshot()
	if n := len(b.changes); n > 0 && sameLine(t, b.text.Mark(b.changes[n-1]), pos) {
		b.text.MoveMark(b.changes[n-1], pos)
	} else {
		b.changes = append(b.changes, b.text.SetMark(pos, kernel.Right))
	}
	b.changeAt = len(b.changes)
}

func sameLine(t kernel.Snapshot, p, q int) bool {
	return vim.LineStart(t, p) == vim.LineStart(t, q)
}

// openBuffer reads path into a buffer, and returns what to tell the user.
func openBuffer(id int, path string) (*buffer, string, error) {
	f, text, err := read(path)
	if err != nil {
		return nil, "", err
	}
	b := &buffer{id: id, file: f, text: kernel.New(text, nil), before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	b.top = b.text.SetMark(0, kernel.Left)
	msg := ""
	switch {
	case f.readonly:
		msg = fmt.Sprintf("\"%s\" is not valid UTF-8, opened read-only", path)
	case f.isNew && path != "":
		msg = fmt.Sprintf("\"%s\" [New]", path)
	}
	return b, msg, nil
}

func (b *buffer) modified() bool { return b.text.Head() != b.saved }

// name is the buffer's path, or [No Name] for a buffer with no file.
func (b *buffer) name() string {
	if b.file.path == "" {
		return "[No Name]"
	}
	return b.file.path
}

// is reports whether the buffer holds the file at path.
func (b *buffer) is(path string) bool {
	x, err1 := filepath.Abs(b.file.path)
	y, err2 := filepath.Abs(path)
	return b.file.path != "" && err1 == nil && err2 == nil && x == y
}
