package editor

import (
	"cmp"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/internal/trace"
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
	e.proposeEdit(b, ed.Start, ed.End, ed.Text)
	return nil
}

// proposeEdit keeps the replacement of [start, end) of b with text as a proposal
// of the session's, and returns its number.
func (e *Session) proposeEdit(b *buffer, start, end int, text string) int {
	// An insert keeps its end with its start, so text typed there goes
	// before both.
	gravity := kernel.Left
	if start == end {
		gravity = kernel.Right
	}
	e.proposed++
	d := decoration{id: e.proposed, buf: b, start: b.text.SetMark(start, kernel.Right), end: b.text.SetMark(end, gravity), style: "proposal", note: text}
	e.decos[e] = append(e.decos[e], d)
	if d.inline() {
		b.inlines++
	}
	trace.Record("proposal", fmt.Sprintf("%s proposed %d on %s", e.label(), d.id, b.name()), 0)
	e.emit("proposals.changed", "", b)
	return d.id
}

// ProposedEdit is one edit of a change a client proposes: [Start, End) of
// a buffer's text, named by its number or a file's path, and its new text.
// With Version, the buffer must still be at it (B387).
type ProposedEdit struct {
	Buffer  int             `json:"buffer,omitempty"`
	Path    string          `json:"path,omitempty"`
	Version *kernel.Version `json:"version,omitempty"`
	Start   int             `json:"start"`
	End     int             `json:"end"`
	Text    string          `json:"text"`
}

// Propose keeps each of edits as a proposal of the session's and returns
// their numbers, or keeps none when it refuses one (B387).
func (e *Session) Propose(edits []ProposedEdit) ([]int, error) {
	type checked struct {
		b *buffer
		ProposedEdit
	}
	var cs []checked
	for _, ed := range edits {
		b, err := e.proposedBuffer(ed)
		if err != nil {
			return nil, err
		}
		t := b.text.Snapshot()
		switch {
		case ed.Version != nil && *ed.Version != t.Version():
			return nil, fmt.Errorf("%s is at version %d, not %d", b.name(), t.Version(), *ed.Version)
		case ed.Start < 0 || ed.Start > ed.End || ed.End > t.Len():
			return nil, fmt.Errorf("%d to %d is outside %s, which is %d bytes", ed.Start, ed.End, b.name(), t.Len())
		}
		for _, c := range cs {
			if c.b == b && (ed.Start < c.End && c.Start < ed.End || ed.Start == c.Start) {
				return nil, fmt.Errorf("two edits of %s overlap at %d", b.name(), max(ed.Start, c.Start))
			}
		}
		cs = append(cs, checked{b, ed})
	}
	ids := make([]int, len(cs))
	for i, c := range cs {
		ids[i] = e.proposeEdit(c.b, c.Start, c.End, c.Text)
	}
	return ids, nil
}

// proposedBuffer returns the buffer ed names, opening the file at its path
// with no window when no buffer holds it.
func (e *Session) proposedBuffer(ed ProposedEdit) (*buffer, error) {
	if ed.Path == "" {
		b, err := e.buffer(ed.Buffer)
		if err != nil {
			return nil, err
		}
		return b, e.barred(b)
	}
	path, err := filepath.Abs(ed.Path)
	if err != nil {
		return nil, fmt.Errorf("propose to %s: %w", ed.Path, err)
	}
	if err := e.outside(path); err != nil {
		return nil, err
	}
	if i := slices.IndexFunc(e.bufs, func(b *buffer) bool { return b.is(path) }); i >= 0 {
		return e.bufs[i], e.barred(e.bufs[i])
	}
	if _, err := e.add(path); err != nil {
		return nil, err
	}
	return e.bufs[len(e.bufs)-1], nil
}

