package editor_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/y3owk1n/fude/editor"
	filesmod "github.com/y3owk1n/fude/modules/files"
	listingmod "github.com/y3owk1n/fude/modules/listing"
	windowmod "github.com/y3owk1n/fude/modules/window"
)

// B27
func TestB27Scrolling(t *testing.T) {
	var lines []string
	for i := 1; i <= 30; i++ {
		lines = append(lines, fmt.Sprintf("  %d", i))
	}
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	line := func(e *editor.Session, pos int) int { return strings.Count(text(e)[:pos], "\n") + 1 }
	for _, c := range []struct {
		keys      string
		top, line int
	}{
		{"<C-e>", 2, 2},
		{"3<C-e>", 4, 4},
		{"10G<C-y>", 1, 10},
		{"<C-d>", 9, 12},
		{"<C-d><C-d>", 9, 23},
		{"2<C-d>", 3, 3},
		{"30G<C-u>", 1, 19},
		{"<C-f>", 21, 21},
		{"<C-f><C-f>", 30, 30},
		{"30G<C-b>", 1, 22},
		{"15Gzz", 5, 15},
		{"15Gzt", 15, 15},
		{"15Gzb", 1, 15},
		{"M", 1, 11},
		{"L", 1, 22},
		{"3L", 1, 20},
		{"2H", 1, 2},
		{"30GH", 9, 9},
		{"20Gzt5<C-e>", 25, 25},
		{"G<C-d>", 9, 30},
	} {
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if top, cur := line(e, e.Top()), line(e, e.Cursor()); top != c.top || cur != c.line {
			t.Errorf("%q: top %d, cursor line %d, want %d and %d", c.keys, top, cur, c.top, c.line)
		}
	}
}

// B23
func TestB23Splits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ keys, want string }{
		// A new window goes below or right, and the cursor goes to it.
		{"<C-w>s", "0,0 80x12 *0,12 80x11"},
		{"<C-w>v", "0,0 40x23 *41,0 39x23"},
		{"<C-w>s<C-w>v", "0,0 80x12 0,12 40x11 *41,12 39x11"},
		{"<C-w>s<C-w>k", "*0,0 80x12 0,12 80x11"},
		{"<C-w>v<C-w>h", "*0,0 40x23 41,0 39x23"},
		{"<C-w>v<C-w>h<C-w>l", "0,0 40x23 *41,0 39x23"},
	} {
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if got := windows(e); got != c.want {
			t.Errorf("%q: windows %q, want %q", c.keys, got, c.want)
		}
	}

	// Both windows show one buffer, so an edit in one moves the cursor in
	// the other, and an edit after it leaves it where it was.
	e := open(t, path)
	e.SetSize(80, 23)
	feed(e, "jjl<C-w>sggdd<C-w>k")
	if text(e) != "two\nthree\nfour" || e.Cursor() != 5 {
		t.Errorf("after dd above it, the other window is at %d in %q, want 5", e.Cursor(), text(e))
	}
	e = open(t, path)
	e.SetSize(80, 23)
	feed(e, "jjl<C-w>sjx<C-w>k")
	if e.Cursor() != 9 {
		t.Errorf("after x below it, the other window is at %d, want 9", e.Cursor())
	}
	// split_below and split_right off put a new window above or left.
	config(t, "split_below = false\nsplit_right = false\n")
	for _, c := range []struct{ keys, want string }{
		{"<C-w>s", "*0,0 80x12 0,12 80x11"},
		{"<C-w>v", "*0,0 40x23 41,0 39x23"},
	} {
		e := start(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if got := windows(e); got != c.want {
			t.Errorf("%q with split_below and split_right off: windows %q, want %q", c.keys, got, c.want)
		}
	}
}

