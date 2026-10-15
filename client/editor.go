package client

import (
	"errors"
	"slices"
	"strconv"

	"github.com/y3owk1n/fude/clipboard"
	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// Editor runs the vim grammar on a replica, so keys edit the copy with no
// round trip. Undo, redo and the commands the grammar leaves to its host,
// such as :w, go to the server, which keeps the one timeline (B98).
type Editor struct {
	r       *Replica
	m       *vim.Machine
	cursor  kernel.MarkID
	marks   map[rune]kernel.MarkID
	top     int
	rows    int
	message string        // the server's message for the last command it ran
	dropped []kernel.Edit // edits a sync inside a key dropped, for Sync to return

	jumps    []kernel.MarkID // where jumps left from, oldest first
	jumpAt   int             // the jump <C-o> last moved to, or len(jumps)
	changes  []kernel.MarkID // where edits happened, oldest first
	changeAt int             // the change g; last moved to, or len(changes)
}

// NewEditor runs the grammar on r, with the cursor at its start and a view
// of rows lines.
func NewEditor(r *Replica, rows int) *Editor {
	e := &Editor{r: r, marks: map[rune]kernel.MarkID{}, rows: rows, cursor: r.text.SetMark(0, kernel.Right)}
	e.m = vim.New(host{e})
	return e
}

// Feed handles one key.
func (e *Editor) Feed(k vim.Key) error {
	e.message = ""
	return e.m.Feed(k)
}

// Text returns the copy's text.
func (e *Editor) Text() string { return e.r.Text() }

// Cursor returns the cursor's byte offset.
func (e *Editor) Cursor() int { return e.r.text.Mark(e.cursor) }

// Mode returns the vim mode.
func (e *Editor) Mode() vim.Mode { return e.m.Mode() }

// Message returns what the last key has to tell the user, or "".
func (e *Editor) Message() string {
	if msg := e.m.Message(); msg != "" {
		return msg
	}
	return e.message
}

// Sync is the replica's Sync, between commands only, as an insert is one
// node only once it ends.
func (e *Editor) Sync() ([]kernel.Edit, error) {
	if !e.m.Idle() {
		return nil, nil
	}
	dropped, err := e.r.Sync()
	dropped, e.dropped = append(e.dropped, dropped...), nil
	return dropped, err
}

// server syncs, runs keys in the client's session on the server, and takes
// the edits they made.
func (e *Editor) server(keys string) ([][]kernel.Edit, error) {
	dropped, err := e.r.Sync()
	e.dropped = append(e.dropped, dropped...)
	if err != nil {
		return nil, err
	}
	var st struct {
		Message string `json:"message"`
	}
	if err := e.r.c.call("keys", map[string]any{"keys": keys}, &st); err != nil {
		return nil, err
	}
	e.message = st.Message
	_, remote, err := e.r.pull()
	return remote, err
}

// step is undo or redo on the server, which reverts or restores the
// client's own changes only (B77).
func (e *Editor) step(keys string) (int, int, error) {
	remote, err := e.server(keys)
	if err != nil {
		return 0, 0, err
	}
	if len(remote) == 0 || len(remote[0]) == 0 {
		return 0, 0, vim.ErrNoStep
	}
	start := remote[0][0].Start
	return start, start, nil
}

// host is the grammar's host on the replica.
type host struct{ e *Editor }

func (h host) Text() vim.Text    { return h.e.r.text.Snapshot() }
func (h host) Cursor() int       { return h.e.Cursor() }
func (h host) SetCursor(pos int) { h.e.r.text.MoveMark(h.e.cursor, pos) }
func (h host) BeginGroup()       { h.e.r.Begin() }
func (h host) EndGroup()         { h.e.r.End() }

func (h host) Apply(edits []vim.Edit, _ string) error {
	out := make([]kernel.Edit, len(edits))
	for i, ed := range edits {
		out[i] = kernel.Edit(ed)
	}
	if err := h.e.r.Edit(out...); err != nil {
		return err
	}
	// The changelist keeps one place per line, the newest.
	e, t := h.e, h.e.r.text.Snapshot()
	if n := len(e.changes); n > 0 && t.LineOf(e.r.text.Mark(e.changes[n-1])) == t.LineOf(edits[0].Start) {
		e.r.text.MoveMark(e.changes[n-1], edits[0].Start)
	} else {
		e.changes = append(e.changes, e.r.text.SetMark(edits[0].Start, kernel.Right))
	}
	e.changeAt = len(e.changes)
	return nil
}

func (h host) Unhandled(keys string) {
	if _, err := h.e.server(keys); err != nil {
		h.e.message = err.Error()
	}
}

func (h host) Undo() (int, int, error) { return h.e.step("u") }
func (h host) Redo() (int, int, error) { return h.e.step("<C-r>") }

// Clipboard and SetClipboard are the system clipboard of the client's
// machine (B36).
func (h host) Clipboard() (string, error)     { return clipboard.Read() }
func (h host) SetClipboard(text string) error { return clipboard.Write(text) }

func (h host) SetMark(name rune, pos int) {
	if id, ok := h.e.marks[name]; ok {
		h.e.r.text.MoveMark(id, pos)
		return
	}
	h.e.marks[name] = h.e.r.text.SetMark(pos, kernel.Right)
}

func (h host) Mark(name rune) (int, bool) {
	id, ok := h.e.marks[name]
	if !ok {
		return 0, false
	}
	return h.e.r.text.Mark(id), true
}

func (h host) Viewport() (int, int) { return h.e.top, h.e.rows }
func (h host) SetTop(top int)       { h.e.top = top }

// PushJump records pos as a jump, dropping an older one on its line.
func (h host) PushJump(pos int) {
	e, t := h.e, h.e.r.text.Snapshot()
	e.jumps = slices.DeleteFunc(e.jumps, func(j kernel.MarkID) bool { return t.LineOf(e.r.text.Mark(j)) == t.LineOf(pos) })
	e.jumps = append(e.jumps, e.r.text.SetMark(pos, kernel.Right))
	e.jumpAt = len(e.jumps)
}

// Jump moves n jumps back, or forward when n is negative. The first step
// back records where the cursor is, so <C-i> can return to it.
func (h host) Jump(n int) bool {
	e := h.e
	if n > 0 && e.jumpAt == len(e.jumps) {
		h.PushJump(e.Cursor())
		e.jumpAt = len(e.jumps) - 1
	}
	i := e.jumpAt - n
	if i < 0 || i >= len(e.jumps) {
		return false
	}
	e.jumpAt = i
	h.SetCursor(e.r.text.Mark(e.jumps[i]))
	return true
}

// Change moves n places back in the changelist, or forward when n is
// negative.
func (h host) Change(n int) bool {
	e := h.e
	i := e.changeAt - n
	if i < 0 || i >= len(e.changes) {
		return false
	}
	e.changeAt = i
	h.SetCursor(e.r.text.Mark(e.changes[i]))
	return true
}

// Step is g- and g+ on the server, whose timeline it moves through.
func (h host) Step(n int) (int, bool) {
	keys := "g+"
	if n < 0 {
		n, keys = -n, "g-"
	}
	start, _, err := h.e.step(strconv.Itoa(n) + keys)
	if err != nil && !errors.Is(err, vim.ErrNoStep) {
		h.e.message = err.Error()
	}
	return start, err == nil
}

func (h host) IndentWidth() int { return vim.Tabstop }
