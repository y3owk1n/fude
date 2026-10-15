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

func (h *host) Text() vim.Text         { return text(h.text) }
func (h *host) Cursor() int            { return h.cursor }
func (h *host) SetCursor(pos int)      { h.cursor = pos }
func (h *host) BeginGroup()            { h.depth++ }
func (h *host) Unhandled(string)       {}
func (h *host) Undo() (int, int, bool) { return 0, 0, false }
func (h *host) Redo() (int, int, bool) { return 0, 0, false }
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

// B8
func TestB8PasteIsTypedText(t *testing.T) {
	_, h, m := run(t, "a|b", "i")
	if err := m.Paste("x<Esc>"); err != nil {
		t.Fatal(err)
	}
	if h.text != "ax<Esc>b" || m.Mode() != vim.Insert {
		t.Errorf("insert-mode paste: got %q in mode %v", h.text, m.Mode())
	}
	_, h, m = run(t, "a|b", "")
	if err := m.Paste("dd"); err != nil {
		t.Fatal(err)
	}
	if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != "abd|d" || m.Mode() != vim.Normal {
		t.Errorf("normal-mode paste: got %q in mode %v", got, m.Mode())
	}
}

// B10
func TestB10Motions(t *testing.T) {
	cases(t, [][3]string{
		{"|abcdef", "3l", "abc|def"},
		{"|abcdef", "10l", "abcde|f"},
		{"abc|def", "2h", "a|bcdef"},
		{"abcd|ef\nab\nabcdef", "j", "abcdef\na|b\nabcdef"},
		{"abcd|ef\nab\nabcdef", "jj", "abcdef\nab\nabcd|ef"},
		{"abcd|ef\nab\nabcdef", "5j", "abcdef\nab\nabcd|ef"},
		{"abcdef\nab\nabcd|ef", "k", "abcdef\na|b\nabcdef"},
		{"\t|x\nabcdefghij", "j", "\tx\nabcdefgh|ij"},
		{"abcd|ef\nab\nabcdef", "G", "abcdef\nab\nabcd|ef"},
		{"abcdef\nab\nabcd|ef", "gg", "abcd|ef\nab\nabcdef"},
		{"abcd|ef\nab\nabcdef", "2G", "abcdef\na|b\nabcdef"},
		{"|foo bar.baz\n  qux", "w", "foo |bar.baz\n  qux"},
		{"|foo bar.baz\n  qux", "2w", "foo bar|.baz\n  qux"},
		{"|foo bar.baz\n  qux", "2W", "foo bar.baz\n  |qux"},
		{"|foo bar.baz\n  qux", "e", "fo|o bar.baz\n  qux"},
		{"|foo bar.baz\n  qux", "3E", "foo bar.baz\n  qu|x"},
		{"foo bar.baz\n  |qux", "b", "foo bar.|baz\n  qux"},
		{"foo bar.baz\n  |qux", "B", "foo |bar.baz\n  qux"},
		{"|a\n\nb", "w", "a\n|\nb"},
		{"a\n\n|b", "b", "a\n|\nb"},
		{"foo bar.baz\n  qu|x", "0", "foo bar.baz\n|  qux"},
		{"foo bar.baz\n  qu|x", "^", "foo bar.baz\n  |qux"},
		{"|foo bar.baz\n  qux", "$", "foo bar.ba|z\n  qux"},
		{"|foo bar.baz\n  qux", "2$", "foo bar.baz\n  qu|x"},
		{"|a,b,c,d", "f,", "a|,b,c,d"},
		{"|a,b,c,d", "2f,", "a,b|,c,d"},
		{"|a,b,c,d", "t,", "|a,b,c,d"},
		{"|a,b,c,d", "t,;", "a,|b,c,d"},
		{"|a,b,c,d", "f,;,", "a|,b,c,d"},
		{"a,b,c,|d", "F,", "a,b,c|,d"},
		{"a,b,c,|d", "2T,", "a,b,|c,d"},
	})
}