// B91
func TestB91WindowCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ keys, want string }{
		{"<C-w>s<C-w>c", "*0,0 80x23"},
		{"<C-w>s<C-w>s", "0,0 80x8 0,8 80x8 *0,16 80x7"},
		{"<C-w>s<C-w>s<C-w>o", "*0,0 80x23"},
		{"<C-w>s<C-w>s<C-w>=", "0,0 80x8 0,8 80x8 *0,16 80x7"},
		{"<C-w>s:q<CR>", "*0,0 80x23"},
		{"<C-w>v<C-w>q", "*0,0 80x23"},
	} {
		e := open(t, path)
		e.SetSize(80, 23)
		feed(e, c.keys)
		if got := windows(e); got != c.want || e.Done() {
			t.Errorf("%q: windows %q (done %v), want %q", c.keys, got, e.Done(), c.want)
		}
	}
	e := open(t, path)
	e.SetSize(80, 23)
	feed(e, "<C-w>c")
	if e.Message() != "cannot close the last window" {
		t.Errorf("closing the last window said %q", e.Message())
	}
	feed(e, ":q<CR>")
	if !e.Done() {
		t.Error(":q in the last window did not quit")
	}
}

// B193
func TestB193WindowSizes(t *testing.T) {
	e := withCommands(open(t, ""))
	e.SetSize(80, 20)
	heights := func() []int {
		var out []int
		for _, v := range e.Views() {
			out = append(out, v.H)
		}
		return out
	}
	for _, c := range []struct {
		keys string
		want []int
	}{
		// The new window is below, and is the current one.
		{"<C-w>s", []int{10, 10}},
		{"3<C-w>+", []int{7, 13}},
		{"<C-w>=", []int{10, 10}},
		{":resize 5<CR>", []int{15, 5}},
		{":resize +2<CR>", []int{13, 7}},
		{"2<C-w>-", []int{15, 5}},
		// The other window keeps a row and its status line.
		{"<C-w>_", []int{2, 18}},
	} {
		if feed(e, c.keys); !slices.Equal(heights(), c.want) {
			t.Errorf("%s left the windows %v rows tall, want %v", c.keys, heights(), c.want)
		}
	}
	feed(e, "<C-w>o<C-w>v:vertical resize 30<CR>")
	if vs := e.Views(); len(vs) != 2 || vs[0].W != 49 || vs[1].W != 30 {
		t.Errorf(":vertical resize 30 left the windows %d and %d wide", vs[0].W, vs[1].W)
	}
}

// B21, in every window
func TestB21MatchesInEveryWindow(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a"), "xa\n")
	change(t, filepath.Join(dir, "b"), "ax\n")
	e := open(t, filepath.Join(dir, "a"))
	feed(e, ":vs "+filepath.Join(dir, "b")+"<CR>/a<CR>")
	for _, v := range e.Views() {
		if want := strings.Index(v.Text.Slice(0, v.Text.Len()), "a"); len(v.Matches) != 1 || v.Matches[0] != [2]int{want, want + 1} {
			t.Errorf("%s shows matches %v", v.Name, v.Matches)
		}
	}
}

// B37
func TestB37Wrap(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, strings.Repeat("a", 20)+"\nb\nc\n")
	e := open(t, path)
	// Three rows of text, ten columns wide: the first line takes two.
	e.SetSize(10, 4)
	if feed(e, "j"); e.Top() != 0 {
		t.Fatalf("on line 2 the window starts at %d, want 0", e.Top())
	}
	if feed(e, "j"); e.Top() != 21 {
		t.Fatalf("on line 3 the window starts at %d, want line 2 at 21", e.Top())
	}
	// Without wrap the view scrolls sideways to the cursor's column.
	feed(e, "gg:set wrap false<CR>$")
	if v := e.Views()[0]; v.Top != 0 || v.Left != 10 {
		t.Fatalf("on the last of 20 columns in 10 the view starts at %d, column %d, want 0 and 10", v.Top, v.Left)
	}
}

