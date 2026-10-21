package vim

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Each cursor keeps slots private marks from cursorMark+id*slots: where it
// is, its visual anchor, where its insert began, its own of ownMarks, then
// what it yanked or put at flashMark.
// The cursor keys move uses id 0 while the others run. lastMark is the first
// of the marks that keep the cursors <C-l> removed, the cursor's first.
const (
	cursorMark = 0x100000
	slots      = 16
	lastMark   = 0xF8000
	ownMarks   = "[]<>.^"
	flashMark  = 3 + len(ownMarks)
)

// context is what a command at one cursor reads and writes besides the text
// and its marks (B395).
type context struct {
	id          int
	has         int // a bit for each of ownMarks the cursor holds
	regs        map[rune]register
	want        int
	wantSet     bool
	anchorFirst bool
	lastVisual  Key
	autoIndent  bool
	replaced    []string
	blockInsert *blockInsert
}

// state is the mode a command starts in, which it starts in again at each
// cursor.
type state struct {
	mode           Mode
	visual         Key
	oneshot, armed bool
}

// quiet is the host while a command runs at the cursors. Its groups are the
// command's, and jumps, the window and the clipboard stay the cursor's.
type quiet struct{ Host }

func (quiet) BeginGroup()               {}
func (quiet) EndGroup()                 {}
func (quiet) Unhandled(string) error    { return nil }
func (quiet) PushJump(int)              {}
func (quiet) Jump(int) bool             { return false }
func (quiet) Change(int) bool           { return false }
func (quiet) SetTop(int)                {}
func (quiet) SetLeft(int)               {}
func (quiet) SetClipboard(string) error { return nil }

func (q quiet) SetMark(name rune, pos int) {
	if name != '\'' {
		q.Host.SetMark(name, pos)
	}
}

func mark(x *context, slot int) rune { return rune(cursorMark + x.id*slots + slot) }

// at returns where cursor x is.
func (m *Machine) at(x *context) int {
	p, _ := m.h.Mark(mark(x, 0))
	return p
}

// save keeps the machine's cursor and its state in x.
func (m *Machine) save(x *context) {
	m.h.SetMark(mark(x, 0), m.h.Cursor())
	m.h.SetMark(mark(x, 1), m.anchor)
	m.h.SetMark(mark(x, 2), m.insStart)
	x.has = 0
	for i, name := range ownMarks {
		if p, ok := m.h.Mark(name); ok {
			m.h.SetMark(mark(x, 3+i), p)
			x.has |= 1 << i
		}
	}
	x.regs, x.want, x.wantSet, x.anchorFirst, x.lastVisual = m.regs, m.want, m.wantSet, m.anchorFirst, m.lastVisual
	x.autoIndent, x.replaced, x.blockInsert = m.autoIndent, m.replaced, m.blockInsert
}

// load makes x the machine's cursor and state.
func (m *Machine) load(x *context) {
	m.h.SetCursor(m.at(x))
	m.anchor, _ = m.h.Mark(mark(x, 1))
	m.insStart, _ = m.h.Mark(mark(x, 2))
	for i, name := range ownMarks {
		if x.has&(1<<i) != 0 {
			p, _ := m.h.Mark(mark(x, 3+i))
			m.h.SetMark(name, p)
		}
	}
	m.regs, m.want, m.wantSet, m.anchorFirst, m.lastVisual = x.regs, x.want, x.wantSet, x.anchorFirst, x.lastVisual
	m.autoIndent, m.replaced, m.blockInsert = x.autoIndent, x.replaced, x.blockInsert
}

// Cursors returns where the multicursors are, lowest first. The cursor keys
// move is not one of them (B392).
func (m *Machine) Cursors() []int {
	out := make([]int, len(m.cursors))
	for i, x := range m.cursors {
		out[i] = m.at(x)
	}
	return out
}

