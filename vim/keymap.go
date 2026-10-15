package vim

// keymap is a trie of the mappings of one mode. A node with end set completes
// a mapping, which runs the keys in to.
type keymap struct {
	to   []Key
	end  bool
	next map[Key]*keymap
}

// Map makes typing from in mode run to instead. mode is Normal, Visual or
// Insert, and Insert mappings apply in replace mode too. The machine never
// maps the keys a mapping runs.
func (m *Machine) Map(mode Mode, from, to string) {
	keys := Keys(from)
	if len(keys) == 0 {
		return
	}
	if m.maps == nil {
		m.maps = map[Mode]*keymap{}
	}
	if m.maps[mode] == nil {
		m.maps[mode] = &keymap{}
	}
	n := m.maps[mode]
	for _, k := range keys {
		if n.next == nil {
			n.next = map[Key]*keymap{}
		}
		if n.next[k] == nil {
			n.next[k] = &keymap{}
		}
		n = n.next[k]
	}
	n.to, n.end = Keys(to), true
}

// Unmap removes the mapping of from in mode, and with it any prefix only it
// used, so typing that prefix no longer waits.
func (m *Machine) Unmap(mode Mode, from string) {
	m.maps[mode].unmap(Keys(from))
}

// unmap removes keys from below n, and reports whether n is left with
// nothing, for its parent to drop it.
func (n *keymap) unmap(keys []Key) bool {
	if n == nil {
		return false
	}
	if len(keys) == 0 {
		n.to, n.end = nil, false
	} else if child := n.next[keys[0]]; child.unmap(keys[1:]) {
		delete(n.next, keys[0])
	}
	return !n.end && len(n.next) == 0
}

// Waiting reports whether typed keys are held because they start a longer
// mapping. The host calls Timeout if no key comes in time.
func (m *Machine) Waiting() bool { return len(m.held) > 0 }

// Timeout runs the held keys as if no longer mapping could follow.
func (m *Machine) Timeout() error {
	m.beep, m.msg = false, ""
	if len(m.held) == 0 {
		return nil
	}
	return m.flush()
}

// keymap returns the mappings that apply to the next key. None do while a
// command waits for a character or an operator waits for its target.
func (m *Machine) keymap() *keymap {
	switch {
	case m.pending != "":
		return nil
	case m.mode == Normal && m.op == "":
		return m.maps[Normal]
	case m.mode == Visual:
		return m.maps[Visual]
	case m.mode == Insert, m.mode == Replace:
		return m.maps[Insert]
	}
	return nil
}

// press runs a typed key through the keymap. It holds the key while the held
// keys start a longer mapping.
func (m *Machine) press(k Key) error {
	m.held = append(m.held, k)
	n := m.keymap()
	for _, k := range m.held {
		if n == nil {
			break
		}
		n = n.next[k]
	}
	if n != nil && n.next != nil {
		return nil
	}
	return m.flush()
}

// flush runs the longest mapping the held keys start with, or else the first
// held key as typed, then presses the keys after it again.
func (m *Machine) flush() error {
	keys := m.held
	m.held = nil
	n, to := 1, keys[:1]
	node := m.keymap()
	for i := 0; i < len(keys) && node != nil; i++ {
		if node = node.next[keys[i]]; node != nil && node.end {
			n, to = i+1, node.to
		}
	}
	if err := m.run(to, false); err != nil {
		return err
	}
	for _, k := range keys[n:] {
		if err := m.press(k); err != nil {
			return err
		}
	}
	return nil
}