// B38
func TestB38Ruler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "ab\n\tx\n\n4\n5\n6\n")
	e := open(t, path)
	e.SetSize(40, 4)
	for _, c := range [][2]string{{"", "1,1 Top"}, {"j$", "2,2-9 Top"}, {"j", "3,0-1 Top"}, {"j", "4,1 33%"}, {"G", "6,1 Bot"}} {
		feed(e, c[0])
		if got := strings.Join(strings.Fields(e.Views()[0].Ruler), " "); got != c[1] {
			t.Errorf("after %q the ruler is %q, want %q", c[0], got, c[1])
		}
	}
	if feed(e, "2d"); e.Shown() != "2d" {
		t.Errorf("2d shows %q", e.Shown())
	}
	if feed(e, "<Esc>"); e.Shown() != "" {
		t.Errorf("after <Esc> it shows %q", e.Shown())
	}
}

// B39
func TestB39Set(t *testing.T) {
	e := withCommands(open(t, ""))
	feed(e, ":set number true<CR>:set relative_number true<CR>")
	if v := e.Views()[0]; !v.Number || !v.Relative || v.Gutter != 4 {
		t.Fatalf("after :set number true and relative_number true the view is %+v", v)
	}
	// A window split from it starts the same, and :set changes only the
	// current one.
	feed(e, ":sp<CR>:set number false<CR>:set relative_number false<CR>")
	if vs := e.Views(); vs[1].Number || vs[1].Relative || vs[1].Gutter != 0 || !vs[0].Number {
		t.Fatalf("after :set number false and relative_number false in a split the views are %+v", vs)
	}
	// A name alone shows the option, and :set alone lists them in a window.
	if feed(e, ":set number<CR>"); e.Message() != "number false" {
		t.Errorf(":set number said %q", e.Message())
	}
	feed(e, ":set<CR>")
	if listed := strings.Join(strings.Fields(text(e)), " "); e.Views()[2].Name != "[set]" || !strings.Contains(listed, "relative_number false") || !strings.Contains(listed, "scroll_off 0") || !strings.Contains(listed, "key_timeout 1000") {
		t.Errorf(":set listed %q", text(e))
	}
	feed(e, "q")
	// A :set with a bad value changes nothing.
	for keys, want := range map[string]string{":set number bogus<CR>": `number takes true or false, not "bogus"`, ":set number on<CR>": `number takes true or false, not "on"`} {
		if feed(e, keys); e.Views()[1].Number || e.Message() != want {
			t.Errorf("%s left number %v and said %q", keys, e.Views()[1].Number, e.Message())
		}
	}
	if feed(e, ":set wide<CR>"); e.Message() != "no option named wide" {
		t.Errorf(":set wide said %q", e.Message())
	}
	// config.toml sets what windows start with.
	config(t, "number = true\n")
	if v := start(t).Views()[0]; !v.Number || v.Relative {
		t.Errorf("with number = true a window starts as %+v", v)
	}
}

// B116
func TestB116ScrollOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	var lines []string
	for i := range 20 {
		lines = append(lines, strconv.Itoa(i))
	}
	change(t, path, strings.Join(lines, "\n")+"\n")
	e := open(t, path)
	// Seven rows, and two lines kept below the cursor's.
	e.SetSize(40, 8)
	feed(e, ":set scroll_off 2<CR>")
	if feed(e, "4j"); e.Top() != 0 {
		t.Fatalf("on line 5 the view starts at %d, want 0", e.Top())
	}
	if feed(e, "j"); e.Top() != 2 {
		t.Fatalf("on line 6 the view starts at %d, want line 2 at 2", e.Top())
	}
	if feed(e, ":set scroll_off<CR>"); e.Message() != "scroll_off 2" {
		t.Errorf(":set scroll_off said %q", e.Message())
	}
	if feed(e, ":set scroll_off x<CR>"); !strings.Contains(e.Message(), "takes a number") {
		t.Errorf(":set scroll_off x said %q", e.Message())
	}
}

// B117
func TestB117CursorLine(t *testing.T) {
	e := open(t, "")
	if feed(e, ":set cursor_line true<CR>"); !e.Views()[0].CursorLine {
		t.Fatal(":set cursor_line true left the window without it")
	}
}

