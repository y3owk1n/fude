// Package kernel holds buffers: text, marks, transactions and the timeline.
// Write a Buffer from one goroutine. Snapshots are safe on any goroutine.
package kernel

import (
	"errors"
	"fmt"
	"slices"
	"time"

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
	Time   time.Time // the host's clock, since the kernel reads none
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
	Time    time.Time // when its first transaction was made
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
	redo    map[NodeID]NodeID // the child each node was last undone from
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
	c := b.edit(t.Edits)
	b.version++

	if b.group == nil {
		b.group = &Node{ID: b.next(t.Origin), Parent: b.head, Label: t.Label, Time: t.Time}
	}
	b.group.Changes = append(b.group.Changes, c)
	if b.depth == 0 {
		b.commit()
	}
	return nil
}

// Undo reverts the head node and moves the head to its parent. It returns
// where the reverted text starts, or false at the root.
func (b *Buffer) Undo() (int, bool) {
	i, ok := b.index(b.head)
	if !ok {
		return 0, false
	}
	n := b.nodes[i]
	start := -1
	for j := len(n.Changes) - 1; j >= 0; j-- {
		start = b.replay(inverse(n.Changes[j]), start)
	}
	if b.redo == nil {
		b.redo = map[NodeID]NodeID{}
	}
	b.redo[n.Parent] = n.ID
	b.head = n.Parent
	b.version++
	return start, true
}

// Redo reapplies the child of the head that was last undone, or the newest
// child. It returns where the reapplied text starts, or false at a leaf.
func (b *Buffer) Redo() (int, bool) {
	id, ok := b.redo[b.head]
	if !ok {
		for _, n := range b.nodes {
			if n.Parent == b.head {
				id, ok = n.ID, true
			}
		}
	}
	i, found := b.index(id)
	if !ok || !found {
		return 0, false
	}
	start := -1
	for _, c := range b.nodes[i].Changes {
		start = b.replay(c.Edits, start)
	}
	b.head = id
	b.version++
	return start, true
}

// At returns the text at node id without moving to it, or false if there
// is no such node. The zero NodeID is the text before any edit.
func (b *Buffer) At(id NodeID) (Snapshot, bool) {
	up, down, ok := b.path(id)
	if !ok {
		return Snapshot{}, false
	}
	text := b.text
	for _, n := range up {
		for j := len(n.Changes) - 1; j >= 0; j-- {
			text = replace(text, inverse(n.Changes[j]))
		}
	}
	for _, n := range down {
		for _, c := range n.Changes {
			text = replace(text, c.Edits)
		}
	}
	return Snapshot{text, b.version}, true
}

// Goto moves the text to node id along the timeline, on any branch, without
// adding a node. It returns where the last change it made starts, or false
// if there is no such node.
func (b *Buffer) Goto(id NodeID) (int, bool) {
	up, down, ok := b.path(id)
	if !ok {
		return 0, false
	}
	start := 0
	for range up {
		start, _ = b.Undo()
	}
	for _, n := range down {
		start = -1
		for _, c := range n.Changes {
			start = b.replay(c.Edits, start)
		}
		b.head = n.ID
		b.version++
	}
	return start, true
}

// path returns the nodes to undo from the head, newest first, and the nodes
// to then redo, oldest first, to reach id.
func (b *Buffer) path(id NodeID) (up, down []Node, ok bool) {
	chain := func(id NodeID) ([]Node, bool) {
		var out []Node
		for id != (NodeID{}) {
			i, found := b.index(id)
			if !found {
				return nil, false
			}
			out = append(out, b.nodes[i])
			id = b.nodes[i].Parent
		}
		return out, true
	}
	from, ok1 := chain(b.head)
	to, ok2 := chain(id)
	if !ok1 || !ok2 {
		return nil, nil, false
	}
	// Drop the ancestors both share, which end both chains.
	for len(from) > 0 && len(to) > 0 && from[len(from)-1].ID == to[len(to)-1].ID {
		from, to = from[:len(from)-1], to[:len(to)-1]
	}
	slices.Reverse(to)
	return from, to, true
}

// replace applies edits to text without touching marks.
func replace(text rope.Rope, edits []Edit) rope.Rope {
	for i := len(edits) - 1; i >= 0; i-- {
		text = text.Replace(edits[i].Start, edits[i].End, edits[i].Text)
	}
	return text
}

// replay applies edits and returns the lowest position changed so far. start
// is the lowest position from earlier edits, or -1, and replay maps it
// through these edits first.
func (b *Buffer) replay(edits []Edit, start int) int {
	if start >= 0 {
		m := mark{start, Left}
		for i := len(edits) - 1; i >= 0; i-- {
			m.pos = move(m, edits[i])
		}
		start = m.pos
	}
	b.edit(edits)
	if len(edits) > 0 && (start < 0 || edits[0].Start < start) {
		start = edits[0].Start
	}
	return start
}

// edit applies checked edits to the text and marks.
func (b *Buffer) edit(edits []Edit) Change {
	c := Change{Edits: edits, Removed: make([]string, len(edits))}
	text := b.text
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		c.Removed[i] = b.text.Slice(e.Start, e.End)
		text = text.Replace(e.Start, e.End, e.Text)
		for m := range b.marks {
			b.marks[m].pos = move(b.marks[m], e)
		}
	}
	b.text = text
	return c
}

// inverse returns the edits that undo c, in offsets of the text after it.
func inverse(c Change) []Edit {
	inv := make([]Edit, len(c.Edits))
	shift := 0
	for i, e := range c.Edits {
		start := e.Start + shift
		inv[i] = Edit{Start: start, End: start + len(e.Text), Text: c.Removed[i]}
		shift += len(e.Text) - (e.End - e.Start)
	}
	return inv
}

func (b *Buffer) index(id NodeID) (int, bool) {
	for i := len(b.nodes) - 1; i >= 0; i-- {
		if b.nodes[i].ID == id {
			return i, true
		}
	}
	return 0, false
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

// MoveMark puts a mark at pos, clamped to the text.
func (b *Buffer) MoveMark(id MarkID, pos int) {
	b.marks[id].pos = min(max(pos, 0), b.text.Len())
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
