package vim_test

import (
	"cmp"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/vim"
)

// host is the minimal host over a string. It counts undo steps the way the
// kernel makes nodes.
type host struct {
	vim.Defaults
	text    string
	cursor  int
	depth   int
	applied bool
	steps   int
	marks   map[rune]int // they move with edits, staying after text inserted at them
	top     int
	keys    []string // what reached Unhandled
	cols    int      // how wide the view is, 80 when 0
	clip    string
	fold    [2]int // a closed fold's first and last line, when last > 0
	indent  int    // the indent width, the tab's when 0
	words   bool   // the rows end after a space, as a float's do
	left    int    // the first column shown, when lines do not wrap
	shown   int    // how many columns show then
	name    string // the file's name
	refuse  string // a line Unhandled fails, as the host's commands may
}

func (h *host) FileName() string { return h.name }

func (h *host) Sideways() (int, int) { return h.left, h.shown }
func (h *host) SetLeft(left int)     { h.left = left }

func (h *host) WordWrap() bool { return h.words }

func (h *host) IndentWidth() int { return cmp.Or(h.indent, vim.Tabstop) }

func (h *host) Fold(line int) (int, int) {
	if h.fold[1] > 0 && line >= h.fold[0] && line <= h.fold[1] {
		return h.fold[0], h.fold[1]
	}
	return line, line
}

func (h *host) Clipboard() (string, error)  { return h.clip, nil }
func (h *host) SetClipboard(s string) error { h.clip = s; return nil }

func (h *host) Viewport() (int, int, int) { return h.top, 22, cmp.Or(h.cols, 80) }
func (h *host) SetTop(top int)            { h.top = top }

type text string

func (t text) Len() int                    { return len(t) }
func (t text) Slice(start, end int) string { return string(t[start:end]) }
func (t text) Lines() int                  { return strings.Count(string(t), "\n") + 1 }
func (t text) LineOf(pos int) int          { return strings.Count(string(t[:pos]), "\n") }
func (t text) LineStart(line int) int {
	pos := 0
	for range line {
		pos += strings.IndexByte(string(t[pos:]), '\n') + 1
	}
	return pos
}

func (h *host) Text() vim.Text    { return text(h.text) }
func (h *host) Cursor() int       { return h.cursor }
func (h *host) SetCursor(pos int) { h.cursor = pos }
func (h *host) BeginGroup()       { h.depth++ }
func (h *host) Unhandled(k string) error {
	h.keys = append(h.keys, k)
	if k == h.refuse {
		return errors.New("refused " + k)
	}
	return nil
}

func (h *host) SetMark(r rune, pos int) {
	if h.marks == nil {
		h.marks = map[rune]int{}
	}
	h.marks[r] = pos
}

func (h *host) Mark(r rune) (int, bool) {
	p, ok := h.marks[r]
	return p, ok
}

func (h *host) DeleteMark(r rune) { delete(h.marks, r) }

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
		for r, p := range h.marks {
			switch {
			case p >= e.End:
				h.marks[r] = p + len(e.Text) - (e.End - e.Start)
			case p > e.Start:
				h.marks[r] = e.Start
			}
		}
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
		{"  a|b", "gIX", "X|  ab"},
		{"|ab", "Axy<Esc>0giX", "abxyX|"},
		{"a|b", "giX", "aX|b"},
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
	// A count types the text that many times, or opens that many lines.
	for _, c := range [][3]string{
		{"|ab", "3ix<Esc>", "xx|xab"},
		{"|ab", "3Ax<Esc>", "abxx|x"},
		{"|ab\ncd", "3ox<Esc>", "ab\nx\nx\n|x\ncd"},
		{"|ab", "3Ox<Esc>", "x\nx\n|x\nab"},
		{"|ab", "3ix<C-c>", "|xab"},
		{"|ab", "v<C-c>", "|ab"},
		{"|ab", ":x<C-c>", "|ab"},
	} {
		if got, _, m := run(t, c[0], c[1]); got != c[2] || m.Mode() != vim.Normal {
			t.Errorf("%q on %q made %q in mode %v, want %q in normal mode", c[1], c[0], got, m.Mode(), c[2])
		}
	}
}

// B232
func TestB232Multi(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		// cgn changes the match under or after the cursor, and . the next.
		{"|foo bar foo", "/foo<CR>0cgnX<Esc>", "|X bar foo"},
		{"|foo bar foo", "/foo<CR>0cgnX<Esc>.", "X bar |X"},
		// <C-n> selects the word, and each <C-n> after places a cursor and
		// selects its next match as a whole word, which c then changes at
		// every cursor.
		{"|ab abc ab y ab", "<C-n><C-n>cZ<Esc>", "Z abc |Z y ab"},
		// A <C-n> with no match left keeps the cursors there are.
		{"|ab x ab", "<C-n><C-n><C-n>d", " x| "},
		// In visual mode it starts from the selected text.
		{"|a.b a.b", "vll<C-n>d", "| "},
	} {
		got, _, _ := run(t, c.start, c.keys)
		if got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}

