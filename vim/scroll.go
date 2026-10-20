package vim

// scrollCall is what a scroll gets: the count typed (0 for none), n for
// the count or 1, the height of the view, the starts of its top line, of
// the cursor's line and of the last line.
type scrollCall struct {
	t                            Text
	cmd                          string
	count, n, h, top, line, last int
}

// scrollKey is a command that moves the view.
type scrollKey struct {
	help string
	// run returns the view's new top line and the cursor's line, or false
	// when the view cannot move.
	run func(m *Machine, x scrollCall) (int, int, bool)
}

// halfPage is <C-d> and <C-u>, which move the cursor with the view.
func halfPage(m *Machine, x scrollCall) (int, int, bool) {
	s := x.h / 2
	if x.count > 0 {
		s = x.count
	}
	switch {
	case x.cmd == "<C-d>" && x.line == x.last, x.cmd == "<C-u>" && x.line == 0:
		m.beep = true
		return 0, 0, false
	case x.cmd == "<C-d>":
		// The view stops once the last line reaches the bottom.
		return max(x.top, min(down(x.t, x.top, s), up(x.t, x.last, x.h-1))), down(x.t, x.line, s), true
	}
	return up(x.t, x.top, s), up(x.t, x.line, s), true
}

// scrolls are the commands that move the view, by their keys.
var scrolls = map[string]scrollKey{
	"<C-e>": {"scrolls the view down n lines", func(m *Machine, x scrollCall) (int, int, bool) {
		top := down(x.t, x.top, x.n)
		return top, max(x.line, top), true
	}},
	"<C-y>": {"scrolls the view up n lines", func(m *Machine, x scrollCall) (int, int, bool) {
		top := up(x.t, x.top, x.n)
		return top, min(x.line, down(x.t, top, x.h-1)), true
	}},
	"<C-d>": {"scrolls down half a view, or n lines, with the cursor", halfPage},
	"<C-u>": {"scrolls up half a view, or n lines, with the cursor", halfPage},
	"<C-f>": {"scrolls down n views", func(m *Machine, x scrollCall) (int, int, bool) {
		top := x.top
		for range x.n {
			top = down(x.t, top, max(x.h-2, 1))
		}
		return top, max(x.line, top), true
	}},
	"<C-b>": {"scrolls up n views", func(m *Machine, x scrollCall) (int, int, bool) {
		top := x.top
		for range x.n {
			top = up(x.t, top, max(x.h-2, 1))
		}
		return top, min(x.line, down(x.t, top, x.h-1)), true
	}},
	"zz":    {"scrolls the cursor's line, or line n, to the middle of the view", toMiddle},
	"z.":    {"scrolls the cursor's line, or line n, to the middle of the view, at its first non-blank", toMiddle},
	"zt":    {"scrolls the cursor's line, or line n, to the top of the view", toTop},
	"z<CR>": {"scrolls the cursor's line, or line n, to the top of the view, at its first non-blank", toTop},
	"zb":    {"scrolls the cursor's line, or line n, to the bottom of the view", toBottom},
	"z-":    {"scrolls the cursor's line, or line n, to the bottom of the view, at its first non-blank", toBottom},
}

// at returns the line zt, zz and zb put in place: line n, or the cursor's.
func (x scrollCall) at() int {
	if x.count > 0 {
		return down(x.t, 0, x.count-1)
	}
	return x.line
}

func toMiddle(_ *Machine, x scrollCall) (int, int, bool) {
	return up(x.t, x.at(), (x.h-1)/2), x.at(), true
}

func toTop(_ *Machine, x scrollCall) (int, int, bool) { return x.at(), x.at(), true }

func toBottom(_ *Machine, x scrollCall) (int, int, bool) {
	return up(x.t, x.at(), x.h-1), x.at(), true
}

// sideways are the commands that scroll a view whose lines do not wrap,
// by their keys. by returns how far one moves the left edge, given n and
// the view's width. zs and ze have none, as they put the cursor's column
// at an edge.
var sideways = map[string]struct {
	help string
	by   func(n, cols int) int
}{
	"zl": {"scrolls the view n columns right", func(n, _ int) int { return n }},
	"zh": {"scrolls the view n columns left", func(n, _ int) int { return -n }},
	"zL": {"scrolls the view half its width right", func(_, cols int) int { return cols / 2 }},
	"zH": {"scrolls the view half its width left", func(_, cols int) int { return -cols / 2 }},
	"zs": {"scrolls the view so the cursor is at its left edge", nil},
	"ze": {"scrolls the view so the cursor is at its right edge", nil},
}

// scrollSideways runs one of sideways, which does nothing while lines wrap.
// The cursor moves the least it must to stay in the view.
func (m *Machine) scrollSideways(cmd string, n int) {
	left, cols := m.h.Sideways()
	if cols <= 0 {
		return
	}
	t, c, tab := m.h.Text(), m.h.Cursor(), m.h.TabWidth()
	ls := LineStart(t, c)
	col := column(t, ls, c, tab)
	switch cmd {
	case "zs":
		left = col
	case "ze":
		left = col - cols + 1
	default:
		left += sideways[cmd].by(n, cols)
	}
	left = max(left, 0)
	m.h.SetLeft(left)
	switch {
	case col < left:
		m.h.SetCursor(clamp(t, colStart(t, ls, left, tab)))
	case col >= left+cols:
		m.h.SetCursor(atColumn(t, ls, left+cols-1, tab))
	}
}

// scroll runs a command that moves the view, or reports false. The cursor
// keeps its column. It moves only as far as it must to stay in the view,
// except that <C-d> and <C-u> move it with the view.
func (m *Machine) scroll(cmd string, count int) (bool, error) {
	if _, ok := sideways[cmd]; ok {
		m.scrollSideways(cmd, max(count, 1))
		return true, nil
	}
	sk, ok := scrolls[cmd]
	if !ok {
		return false, nil
	}
	t, c := m.h.Text(), m.h.Cursor()
	top, h, _ := m.h.Viewport()
	top, line, moved := sk.run(m, scrollCall{t, cmd, count, max(count, 1), max(h, 1), top, LineStart(t, c), LineStart(t, t.Len())})
	if !moved {
		return true, nil
	}
	m.h.SetTop(top)
	switch {
	case cmd == "z." || cmd == "z<CR>" || cmd == "z-":
		m.h.SetCursor(firstNonBlank(t, line, LineEnd(t, line)))
	case line != LineStart(t, c):
		m.h.SetCursor(m.toLine(t, c, line))
	}
	return true, nil
}

// window returns the line H, M or L moves to: the n-th from the top or
// bottom of the view, or its middle.
func (m *Machine) window(t Text, cmd string, n int) int {
	top, h, _ := m.h.Viewport()
	bottom := down(t, top, max(h, 1)-1)
	switch cmd {
	case "H":
		return min(down(t, top, n-1), bottom)
	case "L":
		return max(up(t, bottom, n-1), top)
	}
	lines := 0
	for p := top; p < bottom; p = LineEnd(t, p) + 1 {
		lines++
	}
	return down(t, top, lines/2)
}