// B120, through :set
func TestB120SetIgnoreCase(t *testing.T) {
	e := open(t, "")
	feed(e, "ix ABC abc<Esc>0:set ignore_case true<CR>/abc<CR>")
	if e.Cursor() != 2 {
		t.Fatalf("/abc with ignore_case went to %d, want 2", e.Cursor())
	}
}

// B121
func TestB121MatchParen(t *testing.T) {
	e := open(t, "")
	feed(e, "if(a, (b))<Esc>")
	for _, c := range []struct {
		keys string
		want []int
	}{
		{"0", nil},
		{"l", []int{1, 8}},
		{"f(", []int{5, 7}},
		{"$", []int{8, 1}},
		// In insert mode the bracket before the cursor counts.
		{"a", []int{8, 1}},
	} {
		if feed(e, c.keys); !slices.Equal(e.Views()[0].Pair, c.want) {
			t.Errorf("after %q the pair is %v, want %v", c.keys, e.Views()[0].Pair, c.want)
		}
	}
}

// B142
func TestB142LocalKeymaps(t *testing.T) {
	dir := t.TempDir()
	change(t, filepath.Join(dir, "a"), "A\n")
	change(t, filepath.Join(dir, "b"), "B\n")
	config(t, `[keymap.normal]
"<C-h>" = "<Plug>(window.left)"

[keymap.listing]
"<C-h>" = ""
"<C-v>" = "<Plug>(listing.vsplit)"
x = "<Nop>"

[keymap.finder]
"<C-j>" = "<Plug>(finder.next)"
`)
	t.Chdir(dir)
	e := start(t, filepath.Join(dir, "a"))
	e.Use(listingmod.New())
	e.Use(filesmod.New())
	e.Use(windowmod.New())
	e.SetSize(80, 24)
	feed(e, ":vsplit<CR>-")
	// A key the listing's table drops falls through to [keymap.normal].
	if feed(e, "<C-h>"); !e.Views()[0].Current {
		t.Fatal("<C-h> in a listing did not move to the left window")
	}
	// A key mapped in it runs the listing's action, and <Nop> does nothing.
	if feed(e, "<C-w>ljx<C-v>"); len(e.Views()) != 3 || text(e) != "B" {
		t.Fatalf("<C-v> on b left %d windows showing %q", len(e.Views()), text(e))
	}
	// The finder's keys are an insert mode keymap.
	feed(e, "<C-w>h:pick files<CR>")
	settle(t, e, func() bool {
		f, _ := e.Float()
		return strings.Count(f.Text, "\n") > 0 && strings.Contains(f.Text, "\nb")
	})
	if feed(e, "<C-j><CR>"); text(e) != "B" {
		t.Fatalf("<C-j><CR> in the finder opened %q", text(e))
	}
}

// B143
func TestB143SignColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	change(t, path, "a\n")
	config(t, "sign_column = true")
	e := start(t, path)
	if v := e.Views()[0]; v.SignCols != 2 || v.Gutter != 2 {
		t.Fatalf("with sign_column a file with no signs has %d sign columns", v.SignCols)
	}
	if feed(e, ":set sign_column false<CR>"); e.Views()[0].SignCols != 0 {
		t.Fatal(":set sign_column false kept the sign column")
	}
}

// B144
func TestB144Leader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	for _, c := range []struct{ config, keys string }{
		// <Space> by default, and leader changes it.
		{"[keymap.normal]\n\"<Leader>x\" = \"dd\"", " x"},
		{"leader = \",\"\n[keymap.normal]\n\"<leader>x\" = \"dd\"", ",x"},
	} {
		change(t, path, "a\nb\n")
		config(t, c.config)
		e := start(t, path)
		if feed(e, c.keys); text(e) != "b" {
			t.Errorf("%q with %q left %q", c.keys, c.config, text(e))
		}
	}
}