// place puts a cursor at pos with the state of the cursor keys move, a copy
// of its registers, and returns it. The caller sorts the cursors once it has
// placed them all, which keeps one already at pos as it is.
func (m *Machine) place(pos int) *context {
	x := &context{id: m.ids + 1}
	if n := len(m.free); n > 0 {
		x.id, m.free = m.free[n-1], m.free[:n-1]
	} else {
		m.ids++
	}
	m.save(x)
	m.h.SetMark(mark(x, 0), pos)
	x.regs = maps.Clone(m.regs)
	if m.written == nil {
		m.written = map[rune]bool{}
	}
	m.cursors = append(m.cursors, x)
	return x
}

// sortCursors puts the cursors in order through the text, and makes those
// in one place one.
func (m *Machine) sortCursors() {
	slices.SortStableFunc(m.cursors, func(a, b *context) int { return m.at(a) - m.at(b) })
	m.cursors = slices.CompactFunc(m.cursors, func(a, b *context) bool {
		if m.at(a) != m.at(b) {
			return false
		}
		m.free = append(m.free, b.id)
		return true
	})
}

// remove takes away the cursor at pos, and reports whether there was one.
func (m *Machine) remove(pos int) bool {
	i := slices.IndexFunc(m.cursors, func(x *context) bool { return m.at(x) == pos })
	if i < 0 {
		return false
	}
	m.free = append(m.free, m.cursors[i].id)
	m.cursors = slices.Delete(m.cursors, i, i+1)
	return true
}

// clearCursors is <C-l>, which removes the cursors and keeps where they
// were for gQ. Each register written while they were placed then holds
// what every cursor held there, as lines, in order through the text (B395,
// B397).
func (m *Machine) clearCursors() {
	if len(m.cursors) == 0 {
		return
	}
	m.h.SetMark(lastMark, m.h.Cursor())
	for i, x := range m.cursors {
		m.h.SetMark(rune(lastMark+1+i), m.at(x))
	}
	m.last = len(m.cursors) + 1
	type held struct {
		pos  int
		regs map[rune]register
	}
	all := []held{{m.h.Cursor(), m.regs}}
	for _, x := range m.cursors {
		all = append(all, held{m.at(x), x.regs})
	}
	slices.SortStableFunc(all, func(a, b held) int { return a.pos - b.pos })
	all = slices.CompactFunc(all, func(a, b held) bool { return a.pos == b.pos })
	for name := range m.written {
		var b strings.Builder
		for _, h := range all {
			if r, ok := h.regs[name]; ok {
				b.WriteString(r.text)
				if !strings.HasSuffix(r.text, "\n") {
					b.WriteByte('\n')
				}
			}
		}
		if b.Len() == 0 {
			continue
		}
		if m.regs == nil {
			m.regs = map[rune]register{}
		}
		m.regs[name] = register{text: b.String(), linewise: true}
	}
	if r, ok := m.regs['"']; ok && m.written['"'] && m.clipboard {
		if err := m.h.SetClipboard(r.text); err != nil {
			m.msg = err.Error()
		}
	}
	m.cursors, m.free, m.ids, m.written = nil, nil, 0, nil
}

// ForgetCursors removes the multicursors without what <C-l> does, for a
// host whose text is now another. Their registers stay their own, and gQ
// does not put them back.
func (m *Machine) ForgetCursors() {
	m.cursors, m.free, m.ids, m.written, m.last = nil, nil, 0, nil, 0
}

// restore is gQ, which puts back the cursors <C-l> removed last and the
// cursor where it was.
func (m *Machine) restore() {
	if len(m.cursors) > 0 || m.last == 0 {
		return
	}
	t := m.h.Text()
	for i := 1; i < m.last; i++ {
		p, _ := m.h.Mark(rune(lastMark + i))
		m.place(clamp(t, p))
	}
	m.sortCursors()
	p, _ := m.h.Mark(lastMark)
	m.h.SetCursor(clamp(t, p))
}

// toggle is Q: 1Q adds a cursor where the cursor is, 2Q removes it, and Q
// does whichever it is not. It sets follow off.
func (m *Machine) toggle(count int) {
	m.once, m.follow = true, false
	c := m.h.Cursor()
	if count != 1 && m.remove(c) || count == 2 {
		return
	}
	m.place(c)
	m.sortCursors()
}

