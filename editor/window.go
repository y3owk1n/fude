package editor

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
)

// window shows one buffer. Its cursor and top line are marks in the buffer,
// so edits made in another window move them.
type window struct {
	buf    *buffer
	alt    *buffer // the buffer shown before, for <C-^> and #
	cursor kernel.MarkID
	top    kernel.MarkID
	jumps  []place
	jumpAt int // the jump <C-o> last moved to, or len(jumps)
	opts   options
	left   int // the first screen column shown, without wrap

	x, y, w, h int // where the layout puts it, its status line included
}

// newWindow shows b with the cursor at pos and top as the first line.
func newWindow(b *buffer, pos, top int) *window {
	return &window{buf: b, cursor: b.text.SetMark(pos, kernel.Right), top: b.text.SetMark(top, kernel.Left)}
}

func (w *window) pos() int { return w.buf.text.Mark(w.cursor) }

// gutter is how many columns the window's sign column and line numbers
// take on the left.
func (w *window) gutter() int { return w.signCols() + w.numberCols() + w.iconCols() }

// iconCols is 2 for a listing's icons with the icons option, at the
// gutter's right, and 0 otherwise (B128).
func (w *window) iconCols() int {
	if w.opts.Icons && w.buf.listed != "" {
		return 2
	}
	return 0
}

// signCols is 2 when the buffer has signs, or always with sign_column, and
// 0 otherwise (B119, B143).
func (w *window) signCols() int {
	if w.opts.SignColumn || w.buf.signed || w.buf.signs > 0 || len(w.buf.git) > 0 || w.buf.listed != "" {
		return 2
	}
	return 0
}

// numberCols is how many columns the line numbers take, 0 without them: at
// least 4, else the last line's digits and a space.
func (w *window) numberCols() int {
	if !w.opts.Number && !w.opts.RelativeNumber || w.buf.listed != "" {
		// A listing shows names only.
		return 0
	}
	return max(4, len(strconv.Itoa(w.buf.text.Snapshot().Lines()))+1)
}

// cols is how many columns of text the window shows on a row.
func (w *window) cols() int { return max(w.w-w.gutter(), 1) }

// rows is how many rows the line at start takes in the window.
func (w *window) rows(t kernel.Snapshot, start int) int {
	if !w.opts.Wrap {
		return 1
	}
	return len(vim.Rows(t, start, w.cols()))
}

// show switches the window to b, at the place b was last left.
func (w *window) show(b *buffer) {
	w.buf.last, w.buf.lastTop = w.pos(), w.buf.text.Mark(w.top)
	if w.buf != b {
		w.alt = w.buf
	}
	w.buf = b
	w.cursor = b.text.SetMark(b.last, kernel.Right)
	w.top = b.text.SetMark(b.lastTop, kernel.Left)
}

// node is one part of the layout: a window, or splits that share its space
// equally, side by side when across is set and stacked otherwise.
type node struct {
	win    *window
	across bool
	kids   []*node
	parent *node
}

// place gives n the rectangle at x, y of width w and height h. Side by side
// windows have a column between them for a separator, and any space left
// over goes to the first ones.
func (n *node) place(x, y, w, h int) {
	if n.win != nil {
		n.win.x, n.win.y, n.win.w, n.win.h = x, y, w, h
		return
	}
	k := len(n.kids)
	room := h
	if n.across {
		room = w - (k - 1)
	}
	for i, kid := range n.kids {
		size := room / k
		if i < room%k {
			size++
		}
		if n.across {
			kid.place(x, y, size, h)
			x += size + 1
		} else {
			kid.place(x, y, w, size)
			y += size
		}
	}
}

// windows returns the layout's windows from the top left.
func (n *node) windows() []*window {
	if n.win != nil {
		return []*window{n.win}
	}
	var out []*window
	for _, k := range n.kids {
		out = append(out, k.windows()...)
	}
	return out
}

