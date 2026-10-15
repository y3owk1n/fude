package kernel

import (
	"errors"
	"slices"
	"time"
)

// Errors UndoOrigin returns.
var (
	ErrNothing = errors.New("kernel: no change of that origin to undo")
	ErrOverlap = errors.New("kernel: a later change overlaps it")
)

// logged is the edits made at one version, as edit applied them.
type logged struct {
	at    Version
	edits []Edit
}

// logSize is how many edit lists Map can look back through.
const logSize = 4096

// Map returns where pos in the text at version from is now, or false when an
// edit since replaced it, or from is older than the edits Map remembers. Text
// inserted at pos goes before it.
func (b *Buffer) Map(pos int, from Version) (int, bool) {
	if from > b.version {
		return 0, false
	}
	i := slices.IndexFunc(b.log, func(l logged) bool { return l.at >= from })
	if i < 0 {
		// Nothing changed since from.
		return pos, true
	}
	if i == 0 && b.log[0].at > from && from < b.version {
		// The edits right after from are no longer in the log.
		return 0, false
	}
	for _, l := range b.log[i:] {
		for j := len(l.edits) - 1; j >= 0; j-- {
			e := l.edits[j]
			if e.Start < pos && pos < e.End {
				return 0, false
			}
			pos = move(mark{pos: pos, gravity: Right}, e)
		}
	}
	return pos, true
}

// UndoOrigin reverts the newest change of origin o still in the text, and
// returns where the reverted text starts. When that change is the head node,
// this is Undo. Otherwise its inverse, moved through the changes made after
// it, applies as a new node of o's, labelled label at time at, and it fails
// with ErrOverlap when one of those changes overlaps it.
func (b *Buffer) UndoOrigin(o Origin, label string, at time.Time) (int, error) {
	var later []Node // the nodes after o's change, newest first
	var target Node
	found := false
	for id := b.head; id != (NodeID{}); {
		i, ok := b.index(id)
		if !ok {
			break
		}
		n := b.nodes[i]
		if n.ID.Origin == o && !b.reverted[n.ID] {
			target, found = n, true
			break
		}
		later = append(later, n)
		id = n.Parent
	}
	if !found {
		return 0, ErrNothing
	}
	if len(later) == 0 {
		start, _ := b.Undo()
		return start, nil
	}
	// The change as one edit: the text after it becomes the text before it.
	after, _ := b.At(target.ID)
	before, _ := b.At(target.Parent)
	inv := diff(after.Slice(0, after.Len()), before.Slice(0, before.Len()))
	for i := len(later) - 1; i >= 0; i-- {
		for _, c := range later[i].Changes {
			for j := len(c.Edits) - 1; j >= 0; j-- {
				e := c.Edits[j]
				// An edit overlaps it when their ranges share text, or when it
				// inserts strictly inside it.
				if e.Start < inv.End && inv.Start < e.End || e.Start == e.End && inv.Start < e.Start && e.Start < inv.End {
					return 0, ErrOverlap
				}
				inv.Start = move(mark{pos: inv.Start, gravity: Left}, e)
				inv.End = move(mark{pos: inv.End, gravity: Right}, e)
			}
		}
	}
	if err := b.Apply(Transaction{Base: b.version, Origin: o, Label: label, Time: at, Edits: []Edit{inv}}); err != nil {
		return 0, err
	}
	if b.reverted == nil {
		b.reverted = map[NodeID]bool{}
	}
	b.reverted[target.ID], b.reverted[b.head] = true, true
	return inv.Start, nil
}

// diff returns one edit that turns old into new, keeping the text they start
// and end with.
func diff(old, new string) Edit {
	pre, suf := 0, 0
	for pre < len(old) && pre < len(new) && old[pre] == new[pre] {
		pre++
	}
	for suf < len(old)-pre && suf < len(new)-pre && old[len(old)-1-suf] == new[len(new)-1-suf] {
		suf++
	}
	return Edit{Start: pre, End: len(old) - suf, Text: new[pre : len(new)-suf]}
}
