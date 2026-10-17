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
	lists, ok := b.Since(from)
	if !ok {
		return 0, false
	}
	for _, edits := range lists {
		for j := len(edits) - 1; j >= 0; j-- {
			e := edits[j]
			if e.Start < pos && pos < e.End {
				return 0, false
			}
			pos = move(mark{pos: pos, gravity: Right}, e)
		}
	}
	return pos, true
}

// Since returns the edits made after version from, one list per change,
// oldest first, or false when from is older than the edits it remembers.
// Each list applies to the text the one before it left (B115).
func (b *Buffer) Since(from Version) ([][]Edit, bool) {
	i := slices.IndexFunc(b.log, func(l logged) bool { return l.at >= from })
	switch {
	case from > b.version:
		return nil, false
	case i < 0:
		return nil, from == b.version
	case b.log[i].at > from:
		// The edits right after from are no longer in the log.
		return nil, false
	}
	out := make([][]Edit, 0, len(b.log)-i)
	for _, l := range b.log[i:] {
		out = append(out, slices.Clone(l.edits))
	}
	return out, true
}

// UndoOrigin reverts the newest change of origin o still in the text, and
// returns where the reverted text starts. When that change is the head node,
// this is Undo. Otherwise its inverse, moved through the changes made after
// it, applies as a new node of o's, labelled label at time at, and it fails
// with ErrOverlap when one of those changes overlaps it.
func (b *Buffer) UndoOrigin(o Origin, label string, at time.Time) (int, error) {
	b.close()
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
				// Text another change inserted at its edges stays.
				inv.Start = move(mark{pos: inv.Start, gravity: Right}, e)
				inv.End = max(move(mark{pos: inv.End, gravity: Left}, e), inv.Start)
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
