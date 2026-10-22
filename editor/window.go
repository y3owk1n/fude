package editor

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync/atomic"

	"github.com/y3owk1n/fude/core/kernel"
	"github.com/y3owk1n/fude/vim"
	"github.com/y3owk1n/fude/wire"
)

// window shows one buffer. Its cursor and top line are marks in the buffer,
// so edits made in another window move them.
type window struct {
	id     int // what a module names it by (B340)
	buf    *buffer
	alt    *buffer // the buffer shown before, for <C-^> and #
	cursor kernel.MarkID
	top    kernel.MarkID
	jumps  []place
	jumpAt int // the jump <C-o> last moved to, or len(jumps)
	opts   options
	left   int      // the first screen column shown, without wrap
	saved  *options // its options while the start screen shows (B225)
	own    *options // its options under those its buffer's filetype sets (B94)
	fitted *buffer  // the buffer its filetype's options were last fitted for
	// pins are the starts of the lines a module pinned over its top rows
	// while it showed buf (B217).
	pins struct {
		buf    *buffer
		starts []int
	}
	// guides are, by line, the columns where a module put indent guides
	// while the window showed buf (B153).
	guides struct {
		buf   *buffer
		lines map[int][]int
	}

	x, y, w, h int // where the layout puts it, its status line included

	// anchor is where a float sits over the layout, or "" for a window in
	// it, and from is the window a float or the finder opened from, which
	// takes the focus back as it closes (D55).
	anchor string
	from   *window
	// image is the image a float at the cursor shows under its text, and
	// imageRows and imageCols how big it shows (B309).
	image                string
	imageRows, imageCols int
	was                  slot // where a hidden window was in the layout (B364)
	locked               bool // its module keeps it where it opened (B367)
	// bound windows scroll together, and wrapped is the wrap a bound one
	// had before. To stay level, it hides fill of the rows drawn above its
	// top line while that line starts at fillAt (B523).
	bound, wrapped bool
	fill, fillAt   int
}

// slot is where a window was in the layout: beside the window next to it,
// after it or before, and the size it asked for there (B364).
type slot struct {
	beside        *window
	across, after bool
	size          int
}

// windowIDs is the id the last window got.
var windowIDs atomic.Int64

// newWindow shows b with the cursor at pos and top as the first line.
func newWindow(b *buffer, pos, top int) *window {
	return &window{id: int(windowIDs.Add(1)), buf: b, cursor: b.text.SetMark(pos, kernel.Right), top: b.text.SetMark(top, kernel.Left)}
}

func (w *window) pos() int { return w.buf.text.Mark(w.cursor) }

// gutter is how many columns the window's sign column and line numbers
// take on the left.
func (w *window) gutter() int { return w.signCols() + w.numberCols() + w.iconCols() }

// iconCols is 2 for an explorer's icons with the icons option, at the
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
	if w.opts.SignColumn || w.buf.signs > 0 || w.buf.listed != "" {
		return 2
	}
	return 0
}

// numberCols is how many columns the line numbers take, 0 without them: at
// least 4, else the last line's digits and a space.
func (w *window) numberCols() int {
	if !w.opts.Number && !w.opts.RelativeNumber || w.buf.listed != "" {
		// An explorer shows names only.
		return 0
	}
	return max(4, len(strconv.Itoa(w.buf.text.Snapshot().Lines()))+1)
}

// cols is how many columns of text the window shows on a row.
func (w *window) cols() int { return max(w.w-w.gutter(), 1) }

// prevLine returns the start of the line shown before the one at start,
// over a closed fold, or 0.
func (w *window) prevLine(t kernel.Snapshot, start int) int {
	line := t.LineOf(start)
	if line == 0 {
		return 0
	}
	first, _ := w.fold(t, line-1)
	return t.LineStart(first)
}

// nextLine returns the start of the line shown after the one at start, over
// a closed fold, or false after the last.
func (w *window) nextLine(t kernel.Snapshot, start int) (int, bool) {
	_, last := w.fold(t, t.LineOf(start))
	if last+1 >= t.Lines() {
		return start, false
	}
	return t.LineStart(last + 1), true
}

// rows is how many rows the line at start takes in the window, with tabs
// tab columns wide and what d draws there that the text does not hold. A
// line taller than the window counts as its height.
func (w *window) rows(t kernel.Snapshot, start, tab int, d drawn) int {
	return len(w.starts(t, start, tab, d)) + d.rows(t.LineOf(start))
}

