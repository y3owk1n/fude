package vim

// isJump reports whether a motion is one the jumplist records.
func isJump(cmd string) bool {
	switch cmd {
	case "G", "gg", "%", "(", ")", "{", "}", "n", "N", "*", "#", "H", "M", "L":
		return true
	}
	return len(cmd) > 1 && (cmd[0] == '/' || cmd[0] == '?' || cmd[0] == '`' || cmd[0] == '\'')
}

// walk moves n entries back through the jumplist or the changelist, or
// forward when n is negative, and beeps at either end.
func (m *Machine) walk(step func(int) bool, n int) {
	if !step(n) {
		m.beep = true
		return
	}
	m.h.SetCursor(clamp(m.h.Text(), m.h.Cursor()))
}
