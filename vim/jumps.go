package vim

// history is the jumplist or the changelist: positions kept as host marks so
// they follow edits, and how far back <C-o> or g; has gone.
type history struct {
	marks []rune
	at    int // the entry last moved to, or len(marks) when at the newest end
}

// historyMark is the first of the private mark names the lists use.
const historyMark = 0xF8000

// isJump reports whether a motion is one the jumplist records.
func isJump(cmd string) bool {
	switch cmd {
	case "G", "gg", "%", "(", ")", "{", "}", "n", "N", "*", "#", "H", "M", "L":
		return true
	}
	return len(cmd) > 1 && (cmd[0] == '/' || cmd[0] == '?' || cmd[0] == '`' || cmd[0] == '\'')
}

// remember returns a new private mark at pos.
func (m *Machine) remember(pos int) rune {
	m.marksMade++
	name := historyMark + rune(m.marksMade)
	m.h.SetMark(name, pos)
	return name
}

// pushJump adds pos to the jumplist, dropping any entry on the same line.
func (m *Machine) pushJump(pos int) {
	h := &m.jumps
	t := m.h.Text()
	line := LineStart(t, pos)
	var kept []rune
	for _, r := range h.marks {
		if p, ok := m.h.Mark(r); ok && LineStart(t, p) != line {
			kept = append(kept, r)
		}
	}
	h.marks, h.at = append(kept, m.remember(pos)), len(kept)+1
}

// pushChange adds pos to the changelist, replacing the newest entry when it
// is on the same line.
func (m *Machine) pushChange(pos int) {
	h := &m.changes
	t := m.h.Text()
	if n := len(h.marks); n > 0 {
		if p, ok := m.h.Mark(h.marks[n-1]); ok && LineStart(t, p) == LineStart(t, pos) {
			m.h.SetMark(h.marks[n-1], pos)
			h.at = n
			return
		}
	}
	h.marks = append(h.marks, m.remember(pos))
	h.at = len(h.marks)
}

// walk moves n entries back through h, or forward when n is negative. The
// first step back through the jumplist records where the cursor is, so <C-i>
// can return to it.
func (m *Machine) walk(h *history, n int) {
	if h == &m.jumps && n > 0 && h.at == len(h.marks) {
		m.pushJump(m.h.Cursor())
		h.at = len(h.marks) - 1
	}
	i := h.at - n
	if i < 0 || i >= len(h.marks) {
		m.beep = true
		return
	}
	p, ok := m.h.Mark(h.marks[i])
	if !ok {
		m.beep = true
		return
	}
	h.at = i
	m.h.SetCursor(clamp(m.h.Text(), p))
}
