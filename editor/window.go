package editor

import (
	"cmp"
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
	left   int      // the first screen column shown, without wrap
	saved  *options // its options while the start screen shows (B225)
	own    *options // its options under those its buffer's filetype sets (B94)
	fitted *buffer  // the buffer its filetype's options were last fitted for
	// pinned is the last sticky context worked out, with the text's version,
	// the top, cursor and parse it was for, so a frame that changes none of
	// them asks no tree.
	pinned struct {
		version             kernel.Version
		top, cursor, parses int
		starts              []int
	}

	x, y, w, h int // where the layout puts it, its status line included
}

// newWindow shows b with the cursor at pos and top as the first line.
func newWindow(b *buffer, pos, top int) *window {
	noteRecent(b.file.path)
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
	if w.opts.SignColumn || w.buf.signed || w.buf.signs > 0 || w.buf.listed != "" || len(w.buf.breaks) > 0 || w.buf.stopped {
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
// tab columns wide.
func (w *window) rows(t kernel.Snapshot, start, tab int) int {
	if !w.opts.Wrap {
		return 1
	}
	return len(vim.Rows(t, start, w.cols(), tab))
}

// show switches the window to b, at the place b was last left.
func (w *window) show(b *buffer) {
	w.buf.last, w.buf.lastTop = w.pos(), w.buf.text.Mark(w.top)
	if w.buf != b {
		w.alt = w.buf
	}
	w.buf = b
	if w.saved != nil && b.title != "[start]" {
		w.opts, w.saved = *w.saved, nil
	}
	noteRecent(b.file.path)
	w.cursor = b.text.SetMark(b.last, kernel.Right)
	w.top = b.text.SetMark(b.lastTop, kernel.Left)
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
}

// expandTab reports whether b indents with spaces only. A window that
// shows it decides, or else what a module set, its filetype or the session.
func (e *Session) expandTab(b *buffer) bool {
	for _, w := range e.root.windows() {
		if w.buf == b {
			e.fit(w)
			return w.opts.ExpandTab
		}
	}
	if on, ok := b.set["expand_tab"]; ok {
		return on == "true"
	}
	if on := e.cfg.Filetype[e.filetype(b)].ExpandTab; on != nil {
		return *on
	}
	return e.opts.ExpandTab
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
		w.opts.Spell = *ft.Spell
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
	now, room := l.h, p.h
	if across {
		now, room = l.w, p.w-(len(p.kids)-1)
	}
	l.size = max(to(now, room), 1)
	e.layout()
}

// resizeTo runs :resize, or :vertical resize when across, with arg: n
// sets the size, +n and -n change it, and none makes it as large as it can
// be (B193).
func (e *Session) resizeTo(across bool, arg string) {
	n, err := strconv.Atoi(arg)
	if arg != "" && err != nil {
		e.message = "not a size: " + arg
		return
	}
	e.resize(across, func(now, room int) int {
		switch {
		case arg == "":
			return room
		case arg[0] == '+' || arg[0] == '-':
			return now + n
		}
		return n
	})
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
	if old.own != nil {
		own := *old.own
		nw.own = &own
	}
	nw.alt = old.alt
	l := e.root.leaf(old)
	add := &node{win: nw}
	if p := l.parent; p != nil && p.across == across {
		i := slices.Index(p.kids, l)
		if after {
			i++
		}
		add.parent = p
		p.kids = slices.Insert(p.kids, i, add)
	} else {
		// l becomes a split holding the new window and the old one.
		keep := &node{win: old, parent: l}
		add.parent = l
		l.win, l.across, l.kids = nil, across, []*node{keep, add}
		if !after {
			l.kids = []*node{add, keep}
		}
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

// only closes every window but the current one.
func (e *Session) only() {
	e.root = &node{win: e.win}
	e.layout()
}

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

// moveTo moves to the window next to the current one in direction d, one of
// h, j, k and l, on the cursor's row or column.
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
	x, w, _ := e.area()
	// The buffer line takes the top row while it shows (B258).
	top := 0
	if e.lined = len(e.Buffers()) > 0; e.lined {
		top = 1
	}
	e.root.place(x, top, w, max(e.height-top, 2))
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
	{[]string{"o", "<C-o>"}, "closes every other window", func(e *Session, _ int) error { e.only(); return nil }},
	{[]string{"="}, "makes the windows the same size", func(e *Session, _ int) error { e.root.even(); e.layout(); return nil }},
	{[]string{"+"}, "makes the window taller, by the count", func(e *Session, n int) error {
		e.resize(false, func(now, _ int) int { return now + max(n, 1) })
		return nil
	}},
	{[]string{"-"}, "makes the window shorter, by the count", func(e *Session, n int) error {
		e.resize(false, func(now, _ int) int { return now - max(n, 1) })
		return nil
	}},
	{[]string{">"}, "makes the window wider, by the count", func(e *Session, n int) error {
		e.resize(true, func(now, _ int) int { return now + max(n, 1) })
		return nil
	}},
	{[]string{"<"}, "makes the window narrower, by the count", func(e *Session, n int) error {
		e.resize(true, func(now, _ int) int { return now - max(n, 1) })
		return nil
	}},
	{[]string{"_", "<C-_>"}, "makes the window the count tall, or as tall as it can be", func(e *Session, n int) error {
		e.resize(false, func(_, room int) int { return cmp.Or(n, room) })
		return nil
	}},
	{[]string{"|"}, "makes the window the count wide, or as wide as it can be", func(e *Session, n int) error {
		e.resize(true, func(_, room int) int { return cmp.Or(n, room) })
		return nil
	}},
}

// window runs a <C-w> command, typed after count.
func (e *Session) window(k string, count int) {
	i := slices.IndexFunc(windowKeys, func(w windowKey) bool { return slices.Contains(w.keys, k) })
	if i < 0 {
		return
	}
	if err := windowKeys[i].run(e, count); err != nil {
		e.message = err.Error()
	}
}