// leaf returns the node holding w.
func (n *node) leaf(w *window) *node {
	if n.win == w {
		return n
	}
	for _, k := range n.kids {
		if l := k.leaf(w); l != nil {
			return l
		}
	}
	return nil
}

// split opens a new window on the shown buffer below the current one, or
// right of it when across is set, and moves to it (N16).
func (e *Session) split(across bool) {
	old := e.win
	nw := newWindow(old.buf, old.pos(), old.buf.text.Mark(old.top))
	nw.opts = old.opts
	nw.alt = old.alt
	l := e.root.leaf(old)
	add := &node{win: nw}
	if p := l.parent; p != nil && p.across == across {
		i := index(p.kids, l)
		add.parent = p
		p.kids = slices.Insert(p.kids, i+1, add)
	} else {
		// l becomes a split holding the new window and the old one.
		keep := &node{win: old, parent: l}
		add.parent = l
		l.win, l.across, l.kids = nil, across, []*node{keep, add}
	}
	e.win = nw
	e.layout()
}

// closeWindow closes w. The window before it in the split, or else the one
// after, takes its place and becomes current.
func (e *Session) closeWindow(w *window) error {
	l := e.root.leaf(w)
	p := l.parent
	if p == nil {
		return errors.New("cannot close the last window")
	}
	w.buf.last, w.buf.lastTop = w.pos(), w.buf.text.Mark(w.top)
	i := index(p.kids, l)
	p.kids = append(p.kids[:i], p.kids[i+1:]...)
	next := p.kids[max(i-1, 0)]
	if len(p.kids) == 1 {
		// A split left with one window becomes that window.
		only := p.kids[0]
		*p = node{win: only.win, across: only.across, kids: only.kids, parent: p.parent}
		for _, k := range p.kids {
			k.parent = p
		}
		next = p
	}
	if e.win == w {
		e.win = next.windows()[0]
	}
	e.layout()
	return nil
}

// only closes every window but the current one.
func (e *Session) only() {
	e.root = &node{win: e.win}
	e.layout()
}

// moveTo moves to the window next to the current one in direction d, one of
// h, j, k and l, on the cursor's row or column.
// navigate moves to the window in direction d, h, j, k or l, or inside
// tmux to the pane there when there is no window (B140).
func (e *Session) navigate(d byte) {
	w := e.win
	if e.moveTo(d); e.win != w || os.Getenv("TMUX") == "" {
		return
	}
	flag := map[byte]string{'h': "-L", 'j': "-D", 'k': "-U", 'l': "-R"}[d]
	at := time.Now()
	out, err := exec.Command("tmux", "select-pane", "-t", os.Getenv("TMUX_PANE"), flag).CombinedOutput()
	traced("file", "moved to the tmux pane "+flag, at, err)
	if err != nil {
		e.message = fmt.Sprintf("tmux select-pane %s: %v: %s", flag, err, strings.TrimSpace(string(out)))
	}
}

func (e *Session) moveTo(d byte) {
	w := e.win
	t := w.buf.text.Snapshot()
	row, col := w.y, w.x
	for p := e.Top(); p < vim.LineStart(t, w.pos()) && row < w.y+w.h-2; p = vim.LineEnd(t, p) + 1 {
		row++
	}
	col += min(w.pos()-vim.LineStart(t, w.pos()), w.w-1)
	switch d {
	case 'h':
		col = w.x - 2
	case 'l':
		col = w.x + w.w + 1
	case 'k':
		row = w.y - 1
	case 'j':
		row = w.y + w.h
	}
	for _, o := range e.root.windows() {
		if col >= o.x && col < o.x+o.w && row >= o.y && row < o.y+o.h {
			e.win = o
			return
		}
	}
}

// layout places every window in the space the editor has.
func (e *Session) layout() {
	e.root.place(0, 0, max(e.width, 1), max(e.height, 2))
}

func index[T comparable](s []T, x T) int {
	for i, y := range s {
		if y == x {
			return i
		}
	}
	return -1
}
