package vim_test

import (
	"cmp"
	"fmt"
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
}

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

func (h *host) Text() vim.Text     { return text(h.text) }
func (h *host) Cursor() int        { return h.cursor }
func (h *host) SetCursor(pos int)  { h.cursor = pos }
func (h *host) BeginGroup()        { h.depth++ }
func (h *host) Unhandled(k string) { h.keys = append(h.keys, k) }
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

// B202
func TestB202AutoPairs(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		{"|", "i(", "(|)"},
		{"|)", "i[", "[|])"},
		{"|x", "i(", "(|x"},
		{"|", "i(a)", "(a)|"},
		{"|", `i"`, `"|"`},
		{"|", `i""`, `""|`},
		{"do|n", "a'", "don'|"},
		{"|", "i(<BS>", "|"},
		{"(|)", "i<BS>", "|"},
		{"  |", "a{<CR>", "  {\n\t  |\n  }"},
	} {
		h := &host{text: strings.Replace(c.start, "|", "", 1), cursor: strings.Index(c.start, "|")}
		m := vim.New(h)
		m.AutoPairs(true)
		for _, k := range vim.Keys(c.keys) {
			if err := m.Feed(k); err != nil {
				t.Fatal(err)
			}
		}
		if got := h.text[:h.cursor] + "|" + h.text[h.cursor:]; got != c.want {
			t.Errorf("%q on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
	// A paste is never paired, and the machine pairs nothing unless asked.
	h := &host{}
	m := vim.New(h)
	m.AutoPairs(true)
	if m.Feed("i"); m.Paste("(") != nil || h.text != "(" {
		t.Errorf("pasting ( made %q", h.text)
	}
	if got, _, _ := run(t, "|", "i("); got != "(|" {
		t.Errorf("without AutoPairs, i( made %q", got)
	}
}

// B232
func TestB232Multi(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		// cgn changes the match under or after the cursor, and . the next.
		{"|foo bar foo", "/foo<CR>0cgnX<Esc>", "|X bar foo"},
		{"|foo bar foo", "/foo<CR>0cgnX<Esc>.", "X bar |X"},
		// <C-n> selects the word, and each <C-n> after adds its next match
		// as a whole word, which c then changes everywhere.
		{"|ab abc ab y ab", "<C-n><C-n>cZ<Esc>", "|Z abc Z y ab"},
		// A <C-n> with no match left keeps the regions there are.
		{"|ab x ab", "<C-n><C-n><C-n>d", "| x "},
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

// B219
func TestB219Surround(t *testing.T) {
	for _, c := range []struct{ start, keys, want string }{
		{"say |hi there", "ysiw)", "say |(hi) there"},
		{"say |hi there", "ysiw(", "say |( hi ) there"},
		{"say |hi there", `ysiw"`, `say |"hi" there`},
		{"  |say hi", "yss]", "  |[say hi]"},
		{"f(|a, b)", "ds(", "f|a, b"},
		{"f( |a )", "ds(", "f|a"},
		{`x = "|a"`, `cs"'`, "x = |'a'"},
		{"f(|a)", "cs)}", "f|{a}"},
		{"|ab cd", "vlS]", "|[ab] cd"},
		// Dot repeats a surround with its character.
		{"|a b", "ysiw)W.", "(a) |(b)"},
		// <Esc> cancels one waiting for its character.
		{"|ab", "ysiw<Esc>", "|ab"},
		// A blank names no pair.
		{"(|a)", "ds<Space>", "(|a)"},
		{"(|a)", "cs<Space>x", "(|a)"},
	} {
		h := &host{text: strings.Replace(c.start, "|", "", 1), cursor: strings.Index(c.start, "|")}
		m := vim.New(h)
		m.Surround(true)
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

// B88
func TestB88Normal(t *testing.T) {
	cases(t)
	if _, h, _ := run(t, "|a\nb", ":%normal A!<CR>"); h.steps != 1 {
		t.Errorf(":normal made %d undo steps, want 1", h.steps)
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
}

// B179
func TestB179HostCommandInInsert(t *testing.T) {
	h := &host{text: "abcd", cursor: 2}
	m := vim.New(h)
	m.Command("xy", func(_ vim.Text, pos, _ int) []vim.Edit { return []vim.Edit{{Start: pos, End: pos, Text: "XY"}} })
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
	brackets := func(_ vim.Text, start, end int, _ bool) []vim.Edit {
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
		m.Operator("gx", brackets)
		m.Operator("gq", brackets)
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
		{"a1 b22 c333|\nd4", "V:select x<CR>y", "a|1 b22 c333\nd4"},
		{"a1 b22 c333|\nd4", "V:select x<CR>ld", "a|b22 c333\nd4"},
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
		if c[1] == "V:select x<CR>y" {
			if r, _, _ := m.Register('"'); r != "1\n22\n333" {
				t.Errorf("y on regions yanked %q, want them joined by newlines", r)
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
	} {
		if got, _, _ := run(t, c.start, c.keys); got != c.want {
			t.Errorf("%s on %q made %q, want %q", c.keys, c.start, got, c.want)
		}
	}
}

// B133
func TestB133CommandLineEditing(t *testing.T) {
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
}

// B135
func TestB135CommandLineRegister(t *testing.T) {
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
		{"|a\n  b\nc", ":join<CR>", "a| b\nc"},
		{"|a\nb\nc", ":1,3j!<CR>", "ab|c"},
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
}
