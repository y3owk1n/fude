package vim

import (
	"cmp"
	"regexp"
	"slices"
	"strings"
)

// Hint is a key that may come next while a command is being typed. Keys
// are those typed so far and it. To is what a mapping runs, and "" for the
// machine's own keys. More counts the keys that go on past it.
type Hint struct {
	Key, Keys, Help, To string
	More                int
}

// candidate is keys that may be typed, what they do, and the placeholder
// that follows them, such as {char}.
type candidate struct {
	keys      []Key
	help, to  string
	following string
}

// placeholder is what stands for typed text in notation such as f{char},
// <C-w>{key} or /{pattern}<CR>.
var placeholder = regexp.MustCompile(`\{[a-z-]+\}`)

// Hints returns the keys that may follow those typed so far in normal or
// visual mode, from the machine's own keys, the host's extra ones and the
// mappings, or nil when no command is being typed (B172).
func (m *Machine) Hints(extra []Binding) []Hint {
	var prefix []Key
	var kinds []string
	switch {
	case m.mode == Visual:
		kinds = []string{"visual", "motion", "object", "operator"}
	case m.mode == Normal && m.op != "":
		kinds = []string{"motion", "object"}
	case m.mode == Normal:
		kinds = []string{"normal", "motion", "operator"}
	default:
		return nil
	}
	switch {
	case len(m.held) > 0:
		prefix = m.held
	case m.pending != "":
		prefix = []Key{m.pending}
	case m.op == "":
		return nil
	}
	var cands []candidate
	seen := map[string]bool{}
	add := func(c candidate) {
		if id := keysText(c.keys) + c.following; !seen[id] {
			seen[id] = true
			cands = append(cands, c)
		}
	}
	// Mappings come first, so one wins over the machine's key it shadows.
	local, own := m.keymaps()
	for _, n := range []*keymap{local, own} {
		var walk func(n *keymap, keys []Key)
		walk = func(n *keymap, keys []Key) {
			if n.end {
				add(candidate{keys: keys, to: keysText(n.to)})
			}
			for k, next := range n.next {
				walk(next, append(slices.Clip(keys), k))
			}
		}
		if n != nil {
			walk(n, nil)
		}
	}
	if m.op != "" {
		// An operator typed twice acts on the line.
		add(candidate{keys: Keys(m.op), help: "acts on the line"})
	}
	for _, b := range append(Bindings(), extra...) {
		if !slices.Contains(kinds, b.Kind) {
			continue
		}
		notation, following := b.Keys, ""
		if loc := placeholder.FindStringIndex(notation); loc != nil {
			notation, following = notation[:loc[0]], notation[loc[0]:]
		}
		if notation != "" {
			add(candidate{keys: Keys(notation), help: b.Help, following: following})
		}
	}
	return nextKeys(prefix, cands)
}

// nextKeys groups the candidates that start with prefix by the key after
// it.
func nextKeys(prefix []Key, cands []candidate) []Hint {
	byKey := map[string]*Hint{}
	var out []*Hint
	hint := func(key string) *Hint {
		if h := byKey[key]; h != nil {
			return h
		}
		h := &Hint{Key: key, Keys: keysText(prefix) + key}
		byKey[key] = h
		out = append(out, h)
		return h
	}
	for _, c := range cands {
		if len(c.keys) < len(prefix) || !slices.Equal(c.keys[:len(prefix)], prefix) {
			continue
		}
		rest := c.keys[len(prefix):]
		switch {
		case len(rest) == 0 && c.following != "":
			h := hint(c.following)
			h.Help, h.To = cmp.Or(h.Help, c.help), cmp.Or(h.To, c.to)
		case len(rest) == 1:
			h := hint(keysText(rest))
			if h.Help == "" && h.To == "" {
				h.Help, h.To = c.help, c.to
			}
		case len(rest) > 1:
			hint(keysText(rest[:1])).More++
		}
	}
	// A placeholder such as {key} shows only when no key in full does.
	if slices.ContainsFunc(out, func(h *Hint) bool { return !placeholder.MatchString(h.Key) }) {
		out = slices.DeleteFunc(out, func(h *Hint) bool { return placeholder.MatchString(h.Key) })
	}
	slices.SortFunc(out, func(a, b *Hint) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Key), strings.ToLower(b.Key)), strings.Compare(a.Key, b.Key))
	})
	hints := make([]Hint, len(out))
	for i, h := range out {
		hints[i] = *h
	}
	return hints
}

// Back drops the last key of the command being typed, as <BS> does while
// hints show (B172).
func (m *Machine) Back() {
	switch {
	case len(m.held) > 0:
		m.held = m.held[:len(m.held)-1]
		m.ahead = min(m.ahead, len(m.held))
	case m.pending != "":
		m.pending = ""
	case m.op != "":
		m.op, m.opCount = "", 0
	default:
		m.Cancel()
		return
	}
	if len(m.shown) > 0 {
		m.shown = m.shown[:len(m.shown)-1]
	}
}

// Cancel drops the command being typed and the held keys, running none of
// them, as <Esc> does while hints show (B172).
func (m *Machine) Cancel() {
	m.held, m.ahead, m.pending, m.op, m.count, m.opCount, m.reg, m.shown = nil, 0, "", "", 0, 0, 0, nil
}
