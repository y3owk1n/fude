// Package rope is a persistent, balanced rope of bytes.
package rope

import "strings"

// maxLeaf is the most bytes a leaf holds.
const maxLeaf = 1024

// Rope is immutable. Edits return a new rope sharing untouched nodes.
type Rope struct{ root *node }

// node is a leaf when it has no children. Internal nodes have both.
type node struct {
	left, right *node
	text        string
	length      int
	lines       int // newlines
	height      int
}

// New returns a rope holding s.
func New(s string) Rope { return Rope{build(s)} }

// Len returns the length in bytes.
func (r Rope) Len() int { return length(r.root) }

// Slice returns the bytes in [start, end).
func (r Rope) Slice(start, end int) string {
	if start < 0 || end > r.Len() || start > end {
		panic("rope: slice out of range")
	}
	var b strings.Builder
	b.Grow(end - start)
	collect(r.root, start, end, &b)
	return b.String()
}

// Lines returns the number of lines, one more than the newlines.
func (r Rope) Lines() int { return lines(r.root) + 1 }

// LineStart returns where line n starts, counting from 0.
func (r Rope) LineStart(n int) int {
	if n < 0 || n >= r.Lines() {
		panic("rope: line out of range")
	}
	pos := 0
	for nd := r.root; n > 0; {
		if nd.left == nil {
			i := 0
			for range n {
				i += strings.IndexByte(nd.text[i:], '\n') + 1
			}
			return pos + i
		}
		if n <= nd.left.lines {
			nd = nd.left
		} else {
			n, pos, nd = n-nd.left.lines, pos+nd.left.length, nd.right
		}
	}
	return pos
}

// LineOf returns the line pos is on, counting from 0.
func (r Rope) LineOf(pos int) int {
	if pos < 0 || pos > r.Len() {
		panic("rope: position out of range")
	}
	line := 0
	for nd := r.root; nd != nil; {
		if nd.left == nil {
			return line + strings.Count(nd.text[:pos], "\n")
		}
		if pos <= nd.left.length {
			nd = nd.left
		} else {
			line, pos, nd = line+nd.left.lines, pos-nd.left.length, nd.right
		}
	}
	return line
}

// Replace returns a rope with [start, end) replaced by text.
func (r Rope) Replace(start, end int, text string) Rope {
	if start < 0 || end > r.Len() || start > end {
		panic("rope: replace out of range")
	}
	left, rest := split(r.root, start)
	_, right := split(rest, end-start)
	return Rope{join(join(left, build(text)), right)}
}

func build(s string) *node {
	if len(s) == 0 {
		return nil
	}
	if len(s) <= maxLeaf {
		return &node{text: s, length: len(s), lines: strings.Count(s, "\n"), height: 1}
	}
	mid := len(s) / 2
	return branch(build(s[:mid]), build(s[mid:]))
}

func collect(n *node, start, end int, b *strings.Builder) {
	if n == nil || start >= end {
		return
	}
	if n.left == nil {
		b.WriteString(n.text[start:end])
		return
	}
	l := n.left.length
	if start < l {
		collect(n.left, start, min(end, l), b)
	}
	if end > l {
		collect(n.right, max(start-l, 0), end-l, b)
	}
}

// split returns the first i bytes of n and the rest.
func split(n *node, i int) (*node, *node) {
	switch {
	case n == nil:
		return nil, nil
	case i <= 0:
		return nil, n
	case i >= n.length:
		return n, nil
	case n.left == nil:
		return build(n.text[:i]), build(n.text[i:])
	case i < n.left.length:
		l, r := split(n.left, i)
		return l, join(r, n.right)
	default:
		l, r := split(n.right, i-n.left.length)
		return join(n.left, l), r
	}
}

// join concatenates a and b, keeping AVL balance.
func join(a, b *node) *node {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case a.left == nil && b.left == nil && a.length+b.length <= maxLeaf:
		return build(a.text + b.text)
	case a.height > b.height+1:
		return balance(branch(a.left, join(a.right, b)))
	case b.height > a.height+1:
		return balance(branch(join(a, b.left), b.right))
	default:
		return branch(a, b)
	}
}

func balance(n *node) *node {
	switch d := height(n.left) - height(n.right); {
	case d > 1:
		l := n.left
		if height(l.left) < height(l.right) {
			l = rotateLeft(l)
		}
		return rotateRight(branch(l, n.right))
	case d < -1:
		r := n.right
		if height(r.right) < height(r.left) {
			r = rotateRight(r)
		}
		return rotateLeft(branch(n.left, r))
	default:
		return n
	}
}

func rotateRight(n *node) *node {
	return branch(n.left.left, branch(n.left.right, n.right))
}

func rotateLeft(n *node) *node {
	return branch(branch(n.left, n.right.left), n.right.right)
}

func branch(l, r *node) *node {
	return &node{left: l, right: r, length: l.length + r.length, lines: l.lines + r.lines, height: max(l.height, r.height) + 1}
}

func length(n *node) int {
	if n == nil {
		return 0
	}
	return n.length
}

func lines(n *node) int {
	if n == nil {
		return 0
	}
	return n.lines
}

func height(n *node) int {
	if n == nil {
		return 0
	}
	return n.height
}