// starts returns where each row of the line at start begins, up to the
// window's height. A line takes one row without wrap, and a float's rows,
// or with line_break any window's, end after a space (B364).
func (w *window) starts(t kernel.Snapshot, start, tab int, d drawn) []int {
	n := max(w.h, 1)
	switch {
	case !w.opts.Wrap:
		return []int{start}
	case w.anchor != "" || w.opts.LineBreak:
		return vim.WordRows(t, start, n, w.cols(), w.hang(t, start, tab, d.show), tab, d.inlines)
	}
	return vim.Rows(t, start, n, w.cols(), w.hang(t, start, tab, d.show), tab, d.inlines)
}

// hang returns the columns the rows after the first of the line at start
// begin with: its indent with break_indent, then show, show_break's text.
// A float's start in none (B489).
func (w *window) hang(t kernel.Snapshot, start, tab int, show string) int {
	if w.anchor != "" {
		return 0
	}
	return vim.Hang(t, start, w.cols(), tab, w.opts.BreakIndent, show)
}

// clip is how long a line is, in bytes, before a window without wrap
// shows only the part of it in view.
const clip = 4096

// whole is how many bytes a window's lines hold at most for it to show them
// whole, though they may wrap to more rows than it has.
const whole = 64 << 10

// view returns the text w shows from top, as ranges in order, and what
// draws there that the text does not hold. The lines shown make one range,
// which ends at the end of the last or, past whole bytes, where the first
// row with no room starts. Without wrap a line longer than clip is a range
// of its own, the part of it in view. A closed fold counts as one line
// (B224).
func (e *Session) view(w *window, t kernel.Snapshot, top, tab int) ([]wire.Range, drawn) {
	room := max(w.h-1, 1)
	// The lines a row each would show.
	last := t.LineStart(min(t.LineOf(top)+room-1, t.Lines()-1))
	if len(w.buf.folds) > 0 {
		last = top
		for range room - 1 {
			next, ok := w.nextLine(t, last)
			if !ok {
				break
			}
			last = next
		}
	}
	d := e.shown(w, top, vim.LineEnd(t, last))
	// A session given no size yet shows its windows' lines whole.
	if end := vim.LineEnd(t, last); e.width == 0 || end-top <= whole {
		return []wire.Range{{Start: top, End: end}}, d
	}
	var out []wire.Range
	open := false // the last range takes the next line
	for p := top; ; {
		starts, end := w.starts(t, p, tab, d), vim.LineEnd(t, p)
		room -= d.rows(t.LineOf(p))
		cut := room > 0 && len(starts) > room
		if cut {
			end = starts[room]
		}
		switch {
		case !w.opts.Wrap && end-p > clip:
			var r wire.Range
			if len(d.inlines) == 0 {
				r.Start, r.Col = w.buf.columnStart(t, p, w.left, tab)
				r.End, _ = w.buf.columnStart(t, p, w.left+w.cols(), tab)
			} else {
				r.Start, r.End = vim.ColumnStart(t, p, w.left, tab, d.inlines), vim.ColumnStart(t, p, w.left+w.cols(), tab, d.inlines)
				r.Col = vim.Column(t, p, r.Start, tab, nil)
			}
			out, open = append(out, r), false
		case open:
			out[len(out)-1].End = end
		default:
			out, open = append(out, wire.Range{Start: p, End: end}), true
		}
		room -= len(starts)
		next, ok := w.nextLine(t, p)
		if cut || !ok || room <= 0 {
			return out, d
		}
		p = next
	}
}

// screenAt returns the row of its line that w's cursor shows on, and the
// column of the window's text.
func (w *window) screenAt(t kernel.Snapshot, tab int, d drawn) (row, col int) {
	c := w.pos()
	ls := vim.LineStart(t, c)
	// column returns the column p begins at on the cursor's line.
	column := func(p int) int {
		if len(d.inlines) == 0 {
			return w.buf.column(t, ls, p, tab)
		}
		return vim.Column(t, ls, p, tab, d.inlines)
	}
	if !w.opts.Wrap {
		return 0, column(c) - w.left
	}
	starts := w.starts(t, ls, tab, d)
	r := len(starts) - 1
	for r > 0 && starts[r] > c {
		r--
	}
	// A cursor on the window's last row or past it is out of view.
	if r == w.h-1 {
		return r, 0
	}
	col = column(c) - column(starts[r])
	if r > 0 {
		col += w.hang(t, ls, tab, d.show)
	}
	return r, col
}