// B217
func TestB217StickyContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, "package p\n\nfunc F() {\n\tg := func() {\n"+strings.Repeat("\t\tx()\n", 30)+"\t}\n}\n")
	e := start(t, path)
	e.SetSize(80, 10)
	// Scrolled into g in F, both first lines are pinned, with their highlights.
	feed(e, "20Gzt3j")
	settle(t, e, func() bool {
		v := e.Views()[0]
		return slices.Equal(v.Context, []int{11, 22}) && len(v.ContextHighlights) > 0
	})
	// With sticky_context_lines 1, only the innermost line pins.
	if feed(e, ":set sticky_context_lines 1<CR>"); !slices.Equal(e.Views()[0].Context, []int{22}) {
		t.Errorf("with sticky_context_lines 1, %v is pinned", e.Views()[0].Context)
	}
	// With the cursor on the top line, nothing covers it.
	if feed(e, "zt"); len(e.Views()[0].Context) != 0 {
		t.Errorf("with the cursor on the top line, %v is pinned", e.Views()[0].Context)
	}
	// :set sticky_context false turns it off in the window.
	if feed(e, "3j"); len(e.Views()[0].Context) == 0 {
		t.Fatal("with the cursor below the top again, nothing is pinned")
	}
	if feed(e, ":set sticky_context false<CR>"); len(e.Views()[0].Context) != 0 {
		t.Errorf("with sticky_context off, %v is pinned", e.Views()[0].Context)
	}
}

// B21, while the search is typed
func TestB21IncrementalSearch(t *testing.T) {
	var src strings.Builder
	for i := range 100 {
		fmt.Fprintf(&src, "line %d\n", i)
	}
	src.WriteString("needle\n")
	path := filepath.Join(t.TempDir(), "a")
	change(t, path, src.String())
	e := open(t, path)
	e.SetSize(80, 20)
	// A client sizes the editor before each frame, which reveals the cursor.
	top := func() int { e.SetSize(80, 20); return e.Text().LineOf(e.Views()[0].Top) }
	// The match out of view comes into it, lit apart, while the cursor stays.
	feed(e, "/need")
	line := top()
	v := e.Views()[0]
	if at := strings.Index(text(e), "needle"); v.Searched != [2]int{at, at + 4} || line == 0 || e.Cursor() != 0 {
		t.Fatalf("typing /need lit %v with the view at line %d and the cursor at %d", v.Searched, line, e.Cursor())
	}
	// <Esc> puts the view back, and a search that runs keeps it.
	if feed(e, "<Esc>"); top() != 0 || e.Views()[0].Searched != [2]int{} {
		t.Errorf("after <Esc> the view is at line %d, lighting %v", top(), e.Views()[0].Searched)
	}
	if feed(e, "/needle<CR>"); top() == 0 || e.Text().LineOf(e.Cursor()) != 100 {
		t.Errorf("the search went to line %d with the view at line %d", e.Text().LineOf(e.Cursor()), top())
	}
	// With inc_search off the view stays put while a search is typed.
	feed(e, "gg:set inc_search false<CR>/need")
	if top() != 0 || e.Views()[0].Searched != [2]int{} {
		t.Errorf("with inc_search off, /need moved the view to line %d and lit %v", top(), e.Views()[0].Searched)
	}
}

// B21, B217: a search that shows its match deep in a function, with the
// cursor above the view, pins the function's line.
func TestB21IncrementalSearchPinsContext(t *testing.T) {
	var src strings.Builder
	src.WriteString("package p\n\nfunc f() {\n")
	for i := range 100 {
		fmt.Fprintf(&src, "\tx%d := 1\n", i)
	}
	src.WriteString("\tneedle := 2\n}\n")
	path := filepath.Join(t.TempDir(), "a.go")
	change(t, path, src.String())
	e := open(t, path)
	e.SetSize(80, 20)
	captures(t, e)
	feed(e, "/needle")
	e.SetSize(80, 20)
	if v := e.Views()[0]; len(v.Context) != 1 || v.Text.Slice(v.Context[0], v.Context[0]+4) != "func" {
		t.Errorf("with the match in view, the context is %v", v.Context)
	}
}
