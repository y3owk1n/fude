package vim_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/vim"
)

// shown returns h's text with | at the cursor and * at each other cursor.
func shown(h *host, m *vim.Machine) string {
	at := m.Cursors()
	var b strings.Builder
	for i := 0; i <= len(h.text); i++ {
		if i == h.cursor {
			b.WriteByte('|')
		}
		if slices.Contains(at, i) && i != h.cursor {
			b.WriteByte('*')
		}
		if i < len(h.text) {
			b.WriteByte(h.text[i])
		}
	}
	return b.String()
}

// cursorCase is keys fed to start, and the text and cursors they make.
type cursorCase struct{ start, keys, want string }

// checkCursors feeds each case's keys and checks what they make. The error
// of a command that fails at one cursor, as p with nothing in its register,
// is the message the case does not check.
func checkCursors(t *testing.T, cases []cursorCase) {
	t.Helper()
	for _, c := range cases {
		h := &host{text: strings.Replace(c.start, "|", "", 1), cursor: strings.Index(c.start, "|")}
		m := vim.New(h)
		for _, k := range vim.Keys(c.keys) {
			_ = m.Feed(k)
		}
		if got := shown(h, m); got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}

const (
	three = "|foo bar\nfoo baz\nfoo qux"
	// mixed has an indent, an empty line, punctuation and a tab.
	mixed  = "|  foo bar\n\nqux.quux(baz)\n\tcorge grault\nend"
	spaced = "|foo bar baz\nqux quux\nab\ncorge grault garply"
)

// B392
func TestB392CursorsRepeatEdits(t *testing.T) {
	checkCursors(t, []cursorCase{
		// An edit runs at every cursor, a motion only at the cursor.
		{three, "QjQjx", "*oo bar\n*oo baz\n|oo qux"},
		{three, "Qjjx", "*oo bar\nfoo baz\n|oo qux"},
		{three, "Qjwx", "*oo bar\nfoo |az\nfoo qux"},
		// Each cursor's own word.
		{"|a bb\nccc d", "Qjdiw", "* bb\n| d"},
		// Q toggles, 1Q only adds, 2Q only removes.
		{three, "QjkQ", "|foo bar\nfoo baz\nfoo qux"},
		{three, "Q1Qx", "|oo bar\nfoo baz\nfoo qux"},
		{three, "2Qx", "|oo bar\nfoo baz\nfoo qux"},
		// A cursor where the cursor is moves with it and edits once.
		{three, "QjQkx", "|oo bar\n*oo baz\nfoo qux"},
		// . repeats at every cursor.
		{three, "Qjx.", "*o bar\n|o baz\nfoo qux"},
		// One that fails at a cursor, as P with nothing yanked on the empty
		// line, leaves it.
		{mixed, "zqjyiwP", " |   foo bar\n*\nqu*xqux.quux(baz)\n*\t\tcorge grault\nen*dend"},
	})
	// A mapping's keys run at each cursor, but its motions do not.
	_, h, m := run(t, three, "")
	m.Map(vim.Normal, "K", "Gdd")
	for _, k := range vim.Keys("QjK") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "foo baz" {
		t.Errorf("K mapped to Gdd at two cursors left %q", h.text)
	}
	// What every cursor did is one undo step.
	if _, h, _ := run(t, three, "QjQjx"); h.steps != 1 {
		t.Errorf("x at three cursors made %d undo steps", h.steps)
	}
}

// B393
func TestB393InsertAtCursors(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "QjQjA!<Esc>", "foo bar*!\nfoo baz*!\nfoo qux|!"},
		// <C-w> takes each cursor's own word.
		{"|a\nbbb", "QjA xy<C-w>z<Esc>", "a *z\nbbb |z"},
		{three, "QjA<CR>z<Esc>", "foo bar\n*z\nfoo baz\n|z\nfoo qux"},
		// A cursor that cannot start insert mode is removed.
		{"|a;b\ncd\ne;f", "QjQjct;X<Esc>", "*X;b\ncd\n|X;f"},
		// <C-o> runs its command as normal mode does, and typing goes on.
		{"|ab\ncd", "QjiX<C-o>lY<Esc>", "X*Yab\nXc|Yd"},
	})
}

// B393, as a completion menu chooses
func TestB393Complete(t *testing.T) {
	_, h, m := run(t, "|a b\ncd e", "QjAx")
	if err := m.Complete(2, "xyz"); err != nil {
		t.Fatal(err)
	}
	if got, want := shown(h, m), "a xyz*\ncd xyz|"; got != want {
		t.Errorf("a completion at two cursors made %q, want %q", got, want)
	}
}

// B394
func TestB394VisualAtCursors(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "QjQjvex", "* bar\n* baz\n| qux"},
		{three, "QjQjve<Esc>x", "fo* bar\nfo* baz\nfo| qux"},
	})
}