// decide accepts or rejects the proposals numbered in arg, or the newest
// on the current buffer. Accepting applies them as one node of their
// proposer's origin on each buffer (B72, B388), and a stale one stays
// (B74).
func (e *Session) decide(arg string, accept bool) error {
	if err := e.mayEdit(); accept && err != nil {
		return err
	}
	ids, err := e.proposalIDs(arg)
	if err != nil {
		return err
	}
	// Those of one buffer and one proposer go together, as one group.
	type found struct {
		id    int
		owner *Session
		b     *buffer
	}
	var fs []found
	for _, id := range ids {
		owner, d := e.proposal(id)
		fs = append(fs, found{id, owner, d.buf})
	}
	slices.SortStableFunc(fs, func(x, y found) int {
		return cmp.Or(cmp.Compare(x.b.id, y.b.id), strings.Compare(string(x.owner.origin), string(y.owner.origin)))
	})
	var stale []string
	var open *found
	end := func() {
		if open != nil {
			open.b.text.EndGroup(open.owner.origin)
			open.b.head = open.b.text.Head()
			open = nil
		}
	}
	defer end()
	for i, f := range fs {
		_, d := e.proposal(f.id)
		if accept && d.style == "proposal.stale" {
			stale = append(stale, strconv.Itoa(f.id))
			continue
		}
		// The proposal goes first, or the edit that accepts it would mark it
		// stale.
		e.dropProposal(f.owner, f.id)
		if !accept {
			f.owner.tell(f.id, "rejected")
			continue
		}
		if open == nil || open.b != f.b || open.owner != f.owner {
			end()
			open = &fs[i]
			f.b.text.BeginGroup(f.owner.origin)
		}
		t := kernel.Transaction{Base: f.b.text.Snapshot().Version(), Origin: f.owner.origin, Label: "proposal", Time: e.now(), Edits: []kernel.Edit{{Start: f.b.text.Mark(d.start), End: f.b.text.Mark(d.end), Text: d.note}}}
		if err := f.b.text.Apply(t); err != nil {
			return err
		}
		f.owner.tell(f.id, "accepted")
	}
	switch len(stale) {
	case 0:
		return nil
	case 1:
		return fmt.Errorf("proposal %s is stale", stale[0])
	}
	return fmt.Errorf("proposals %s and %s are stale", strings.Join(stale[:len(stale)-1], ", "), stale[len(stale)-1])
}

// proposalIDs returns the proposals arg numbers, or the newest on the
// current buffer when it numbers none.
func (e *Session) proposalIDs(arg string) ([]int, error) {
	var ids []int
	for _, f := range strings.Fields(arg) {
		id, err := strconv.Atoi(f)
		if owner, _ := e.proposal(id); err != nil || owner == nil {
			return nil, errors.New("no proposal " + f)
		}
		ids = append(ids, id)
	}
	if len(ids) > 0 {
		return ids, nil
	}
	newest := 0
	for o, ds := range e.decos {
		if _, ok := o.(*Session); ok {
			for _, d := range ds {
				if d.buf == e.win.buf {
					newest = max(newest, d.id)
				}
			}
		}
	}
	if newest == 0 {
		return nil, errors.New("no proposal")
	}
	return []int{newest}, nil
}

// proposal returns the proposal numbered id and the session that proposed
// it, or nil.
func (e *Session) proposal(id int) (*Session, decoration) {
	for o, ds := range e.decos {
		if s, ok := o.(*Session); ok {
			if i := slices.IndexFunc(ds, func(d decoration) bool { return d.id == id }); i >= 0 {
				return s, ds[i]
			}
		}
	}
	return nil, decoration{}
}

// dropProposal removes owner's proposal numbered id.
func (e *Session) dropProposal(owner *Session, id int) {
	ds := e.decos[owner]
	i := slices.IndexFunc(ds, func(d decoration) bool { return d.id == id })
	d := ds[i]
	e.decos[owner] = slices.Delete(ds, i, i+1)
	d.remove()
	e.emit("proposals.changed", "", d.buf)
}

// tell tells the session's client what became of its proposal numbered
// id: accepted, rejected or stale (B389).
func (e *Session) tell(id int, state string) {
	trace.Record("proposal", fmt.Sprintf("%d of %s %s", id, e.label(), state), 0)
	if e.notices != nil {
		e.notices.add(notice{id, state})
	}
}

// notice is what a client hears of one of its proposals.
type notice struct {
	ID    int    `json:"id"`
	State string `json:"state"`
}

// notices are what a client has yet to hear of its proposals, in order.
// The loop adds to them and the connection takes them, so neither waits
// for the other.
type notices struct {
	mu   sync.Mutex
	list []notice
	wake chan struct{}
}

func (n *notices) add(x notice) {
	n.mu.Lock()
	n.list = append(n.list, x)
	n.mu.Unlock()
	select {
	case n.wake <- struct{}{}:
	default: // it is already awake
	}
}

func (n *notices) take() []notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	list := n.list
	n.list = nil
	return list
}
