package vim_test

import (
	"strings"
	"testing"

	"github.com/y3owk1n/fude/vim"
)

// host is the minimal host over a string. It counts undo steps the way the
// kernel makes nodes.
type host struct {
	text    string
	cursor  int
	depth   int
	applied bool
	steps   int
}

type text string

func (t text) Len() int                    { return len(t) }
func (t text) Slice(start, end int) string { return string(t[start:end]) }

func (h *host) Text() vim.Text    { return text(h.text) }
func (h *host) Cursor() int       { return h.cursor }
func (h *host) SetCursor(pos int) { h.cursor = pos }
func (h *host) BeginGroup()       { h.depth++ }
func (h *host) Unhandled(string)  {}
func (h *host) EndGroup() {
	h.depth--
	if h.depth == 0 && h.applied {
		h.steps++
		h.applied = false
	}
}

func (h *host) Apply(edits []vim.Edit, _ string) error {
	for i := len(edits) - 1; i >= 0; i-- {
		e := edits[i]
		h.text = h.text[:e.Start] + e.Text + h.text[e.End:]
	}
	if h.depth == 0 {
		h.steps++
	} else {
		h.applied = true
	}
	return nil
}

// run feeds keys to a host built from "ab|c", where | marks the cursor, and
// returns the result in the same form.
func run(t *testing.T, start, keys string) (string, *host, *vim.Machine) {
	t.Helper()
	h := &host{text: strings.Replace(start, "|", "", 1), cursor: strings.Index(start, "|")}
	m := vim.New(h)
	for _, k := range vim.Keys(keys) {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	return h.text[:h.cursor] + "|" + h.text[h.cursor:], h, m
}

// B6
func TestB6EnterAndLeaveInsert(t *testing.T) {
	cases := []struct{ start, keys, want string }{
		{"ab|c", "iX", "abX|c"},
		{"ab|c", "aX", "abcX|"},
		{"  ab|c", "IX", "  X|abc"},
		{"a|bc\nd", "AX", "abcX|\nd"},
		{"a|bc\nd", "oX", "abc\nX|\nd"},
		{"abc\nd|e", "OX", "abc\nX|\nde"},
	}
	for _, c := range cases {
		if got, _, m := run(t, c.start, c.keys); got != c.want || m.Mode() != vim.Insert {
			t.Errorf("%q on %q: got %q, want %q in insert mode", c.keys, c.start, got, c.want)
		}
	}
	if got, _, m := run(t, "ab|c", "iX<Esc>"); got != "ab|Xc" || m.Mode() != vim.Normal {
		t.Errorf("<Esc> should leave insert and step left: got %q", got)
	}
	if got, _, _ := run(t, "|abc", "i<Esc>"); got != "|abc" {
		t.Errorf("<Esc> at line start should not move: got %q", got)
	}
}

// B7
func TestB7InsertIsOneUndoStep(t *testing.T) {
	if got, h, _ := run(t, "|", "ihello<CR>world<BS><Esc>"); h.steps != 1 || got != "hello\nwor|l" {
		t.Errorf("got %q in %d steps, want one step", got, h.steps)
	}
	if got, h, _ := run(t, "|", "ihe<Left>llo<Esc>"); h.steps != 2 || got != "hll|oe" {
		t.Errorf("<Left> should split the step: got %q in %d steps", got, h.steps)
	}
}

// B9
func TestB9GraphemeClusters(t *testing.T) {
	cases := []struct{ start, keys, want string }{
		{"|a👍🏽b", "l", "a|👍🏽b"},
		{"|a👍🏽b", "ll", "a👍🏽|b"},
		{"a👍🏽|b", "h", "a|👍🏽b"},
		{"|xéy", "ll", "xé|y"},
	}
	for _, c := range cases {
		if got, _, _ := run(t, c.start, c.keys); got != c.want {
			t.Errorf("%q on %q: got %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}