// cases runs each start, keys and want through run.
func cases(t *testing.T, cs [][3]string) {
	t.Helper()
	for _, c := range cs {
		if got, _, _ := run(t, c[0], c[1]); got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B11
func TestB11OperatorsWithMotions(t *testing.T) {
	cases(t, [][3]string{
		{"foo |bar baz", "dw", "foo |baz"},
		{"one |two\n  three", "dw", "one| \n  three"},
		{"|\nb", "dw", "|b"},
		{"one |two\nthree", "2dw", "one| "},
		{"foo |bar baz", "2d2w", "foo| "},
		{"foo |bar baz", "cwX<Esc>", "foo |X baz"},
		{"foo ba|r baz", "cwX<Esc>", "foo ba|X baz"},
		{"a |  b", "cwX<Esc>", "a |Xb"},
		{"foo |bar baz", "de", "foo | baz"},
		{"ab|c", "d$", "a|b"},
		{"ab|c", "2dh", "|c"},
		{"foo bar ba|z", "db", "foo bar |z"},
		{"foo |bar", "d0", "|bar"},
		{"a(b|c)", "dF(", "a|c)"},
		{"a|bc\nd\ne", "d2j", "|"},
		{"a|bc", "dj", "a|bc"},
		{"a\n  b|c\n  d", "dk", "  |d"},
		{"a|b\nc\nd", "cjX<Esc>", "|X\nd"},
	})
}

// B12
func TestB12LineOperators(t *testing.T) {
	cases(t, [][3]string{
		{"a\nb|cd\ne", "dd", "a\n|e"},
		{"ab\n  x|yz\nlast", "dd", "ab\nlas|t"},
		{"a\nbc|d", "dd", "|a"},
		{"a|b\nc\nd", "2dd", "|d"},
		{"  a|bc\nd", "ccX<Esc>", "  |X\nd"},
		{"ab|c\nd", "yyP", "|abc\nabc\nd"},
		{"ab|c", "yy3p", "abc\n|abc\nabc\nabc"},
	})
}

// B13
func TestB13TextObjects(t *testing.T) {
	cases(t, [][3]string{
		{"foo b|ar baz", "diw", "foo | baz"},
		{"foo b|ar baz", "daw", "foo |baz"},
		{"foo bar ba|z", "daw", "foo ba|r"},
		{"foo |  bar", "diw", "foo|bar"},
		{"foo |  bar", "daw", "fo|o"},
		{"foo.b|ar", "diw", "foo|."},
		{"a b| c d", "d2iw", "a b| d"},
		{"a b| c d", "d2aw", "a |b"},
		{"a b|c", "d3iw", "a b|c"},
		{"|\nb", "diw", "|\nb"},
		{`x = "a|b c" + "d"`, `di"`, `x = "|" + "d"`},
		{`x = "a|b c" + "d"`, `da"`, `x = |+ "d"`},
		{`|x = "ab"`, `di"`, `x = "|"`},
		{`x = "ab|"`, `di"`, `x = "|"`},
		{`"a\\"|b"`, `di"`, `"a\\"|"`},
		{`"a" x| "b"`, `di"`, `"a"|"b"`},
		{`"a|b"`, `d2i"`, `|`},
		{`x "a|b"`, `da"`, `|x`},
		{"f(a, (b|), c)", "di(", "f(a, (|), c)"},
		{"f(a, (b|), c)", "d2i(", "f(|)"},
		{"f(a, (b|), c)", "da(", "f(a, |, c)"},
		{"f|(a)", "di)", "f(|)"},
		{"f(|)", "di(", "f(|)"},
		{"x |f(a)", "di(", "x f(|)"},
		{"f(\n  a,|\n  b\n)", "di(", "f(\n|)"},
		{"f(a, b|)", "ci(X<Esc>", "f(|X)"},
		{"{a [b <c|>]}", "di<", "{a [b <|>]}"},
		{"{a [b <c|>]}", "di[", "{a [|]}"},
		{"{a [b <c|>]}", "diB", "{|}"},
		{"One. Tw|o three. Four.", "dis", "One. | Four."},
		{"One. Tw|o three. Four.", "das", "One. |Four."},
		{"One. Two three.| Four.", "dis", "One. Two three.|Four."},
		{"One. Two three.| Four.", "das", "One. Two three|."},
		{"One.\nTw|o.\n\nX.", "dis", "One.\n|\nX."},
		{"Hi (there.) Ne|xt.", "dis", "Hi (there.)| "},
		{"a.\nTw|o.\nb.", "dis", "a.\n|b."},
		{"One.\nTw|o.", "dis", "One.\n|"},
		{"a\nb|\n\nc", "dip", "|\nc"},
		{"a\nb|\n\nc", "dap", "|c"},
		{"a\n\nb|", "dap", "|a"},
		{"a\n|  \nb", "dip", "a\n|b"},
		{"a\n|\n\nb\nc\n\nd", "d2ap", "|a"},
		{"a|\nb\n\nc\nd", "d2ip", "|c\nd"},
	})
}

// B14
func TestB14Registers(t *testing.T) {
	cases(t, [][3]string{
		{"x|yz", "\"ayl\"Ayl\"ap", "xyy|yz"},
		{"a|b\nc\nd", "\"add\"bdd\"ap\"bp", "d\nab\n|c"},
		{"one\n|two", "yy\"_ddP", "|two\none"},
	})
}

// B15
func TestB15Put(t *testing.T) {
	cases(t, [][3]string{
		{"hello |world", "ywwp", "hello worldworl|d"},
		{"a|b", "ywP", "a|bb"},
		{"ab|c", "yl3p", "abccc|c"},
		{"one |two\nthree", "2ywP", "one |two\nthreetwo\nthree"},
		{"one |two\nthree", "2ywp", "one t|two\nthreewo\nthree"},
		{"a|bc\nde", "yjjp", "abc\nde\n|abc\nde"},
		{"x|y\nz", "yyjp", "xy\nz\n|xy"},
	})
}

// B16
func TestB16DotRepeat(t *testing.T) {
	cases(t, [][3]string{
		{"|a b c d e", "dw.", "|c d e"},
		{"|a b c d e f g", "2dw.", "|e f g"},
		{"|a b c d e f g", "2dw3.", "|f g"},
		{"|a b c d e f g", "2d2w.", "|"},
		{"|ab", "ihi<Esc>.", "hh|iiab"},
		{"|foo bar", "cwX<Esc>w.", "X |X"},
		{"|a", "yyp.", "a\na\n|a"},
		{"|a b", "\"adw\"bdw..", "|"},
	})
}

// B17
func TestB17Macros(t *testing.T) {
	cases(t, [][3]string{
		{"|a\nb\nc", "qaA!<Esc>jq@a@@", "a!\nb!\nc|!"},
		{"|a\nb\nc\nd", "qaA!<Esc>jq2@a", "a!\nb!\nc!\n|d"},
		{"|1\n2\n3", "qaqqaA!<Esc>j@aq@a", "1!\n2!\n3|!"},
		{"|a\nb", "qaA<lt>x<Esc>jq@a", "a<x\nb<|x"},
	})
	// A macro is text in a register, so putting it, editing it and yanking it
	// back changes what @ runs.
	_, h, m := run(t, "|a b", "qadwq")
	if text, _ := m.Register('a'); text != "dw" {
		t.Fatalf("recorded %q, want %q", text, "dw")
	}
	for _, k := range vim.Keys("o2dw<Esc>0\"ay$dd@a") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "" {
		t.Errorf("edited macro left %q, want it to delete two words", h.text)
	}
}

// B18
func TestB18Visual(t *testing.T) {
	cases(t, [][3]string{
		{"a|bcd", "vld", "a|d"},
		{"a|bc\nde", "v$d", "a|de"},
		{"ab|c\ndef", "vjd", "a|b"},
		{"abc\nd|ef", "vkd", "a|f"},
		{"a|b\n  cd\nx", "Vjd", "|x"},
		{"x\n  a|b\ncd", "Vd", "x\nc|d"},
		{"ab|c\nde", "vjy", "ab|c\nde"},
		{"abc\nd|ef", "vky", "a|bc\ndef"},
		{"a|b\ncd", "Vjy", "|ab\ncd"},
		{"a|bcd", "vlc<Esc>", "|ad"},
		{"ab|cd\nx", "VcX<Esc>", "|X\nx"},
		{"a|bcd", "vlohd", "|d"},
		{"foo b|ar baz", "viwd", "foo | baz"},
		{"a\nb|\n\nc", "vapd", "|c"},
		{"a|bcd\nefgh\nijkl", "<C-v>jld", "a|d\neh\nijkl"},
		{"a|bcd\nefgh\nijkl", "<C-v>jly", "a|bcd\nefgh\nijkl"},
		{"a|bcd\nefgh\nijkl", "<C-v>jlcX<Esc>", "a|Xd\neXh\nijkl"},
		{"a|bcd\nef\nijkl", "<C-v>jj$d", "|a\ne\ni"},
		{"a|bcd\nefgh", "<C-v>jlyGo<Esc>p", "abcd\nefgh\n|bc\nfg"},
		{"a|bcd\nefgh\nx", "<C-v>jly0P", "|bcabcd\nfgefgh\nx"},
		{"ab|c", "vV<Esc>", "ab|c"},
		{"a|bc\nd", "vjVd", "|"},
	})
}

// B19
func TestB19EscCancelsOperator(t *testing.T) {
	cases(t, [][3]string{{"foo |bar", "d<Esc>w", "foo ba|r"}})
}

// B9
func TestB9GraphemeClusters(t *testing.T) {
	cases(t, [][3]string{
		{"|a👍🏽b", "l", "a|👍🏽b"},
		{"|a👍🏽b", "ll", "a👍🏽|b"},
		{"a👍🏽|b", "h", "a|👍🏽b"},
		{"|xéy", "ll", "xé|y"},
	})
}