// eachLine is Q in visual mode, which places a cursor on each selected line
// at the column the cursor shows in, and sets follow on.
func (m *Machine) eachLine() {
	m.once, m.follow = true, true
	tab := m.h.TabWidth()
	t, c := m.h.Text(), m.h.Cursor()
	first, last := t.LineOf(min(m.anchor, c)), t.LineOf(max(m.anchor, c))
	m.leaveVisual()
	col := math.MaxInt
	if !m.wantSet || m.want != math.MaxInt {
		col = m.shownColumn(t, c)
	}
	for line := first; line <= last; line++ {
		m.place(atColumn(t, t.LineStart(line), col, tab))
	}
	m.sortCursors()
}

// along is zq{motion}. It places a cursor at each place the motion reaches
// from the cursor, going on from each, to the end of the text or of the
// selection, and at the start too, unless the motion is f, t, F or T away
// from the start of the text. A t next to its character goes on past it,
// as ; does. A search places one at every match, which gv then selects. The
// cursor goes to the first placed, unless one is where it is (B398).
func (m *Machine) along(cmd string, count int) error {
	m.once = true
	t, c := m.h.Text(), m.h.Cursor()
	ranges := [][2]int{{c, t.Len()}}
	if m.mode == Visual {
		r := m.selection()
		first, last := t.LineOf(min(m.anchor, c)), t.LineOf(max(m.anchor, c))
		m.leaveVisual()
		ranges = [][2]int{{r.start, r.end}}
		if r.block {
			ranges = ranges[:0]
			for line := first; line <= last; line++ {
				ls := t.LineStart(line)
				ranges = append(ranges, [2]int{colStart(t, ls, r.left, m.h.TabWidth()), LineEnd(t, ls)})
			}
		}
	}
	var placed []int
	if strings.ContainsAny(cmd[:1], "nN*#/?") || cmd == "gn" || cmd == "gN" {
		var err error
		if placed, err = m.everyMatch(cmd, count, c, ranges); err != nil {
			return err
		}
		ranges = nil
	}
	find := strings.ContainsAny(cmd[:1], "ftFT")
	for _, r := range ranges {
		var got []int
		for p := r[0]; ; {
			m.h.SetCursor(p)
			to, k, ok := m.motion(cmd, count, "")
			if cmd[0] == 't' && ok && k != failed && to == p {
				m.h.SetCursor(next(t, p, LineEnd(t, p)))
				to, k, ok = m.motion(cmd, count, "")
			}
			if to = clamp(t, to); !ok || k == failed || to <= p || to >= r[1] {
				break
			}
			got, p = append(got, to), to
		}
		if len(got) > 0 && (!find || r[0] == 0) {
			got = append([]int{r[0]}, got...)
		}
		for _, p := range got {
			m.place(p)
		}
		placed = append(placed, got...)
	}
	m.beep = false
	m.sortCursors()
	if len(placed) > 0 && !slices.Contains(placed, c) {
		c = placed[0]
	}
	m.h.SetCursor(clamp(t, c))
	return nil
}

// everyMatch places a cursor at every match in ranges of the search cmd
// makes, each with its match as its last selection, and returns where.
func (m *Machine) everyMatch(cmd string, count, c int, ranges [][2]int) ([]int, error) {
	if cmd != "gn" && cmd != "gN" {
		// The search sets its pattern, as * takes the word under the cursor.
		if _, k, ok := m.motion(cmd, count, ""); !ok || k == failed {
			m.beep = true
			return nil, nil
		}
	}
	m.h.SetCursor(c)
	p, err := m.compile(m.search.pattern)
	if err != nil {
		return nil, err
	}
	t := m.h.Text()
	var placed []int
	for _, r := range ranges {
		for pos := r[0]; ; {
			at, ok := p.first(t, pos, r[1])
			if !ok {
				break
			}
			end := p.endAt(t, at)
			m.selected(m.place(at), at, end)
			placed = append(placed, at)
			if at == c {
				m.h.SetMark('<', at)
				m.h.SetMark('>', prev(t, LineStart(t, end), end))
				m.lastVisual, m.anchorFirst = "v", true
			}
			pos = max(end, next(t, at, t.Len()))
		}
	}
	return placed, nil
}