// B395
func TestB395OwnRegisters(t *testing.T) {
	checkCursors(t, []cursorCase{
		// Each yanks and puts its own.
		{three, "QjQjwyiwp", "ffo*ooo bar\nffo*ooo baz\nfoo qqu|xux"},
	})
	_, _, m := run(t, three, "QjQjwyiw<C-l>")
	if r, _, _ := m.Register('"'); r != "foo\nfoo\nqux\n" {
		t.Errorf("<C-l> after yanks at three cursors left %q in \"", r)
	}
	_, _, m = run(t, three, `QjQjw"ayiw<C-l>`)
	if r, _, _ := m.Register('a'); r != "foo\nfoo\nqux\n" {
		t.Errorf("<C-l> after yanks at three cursors left %q in a", r)
	}
	if r, _, _ := m.Register('0'); r != "" {
		t.Errorf("yanks into a wrote %q into 0", r)
	}
	// With the clipboard on, each still puts its own, and the clipboard gets
	// them all once they are removed.
	h := &host{text: strings.TrimPrefix(three, "|")}
	m = vim.New(h)
	m.UseClipboard(true)
	for _, k := range vim.Keys("QjQjwyiwp") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := shown(h, m), "ffo*ooo bar\nffo*ooo baz\nfoo qqu|xux"; got != want {
		t.Errorf("yiwp at three cursors with the clipboard made %q, want %q", got, want)
	}
	if err := m.Feed("<C-l>"); err != nil || h.clip != "foo\nfoo\nqux\n" {
		t.Errorf("<C-l> left %q on the clipboard, %v", h.clip, err)
	}
	// A cursor where the yank fails, on the empty line, stops none after it.
	_, _, m = run(t, mixed, "zqjyiw<C-l>")
	if r, _, _ := m.Register('"'); r != "  \nqux\n\t\nend\n" {
		t.Errorf("<C-l> after yiw on every line left %q in \"", r)
	}
}

// B396
func TestB396Follow(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "QjQjq=wx", "foo *ar\nfoo *az\nfoo |ux"},
		{three, "QjQjq=q=wx", "*oo bar\n*oo baz\nfoo |ux"},
		{three, "QjQj1q=Gx", "*oo bar\n*oo baz\n|oo qux"},
		// Cursors that land in one place become one.
		{three, "QjQj1q=gg9j", "foo bar\nfoo baz\n|foo qux"},
		// Q sets follow off, and zq leaves it.
		{three, "1q=QjQjwx", "*oo bar\n*oo baz\nfoo |ux"},
		{three, "1q=zqjwx", "foo |ar\nfoo *az\nfoo *ux"},
		// A motion that fails at the cursor, the last w, moves none.
		{mixed, "QjjQjq=wwwww", "  foo bar\n\n*qux.quux(*baz)\n\tcorge grault\nen|d"},
		// One that lands where the cursor is becomes it.
		{mixed, "QjjQjq=})", "  foo bar\n\n*qux.quux(baz)\n\tcorge grault\nen|d"},
		{mixed, "QjjQjq=kkk", mixed},
	})
}

// B397
func TestB397ClearAndRestore(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "QjQj<C-l>x", "foo bar\nfoo baz\n|oo qux"},
		{three, "QjQj<C-l>gggQ", "*foo bar\n*foo baz\n|foo qux"},
		{three, "QjQj<C-l>QgQ", "foo bar\nfoo baz\n|foo qux"},
		{three, "Qjx<C-l>gQx", "*o bar\n|o baz\nfoo qux"},
	})
}

// B398
func TestB398PlaceAlong(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "zqj", "|foo bar\n*foo baz\n*foo qux"},
		{three, "zqwiX<Esc>", "|Xfoo *Xbar\n*Xfoo *Xbaz\n*Xfoo *Xqu*Xx"},
		{three, "jVjzqw", "foo bar\n*foo *baz\n|foo *qu*x"},
		{"|ab x ab\nab", "zq*", "|ab x *ab\n*ab"},
		{"|ab x ab\nab", "zq*gvcZ<Esc>", "|Z x *Z\n*Z"},
		{"|ab x ab\nab", "/b<CR>zqn", "a|b x a*b\na*b"},
		{three, "wVjjQ", "foo *bar\nfoo *baz\nfoo |qux"},
		{three, "wVjjQx", "foo *ar\nfoo *az\nfoo |ux"},
		// From a tab, the column is its last.
		{mixed, "VjjjQ", "  foo b*ar\n*\nqux.quu*x(baz)\n|\tcorge grault\nend"},
		// f and t skip the start but at the start of the text, a t next to
		// its character goes past it, and F places none. The cursor goes to
		// the first placed.
		{spaced, "wzqf ", "foo bar| baz\nqux quux\nab\ncorge grault garply"},
		{spaced, "zqt ", "|fo*o ba*r baz\nqux quux\nab\ncorge grault garply"},
		{spaced, "zqF ", spaced},
		{spaced, "Vjjjzqf ", "|foo* bar* baz\nqux quux\nab\ncorge grault garply"},
		{spaced, "zq/a<CR>", "foo b|ar b*az\nqux quux\n*ab\ncorge gr*ault g*arply"},
	})
}

// B399
func TestB399ToCursor(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "QjQj]C", "|foo bar\n*foo baz\nfoo qux"},
		{three, "QjQj2]C", "*foo bar\n|foo baz\nfoo qux"},
		{three, "QjQj[C", "*foo bar\n|foo baz\nfoo qux"},
		{three, "QjQj]CQ", "|foo bar\n*foo baz\nfoo qux"},
		{three, "QjQj<C-l>ggd]C", "|foo baz\nfoo qux"},
	})
}

// B400
func TestB400Number(t *testing.T) {
	checkCursors(t, []cursorCase{
		{three, "QjQjg<C-a>", "1*foo bar\n2*foo baz\n3|foo qux"},
		{three, "QjQj5g<C-a>", "5*foo bar\n6*foo baz\n7|foo qux"},
	})
}

// B401
func TestB401Selections(t *testing.T) {
	_, _, m := run(t, three, "QjQjve")
	s, _ := m.Selection()
	if want := [][2]int{{0, 3}, {8, 11}, {16, 19}}; !slices.Equal(s.Regions, want) {
		t.Errorf("ve at three cursors selected %v, want %v", s.Regions, want)
	}
}
