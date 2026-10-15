package editor

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// propose runs a command's keys at pos on a copy of b's text, and keeps the
// change they make as a proposal on b. The copy shares b's syntax, whose
// tree is of the same text until the keys edit the copy.
func (e *Session) propose(b *buffer, pos int, keys, text string) error {
	t := b.text.Snapshot()
	old := t.Slice(0, t.Len())
	cp := &buffer{file: b.file, text: kernel.New(old, nil), syntax: b.syntax, before: map[kernel.NodeID]int{}, marks: map[rune]kernel.MarkID{}}
	win := e.win
	e.win = newWindow(cp, pos, 0)
	e.command(keys, text)
	if e.vim.Mode() != vim.Normal {
		e.Feed("<Esc>")
	}
	e.win = win
	now := cp.text.Snapshot()
	ed := replace(old, now.Slice(0, now.Len()))
	if ed.Start == ed.End && ed.Text == "" {
		return errors.New("the command changes nothing")
	}
	// An insert keeps its end with its start, so text typed there goes
	// before both.
	gravity := kernel.Left
	if ed.Start == ed.End {
		gravity = kernel.Right
	}
	e.proposed++
	e.decos[e] = append(e.decos[e], decoration{e.proposed, b, b.text.SetMark(ed.Start, kernel.Right), b.text.SetMark(ed.End, gravity), "proposal", ed.Text, nil})
	return nil
}

// decide accepts or rejects the proposal numbered arg, or the newest on the
// current buffer. Accepting applies it as one node of its proposer's
// origin (B72), and a stale one cannot be accepted (B74).
func (e *Session) decide(arg string, accept bool) {
	var owner *Session
	i := -1
	for o, ds := range e.decos {
		s, ok := o.(*Session)
		if !ok {
			continue
		}
		for j, d := range ds {
			if arg == strconv.Itoa(d.id) || arg == "" && d.buf == e.win.buf && (i < 0 || d.id > e.decos[owner][i].id) {
				owner, i = s, j
			}
		}
	}
	if i < 0 {
		e.message = strings.TrimSpace("no proposal " + arg)
		return
	}
	d := e.decos[owner][i]
	if accept && d.style == "proposal.stale" {
		e.message = fmt.Sprintf("proposal %d is stale", d.id)
		return
	}
	b := d.buf
	edit := kernel.Edit{Start: b.text.Mark(d.start), End: b.text.Mark(d.end), Text: d.note}
	// The proposal goes first, or the edit that accepts it would mark it
	// stale.
	e.decos[owner] = slices.Delete(e.decos[owner], i, i+1)
	d.remove()
	if !accept {
		return
	}
	t := kernel.Transaction{Base: b.text.Snapshot().Version(), Origin: owner.origin, Label: "proposal", Time: e.now(), Edits: []kernel.Edit{edit}}
	if err := b.text.Apply(t); err != nil {
		e.message = err.Error()
		return
	}
	b.head = b.text.Head()
}
