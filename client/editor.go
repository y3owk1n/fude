package client

import (
	"cmp"
	"errors"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/clipboard"
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
	cols    int
	message string        // the server's message for the last command it ran
	confirm string        // the server's yes or no question, which only y and n answer (B362)
	dropped []kernel.Edit // edits a sync inside a key dropped, for Sync to return

	jumps    []kernel.MarkID // where jumps left from, oldest first
	jumpAt   int             // the jump <C-o> last moved to, or len(jumps)
	changes  []kernel.MarkID // where edits happened, oldest first
	changeAt int             // the change g; last moved to, or len(changes)
}

// NewEditor runs the grammar on r, with the cursor at its start and a window
// of rows lines, cols columns wide.
func NewEditor(r *Replica, rows, cols int) *Editor {
	e := &Editor{r: r, marks: map[rune]kernel.MarkID{}, rows: rows, cols: cols, cursor: r.text.SetMark(0, kernel.Right)}
	e.m = vim.New(host{e})
	return e
}

// Feed handles one key. While the server asks a question, y and n go to
// it and every other key does nothing (B362).
func (e *Editor) Feed(k vim.Key) error {
	e.message = ""
	if e.confirm != "" {
		if k == "y" || k == "n" {
			_, err := e.server(string(k))
			return err
		}
		return nil
	}
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

// Confirm returns the server's yes or no question, or "".
func (e *Editor) Confirm() string { return e.confirm }

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
		Confirm string `json:"confirm"`
	}
	if err := e.r.c.Call("keys", map[string]any{"keys": keys}, &st); err != nil {
		return nil, err
	}
	e.message, e.confirm = st.Message, st.Confirm
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
	e.changes = e.trim(e.changes)
	e.changeAt = len(e.changes)
	return nil
}

func (h host) Unhandled(keys string) error {
	// An ex line, such as ":w", runs on the server's command line.
	if strings.HasPrefix(keys, ":") {
		keys += "<CR>"
	}
	_, err := h.e.server(keys)
	return err
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

// ShowMark reports false, as a replica holds one text.
func (h host) ShowMark(rune) bool { return false }

func (h host) Mark(name rune) (int, bool) {
	id, ok := h.e.marks[name]
	if !ok {
		return 0, false
	}
	return h.e.r.text.Mark(id), true
}

func (h host) DeleteMark(name rune) {
	if id, ok := h.e.marks[name]; ok {
		h.e.r.text.DeleteMark(id)
		delete(h.e.marks, name)
	}
}

func (h host) Viewport() (int, int, int) { return h.e.top, h.e.rows, h.e.cols }
func (h host) SetTop(top int)            { h.e.top = top }

// PushJump records pos as a jump, dropping an older one on its line.
func (h host) PushJump(pos int) {
	e, t := h.e, h.e.r.text.Snapshot()
	e.jumps = slices.DeleteFunc(e.jumps, func(j kernel.MarkID) bool {
		if t.LineOf(e.r.text.Mark(j)) != t.LineOf(pos) {
			return false
		}
		e.r.text.DeleteMark(j)
		return true
	})
	e.jumps = e.trim(append(e.jumps, e.r.text.SetMark(pos, kernel.Right)))
	e.jumpAt = len(e.jumps)
}

// placesKept is how many places a jumplist or changelist keeps (B90).
const placesKept = 100

// trim drops the oldest of places past placesKept, with their marks.
func (e *Editor) trim(places []kernel.MarkID) []kernel.MarkID {
	over := len(places) - placesKept
	if over <= 0 {
		return places
	}
	for _, id := range places[:over] {
		e.r.text.DeleteMark(id)
	}
	return places[over:]
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

// IndentWidth is the buffer's setting in the server's config.toml (B94).
func (h host) IndentWidth() int { return cmp.Or(h.e.r.indent, vim.Tabstop) }

// TabWidth is the buffer's setting in the server's config.toml.
func (h host) TabWidth() int { return cmp.Or(h.e.r.tab, vim.Tabstop) }

// Fold reports no folds, as a client's replica keeps no window of its own.
func (h host) Fold(line int) (int, int) { return line, line }

// Shown reports no inlines, WordWrap no rows that end at a space, Hang
// no rows that start in, and Sideways no view to scroll, for the same
// reason.
func (h host) Shown(int, int) []vim.Inline     { return nil }
func (h host) WordWrap() bool                  { return false }
func (h host) Hang(int) int                    { return 0 }
func (h host) RowFrom(start, _ int) (int, int) { return start, 0 }
func (h host) Sideways() (int, int)            { return 0, 0 }
func (h host) SetLeft(int)                     {}

// FileName is "", as the replica knows its buffer by number alone.
func (h host) FileName() string { return "" }

// Editable lets the replica change, as fude says whether the change holds.
func (h host) Editable() error { return nil }