// placeSelecting places a cursor that selects [s, e) in visual mode.
func (m *Machine) placeSelecting(s, e int) {
	t := m.h.Text()
	x := m.place(prev(t, LineStart(t, e), e))
	m.h.SetMark(mark(x, 1), s)
}

// selected makes [s, e) cursor x's last selection, which its gv selects.
func (m *Machine) selected(x *context, s, e int) {
	t := m.h.Text()
	lt, gt := strings.IndexByte(ownMarks, '<'), strings.IndexByte(ownMarks, '>')
	m.h.SetMark(mark(x, 3+lt), s)
	m.h.SetMark(mark(x, 3+gt), prev(t, LineStart(t, e), e))
	x.has |= 1<<lt | 1<<gt
	x.lastVisual, x.anchorFirst = "v", true
}

// number is g<C-a> with cursors, which puts a number before each, counting
// up from n through the text (B400).
func (m *Machine) number(n int) error {
	m.once = true
	if len(m.cursors) == 0 {
		return nil
	}
	c := m.h.Cursor()
	at := append(m.Cursors(), c)
	slices.Sort(at)
	at = slices.Compact(at)
	edits := make([]Edit, len(at))
	shift := 0
	for i, p := range at {
		edits[i] = Edit{Start: p, End: p, Text: strconv.Itoa(n + i)}
		if p <= c {
			shift += len(edits[i].Text)
		}
	}
	if err := m.apply(edits, "g<C-a>"); err != nil {
		return err
	}
	m.h.SetCursor(c + shift)
	return nil
}

// toCursor is ]C or [C. It goes to the n-th cursor after or before c,
// going round, or with none placed, to the n-th of those <C-l> removed
// last (B399).
func (m *Machine) toCursor(x motionCall) (int, kind) {
	at := m.Cursors()
	if len(at) == 0 {
		for i := 1; i < m.last; i++ {
			p, _ := m.h.Mark(rune(lastMark + i))
			at = append(at, p)
		}
		slices.Sort(at)
	}
	at = slices.DeleteFunc(at, func(p int) bool { return p == x.c })
	if len(at) == 0 {
		return x.c, failed
	}
	i, _ := slices.BinarySearch(at, x.c)
	if x.cmd == "]C" {
		i += x.n - 1
	} else {
		i -= x.n
	}
	i = ((i % len(at)) + len(at)) % len(at)
	return at[i], exclusive
}

// everywhere runs run at the cursor, then at each multicursor, as one undo
// step.
func (m *Machine) everywhere(run func() error) error {
	if len(m.cursors) == 0 {
		return run()
	}
	m.note()
	m.h.BeginGroup()
	defer m.h.EndGroup()
	if err := run(); err != nil {
		return err
	}
	return m.cascade(m.from, run)
}

// beginStep starts a command typed while cursors are placed.
func (m *Machine) beginStep() {
	m.stepping, m.step = true, m.step[:0]
	m.edited, m.operated, m.went, m.once, m.grouped = false, false, false, false, false
	m.note()
}

// note keeps the state a command starts from and the cursors where the
// cursor is, for cascade.
func (m *Machine) note() {
	m.from = state{m.mode, m.visual, m.oneshot, m.armed}
	c := m.h.Cursor()
	m.with = m.with[:0]
	for _, x := range m.cursors {
		if m.at(x) == c {
			m.with = append(m.with, x)
		}
	}
}

// endStep runs the command just done at every cursor when it runs again
// there, as one undo group with it.
func (m *Machine) endStep() error {
	m.stepping = false
	var err error
	if m.again() {
		if !m.grouped {
			m.h.BeginGroup()
			m.grouped = true
		}
		keys := m.step
		err = m.cascade(m.from, func() error { return m.run(keys, false) })
	}
	if m.grouped {
		m.grouped = false
		m.h.EndGroup()
	}
	return err
}

