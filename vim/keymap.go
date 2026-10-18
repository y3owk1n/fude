package vim

import (
	"maps"
	"slices"
	"strings"
)

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

// Each calls f with each of the machine's own mappings in mode, in the
// order of their keys.
func (m *Machine) Each(mode Mode, f func(from, to string)) { m.maps.Each(mode, f) }

// Each calls f with each mapping in mode, in the order of their keys.
func (ks *Keymaps) Each(mode Mode, f func(from, to string)) {
	var walk func(n *keymap, from string)
	walk = func(n *keymap, from string) {
		if n.end {
			f(from, keysText(n.to))
		}
		for _, k := range slices.Sorted(maps.Keys(n.next)) {
			walk(n.next[k], from+keysText([]Key{k}))
		}
	}
	if n := ks.modes[mode]; n != nil {
		walk(n, "")
	}
}

// Notation returns keys written as mappings list them, such as <Space>w
// for "<space>w" or " w".
func Notation(keys string) string { return keysText(Keys(keys)) }

// keysText is keys as a mapping writes them, with a space and a tab named
// so they show.
func keysText(keys []Key) string {
	var b strings.Builder
	for _, k := range keys {
		switch k {
		case " ":
			b.WriteString("<Space>")
		case "\t":
			b.WriteString("<Tab>")
		default:
			b.WriteString(string(k))
		}
	}
	return b.String()
}

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
	case m.pending != "" || m.wait != nil:
		// A key a command waits for, such as f's or ds's character, is
		// never mapped.
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
		// the machine's (N17).
		if n.end {
			return m.flush()
		}
		return nil
	}
	if n := own.walk(m.held); n != nil && n.next != nil {
		// In insert mode a character that may start a mapping, such as the
		// j of jk, types at once, and the mapping deletes it again when it
		// comes (B229).
		if m.ahead == len(m.held)-1 && m.typesAhead(k) {
			if err := m.feed(k); err != nil {
				return err
			}
			m.ahead++
		}
		return nil
	}
	return m.flush()
}

// typesAhead reports whether k, held for a longer mapping, types at once:
// a character other than a bracket or quote, in insert mode with one
// cursor.
func (m *Machine) typesAhead(k Key) bool {
	return m.mode == Insert && k.isChar() && len(m.cursors) == 0 && m.blockInsert == nil && !strings.Contains("()[]{}\"'`", string(k))
}

// flush runs the longest mapping the held keys start with, or else the first
// held key as typed, then presses the keys after it again.
func (m *Machine) flush() error {
	keys, ahead := m.held, m.ahead
	m.held, m.ahead = nil, 0
	n, to := 0, keys[:1] // the keys the longest mapping takes, and what it runs
	local, own := m.keymaps()
	for _, node := range []*keymap{local, own} {
		for i := 0; i < len(keys) && node != nil; i++ {
			if node = node.next[keys[i]]; node != nil && node.end && i+1 > n {
				n, to = i+1, node.to
			}
		}
	}
	switch {
	case ahead > 0 && n == 0:
		// The keys typed ahead stay, and the rest run.
		for _, k := range keys[ahead:] {
			if err := m.press(k); err != nil {
				return err
			}
		}
		return nil
	case ahead > 0:
		// A mapping came, so the keys typed ahead go before it runs.
		if err := m.untype(keys[:ahead]); err != nil {
			return err
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

// untype deletes the text keys typed ahead before the cursor, and forgets
// them for dot repeat.
func (m *Machine) untype(keys []Key) error {
	var typed strings.Builder
	for _, k := range keys {
		typed.WriteString(string(k))
	}
	c := m.h.Cursor()
	start := max(c-typed.Len(), 0)
	if err := m.apply([]Edit{{Start: start, End: c}}, m.label); err != nil {
		return err
	}
	m.h.SetCursor(start)
	m.keys = m.keys[:max(len(m.keys)-len(keys), 0)]
	return nil
}