// B229
func TestB229TypeAhead(t *testing.T) {
	h := &host{}
	m := vim.New(h)
	m.Map(vim.Insert, "jk", "<Esc>")
	feed := func(keys string) {
		for _, k := range vim.Keys(keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
	}
	// The j of jk shows at once, and goes when the k comes.
	if feed("ihej"); h.text != "hej" || !m.Waiting() {
		t.Fatalf("typing hej shows %q, waiting %v", h.text, m.Waiting())
	}
	if feed("k"); h.text != "he" || m.Mode() != vim.Normal {
		t.Fatalf("jk left %q in mode %v", h.text, m.Mode())
	}
	// A j another key follows, or the timeout, stays once.
	if feed("ajx"); h.text != "hejx" {
		t.Fatalf("jx made %q", h.text)
	}
	if feed("j"); m.Timeout() != nil || h.text != "hejxj" {
		t.Fatalf("j and the timeout made %q", h.text)
	}
	// Dot repeats what was typed, not the j that went.
	feed("<Esc>0D")
	if feed("Ahijk."); h.text != "hihi" {
		t.Errorf("Ahijk then dot made %q, want hihi", h.text)
	}
}

// B223
func TestB223Folds(t *testing.T) {
	// Lines 1 to 3 are a closed fold.
	for _, c := range []struct{ start, keys, want string }{
		{"|a\nb\nc\nd\ne", "j", "a\n|b\nc\nd\ne"},
		{"|a\nb\nc\nd\ne", "2j", "a\nb\nc\nd\n|e"},
		{"a\nb\nc\nd\n|e", "k", "a\n|b\nc\nd\ne"},
		{"a\n|b\nc\nd\ne", "dd", "a\n|e"},
		{"|a\nb\nc\nd\ne", "dj", "|e"},
	} {
		h := &host{text: strings.Replace(c.start, "|", "", 1), cursor: strings.Index(c.start, "|"), fold: [2]int{1, 3}}
		m := vim.New(h)
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}

// B211
func TestB211SmartIndent(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		// A line ending in an opening bracket indents the next a level more.
		{"  if x {|", "o", "  if x {\n\t  |"},
		{"f(|", "a<CR>", "f(\n\t|"},
		// Between a bracket and its closing one, the closing one moves down.
		{"x := []int{|}", "i<CR>", "x := []int{\n\t|\n}"},
		// O above a closing bracket opens a level in.
		{"{\n|}", "O", "{\n\t|\n}"},
		// A closing bracket typed on a line of blanks takes its opener's indent.
		{"  {\n\t\t|", "a}", "  {\n  }|"},
	} {
		if got, _, _ := run(t, c.start, c.keys); got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
	// The level is indent_width wide, as >> shifts, in tabs then spaces.
	h := &host{text: "\tif x {", cursor: 1, indent: 4}
	for _, k := range vim.Keys("o") {
		if err := vim.New(h).Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if want := "\tif x {\n\t    "; h.text != want {
		t.Errorf("o after a bracket with indent_width 4 made %q, want %q", h.text, want)
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
	if got, h, _ := run(t, "|", "ihe<C-g>ullo<Esc>"); h.steps != 2 || got != "hell|o" {
		t.Errorf("<C-g>u should split the step: got %q in %d steps", got, h.steps)
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
	cases(t)
}

// cases runs the cases testdata/grammar.txt has for the test's behaviour
// through run. The editor's tests run the same file (B110).
func cases(t *testing.T) {
	t.Helper()
	src, err := os.ReadFile("testdata/grammar.txt")
	if err != nil {
		t.Fatal(err)
	}
	b := "# " + regexp.MustCompile(`B\d+`).FindString(t.Name())
	_, rest, _ := strings.Cut(string(src), b+"\n")
	section, _, _ := strings.Cut(rest, "\n\n")
	for _, l := range strings.Split(strings.TrimRight(section, "\n"), "\n") {
		var c [3]string
		if _, err := fmt.Sscanf(l, "%q %q %q", &c[0], &c[1], &c[2]); err != nil {
			t.Fatalf("%s: %v", l, err)
		}
		if got, _, _ := run(t, c[0], c[1]); got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B11
func TestB11OperatorsWithMotions(t *testing.T) {
	cases(t)
}

// B12
func TestB12LineOperators(t *testing.T) {
	cases(t)
}

// B13
func TestB13TextObjects(t *testing.T) {
	cases(t)
}

// B14
func TestB14Registers(t *testing.T) {
	cases(t)
	// "% is the file's name, and the registers fude keeps cannot be written.
	h := &host{text: "x", name: "a.txt"}
	m := vim.New(h)
	for _, k := range vim.Keys(`"%p".yy`) {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "xa.txt" || m.Message() != "register . cannot be written" {
		t.Errorf(`"%%p made %q, and ".yy said %q`, h.text, m.Message())
	}
}

// B36
func TestB36Clipboard(t *testing.T) {
	h := &host{text: "ab cd", clip: "x\n"}
	m := vim.New(h)
	// The clipboard's line goes below, "+yiw takes its word, and <C-r>+
	// types it.
	for _, k := range vim.Keys(`"*p"+yiwA<C-r>+<Esc>`) {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "ab cd\nxx" || h.clip != "x" {
		t.Fatalf("got %q with %q on the clipboard", h.text, h.clip)
	}
}

// B15
func TestB15Put(t *testing.T) {
	cases(t)
}

// B16
func TestB16DotRepeat(t *testing.T) {
	cases(t)
}

// B17
func TestB17Macros(t *testing.T) {
	cases(t)
	// A macro is text in a register, so putting it, editing it and yanking it
	// back changes what @ runs.
	_, h, m := run(t, "|a b", "qadwq")
	if text, _, _ := m.Register('a'); text != "dw" {
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
	cases(t)
}

// B19
func TestB19EscCancelsOperator(t *testing.T) {
	cases(t)
}

// B20
func TestB20Marks(t *testing.T) {
	cases(t)
}

// B21
func TestB21Search(t *testing.T) {
	cases(t)
	_, _, m := run(t, "|aa", "")
	for _, k := range vim.Keys("/a\\1<CR>") {
		_ = m.Feed(k)
	}
	if !strings.Contains(m.Message(), "backtracking") {
		t.Errorf("a backreference gave message %q, want it refused", m.Message())
	}
}

// B22
func TestB22Substitute(t *testing.T) {
	cases(t)
	if _, h, _ := run(t, "|a\na\na", ":%s/a/b/<CR>"); h.steps != 1 {
		t.Errorf("a substitute over three lines made %d undo steps, want 1", h.steps)
	}
}

// B28
func TestB28StarSearch(t *testing.T) {
	cases(t)
}

// B25
func TestB25EditingCommands(t *testing.T) {
	cases(t)
	// ga shows the character under the cursor, with its digraph, and g8
	// its bytes.
	for keys, want := range map[string]string{"ga": "é is 233, hex e9, octal 351, digraph e'", "g8": "c3 a9"} {
		_, _, m := run(t, "|é", keys)
		if m.Message() != want {
			t.Errorf("%s said %q, want %q", keys, m.Message(), want)
		}
	}
}

// B26
func TestB26Increment(t *testing.T) {
	cases(t)
}

// B27
func TestB27Motions(t *testing.T) {
	cases(t)
}

// B27, on a line that wraps in a view 20 columns wide
func TestB27ScreenRows(t *testing.T) {
	h := &host{text: "abcdefghijklmnopqrstuvwxyz0123\nshort", cursor: 3, cols: 20}
	m := vim.New(h)
	for _, c := range []struct {
		keys string
		want int
	}{{"gj", 23}, {"gj", 34}, {"gk", 23}, {"gk", 3}} {
		if err := m.Feed(vim.Key(c.keys[:1])); err != nil {
			t.Fatal(err)
		}
		if err := m.Feed(vim.Key(c.keys[1:])); err != nil || h.cursor != c.want {
			t.Fatalf("%s went to %d, want %d: %v", c.keys, h.cursor, c.want, err)
		}
	}
}

// B27, for the keys that move along a screen row or scroll sideways
func TestB27Sideways(t *testing.T) {
	feed := func(h *host, keys string) {
		m := vim.New(h)
		for _, k := range vim.Keys(keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
	}
	text := "abcdefghijklmnopqrstuvwxyz"
	// Wrapped 10 columns wide, the cursor's row is klmnopqrst.
	for keys, want := range map[string]int{"g0": 10, "g^": 10, "gm": 15, "g$": 19} {
		h := &host{text: text, cursor: 13, cols: 10}
		if feed(h, keys); h.cursor != want {
			t.Errorf("%s on a wrapped row went to %d, want %d", keys, h.cursor, want)
		}
	}
	// Without wrap the view shows 10 columns from column 5.
	for keys, want := range map[string]int{"g0": 5, "gm": 10, "g$": 14} {
		h := &host{text: text, cursor: 7, cols: math.MaxInt, left: 5, shown: 10}
		if feed(h, keys); h.cursor != want {
			t.Errorf("%s in a view from column 5 went to %d, want %d", keys, h.cursor, want)
		}
	}
	for _, c := range []struct {
		keys                           string
		cursor, left, wantLeft, wantAt int
	}{
		{"zl", 0, 0, 1, 1},
		{"3zh", 5, 4, 1, 5},
		{"zh", 3, 0, 0, 3},
		{"zL", 0, 0, 5, 5},
		{"zH", 12, 8, 3, 12},
		{"zs", 7, 0, 7, 7},
		{"ze", 15, 0, 6, 15},
		{"zh", 19, 10, 9, 18},
	} {
		h := &host{text: text, cursor: c.cursor, cols: math.MaxInt, left: c.left, shown: 10}
		if feed(h, c.keys); h.left != c.wantLeft || h.cursor != c.wantAt {
			t.Errorf("%s from column %d with the cursor at %d left the view at %d and the cursor at %d, want %d and %d", c.keys, c.left, c.cursor, h.left, h.cursor, c.wantLeft, c.wantAt)
		}
	}
	// While lines wrap, they do not scroll sideways.
	h := &host{text: text, cursor: 3}
	if feed(h, "zl"); h.left != 0 || h.cursor != 3 {
		t.Errorf("zl with wrap moved the view to %d and the cursor to %d", h.left, h.cursor)
	}
}

// B364, for gj in a float, whose rows end after a space
func TestB364WordRows(t *testing.T) {
	for _, c := range []struct {
		words bool
		want  int
	}{{false, 9}, {true, 8}} {
		h := &host{text: "aaa bbb ccc ddd", cols: 9, words: c.words}
		m := vim.New(h)
		if err := m.Feed("g"); err != nil {
			t.Fatal(err)
		}
		if err := m.Feed("j"); err != nil || h.cursor != c.want {
			t.Errorf("with words %v, gj went to %d, want %d: %v", c.words, h.cursor, c.want, err)
		}
	}
}

// B120
func TestB120IgnoreCase(t *testing.T) {
	for _, c := range []struct {
		ignore, smart bool
		keys          string
		want          int
	}{
		{false, false, "/abc<CR>", 10},
		{true, false, "/abc<CR>", 2},
		{true, true, "/abc<CR>", 2},
		{true, true, "/Abc<CR>", 6},
		{true, true, `/\Cabc<CR>`, 10},
		{false, false, `/abc\c<CR>`, 2},
	} {
		h := &host{text: "x ABC Abc abc"}
		m := vim.New(h)
		m.IgnoreCase(c.ignore, c.smart)
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if h.cursor != c.want {
			t.Errorf("%s with ignore %v and smart %v went to %d, want %d", c.keys, c.ignore, c.smart, h.cursor, c.want)
		}
	}
}

// B122
func TestB122ExpandTab(t *testing.T) {
	for _, c := range []struct {
		expand      bool
		start, keys string
		want        string
	}{
		{false, "|x", ">>", "\tx"},
		{true, "|x", ">>", "        x"},
		{true, "ab|c", "i<Tab><Esc>", "ab      c"},
		{false, "ab|c", "i<Tab><Esc>", "ab\tc"},
		{true, "ab|c", "i<C-v><Tab><Esc>", "ab\tc"},
	} {
		h := &host{text: strings.Replace(c.start, "|", "", 1), cursor: strings.Index(c.start, "|")}
		m := vim.New(h)
		m.ExpandTab(c.expand)
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if h.text != c.want {
			t.Errorf("%s on %q with expand %v made %q, want %q", c.keys, c.start, c.expand, h.text, c.want)
		}
	}
}

// B29
func TestB29InsertKeys(t *testing.T) {
	cases(t)
	// <C-w>, <C-u> and <C-o> each end the undo step, so typing around them
	// makes two.
	for _, keys := range []string{"ifoo<C-w>x<Esc>", "ifoo<C-u>x<Esc>", "ifoo<C-o>0x<Esc>"} {
		if _, h, _ := run(t, "|", keys); h.steps != 2 {
			t.Errorf("%q made %d undo steps, want 2", keys, h.steps)
		}
	}
}

// B85
func TestB85VisualExtras(t *testing.T) {
	cases(t)
}

// B235
func TestB235VisualShiftKeeps(t *testing.T) {
	cases(t)
}

// B236
func TestB236MoveLines(t *testing.T) {
	cases(t)
	if _, h, _ := run(t, "|a\nb\nc\nd", "2]e"); h.steps != 1 {
		t.Errorf("2]e made %d undo steps, want 1", h.steps)
	}
}

// B429
func TestB429BlankLines(t *testing.T) {
	cases(t)
}

// B86
func TestB86LineCommands(t *testing.T) {
	cases(t)
}

// B87
func TestB87Global(t *testing.T) {
	cases(t)
	if _, h, _ := run(t, "|1x\n2\n3x", ":g/x/d<CR>"); h.steps != 1 {
		t.Errorf(":g made %d undo steps, want 1", h.steps)
	}
}

// B403
func TestB403MarkCommands(t *testing.T) {
	cases(t)
}

// B404
func TestB404VisualMacro(t *testing.T) {
	cases(t)
}

// B405
func TestB405Put(t *testing.T) {
	cases(t)
}

// B88
func TestB88Normal(t *testing.T) {
	cases(t)
	if _, h, _ := run(t, "|a\nb", ":%normal A!<CR>"); h.steps != 1 {
		t.Errorf(":normal made %d undo steps, want 1", h.steps)
	}
	// :normal! leaves the mappings out.
	_, h, m := run(t, "|ab\ncd", "")
	m.Map(vim.Normal, "x", "dd")
	for _, k := range vim.Keys(":normal! x<CR>:2normal x<CR>") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "b" {
		t.Errorf(":normal! x then :normal x left %q, want the x unmapped and then dd", h.text)
	}
}

// B9
func TestB9GraphemeClusters(t *testing.T) {
	cases(t)
}

// B84, B111
func TestB84MappingPrefixWaits(t *testing.T) {
	cs := []struct {
		keys, want string
		waits      bool
	}{
		{"ijkX", "one\n|wo three\nfour", false},
		{"ijxk<Esc>", "one\ntwjx|ko three\nfour", false},
		{",d", "one\nfo|ur", false},
		{",", "one\ntw| three\nfour", true},
		{",,", "one\ntw|three\nfour", true},
		{"dw", "one\ntw|three\nfour", false},
		{"w", "one\n|two three\nfour", false},
		{"x", "one\nfo|ur", false},
	}
	for _, c := range cs {
		h := &host{text: "one\ntwo three\nfour", cursor: 6}
		m := vim.New(h)
		m.Map(vim.Insert, "jk", "<Esc>")
		m.Map(vim.Normal, ",", "x")
		m.Map(vim.Normal, ",d", "dd")
		m.Map(vim.Normal, "x", "dd")
		m.Map(vim.Normal, "w", "b")
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if m.Waiting() != c.waits {
			t.Errorf("%q: waiting is %v, want %v", c.keys, m.Waiting(), c.waits)
		}
		if err := m.Timeout(); err != nil {
			t.Fatal(err)
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c.want {
			t.Errorf("%q: got %q, want %q", c.keys, got, c.want)
		}
	}
}

// B112
func TestB112Unhandled(t *testing.T) {
	_, h, _ := run(t, "|a\nb\nc\nd", ":w x<CR>2<C-w>v3:w<CR>3:d<CR>")
	if want := []string{":w x", "2<C-w>v", ":.,.+2w"}; !slices.Equal(h.keys, want) || h.text != "d" {
		t.Fatalf("Unhandled got %q and the text is %q, want %q and 3:d deleting three lines", h.keys, h.text, want)
	}
	// A line the host fails stops :g, and Feed returns why.
	h = &host{text: "a\nb", refuse: ":w"}
	m := vim.New(h)
	var err error
	for _, k := range vim.Keys(":g/./w<CR>") {
		if err = m.Feed(k); err != nil {
			break
		}
	}
	if err == nil || err.Error() != "refused :w" || len(h.keys) != 1 {
		t.Errorf(":g/./w with :w refused returned %v after %q", err, h.keys)
	}
}

// B179
func TestB179HostCommandInInsert(t *testing.T) {
	h := &host{text: "abcd", cursor: 2}
	m := vim.New(h)
	m.Command("xy", func(_ vim.Text, pos, _ int) ([]vim.Edit, string) {
		return []vim.Edit{{Start: pos, End: pos, Text: "XY"}}, ""
	})
	m.Map(vim.Insert, "<C-j>", "<Plug>(xy)")
	for _, k := range vim.Keys("i<C-j>z<Esc>") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "abXYzcd" {
		t.Errorf("typing z after the command left %q, want abXYzcd", h.text)
	}
}

// B593
func TestB593HostCommandKeys(t *testing.T) {
	h := &host{text: "abcdefgh"}
	m := vim.New(h)
	m.Command("cut", func(vim.Text, int, int) ([]vim.Edit, string) { return nil, "x" })
	// x maps to the command, which runs x through no mapping.
	m.Map(vim.Normal, "x", "<Plug>(cut)")
	for _, k := range vim.Keys("2x.") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "efgh" {
		t.Errorf("2x. left %q, want efgh", h.text)
	}
}

// B113, for a target
func TestB113HostObject(t *testing.T) {
	// id selects the run of digits around the cursor.
	digits := func(t vim.Text, pos, _ int) (int, int, bool) {
		s := t.Slice(0, t.Len())
		start, end := pos, pos
		for start > 0 && s[start-1] >= '0' && s[start-1] <= '9' {
			start--
		}
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
		}
		return start, end, start < end
	}
	for _, c := range [][3]string{
		{"ab 12|34 cd", "did", "ab | cd"},
		{"ab 12|34 cd", "cidX<Esc>", "ab |X cd"},
		{"ab 12|34 cd", "yid$p", "ab 1234 cd123|4"},
		{"ab 12|34 cd", "vidd", "ab | cd"},
		{"ab |cd", "did", "ab |cd"},
	} {
		h := &host{text: strings.Replace(c[0], "|", "", 1), cursor: strings.Index(c[0], "|")}
		m := vim.New(h)
		m.Object("id", digits)
		for _, k := range vim.Keys(c[1]) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B113, for an operator
func TestB113HostOperator(t *testing.T) {
	// gx puts brackets around its target.
	brackets := func(_ vim.Text, start, end int, _ bool, _ []string) []vim.Edit {
		return []vim.Edit{{Start: start, End: start, Text: "["}, {Start: end, End: end, Text: "]"}}
	}
	for _, c := range [][3]string{
		{"ab |cd ef", "gxiw", "ab |[cd] ef"},
		{"ab |cd ef", "gxe", "ab |[cd] ef"},
		{"ab |cd ef", "gxx", "|[ab cd ef]"},
		{"ab |cd ef", "vlgx", "ab |[cd] ef"},
		{"ab |cd ef\ngh", "gxj", "[ab| cd ef\ngh]"},
		// q waits for a register name, except to double an operator.
		{"ab |cd ef", "gqq", "|[ab cd ef]"},
	} {
		h := &host{text: strings.Replace(c[0], "|", "", 1), cursor: strings.Index(c[0], "|")}
		m := vim.New(h)
		m.Operator("gx", 0, brackets)
		m.Operator("gq", 0, brackets)
		for _, k := range vim.Keys(c[1]) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
	}
}

// B42, with a selector that picks runs of digits
func TestB42SelectRegions(t *testing.T) {
	digits := func(t vim.Text, _ string, start, end int) ([][2]int, error) {
		s := t.Slice(0, t.Len())
		var out [][2]int
		for i := start; i < end; i++ {
			if j := i; s[i] >= '0' && s[i] <= '9' {
				for j < end && s[j] >= '0' && s[j] <= '9' {
					j++
				}
				out, i = append(out, [2]int{i, j}), j
			}
		}
		return out, nil
	}
	for _, c := range [][3]string{
		{"a1 b22 c333|\nd4", "V:select x<CR>d", "a| b c\nd4"},
		{"a1 b22 c333|\nd4", "V:select x<CR>cXY<BS>Z<Esc>", "aX|Z bXZ cXZ\nd4"},
		{"a1 b22 c333|\nd4", "V:select x<CR>y<C-l>", "a|1 b22 c333\nd4"},
		// Each key runs at every cursor, so l widens each selection.
		{"a1 b22 c333|\nd4", "V:select x<CR>ld", "a|bc\nd4"},
	} {
		h := &host{text: strings.Replace(c[0], "|", "", 1), cursor: strings.Index(c[0], "|")}
		m := vim.New(h)
		m.Selector(digits)
		for _, k := range vim.Keys(c[1]) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c[2] {
			t.Errorf("%q on %q: got %q, want %q", c[1], c[0], got, c[2])
		}
		if c[1] == "V:select x<CR>y<C-l>" {
			if r, _, _ := m.Register('"'); r != "1\n22\n333\n" {
				t.Errorf("y at the cursors joined %q, want their lines", r)
			}
		}
	}
	h := &host{text: "abc"}
	m := vim.New(h)
	m.Selector(digits)
	var err error
	for _, k := range vim.Keys("V:select x<CR>") {
		err = m.Feed(k)
	}
	if err == nil || err.Error() != "the query selected nothing" {
		t.Errorf("a query with no match gave %v", err)
	}
}

// B114
func TestB114Unmap(t *testing.T) {
	h := &host{text: "ab cd"}
	m := vim.New(h)
	m.Map(vim.Normal, "grn", ":Rename<CR>")
	m.Map(vim.Normal, "gq", "x")
	m.Unmap(vim.Normal, "grn")
	for _, k := range vim.Keys("gr") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if m.Waiting() {
		t.Error("gr waits after the only mapping it began was removed")
	}
	if err := m.Feed("<Esc>"); err != nil {
		t.Fatal(err)
	}
	for _, k := range vim.Keys("gq") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "b cd" {
		t.Errorf("a mapping beside the removed one left %q, want it to still run", h.text)
	}
}

// B132
func TestB132ArrowKeys(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		// In normal mode they are j, k, 0, $ and x.
		{"ab|c\nxy", "<Down>", "abc\nx|y"},
		{"abc\nx|y", "<Up><End>", "ab|c\nxy"},
		{"a|bc", "<End><Home><Del>", "|bc"},
		// In insert mode <Down> keeps the column, or ends a shorter line.
		{"ab|c\nx", "i<Down>Z<Esc>", "abc\nx|Z"},
		{"ab|c", "i<Home>Z<End>Y<Esc>", "Zabc|Y"},
		// <Del> at the end of a line joins the next one.
		{"ab|c\nx", "a<Del><Esc>", "ab|cx"},
		// <Left> and <Right> are h and l, and <BS> goes onto the line
		// before, whose break an operator takes.
		{"a|bc", "<Left>x", "|bc"},
		{"a|bc", "<Right>x", "a|b"},
		{"ab\n|cd", "<BS>", "a|b\ncd"},
		{"ab\n|cd", "d<BS>", "ab|cd"},
		{"ab\n|cd", "d2<BS>", "a|cd"},
		// With Shift or Ctrl the arrows move by words, and <C-End> goes to
		// the last character.
		{"|ab cd\nef", "<S-Right>x", "ab |d\nef"},
		{"ab cd\n|ef", "<S-Left>x", "ab |d\nef"},
		{"|ab cd\nef", "<C-End>", "ab cd\ne|f"},
		{"|ab cd", "A<S-Left>x<Esc>", "ab |xcd"},
	} {
		if got, _, _ := run(t, c.start, c.keys); got != c.want {
			t.Errorf("%s on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}

// B133
func TestB133CommandLineEditing(t *testing.T) {
	cases(t)
	_, _, m := run(t, "|x", ":one<CR>:two<CR>")
	for _, c := range []struct{ keys, line string }{
		// The cursor moves, and text goes in where it is.
		{":ac<Left>b", ":ab|c"},
		{"<Home>X<End>Y", ":XabcY|"},
		{"<Left><Left><Del><BS>", ":Xa|Y"},
		{" foo bar<C-w>", ":Xa foo |Y"},
		{"<C-u>", ":|Y"},
		// <Up> recalls older lines that start with the text typed, and
		// <Down> comes back to it.
		{"<Esc>:o<Up>", ":one|"},
		{"<Up>", ":one|"},
		{"<Down>", ":o|"},
		{"<Esc>:<Up><Up>", ":one|"},
		// <C-p> and <C-n> recall every line, whatever is typed.
		{"<Esc>:o<C-p>", ":two|"},
		{"<C-p>", ":one|"},
		{"<C-n><C-n>", ":o|"},
	} {
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := m.Line()[:m.LineCursor()] + "|" + m.Line()[m.LineCursor():]; got != c.line {
			t.Errorf("after %q the line is %q, want %q", c.keys, got, c.line)
		}
	}
	// The history keeps its newest 10000 lines, so after 10001 more the
	// oldest it recalls is the second of them.
	m.Feed(vim.Key("<Esc>"))
	for i := range 10001 {
		for _, k := range vim.Keys(fmt.Sprintf(":%05d<CR>", i)) {
			m.Feed(k)
		}
	}
	m.Feed(vim.Key(":"))
	for range 10001 {
		m.Feed(vim.Key("<Up>"))
	}
	if m.Line() != ":00001" {
		t.Errorf("the oldest line recalled is %q, want :00001", m.Line())
	}
}

// B135
func TestB135CommandLineRegister(t *testing.T) {
	cases(t)
	_, _, m := run(t, "foo |bar", `"ayy:<C-r><C-w> <C-r>a`)
	if m.Line() != ":bar foo bar" {
		t.Errorf("<C-r><C-w> and <C-r>a made %q, want %q", m.Line(), ":bar foo bar")
	}
}

// B137
func TestB137Clipboard(t *testing.T) {
	h := &host{text: "ab cd\nxy zw"}
	m := vim.New(h)
	m.UseClipboard(true)
	feed := func(keys string) {
		for _, k := range vim.Keys(keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
	}
	// A yank without a register copies to the clipboard, and "_ does not.
	if feed("yiw"); h.clip != "ab" {
		t.Fatalf("yiw put %q on the clipboard", h.clip)
	}
	if feed(`"_dd`); h.clip != "ab" {
		t.Fatalf(`"_dd put %q on the clipboard`, h.clip)
	}
	// A block copied in the editor puts as a block, though the clipboard
	// holds its text only.
	h.text, h.cursor = "ab\ncd", 0
	if feed("<C-v>jly$p"); h.text != "abab\ncdcd" {
		t.Fatalf("a block put made %q", h.text)
	}
	// Text copied elsewhere puts from the clipboard.
	h.clip, h.text, h.cursor = "zz", "a", 0
	if feed("p"); h.text != "azz" {
		t.Fatalf("p after a copy elsewhere made %q", h.text)
	}
}

// B141, where <C-h> deletes as <BS> does
func TestB141CtrlHDeletes(t *testing.T) {
	if got, _, _ := run(t, "ab|c", "i<C-h><Esc>"); got != "|ac" {
		t.Errorf("i<C-h> made %q, want %q", got, "|ac")
	}
	if _, _, m := run(t, "|x", ":ab<C-h>"); m.Line() != ":a" {
		t.Errorf(":ab<C-h> left the line %q", m.Line())
	}
}

// B156
func TestB156TextCommands(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		{"|b\na\nc\na", ":sort<CR>", "|a\na\nb\nc"},
		{"|b\na\nc\na", ":sort! u<CR>", "|c\nb\na"},
		{"|x10\nx9\ny\nx-1", ":sort n<CR>", "|y\nx-1\nx9\nx10"},
		{"|B\na", ":sort i<CR>", "|a\nB"},
		{"z\n|c\nb\na", ":2,3sort<CR>", "z\n|b\nc\na"},
		{"|a\n  b\nc", ":join<CR>", "|a b\nc"},
		{"|a\nb\nc", ":1,3j!<CR>", "|abc"},
	} {
		if got, _, _ := run(t, c.start, c.keys); got != c.want {
			t.Errorf("%s on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
	// :retab rewrites leading blanks as expand_tab says.
	h := &host{text: "\tx\n  \ty"}
	m := vim.New(h)
	m.ExpandTab(true)
	for _, k := range vim.Keys(":retab<CR>") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.text != "        x\n        y" {
		t.Errorf(":retab made %q", h.text)
	}
	// !{motion} opens the command line with the lines' range, for a command
	// to put them through.
	for keys, want := range map[string]string{"!j": ":.,.+1!", "!!": ":.!", "Vj!": ":.,.+1!"} {
		if _, _, m := run(t, "|a\nb\nc", keys); m.Line() != want {
			t.Errorf("%s opened %q, want %q", keys, m.Line(), want)
		}
	}
}

// B484
func TestB484WrapScan(t *testing.T) {
	for _, c := range []struct {
		wrap    bool
		keys    string
		want    int
		message string
	}{
		{true, "/abc<CR>", 0, "search hit the end, continuing at the start"},
		{false, "/abc<CR>", 6, "search hit the end without a match for abc"},
		{false, "?x<CR>", 4, ""},
		{false, "?x<CR>?<CR>", 4, "search hit the start without a match for x"},
		{false, "$/abc/e<CR>", 8, "search hit the end without a match for abc"},
		{false, "0*", 6, ""},
	} {
		h := &host{text: "abc x abc", cursor: 6}
		m := vim.New(h)
		m.WrapScan(c.wrap)
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if h.cursor != c.want || m.Message() != c.message {
			t.Errorf("%s with wrap_scan %v went to %d saying %q, want %d and %q", c.keys, c.wrap, h.cursor, m.Message(), c.want, c.message)
		}
	}
}

// B490
func TestB490VirtualEdit(t *testing.T) {
	for _, c := range []struct {
		where, text, keys, want string
		past                    int
	}{
		{"none", "ab", "5l", "a|b", 0},
		{"onemore", "ab", "5l", "ab|", 0},
		{"all", "ab", "5l", "ab|", 3},
		{"all", "ab", "5l2h", "ab|", 1},
		{"all", "ab", "5lix<Esc>", "ab   |x", 0},
		{"all", "ab", "5li<Esc>", "a|b", 0},
		{"all", "abcdef\nab", "$j", "abcdef\na|b", 0},
		{"all", "abcdef\nab", "5lj", "abcdef\nab|", 3},
		{"block", "abcdef\nab", "5lj", "abcdef\na|b", 0},
		{"block", "abcdef\nab", "<C-v>j5l", "abcdef\nab|", 3},
		{"block", "abcdef\nab", "<C-v>j5lAx<Esc>", "|abcdefx\nab    x", 0},
	} {
		h := &host{text: c.text}
		m := vim.New(h)
		m.VirtualEdit(c.where)
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c.want || m.Past() != c.past {
			t.Errorf("%q with virtual_edit %s made %q, %d past the end, want %q, %d", c.keys, c.where, got, m.Past(), c.want, c.past)
		}
	}
	// The block's right edge is the cursor's column past the line's end.
	h := &host{text: "abcdef\nab"}
	m := vim.New(h)
	m.VirtualEdit("block")
	for _, k := range vim.Keys("<C-v>j5l") {
		if err := m.Feed(k); err != nil {
			t.Fatal(err)
		}
	}
	if sel, _ := m.Selection(); sel.Left != 0 || sel.Right != 5 {
		t.Errorf("the block spans columns %d to %d, want 0 to 5", sel.Left, sel.Right)
	}
}

// B450
func TestB450TextWidth(t *testing.T) {
	for _, c := range [][3]string{
		{"|", "ione two three four five<Esc>", "one two\nthree four\nfiv|e"},
		{"|", "i  // one two three<Esc>", "  // one\n  // two\n  // thre|e"},
		{"|", "i- one two three four<Esc>", "- one two\n  three\n  fou|r"},
		{"|", "ionetwothreefour<Esc>", "onetwothreefou|r"},
	} {
		h := &host{text: strings.Replace(c[0], "|", "", 1), cursor: strings.Index(c[0], "|")}
		m := vim.New(h)
		m.TextWidth(10)
		for _, k := range vim.Keys(c[1]) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c[2] {
			t.Errorf("%q with text_width 10 made %q, want %q", c[1], got, c[2])
		}
	}
}

// B453
func TestB453Abbreviations(t *testing.T) {
	for _, c := range [][2]string{
		{"o teh.<Esc>", "xx teh\n the|."},
		{"oteh<Esc>", "xx teh\nth|e"},
		{"oteh<C-]>x<Esc>", "xx teh\nthe|x"},
		// A word that is part of a longer one, or that was there before
		// the insert began, stays.
		{"oxteh <Esc>", "xx teh\nxteh| "},
		{"A <Esc>", "xx teh| "},
	} {
		h := &host{text: "xx teh"}
		m := vim.New(h)
		m.Abbreviate(map[string]string{"teh": "the"})
		for _, k := range vim.Keys(c[0]) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c[1] {
			t.Errorf("%q made %q, want %q", c[0], got, c[1])
		}
	}
}

// B500
func TestB500UndoLine(t *testing.T) {
	cases(t)
}
