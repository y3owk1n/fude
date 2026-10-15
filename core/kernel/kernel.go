// Package kernel holds buffers: text, marks, transactions and the timeline.
// Write a Buffer from one goroutine. Snapshots are safe on any goroutine.
package kernel

import (
	"errors"
	"fmt"
	"slices"

	"github.com/y3owk1n/fude/core/rope"
)

// Apply returns ErrStale when a transaction's base is not the current version.
var ErrStale = errors.New("kernel: stale base version")

// Version counts the transactions applied to a buffer in this session.
type Version uint64

// Origin is the client an edit came from.
type Origin string

// NodeID is a node's origin and that origin's sequence number. The zero
// NodeID is the root.
type NodeID struct {
	Origin Origin
	Seq    uint64
}

// Edit replaces [Start, End) with Text. Offsets are in the text before the
// transaction.
type Edit struct {
	Start, End int
	Text       string
}

// Transaction is an atomic set of sorted, non-overlapping edits.
type Transaction struct {
	Base   Version
	Origin Origin
	Label  string
	Edits  []Edit
}

// Change is an applied transaction's edits and the text each replaced.
type Change struct {
	Edits   []Edit
	Removed []string
}

// Node is one timeline step. It holds one change, or several from a group.
// Treat it as read-only.
type Node struct {
	ID      NodeID
	Parent  NodeID
	Label   string
	Changes []Change
}

// Store receives every node as it is created.
type Store interface {
	Append(Node)
}

// Gravity decides whether a mark stays before or moves after text inserted at
// its position.
type Gravity int

const (
	Left  Gravity = iota // stays before text inserted at its position
	Right                // moves after text inserted at its position
)

// MarkID identifies a mark in its buffer.
type MarkID int

type mark struct {
	pos     int
	gravity Gravity
}

// Snapshot is a buffer's text at one version. It never changes.
type Snapshot struct {
	text    rope.Rope
	version Version
}

// Version returns the version the snapshot was taken at.
func (s Snapshot) Version() Version { return s.version }

// Len returns the length of the text in bytes.
func (s Snapshot) Len() int { return s.text.Len() }

// Slice returns the bytes in [start, end).
func (s Snapshot) Slice(start, end int) string { return s.text.Slice(start, end) }

// Buffer is text with marks and a timeline.
type Buffer struct {
	text    rope.Rope
	version Version
	store   Store
	nodes   []Node
	head    NodeID
	seq     map[Origin]uint64
	marks   []mark
	depth   int
	group   *Node
}

// New returns a buffer holding text. store may be nil.
func New(text string, store Store) *Buffer {
	return &Buffer{text: rope.New(text), store: store, seq: map[Origin]uint64{}}
}

// Load returns a buffer whose text is the text at node head. nodes must be in
// the order a Store received them. store may be nil.
func Load(text string, head NodeID, nodes []Node, store Store) (*Buffer, error) {
	b := New(text, store)
	seen := map[NodeID]bool{{}: true}
	for _, n := range nodes {
		if !seen[n.Parent] {
			return nil, fmt.Errorf("kernel: load node %v: parent %v not loaded before it", n.ID, n.Parent)
		}
		seen[n.ID] = true
		b.seq[n.ID.Origin] = max(b.seq[n.ID.Origin], n.ID.Seq)
	}
	if !seen[head] {
		return nil, fmt.Errorf("kernel: load: head %v is not a loaded node", head)
	}
	b.nodes = slices.Clone(nodes)
	b.head = head
	return b, nil
}

// Snapshot returns the current text.
func (b *Buffer) Snapshot() Snapshot { return Snapshot{b.text, b.version} }

// Head returns the timeline node the text is at.
func (b *Buffer) Head() NodeID { return b.head }

// Nodes returns the timeline in creation order.
func (b *Buffer) Nodes() []Node { return slices.Clone(b.nodes) }

// Apply applies t at its base version, or returns ErrStale. Outside a group
// it makes one node.
func (b *Buffer) Apply(t Transaction) error {
	if t.Base != b.version {
		return fmt.Errorf("kernel: apply %q at version %d, buffer is at %d: %w", t.Label, t.Base, b.version, ErrStale)
	}
	if err := b.check(t.Edits); err != nil {
		return fmt.Errorf("kernel: apply %q: %w", t.Label, err)
	}
	if len(t.Edits) == 0 {
		return nil
	}
	c := Change{Edits: t.Edits, Removed: make([]string, len(t.Edits))}
	text := b.text
	for i := len(t.Edits) - 1; i >= 0; i-- {
		e := t.Edits[i]
		c.Removed[i] = b.text.Slice(e.Start, e.End)
		text = text.Replace(e.Start, e.End, e.Text)
		for m := range b.marks {
			b.marks[m].pos = move(b.marks[m], e)
		}
	}
	b.text = text
	b.version++

	if b.group == nil {
		b.group = &Node{ID: b.next(t.Origin), Parent: b.head, Label: t.Label}
	}
	b.group.Changes = append(b.group.Changes, c)
	if b.depth == 0 {
		b.commit()
	}
	return nil
}

// BeginGroup starts a group that becomes one node. Groups nest.
func (b *Buffer) BeginGroup() { b.depth++ }

// EndGroup ends a group and makes its node if anything was applied.
func (b *Buffer) EndGroup() {
	if b.depth == 0 {
		return
	}
	b.depth--
	if b.depth == 0 && b.group != nil {
		b.commit()
	}
}

// SetMark places a mark at pos, clamped to the text.
func (b *Buffer) SetMark(pos int, g Gravity) MarkID {
	b.marks = append(b.marks, mark{min(max(pos, 0), b.text.Len()), g})
	return MarkID(len(b.marks) - 1)
}

// Mark returns a mark's current position.
func (b *Buffer) Mark(id MarkID) int { return b.marks[id].pos }

func (b *Buffer) check(edits []Edit) error {
	end := 0
	for _, e := range edits {
		if e.Start < end || e.End < e.Start || e.End > b.text.Len() {
			return fmt.Errorf("edit [%d, %d) is out of order or out of range [%d, %d]", e.Start, e.End, end, b.text.Len())
		}
		end = e.End
	}
	return nil
}

func (b *Buffer) next(o Origin) NodeID {
	b.seq[o]++
	return NodeID{o, b.seq[o]}
}

func (b *Buffer) commit() {
	n := *b.group
	b.group = nil
	b.nodes = append(b.nodes, n)
	b.head = n.ID
	if b.store != nil {
		b.store.Append(n)
	}
}

// move returns m's position after e. Marks on a replaced range's edge stay
// outside it, and marks inside go to their gravity's side.
func move(m mark, e Edit) int {
	switch {
	case m.pos < e.Start:
		return m.pos
	case m.pos > e.End:
		return m.pos + len(e.Text) - (e.End - e.Start)
	case m.pos == e.Start && e.End > e.Start:
		return e.Start
	case m.pos == e.End && e.End > e.Start:
		return e.Start + len(e.Text)
	case m.gravity == Left:
		return e.Start
	default:
		return e.Start + len(e.Text)
	}
}
