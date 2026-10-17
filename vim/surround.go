package vim

import "strings"

// Surround makes ys{motion}{char}, yss{char}, S{char} in visual mode,
// ds{char} and cs{old}{new} add, delete and change the brackets or quotes
// around text (B219).
func (m *Machine) Surround(on bool) { m.surround = on }

// charWait is a command waiting for the characters typed after it.
type charWait struct {
	n   int
	got []string
	run func(chars []string) error
}

// takeChar gives k to the command that waits for characters. A key that
// is not a character, such as <Esc>, cancels it.
func (m *Machine) takeChar(k Key) error {
	w := m.wait
	if !k.isChar() {
		m.wait = nil
		return nil
	}
	if w.got = append(w.got, string(k)); len(w.got) < w.n {
		return nil
	}
	m.wait = nil
	return w.run(w.got)
}

// surrounding returns the text that goes before and after what the key ch
// names surrounds: a pair of brackets, with a space inside each when ch is
// the opening one, or ch itself on both sides.
func surrounding(ch string) (string, string) {
	switch ch {
	case ")", "b":
		return "(", ")"
	case "(":
		return "( ", " )"
	case "]", "r":
		return "[", "]"
	case "[":
		return "[ ", " ]"
	case "}", "B":
		return "{", "}"
	case "{":
		return "{ ", " }"
	case ">", "<", "a":
		return "<", ">"
	}
	return ch, ch
}

// surroundRegion puts what ch names around r. A linewise r is its lines'
// text from the first non-blank, as yss surrounds the line.
func (m *Machine) surroundRegion(r region, ch, label string) error {
	t := m.h.Text()
	start, end := r.start, r.end
	if r.lines {
		start, end = firstNonBlank(t, start, LineEnd(t, start)), LineEnd(t, max(end-1, start))
	}
	open, close := surrounding(ch)
	if err := m.apply([]Edit{{Start: start, End: start, Text: open}, {Start: end, End: end, Text: close}}, label); err != nil {
		return err
	}
	m.h.SetCursor(start)
	return nil
}

// resurround deletes the brackets or quotes that old names around the
// cursor, with the blanks inside them when old is an opening bracket, and
// puts what new names in their place, for ds and cs.
func (m *Machine) resurround(old, new string, label string) error {
	t, c := m.h.Text(), m.h.Cursor()
	open, close := surrounding(old)
	o, cl := strings.TrimSpace(open), strings.TrimSpace(close)
	if o == "" || cl == "" {
		m.beep = true
		return nil
	}
	var start, end int
	if o == cl {
		s, e, ok := quote(t, c, o[0], true, false)
		if !ok {
			m.beep = true
			return nil
		}
		start, end = s, e
	} else {
		r, ok := block(t, c, 1, o[0], cl[0], true)
		if !ok {
			m.beep = true
			return nil
		}
		start, end = r.start, r.end
	}
	in, out := start+1, end-1
	if open != o {
		for in < out && strings.Contains(" \t", t.Slice(in, in+1)) {
			in++
		}
		for out > in && strings.Contains(" \t", t.Slice(out-1, out)) {
			out--
		}
	}
	before, after := "", ""
	if new != "" {
		before, after = surrounding(new)
	}
	if err := m.apply([]Edit{{Start: start, End: in, Text: before}, {Start: out, End: end, Text: after}}, label); err != nil {
		return err
	}
	m.h.SetCursor(start)
	return nil
}