// show switches the window to b, at the place b was last left. Its marks
// in the buffer it showed go.
func (w *window) show(b *buffer) {
	w.buf.setMark('"', w.pos())
	w.buf.lastTop = w.buf.text.Mark(w.top)
	w.buf.text.DeleteMark(w.cursor)
	w.buf.text.DeleteMark(w.top)
	if w.buf != b {
		w.alt = w.buf
	}
	w.buf = b
	if w.saved != nil && !b.screen {
		w.opts, w.saved = *w.saved, nil
	}
	last, _ := b.markAt('"')
	w.cursor = b.text.SetMark(last, kernel.Right)
	w.top = b.text.SetMark(b.lastTop, kernel.Left)
}

// drop lets go of the marks w keeps, as it closes: its cursor, its top line
// and its jumps.
func (w *window) drop() {
	w.buf.text.DeleteMark(w.cursor)
	w.buf.text.DeleteMark(w.top)
	for _, j := range w.jumps {
		j.buf.text.DeleteMark(j.mark)
	}
	w.jumps = nil
}

// ownOptions returns w's options without those its buffer's filetype sets.
func (w *window) ownOptions() options {
	if w.own != nil {
		return *w.own
	}
	return w.opts
}

// keep copies into w's own options those :set changed from was, so they
// stay when w shows another buffer.
func (w *window) keep(was options) {
	if w.own == nil {
		return
	}
	wasOn, on, ownOn := was.switches(), w.opts.switches(), w.own.switches()
	for name, p := range on {
		if *p != *wasOn[name] {
			*ownOn[name] = *p
		}
	}
	wasN, n, ownN := was.numbers(), w.opts.numbers(), w.own.numbers()
	for name, p := range n {
		if *p != *wasN[name] {
			*ownN[name] = *p
		}
	}
	for name, v := range w.opts.Modules {
		if was.Modules[name] != v {
			w.own.Modules = with(w.own.Modules, name, v)
		}
	}
}

// fit puts back w's own options, then applies over them the options w's
// buffer's filetype sets, once each time w shows another buffer (B94).
func (e *Session) fit(w *window) {
	if w.fitted == w.buf || w.saved != nil {
		return
	}
	if w.own != nil {
		w.opts, w.own = *w.own, nil
	}
	w.fitted = w.buf
	ft := e.cfg.Filetype[e.filetype(w.buf)]
	set, isSet := w.buf.set["expand_tab"]
	if ft.ExpandTab == nil && ft.Wrap == nil && ft.ColorColumn == nil && ft.Spell == nil && !isSet {
		return
	}
	own := w.opts
	w.own = &own
	if ft.ExpandTab != nil {
		w.opts.ExpandTab = *ft.ExpandTab
	}
	if ft.Wrap != nil {
		w.opts.Wrap = *ft.Wrap
	}
	if ft.Spell != nil {
		w.opts.Modules = with(w.opts.Modules, "spell.enabled", strconv.FormatBool(*ft.Spell))
	}
	if ft.ColorColumn != nil {
		w.opts.ColorColumn = *ft.ColorColumn
	}
	if isSet {
		w.opts.ExpandTab = set == "true"
	}
}

// node is one part of the layout: a window, or splits side by side when
// across is set and stacked otherwise.
type node struct {
	win    *window
	across bool
	kids   []*node
	parent *node
	// size is the rows, or the columns side by side, the node asks for in
	// its split, and 0 shares what the others leave (B193).
	size int
	w, h int // where the layout last placed it
}

