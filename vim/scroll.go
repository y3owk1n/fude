package vim

// scroll runs a command that moves the view, or reports false. The cursor
// keeps its column. It moves only as far as it must to stay in the view,
// except that <C-d> and <C-u> move it with the view.
func (m *Machine) scroll(cmd string, count int) (bool, error) {
	t, c := m.h.Text(), m.h.Cursor()
	top, h, _ := m.h.Viewport()
	h = max(h, 1)
	n := max(count, 1)
	line, last := LineStart(t, c), LineStart(t, t.Len())
	switch cmd {
	case "<C-e>":
		top = down(t, top, n)
		line = max(line, top)
	case "<C-y>":
		top = up(t, top, n)
		line = min(line, down(t, top, h-1))
	case "<C-d>", "<C-u>":
		s := h / 2
		if count > 0 {
			s = count
		}
		switch {
		case cmd == "<C-d>" && line == last, cmd == "<C-u>" && line == 0:
			m.beep = true
			return true, nil
		case cmd == "<C-d>":
			// The view stops once the last line reaches the bottom.
			top = max(top, min(down(t, top, s), up(t, last, h-1)))
			line = down(t, line, s)
		default:
			top, line = up(t, top, s), up(t, line, s)
		}
	case "<C-f>":
		for range n {
			top = down(t, top, max(h-2, 1))
		}
		line = max(line, top)
	case "<C-b>":
		for range n {
			top = up(t, top, max(h-2, 1))
		}
		line = min(line, down(t, top, h-1))
	case "zz":
		top = up(t, line, (h-1)/2)
	case "zt":
		top = line
	case "zb":
		top = up(t, line, h-1)
	default:
		return false, nil
	}
	m.h.SetTop(top)
	if line != LineStart(t, c) {
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