// again reports whether the command just done runs again at the cursors:
// one that edited, ran an operator or started insert or visual mode, every
// key in insert or visual mode but <C-o>, and with follow a motion that
// moves from the cursor. Ex commands and the cursors' own commands run once.
func (m *Machine) again() bool {
	switch {
	case m.once || len(m.cursors) == 0:
		return false
	case m.from.mode == Insert || m.from.mode == Replace:
		return !m.oneshot
	case m.from.mode == Visual, m.edited, m.operated:
		return true
	case !m.from.oneshot && m.mode != Normal:
		return true
	}
	return m.went && m.follow
}

// cascade runs run at every cursor, starting from s, but those in m.with,
// which go where the cursor is. It removes a cursor that run leaves in
// another mode than the cursor's, as one where ct; finds no ;, and makes
// cursors that end in one place one. A command that fails at a cursor
// leaves it there, and cascade returns the first error.
func (m *Machine) cascade(s state, run func() error) error {
	home := &context{}
	m.save(home)
	// What each yanks or put shows, the cursor's too, as marks, so the
	// edits after move it.
	m.h.SetMark(mark(home, flashMark), m.flash[0])
	m.h.SetMark(mark(home, flashMark+1), m.flash[1])
	var flashed []*context
	saved := *m
	m.h, m.cascading = quiet{saved.h}, true
	kept := make([]*context, 0, len(m.cursors))
	var gone []int
	var first error
	for _, x := range saved.cursors {
		if slices.Contains(saved.with, x) {
			kept = append(kept, x)
			continue
		}
		m.load(x)
		m.mode, m.visual, m.oneshot, m.armed = s.mode, s.visual, s.oneshot, s.armed
		m.pending, m.op, m.opCount, m.count, m.reg, m.wait, m.beep, m.flash = "", "", 0, 0, 0, nil, false, [2]int{}
		err := run()
		m.save(x)
		if m.flash[1] > m.flash[0] {
			m.h.SetMark(mark(x, flashMark), m.flash[0])
			m.h.SetMark(mark(x, flashMark+1), m.flash[1])
			flashed = append(flashed, x)
		}
		if m.mode == saved.mode && m.pending == "" && m.wait == nil {
			kept = append(kept, x)
		} else {
			gone = append(gone, x.id)
		}
		if first == nil {
			first = err
		}
	}
	free := append(m.free, gone...)
	*m = saved
	m.cursors, m.free = kept, free
	m.load(home)
	if m.flash[1] > m.flash[0] {
		m.flash[0], _ = m.h.Mark(mark(home, flashMark))
		m.flash[1], _ = m.h.Mark(mark(home, flashMark+1))
	}
	for _, x := range flashed {
		s, _ := m.h.Mark(mark(x, flashMark))
		e, _ := m.h.Mark(mark(x, flashMark+1))
		m.flashes = append(m.flashes, [2]int{s, e})
	}
	for _, x := range m.with {
		m.h.SetMark(mark(x, 0), m.h.Cursor())
		m.h.SetMark(mark(x, 1), m.anchor)
	}
	// One that lands where the cursor is becomes the cursor, unless it went
	// with it.
	c := m.h.Cursor()
	m.cursors = slices.DeleteFunc(m.cursors, func(x *context) bool {
		if m.at(x) != c || slices.Contains(m.with, x) {
			return false
		}
		m.free = append(m.free, x.id)
		return true
	})
	m.sortCursors()
	return first
}

// selections returns each cursor's selection with the cursor's own, for a
// client to draw, or nil without cursors or for a block.
func (m *Machine) selections() [][2]int {
	if len(m.cursors) == 0 || m.visual == "<C-v>" {
		return nil
	}
	r := m.selection()
	out := [][2]int{{r.start, r.end}}
	for _, x := range m.cursors {
		a, _ := m.h.Mark(mark(x, 1))
		r := m.selectionAt(a, m.at(x))
		out = append(out, [2]int{r.start, r.end})
	}
	slices.SortFunc(out, func(a, b [2]int) int { return cmp.Compare(a[0], b[0]) })
	return out
}