// place gives n the rectangle at x, y of width w and height h. Side by side
// windows have a column between them for a separator. A kid that asks for
// a size gets it, as long as every other kid keeps its least: a row and a
// status line, or a column. The other kids share what is left, and the
// first ones get any odd space.
func (n *node) place(x, y, w, h int) {
	n.w, n.h = w, h
	if n.win != nil {
		n.win.x, n.win.y, n.win.w, n.win.h = x, y, w, h
		return
	}
	k := len(n.kids)
	room, least := h, 2
	if n.across {
		room, least = w-(k-1), 1
	}
	sizes, flex := make([]int, k), 0
	for _, kid := range n.kids {
		if kid.size == 0 {
			flex++
		}
	}
	// Every other kid keeps its least, so a size takes only what is left.
	left, fixed := room-least*flex, k-flex
	for i, kid := range n.kids {
		if kid.size == 0 {
			continue
		}
		fixed--
		sizes[i] = max(least, min(kid.size, left-least*fixed))
		left -= sizes[i]
	}
	left += least * flex
	for i, kid := range n.kids {
		if kid.size == 0 {
			sizes[i] = left / flex
			if i < left%flex {
				sizes[i]++
			}
		}
	}
	if flex == 0 {
		sizes[k-1] += left
	}
	for i, kid := range n.kids {
		size := sizes[i]
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

// resize sets the size of the current window's split across, or stacked,
// to what to makes of its size now and the room its split has, and does
// nothing when no split goes that way (B193).
func (e *Session) resize(across bool, to func(now, room int) int) {
	l := e.root.leaf(e.win)
	for l.parent != nil && l.parent.across != across {
		l = l.parent
	}
	p := l.parent
	if p == nil {
		return
	}
	if across {
		l.size = max(to(l.w, p.w-(len(p.kids)-1)), 1)
	} else {
		// A height counts the rows of text, without the status line.
		l.size = max(to(l.h-1, p.h-1), 1) + 1
	}
	e.layout()
}

// even makes every split share its space equally again.
func (n *node) even() {
	n.size = 0
	for _, k := range n.kids {
		k.even()
	}
}

// split opens a new window on the shown buffer below the current one, or
// right of it when across is set, and moves to it. split_below and
// split_right off put it above or left (N16).
func (e *Session) split(across bool) {
	after := e.cfg.SplitBelow
	if across {
		after = e.cfg.SplitRight
	}
	e.splitBeside(across, after)
}

// splitBeside opens a new window on the shown buffer beside the current
// one, after it when after is set, and moves to it.
func (e *Session) splitBeside(across, after bool) {
	old := e.win
	nw := newWindow(old.buf, old.pos(), old.buf.text.Mark(old.top))
	nw.opts, nw.fitted = old.opts, old.fitted
	// The new window is not bound, so it wraps as old did before (B523).
	if old.bound {
		nw.opts.Wrap = old.wrapped
	}
	if old.own != nil {
		own := *old.own
		nw.own = &own
	}
	nw.alt = old.alt
	e.insert(nw, e.root.leaf(old), across, after)
}

// insert puts nw in the layout beside l, after it when after is set, and
// moves to it.
func (e *Session) insert(nw *window, l *node, across, after bool) {
	e.zoomed = nil
	add := &node{win: nw}
	if p := l.parent; p != nil && p.across == across {
		i := slices.Index(p.kids, l)
		if after {
			i++
		}
		add.parent = p
		p.kids = slices.Insert(p.kids, i, add)
	} else {
		// l becomes a split holding the new window and what l held.
		keep := &node{win: l.win, across: l.across, kids: l.kids, parent: l}
		for _, k := range keep.kids {
			k.parent = keep
		}
		add.parent = l
		l.win, l.across, l.kids = nil, across, []*node{keep, add}
		if !after {
			l.kids = []*node{add, keep}
		}
	}
	e.win = nw
	e.layout()
}

// zoom shows w alone across the layout's room, or every window again when
// w is zoomed. The others keep their places and sizes meanwhile (B370).
func (e *Session) zoom(w *window) error {
	if w.anchor != "" {
		return errors.New("a float does not zoom")
	}
	if e.zoomed == w {
		e.zoomed = nil
	} else {
		e.zoomed, e.win = w, w
	}
	e.layout()
	return nil
}

// closeWindow closes w, or hides it while its program runs.
func (e *Session) closeWindow(w *window) error {
	// A window whose program runs hides, and the program goes on (B231).
	if t := w.buf.term; t != nil && !t.exited {
		return e.hide(w)
	}
	if w.anchor != "" {
		e.closeFloat(w)
	} else {
		if err := e.unlay(w); err != nil {
			return err
		}
		w.drop()
	}
	// An ended terminal's buffer goes with its last window, as there is
	// nothing in it to come back to.
	if t := w.buf.term; t != nil && t.exited && slices.Contains(e.bufs, w.buf) && !e.showing(w.buf) {
		e.close(w.buf, true)
	}
	return nil
}

// showing reports whether a window of any session shows b, one out of
// sight too.
func (e *Session) showing(b *buffer) bool {
	for _, s := range e.sessions {
		if slices.ContainsFunc(append(s.windows(), s.hidden...), func(w *window) bool { return w.buf == b }) {
			return true
		}
	}
	return false
}

// hide takes w out of sight, keeping its buffer, its program, its cursor
// and where it was, to show there again (B364).
func (e *Session) hide(w *window) error {
	if w.anchor != "" {
		e.unfloat(w)
	} else {
		l := e.root.leaf(w)
		p := l.parent
		if p == nil {
			return errors.New("cannot hide the last window")
		}
		i := slices.Index(p.kids, l)
		next := i - 1
		if i == 0 {
			next = 1
		}
		w.was = slot{beside: p.kids[next].windows()[0], across: p.across, after: i > next, size: l.size}
		if err := e.unlay(w); err != nil {
			return err
		}
	}
	e.hidden = append(e.hidden, w)
	return nil
}

// unhide shows w again where it was hidden from, and moves to it. A window
// whose neighbour is gone shows below the current one.
func (e *Session) unhide(w *window) {
	e.hidden = slices.DeleteFunc(e.hidden, func(h *window) bool { return h == w })
	if w.anchor != "" {
		if !slices.Contains(e.windows(), w.from) {
			w.from = e.win
		}
		e.floats = append(e.floats, w)
		e.place(w)
		e.win = w
		return
	}
	s := w.was
	if !slices.Contains(e.root.windows(), s.beside) {
		s = slot{beside: e.under(), after: true, size: s.size}
	}
	// It goes beside all of the split its neighbour is in now, when that
	// split is the other way.
	l := e.root.leaf(s.beside)
	for l.parent != nil && l.parent.across != s.across {
		l = l.parent
	}
	e.insert(w, l, s.across, s.after)
	e.root.leaf(w).size = s.size
	e.layout()
}

// unlay takes w out of the layout. The window before it in the split, or
// else the one after, takes its place and becomes current.
func (e *Session) unlay(w *window) error {
	e.zoomed = nil
	l := e.root.leaf(w)
	p := l.parent
	if p == nil {
		return errors.New("cannot close the last window")
	}
	e.closeFloatsOver(w)
	w.buf.setMark('"', w.pos())
	w.buf.lastTop = w.buf.text.Mark(w.top)
	i := slices.Index(p.kids, l)
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

// only closes every window but the current one, and every float but those
// the current window is in or over, as closeWindow does, so a window whose
// program runs hides (B364).
func (e *Session) only() error {
	keep := map[*window]bool{}
	for w := e.win; w != nil; w = w.from {
		keep[w] = true
	}
	for _, w := range append(slices.Clone(e.floats), e.root.windows()...) {
		// Closing a float closes those opened from it.
		if keep[w] || w.anchor != "" && !slices.Contains(e.floats, w) {
			continue
		}
		if err := e.closeWindow(w); err != nil {
			return err
		}
	}
	e.layout()
	return nil
}

// navigate moves to the window in direction d, h, j, k or l, or inside
// moveTo moves to the window next to the current one in direction d, one of
// h, j, k and l, on the cursor's row or column.
func (e *Session) moveTo(d byte) {
	if o := e.beside(d); o != nil {
		e.win = o
	}
}

// beside returns the window next to the current one in direction d, on the
// cursor's row or column, or nil (B330).
func (e *Session) beside(d byte) *window {
	w := e.win
	// For a zoomed window, beside measures from its place in the layout
	// (B370).
	x, y, ww, h := w.x, w.y, w.w, w.h
	if w == e.zoomed {
		x, y, ww, h = e.unzoomed[0], e.unzoomed[1], e.unzoomed[2], e.unzoomed[3]
	}
	t := w.buf.text.Snapshot()
	row, col := y, x
	for p := e.Top(); p < vim.LineStart(t, w.pos()) && row < y+h-2; p = vim.LineEnd(t, p) + 1 {
		row++
	}
	col += min(w.pos()-vim.LineStart(t, w.pos()), ww-1)
	switch d {
	case 'h':
		col = x - 2
	case 'l':
		col = x + ww + 1
	case 'k':
		row = y - 1
	case 'j':
		row = y + h
	}
	for _, o := range e.root.windows() {
		if o != w && col >= o.x && col < o.x+o.w && row >= o.y && row < o.y+o.h {
			return o
		}
	}
	return nil
}

// layout places every window in the space the editor has.
func (e *Session) layout() {
	x, w, _ := e.area()
	// The buffer line takes the top row while it shows (B258).
	top := 0
	if e.lined = len(e.Buffers()) > 0; e.lined {
		top = 1
	}
	e.root.place(x, top, w, max(e.height-top, 2))
	// A zoomed window takes all the room, and the others keep their places
	// for when it is not.
	if z := e.zoomed; z != nil {
		e.unzoomed = [4]int{z.x, z.y, z.w, z.h}
		z.x, z.y, z.w, z.h = x, top, w, max(e.height-top, 2)
	}
	for _, f := range e.floats {
		e.place(f)
	}
	// A terminal is as big as the window showing it, its status line aside.
	for _, win := range e.root.windows() {
		if t := win.buf.term; t != nil {
			if err := t.fit(win.w, win.h-1); err != nil {
				e.message = err.Error()
			}
		}
	}
}

// area returns the columns the windows take, and whether they are
// centered. Centered, each window gets its gutter and as many columns of
// text as the colour column new windows start with, or centerWidth. The
// windows sit in the middle of a wider screen. A lone window's text sits
// in the middle instead, so it stays put as the gutter grows (B199).
func (e *Session) area() (x, w int, centered bool) {
	w = max(e.width, 1)
	text := cmp.Or(e.opts.ColorColumn, centerWidth)
	need := e.root.needs(text)
	switch {
	case !e.centered || need > w:
		return 0, w, false
	case e.root.win != nil:
		return max((w-text)/2-e.root.win.gutter(), 0), need, true
	}
	return (w - need) / 2, need, true
}

// needs returns how many columns n takes centered, with text columns of
// text in each window. Windows side by side add up, with a separator
// between each two. A stack takes as many as its widest window.
func (n *node) needs(text int) int {
	if n.win != nil {
		return text + n.win.gutter()
	}
	need := 0
	for _, k := range n.kids {
		if n.across {
			need += k.needs(text)
		} else {
			need = max(need, k.needs(text))
		}
	}
	if n.across {
		need += len(n.kids) - 1
	}
	return need
}

// centerWidth is how many columns of text :center leaves without a colour
// column.
const centerWidth = 100

// center turns centering on or off (B199). On a screen too narrow it still
// turns on, and centers once the windows fit.
func (e *Session) center() {
	e.centered = !e.centered
	e.layout()
	if _, _, ok := e.area(); e.centered && !ok {
		e.message = fmt.Sprintf("the screen is too narrow to center %d columns", e.root.needs(cmp.Or(e.opts.ColorColumn, centerWidth)))
	}
}

// windowKey is a <C-w> command: its keys after <C-w>, what it does, and
// how.
type windowKey struct {
	keys []string
	help string
	run  func(e *Session, count int) error // count is 0 when none was typed
}

// windowKeys are the <C-w> commands (B91).
var windowKeys = []windowKey{
	{[]string{"s", "S", "<C-s>"}, "splits the window, the new one below, or above with split_below off", func(e *Session, _ int) error { e.split(false); return nil }},
	{[]string{"v", "<C-v>"}, "splits the window, the new one right, or left with split_right off", func(e *Session, _ int) error { e.split(true); return nil }},
	{[]string{"h", "<C-h>"}, "moves to the window left", func(e *Session, _ int) error { e.moveTo('h'); return nil }},
	{[]string{"j", "<C-j>"}, "moves to the window below", func(e *Session, _ int) error { e.moveTo('j'); return nil }},
	{[]string{"k", "<C-k>"}, "moves to the window above", func(e *Session, _ int) error { e.moveTo('k'); return nil }},
	{[]string{"l", "<C-l>"}, "moves to the window right", func(e *Session, _ int) error { e.moveTo('l'); return nil }},
	{[]string{"c", "<C-c>"}, "closes the window", func(e *Session, _ int) error { return e.closeWindow(e.win) }},
	{[]string{"q", "<C-q>"}, "closes the window, and quits with the last", func(e *Session, _ int) error { e.quit(false); return nil }},
	{[]string{"o", "<C-o>"}, "closes every other window", func(e *Session, _ int) error { return e.only() }},
	{[]string{"="}, "makes the windows the same size", func(e *Session, _ int) error { e.root.even(); e.layout(); return nil }},
	{[]string{"w", "<C-w>"}, "moves to the next window, or to window n", func(e *Session, n int) error { e.cycle(n, 1); return nil }},
	{[]string{"W"}, "moves to the window before, or to window n", func(e *Session, n int) error { e.cycle(n, -1); return nil }},
	{[]string{"p", "<C-p>"}, "moves to the window that was current before", func(e *Session, _ int) error { return e.previous() }},
	{[]string{"x", "<C-x>"}, "exchanges the window with the next in its split, or with the n-th", func(e *Session, n int) error { return e.exchange(n) }},
	{[]string{"r", "<C-r>"}, "rotates the windows of its split down or right, n times", func(e *Session, n int) error { return e.rotate(max(n, 1)) }},
	{[]string{"R"}, "rotates the windows of its split up or left, n times", func(e *Session, n int) error { return e.rotate(-max(n, 1)) }},
	{[]string{"H"}, "moves the window to the far left, as tall as the layout", func(e *Session, _ int) error { return e.toEdge("left") }},
	{[]string{"J"}, "moves the window to the bottom, as wide as the layout", func(e *Session, _ int) error { return e.toEdge("below") }},
	{[]string{"K"}, "moves the window to the top, as wide as the layout", func(e *Session, _ int) error { return e.toEdge("above") }},
	{[]string{"L"}, "moves the window to the far right, as tall as the layout", func(e *Session, _ int) error { return e.toEdge("right") }},
}

// cycle moves to window n, or the last when there are fewer, or else step
// windows on from the current one, going round. The floats count after the
// windows of the layout, the newest first (B420).
func (e *Session) cycle(n, step int) {
	ws := e.root.windows()
	for _, f := range slices.Backward(e.floats) {
		ws = append(ws, f)
	}
	if n > 0 {
		e.win = ws[min(n, len(ws))-1]
		return
	}
	i := slices.Index(ws, e.win)
	e.win = ws[((i+step)%len(ws)+len(ws))%len(ws)]
}

// previous moves to the window that was current before this one (B420).
func (e *Session) previous() error {
	if e.prevWin == e.win || !slices.Contains(e.windows(), e.prevWin) {
		return errors.New("no previous window")
	}
	e.win = e.prevWin
	return nil
}

// siblings returns the nodes of the current window's split and its place
// among them, or false when it is alone or one of them is a split.
func (e *Session) siblings() ([]*node, int, bool) {
	l := e.root.leaf(e.win)
	p := l.parent
	if p == nil || slices.ContainsFunc(p.kids, func(k *node) bool { return k.win == nil }) {
		return nil, 0, false
	}
	return p.kids, slices.Index(p.kids, l), true
}

// exchange swaps the current window with the next in its split, or the one
// before for the last, or with the n-th, each keeping its size. The cursor
// stays in its place, in the other window. Beside a split it does nothing
// (B421).
func (e *Session) exchange(n int) error {
	kids, i, ok := e.siblings()
	if !ok {
		return nil
	}
	j := i + 1
	switch {
	case n > 0:
		j = min(n, len(kids)) - 1
	case j == len(kids):
		j = i - 1
	}
	e.zoomed = nil
	kids[i], kids[j] = kids[j], kids[i]
	e.win = kids[i].win
	e.layout()
	return nil
}

// rotate moves each window of the current one's split n places on, the
// last ones going round to the start, or back for a negative n, each
// keeping its size. The cursor goes with its window (B421).
func (e *Session) rotate(n int) error {
	kids, _, ok := e.siblings()
	if !ok {
		if e.root.win == e.win {
			return nil
		}
		return errors.New("cannot rotate while another window is split")
	}
	moved := make([]*node, len(kids))
	for i, k := range kids {
		moved[((i+n)%len(kids)+len(kids))%len(kids)] = k
	}
	copy(kids, moved)
	e.zoomed = nil
	e.layout()
	return nil
}

// toEdge moves the current window to that edge of the layout, spanning it,
// and does nothing when it is the only one (B422).
func (e *Session) toEdge(where string) error {
	if e.root.win == e.win {
		return nil
	}
	return e.move(e.win, where)
}

// window runs a <C-w> command, typed after count.
func (e *Session) window(k string, count int) error {
	i := slices.IndexFunc(windowKeys, func(w windowKey) bool { return slices.Contains(w.keys, k) })
	if i < 0 {
		return nil
	}
	return windowKeys[i].run(e, count)
}
