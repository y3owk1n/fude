package vim

// keymap is a trie of the mappings of one mode. A node with end set completes
// a mapping, which runs the keys in to.
type keymap struct {
	to   []Key
	end  bool
	next map[Key]*keymap
}

// Keymaps are mappings by mode: the machine's own, or a kind of buffer's,
// which the host lays over them while such a buffer is current (B142).
type Keymaps struct {
	modes map[Mode]*keymap
}

// Map makes typing from in mode run to instead. mode is Normal, Visual or
// Insert, and Insert mappings apply in replace mode too. The machine never
// maps the keys a mapping runs, and <Nop> runs nothing.
func (m *Machine) Map(mode Mode, from, to string) { m.maps.Map(mode, from, to) }

// Unmap removes the mapping of from in mode.
func (m *Machine) Unmap(mode Mode, from string) { m.maps.Unmap(mode, from) }

// UseLocal lays local over the machine's own mappings: where both map the
// keys typed, the longer mapping runs, and local's on a tie. nil takes it
// away.
func (m *Machine) UseLocal(local *Keymaps) { m.local = local }

// Map makes typing from in mode run to instead.
func (ks *Keymaps) Map(mode Mode, from, to string) {
	keys := Keys(from)
	if len(keys) == 0 {
		return
	}
	if ks.modes == nil {
		ks.modes = map[Mode]*keymap{}
	}
	if ks.modes[mode] == nil {
		ks.modes[mode] = &keymap{}
	}
	n := ks.modes[mode]
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
func (ks *Keymaps) Unmap(mode Mode, from string) {
	ks.modes[mode].unmap(Keys(from))
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

// keymaps returns the local and the machine's mappings that apply to the
// next key. None do while a command waits for a character or an operator
// waits for its target.
func (m *Machine) keymaps() (local, own *keymap) {
	var mode Mode
	switch {
	case m.pending != "":
		return nil, nil
	case m.mode == Normal && m.op == "":
		mode = Normal
	case m.mode == Visual:
		mode = Visual
	case m.mode == Insert, m.mode == Replace:
		mode = Insert
	default:
		return nil, nil
	}
	if m.local != nil {
		local = m.local.modes[mode]
	}
	return local, m.maps.modes[mode]
}

// walk returns the node keys lead to from n, or nil.
func (n *keymap) walk(keys []Key) *keymap {
	for _, k := range keys {
		if n == nil {
			return nil
		}
		n = n.next[k]
	}
	return n
}

// press runs a typed key through the keymap. It holds the key while the held
// keys start a longer mapping.
func (m *Machine) press(k Key) error {
	m.held = append(m.held, k)
	local, own := m.keymaps()
	if n := local.walk(m.held); n != nil {
		// A local mapping runs at once, without waiting for a longer one of
		// the machine's, as Neovim's <nowait> ones do (N17).
		if n.end {
			return m.flush()
		}
		return nil
	}
	if n := own.walk(m.held); n != nil && n.next != nil {
		return nil
	}
	return m.flush()
}

// flush runs the longest mapping the held keys start with, or else the first
// held key as typed, then presses the keys after it again.
func (m *Machine) flush() error {
	keys := m.held
	m.held = nil
	n, to := 0, keys[:1] // the keys the longest mapping takes, and what it runs
	local, own := m.keymaps()
	for _, node := range []*keymap{local, own} {
		for i := 0; i < len(keys) && node != nil; i++ {
			if node = node.next[keys[i]]; node != nil && node.end && i+1 > n {
				n, to = i+1, node.to
			}
		}
	}
	n = max(n, 1)
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
